package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercises the production backup pipeline. Missing ACME/registry/cleanup components,
// plaintext output, incomplete snapshots or failure to resume Traefik must fail this test.
func vaultRuntimeEncryptedRecovery(t *testing.T, cfg vaultConfig, source *vaultClient, originalKey, secretPath, sentinel string) {
	for _, binary := range []string{"age", "age-keygen"} {
		if _, err := exec.LookPath(binary); err != nil {
			t.Fatalf("Encrypted recovery requires %s", binary)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	// Keep temporary decryption identities in memory-backed storage, outside backup output.
	keyDir, err := os.MkdirTemp("/dev/shm", "devkit-recovery-key-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(keyDir)
	identity := filepath.Join(keyDir, "identity")
	if err = exec.Command("age-keygen", "-o", identity).Run(); err != nil {
		t.Fatal("age-keygen failed")
	}
	recipient, err := exec.Command("age-keygen", "-y", identity).Output()
	if err != nil {
		t.Fatal("Cannot derive test recipient")
	}
	cfg.Backup.Recipient = strings.TrimSpace(string(recipient))
	cfg.Backup.Directory = filepath.Join(t.TempDir(), "backups")
	if err = saveVaultConfig(cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := readConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	raw["proxy"] = proxyConfig{Image: defaultTraefik}
	if err = writeConfigFile(raw); err != nil {
		t.Fatal(err)
	}

	// Exercise restored context, inheritance, history and a deleted-key recovery.
	repo := newGitRepo(t)
	runSecret := func(q secretRequest) secretResponse {
		t.Helper()
		q.Root = repo.root
		out, e := executeSecret(ctx, q)
		if e != nil {
			t.Fatalf("Recovery secret %s: %v", q.Operation, e)
		}
		return out
	}
	confirm := func(q secretRequest) {
		t.Helper()
		preview := runSecret(q)
		if preview.Confirmation == "" {
			t.Fatal("Missing recovery mutation preview")
		}
		q.Confirm = preview.Confirmation
		runSecret(q)
	}
	runSecret(secretRequest{Operation: "register"})
	runSecret(secretRequest{Operation: "set", Scope: "project", Key: "RECOVERY_SHARED", Value: "inherited-v1"})
	runSecret(secretRequest{Operation: "update", Scope: "project", Key: "RECOVERY_SHARED", Value: "inherited-v2", Expected: 1})
	runSecret(secretRequest{Operation: "set", Scope: "branch", Key: "RECOVERY_DELETED", Value: "recover-me"})
	confirm(secretRequest{Operation: "delete", Scope: "branch", Key: "RECOVERY_DELETED"})
	archive, certificate := runtimeACMEFixture(t)
	sourceProxy := runtimeRecoveryProxy(t, filepath.Join(proxyDir(), "compose.yaml"), archive)
	runtimeCertificate(t, sourceProxy.address, certificate)
	started := runtimeDocker(t, nil, "inspect", "--format", "{{.State.StartedAt}}", sourceProxy.id)
	cleanupBefore, err := os.ReadFile(filepath.Join(secretStateDir(), "cleanup.json"))
	if err != nil {
		t.Fatal(err)
	}
	var id string
	if os.Getenv("DEVKIT_OPENBAO_UPGRADE_TEST") == "1" {
		id = runtimeBackupUpgrade(t, cfg, source, originalKey, secretPath, sentinel)
	} else {
		id, err = createVaultBackup()
	}
	if err != nil {
		t.Fatal(err)
	}
	// Backup persists the registry under its lock, advancing its generation.
	registryBefore, err := os.ReadFile(filepath.Join(secretStateDir(), "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = verifyVaultBackupComponents(cfg, id); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(started, runtimeDocker(t, nil, "inspect", "--format", "{{.State.StartedAt}}", sourceProxy.id)) {
		t.Fatal("Backup did not quiesce and restart Traefik")
	}
	runtimeCertificate(t, sourceProxy.address, certificate)
	backupDir := filepath.Join(cfg.Backup.Directory, id)
	decrypted := map[string][]byte{}
	for _, name := range []string{"raft.snap", "registry.json", "cleanup.json", "host.json", "acme.tar"} {
		file := filepath.Join(backupDir, name+".age")
		encrypted, e := readPrivate(file, 64<<20)
		if e != nil || !bytes.HasPrefix(encrypted, []byte("age-encryption.org/v1\n")) {
			t.Fatalf("Missing encrypted component: %s", name)
		}
		if bytes.Contains(encrypted, []byte(sentinel)) || bytes.Contains(encrypted, []byte(originalKey)) {
			t.Fatal("Plaintext recovery material in encrypted output")
		}
		cmd := exec.CommandContext(ctx, "age", "-d", "-i", identity, file)
		decrypted[name], e = cmd.Output()
		if e != nil {
			t.Fatalf("Cannot decrypt %s", name)
		}
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil || len(entries) != 6 {
		t.Fatal("Unexpected backup components (recovery keys must not be included)")
	}
	if !bytes.Equal(decrypted["registry.json"], registryBefore) || !bytes.Equal(decrypted["cleanup.json"], cleanupBefore) {
		t.Fatal("Recovery metadata changed during backup")
	}
	var host struct {
		ACME bool `json:"acme_included"`
	}
	if json.Unmarshal(decrypted["host.json"], &host) != nil || !host.ACME {
		t.Fatal("Backup did not declare ACME inclusion")
	}
	// The exact archive comes from Docker's real certificate-volume capture, not the fixture.
	runtimeCheckACMEArchive(t, decrypted["acme.tar"], certificate)
	// Wrong keys and modified ciphertext must not yield a successful recovery.
	wrong := filepath.Join(keyDir, "wrong")
	if err = exec.Command("age-keygen", "-o", wrong).Run(); err != nil {
		t.Fatal("Test key generation failed")
	}
	if err = exec.Command("age", "-d", "-i", wrong, filepath.Join(backupDir, "raft.snap.age")).Run(); err == nil {
		t.Fatal("Wrong recovery identity accepted")
	}
	damaged := filepath.Join(keyDir, "damaged.age")
	ciphertext, err := os.ReadFile(filepath.Join(backupDir, "raft.snap.age"))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext[len(ciphertext)-1] ^= 1
	if err = os.WriteFile(damaged, ciphertext, 0600); err != nil {
		t.Fatal(err)
	}
	if err = exec.Command("age", "-d", "-i", identity, damaged).Run(); err == nil {
		t.Fatal("Modified ciphertext accepted")
	}
	t.Log("Encrypted Raft/registry/cleanup/host/ACME components decrypt; wrong identity and tampered ciphertext fail; source Traefik resumes")

	// A real encryption failure must resume Traefik and leave no partial backup.
	activeCfg, err := loadVaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	failedCfg := activeCfg
	failedCfg.Backup.Recipient = "age1invalid"
	if err = saveVaultConfig(failedCfg); err != nil {
		t.Fatal(err)
	}
	if _, err = createVaultBackup(); err == nil {
		t.Fatal("Invalid recipient unexpectedly produced a backup")
	}
	runtimeCertificate(t, sourceProxy.address, certificate)
	remaining, err := os.ReadDir(cfg.Backup.Directory)
	if err != nil || len(remaining) != 1 || remaining[0].Name() != id {
		t.Fatal("Failed encryption left partial output or removed the good backup")
	}
	if err = saveVaultConfig(activeCfg); err != nil {
		t.Fatal(err)
	}
	t.Log("Encryption failure resumes Traefik and removes partial backup output")

	// Distinct bind storage and initialization keys prove recovery is independent of source data.
	restoreRoot := t.TempDir()
	t.Setenv("DEVKIT_CONFIG_DIR", filepath.Join(restoreRoot, "config"))
	t.Setenv("DEVKIT_STATE_DIR", filepath.Join(restoreRoot, "state"))
	recovered := cfg
	recovered.Address = runtimeVaultAddress(t)
	if err = saveVaultConfig(recovered); err != nil {
		t.Fatal(err)
	}
	compose := filepath.Join(vaultDir(), "openbao", "compose.json")
	project := "devkit-vault-test-recovery-" + secretID()[:12]
	previousRun := runCommand
	swap(t, &runCommand, func(args []string, o runOpts) (string, error) {
		if len(args) > 3 && args[0] == "docker" && args[1] == "compose" && args[2] == "-f" && args[3] == compose {
			args = append([]string{"docker", "compose", "-p", project}, args[2:]...)
		}
		return previousRun(args, o)
	})
	defer func() {
		if e := exec.Command("docker", "compose", "-p", project, "-f", compose, "down").Run(); e != nil {
			t.Error("Recovery OpenBao cleanup failed: " + project)
		}
	}()
	if err = startVault(recovered); err != nil {
		t.Fatal(err)
	}
	target, err := newVaultClient(recovered.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	runtimeVaultState(t, ctx, target, "uninitialized")
	var bootstrap struct {
		Keys  []string `json:"keys_base64"`
		Token string   `json:"root_token"`
	}
	if err = target.call(ctx, "POST", "sys/init", map[string]int{"secret_shares": 1, "secret_threshold": 1}, &bootstrap); err != nil {
		t.Fatal(err)
	}
	if len(bootstrap.Keys) != 1 || bootstrap.Token == "" {
		t.Fatal("Recovery initialization missing credentials")
	}
	if err = target.call(ctx, "POST", "sys/unseal", map[string]string{"key": bootstrap.Keys[0]}, nil); err != nil {
		t.Fatal(err)
	}
	runtimeVaultState(t, ctx, target, "ready")
	req, err := http.NewRequestWithContext(ctx, "POST", recovered.Address+"/v1/sys/storage/raft/snapshot-force", bytes.NewReader(decrypted["raft.snap"]))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Vault-Token", bootstrap.Token)
	response, err := target.http.Do(req)
	if err != nil {
		t.Fatal("Restore request failed")
	}
	response.Body.Close()
	if response.StatusCode != 204 && response.StatusCode != 200 {
		t.Fatalf("Snapshot restore rejected: HTTP %d", response.StatusCode)
	}
	runtimeVaultState(t, ctx, target, "sealed")
	if err = target.call(ctx, "POST", "sys/unseal", map[string]string{"key": originalKey}, nil); err != nil {
		t.Fatal(err)
	}
	runtimeVaultState(t, ctx, target, "ready")
	if status, e := target.status(ctx); e != nil || status.Version != strings.TrimPrefix(cfg.Image, "openbao/openbao:") {
		t.Fatal("Recovery did not use the backup-compatible original version")
	}
	var login struct {
		Auth struct {
			Token string `json:"client_token"`
		} `json:"auth"`
	}
	if err = target.call(ctx, "POST", "auth/userpass/login/admin", map[string]string{"password": "runtime-human-password-strong"}, &login); err != nil {
		t.Fatal(err)
	}
	if login.Auth.Token == "" {
		t.Fatal("Restored human login missing token")
	}
	target.token = login.Auth.Token
	for _, name := range []string{"registry.json", "cleanup.json"} {
		if err = writePrivate(filepath.Join(secretStateDir(), name), decrypted[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err = provisionVault(ctx, target, "runtime-human-password-strong"); err != nil {
		t.Fatal(err)
	}
	restoredBackend, err := managedVaultClient()
	if err != nil {
		t.Fatal(err)
	}
	if restoredBackend.token == source.token {
		t.Fatal("Recovery reused the source backend credential")
	}
	value, version, err := restoredBackend.read(ctx, secretPath)
	if err != nil || value != sentinel || version != 1 {
		t.Fatal("Encrypted snapshot did not recover original value/version")
	}
	restoredRegistry, err := loadSecretRegistry()
	if err != nil || len(restoredRegistry.Resources) == 0 {
		t.Fatal("Recovered registry unusable")
	}
	var originalRegistry secretRegistry
	if json.Unmarshal(registryBefore, &originalRegistry) != nil || restoredRegistry.Installation != originalRegistry.Installation {
		t.Fatal("Recovery lost installation identity")
	}
	restoredCleanup, err := loadCleanupConfig()
	if err != nil || restoredCleanup.Automatic {
		t.Fatal("Recovery did not retain cleanup off")
	}
	got := runSecret(secretRequest{Operation: "get", Key: "RECOVERY_SHARED", Reveal: true})
	if got.Value != "inherited-v2" {
		t.Fatal("Restored project inheritance lost latest value")
	}
	history := runSecret(secretRequest{Operation: "history", Key: "RECOVERY_SHARED"})
	encoded, err := json.Marshal(history.History)
	if err != nil {
		t.Fatal(err)
	}
	var versions struct {
		Current  int                        `json:"current_version"`
		Versions map[string]json.RawMessage `json:"versions"`
	}
	if json.Unmarshal(encoded, &versions) != nil || versions.Current != 2 || len(versions.Versions) != 2 {
		t.Fatal("Restored version history is incomplete")
	}
	confirm(secretRequest{Operation: "restore", Scope: "branch", Key: "RECOVERY_DELETED"})
	got = runSecret(secretRequest{Operation: "get", Key: "RECOVERY_DELETED", Reveal: true})
	if got.Value != "recover-me" {
		t.Fatal("Deleted-key restoration failed after encrypted recovery")
	}
	value, _, err = source.read(ctx, secretPath)
	if err != nil || value != sentinel {
		t.Fatal("Recovery affected source vault")
	}
	restoredProxy := runtimeRecoveryProxy(t, filepath.Join(t.TempDir(), "proxy", "compose.json"), decrypted["acme.tar"])
	runtimeCertificate(t, restoredProxy.address, certificate)
	t.Log("Fresh OpenBao bind storage recovers with original unseal key, native login, new backend identity, matching registry and cleanup off; fresh Traefik volume serves the original certificate")
}

func runtimeDocker(t *testing.T, in []byte, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", args...)
	if in != nil {
		cmd.Stdin = bytes.NewReader(in)
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Disposable Docker %s failed: %v", args[0], err)
	}
	return out
}
func runtimeVaultAddress(t *testing.T) string {
	t.Helper()
	for i := 0; i < 30; i++ {
		a, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		port := a.Addr().(*net.TCPAddr).Port
		b, e := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port+1))
		a.Close()
		if e == nil {
			b.Close()
			return fmt.Sprintf("http://127.0.0.1:%d", port)
		}
	}
	t.Fatal("No adjacent recovery ports available")
	return ""
}
func runtimeVaultState(t *testing.T, ctx context.Context, c *vaultClient, want string) {
	t.Helper()
	for {
		state, err := c.status(ctx)
		if err == nil && state.State == want {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Recovery vault did not reach %s", want)
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func runtimeACMEFixture(t *testing.T) ([]byte, []byte) {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "recovery.test"}, DNSNames: []string{"recovery.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(90 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	keyDER, e := x509.MarshalECPrivateKey(key)
	if e != nil {
		t.Fatal(e)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	acme, e := json.Marshal(map[string]any{"dns": map[string]any{"Certificates": []any{map[string]any{"domain": map[string]string{"main": "recovery.test"}, "certificate": certPEM, "key": keyPEM, "Store": "default"}}}})
	if e != nil {
		t.Fatal(e)
	}
	var archive bytes.Buffer
	w := tar.NewWriter(&archive)
	if e = w.WriteHeader(&tar.Header{Name: "acme.json", Mode: 0600, Size: int64(len(acme)), Typeflag: tar.TypeReg}); e != nil {
		t.Fatal(e)
	}
	if _, e = w.Write(acme); e != nil {
		t.Fatal(e)
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	return archive.Bytes(), der
}
func runtimeCheckACMEArchive(t *testing.T, archive, certificate []byte) {
	t.Helper()
	r := tar.NewReader(bytes.NewReader(archive))
	found := false
	for {
		h, e := r.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal("Invalid ACME recovery archive")
		}
		if h.Typeflag == tar.TypeDir && h.Name == "./" {
			continue
		}
		if strings.TrimPrefix(h.Name, "./") != "acme.json" || h.Typeflag != tar.TypeReg || h.Mode != 0600 || h.Uid != 0 || h.Gid != 0 {
			t.Fatal("Unexpected ACME archive entry, mode or owner")
		}
		data, e := io.ReadAll(io.LimitReader(r, 1<<20))
		if e != nil {
			t.Fatal(e)
		}
		var acme map[string]struct {
			Certificates []struct {
				Certificate []byte `json:"certificate"`
				Key         []byte `json:"key"`
			}
		}
		if json.Unmarshal(data, &acme) != nil || len(acme["dns"].Certificates) != 1 {
			t.Fatal("ACME certificate missing")
		}
		stored := acme["dns"].Certificates[0]
		pair, e := tls.X509KeyPair(stored.Certificate, stored.Key)
		if e != nil || !bytes.Equal(pair.Certificate[0], certificate) {
			t.Fatal("Recovered ACME certificate/key differ")
		}
		found = true
	}
	if !found {
		t.Fatal("ACME archive empty")
	}
}

type recoveryProxy struct{ id, address string }

func runtimeRecoveryProxy(t *testing.T, file string, archive []byte) recoveryProxy {
	t.Helper()
	project := "devkit-vault-test-acme-" + secretID()[:12]
	volume := project + "-certs"
	runtimeDocker(t, nil, "volume", "create", volume)
	t.Cleanup(func() {
		if e := exec.Command("docker", "compose", "-p", project, "-f", file, "down", "-v").Run(); e != nil {
			t.Error("Test proxy cleanup failed: " + project)
		}
		// Also remove a volume created before a failed Compose startup.
		if e := exec.Command("docker", "volume", "rm", volume).Run(); e != nil {
			if exec.Command("docker", "volume", "inspect", volume).Run() == nil {
				t.Error("Test certificate volume retained: " + volume)
			}
		}
	})
	runtimeDocker(t, archive, "run", "--rm", "-i", "--network", "none", "--mount", "type=volume,src="+volume+",dst=/certs", "--entrypoint", "tar", defaultTraefik, "-C", "/certs", "-xf", "-")
	mode := runtimeDocker(t, nil, "run", "--rm", "--network", "none", "--mount", "type=volume,src="+volume+",dst=/certs,readonly", "--entrypoint", "stat", defaultTraefik, "-c", "%a:%u:%g", "/certs/acme.json")
	if strings.TrimSpace(string(mode)) != "600:0:0" {
		t.Fatal("Restored ACME permissions/owner changed")
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	address := listener.Addr().String()
	listener.Close()
	dir := filepath.Dir(file)
	dynamic := []byte(`{"http":{"routers":{"test":{"rule":"Host(` + "`recovery.test`" + `)","service":"noop@internal","tls":{}}}}}`)
	if e = writePrivate(filepath.Join(dir, "dynamic.json"), dynamic); e != nil {
		t.Fatal(e)
	}
	compose := map[string]any{"name": project, "services": map[string]any{"traefik": map[string]any{"image": defaultTraefik, "network_mode": "host", "command": []string{"--entrypoints.websecure.address=" + address, "--providers.file.filename=/etc/traefik/dynamic.json", "--certificatesresolvers.dns.acme.storage=/certs/acme.json", "--certificatesresolvers.dns.acme.caserver=http://127.0.0.1:1/directory", "--certificatesresolvers.dns.acme.tlschallenge=true", "--log.level=ERROR"}, "volumes": []string{"certs:/certs", filepath.Join(dir, "dynamic.json") + ":/etc/traefik/dynamic.json:ro"}}}, "volumes": map[string]any{"certs": map[string]any{"name": volume}}}
	data, e := json.Marshal(compose)
	if e != nil {
		t.Fatal(e)
	}
	if e = writePrivate(file, data); e != nil {
		t.Fatal(e)
	}
	runtimeDocker(t, nil, "compose", "-p", project, "-f", file, "up", "-d")
	id := strings.TrimSpace(string(runtimeDocker(t, nil, "compose", "-p", project, "-f", file, "ps", "-q", "traefik")))
	if len(id) != 64 {
		t.Fatal("Missing disposable proxy identity")
	}
	return recoveryProxy{id, address}
}
func runtimeCertificate(t *testing.T, address string, want []byte) {
	t.Helper()
	cert, e := x509.ParseCertificate(want)
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	until := time.Now().Add(20 * time.Second)
	for time.Now().Before(until) {
		connection, e := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", address, &tls.Config{RootCAs: roots, ServerName: "recovery.test", MinVersion: tls.VersionTLS12})
		if e == nil {
			state := connection.ConnectionState()
			connection.Close()
			if len(state.PeerCertificates) == 0 || !bytes.Equal(state.PeerCertificates[0].Raw, want) {
				t.Fatal("Traefik served a different certificate")
			}
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("Traefik did not serve the recovered certificate with verified hostname/trust")
}
