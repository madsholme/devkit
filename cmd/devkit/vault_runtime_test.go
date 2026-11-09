package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type vaultRuntimeTransport struct {
	base http.RoundTripper
	t    *testing.T
}

func (v vaultRuntimeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response, e := v.base.RoundTrip(req)
	if e == nil && response.StatusCode >= 400 {
		v.t.Logf("Runtime API %s %s returned HTTP %d", req.Method, req.URL.Path, response.StatusCode)
	}
	return response, e
}

// This test never uses the deployed Compose project, host configuration or credentials.
func TestVaultRuntimeBindRestartAndSnapshot(t *testing.T) {
	if os.Getenv("DEVKIT_OPENBAO_RUNTIME_TEST") != "1" {
		t.Skip("opt in on a Docker host with DEVKIT_OPENBAO_RUNTIME_TEST=1")
	}
	if _, e := exec.Command("docker", "info").Output(); e != nil {
		t.Fatal("Docker daemon required for opted-in runtime acceptance")
	}
	root := t.TempDir()
	t.Setenv("DEVKIT_CONFIG_DIR", filepath.Join(root, "config"))
	t.Setenv("DEVKIT_STATE_DIR", filepath.Join(root, "state"))
	cfg := defaultVaultConfig()
	if os.Getenv("DEVKIT_OPENBAO_UPGRADE_TEST") == "1" {
		cfg.Image = "openbao/openbao:2.7.1"
	}
	var port int
	for i := 0; i < 30; i++ {
		a, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		port = a.Addr().(*net.TCPAddr).Port
		b, e := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port+1))
		a.Close()
		if e == nil {
			b.Close()
			break
		}
		port = 0
	}
	if port == 0 {
		t.Fatal("No test ports available")
	}
	cfg.Address = fmt.Sprintf("http://127.0.0.1:%d", port)
	if e := saveVaultConfig(cfg); e != nil {
		t.Fatal(e)
	}
	project := "devkit-vault-test-" + secretID()[:12]
	sourceCompose := filepath.Join(vaultDir(), "openbao", "compose.json")
	originalRun := runCommand
	swap(t, &runCommand, func(args []string, o runOpts) (string, error) {
		if len(args) > 3 && args[0] == "docker" && args[1] == "compose" && args[2] == "-f" && args[3] == sourceCompose {
			args = append([]string{"docker", "compose", "-p", project}, args[2:]...)
		}
		return originalRun(args, o)
	})
	file := filepath.Join(vaultDir(), "openbao", "compose.json")
	defer func() {
		cmd := exec.Command("docker", "compose", "-p", project, "-f", file, "down")
		if e := cmd.Run(); e != nil {
			t.Error("Disposable container cleanup failed; inspect project " + project)
		}
	}()
	if e := startVault(cfg); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c, _ := newVaultClient(cfg.Address, "")
	for {
		status, e := c.status(ctx)
		if e == nil && status.State == "uninitialized" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("OpenBao did not start")
		case <-time.After(200 * time.Millisecond):
		}
	}
	if e := checkVaultInitStorage(); e != nil {
		t.Fatal(e)
	}
	var init struct {
		Keys  []string `json:"keys_base64"`
		Token string   `json:"root_token"`
	}
	if e := c.call(ctx, "POST", "sys/init", map[string]int{"secret_shares": 1, "secret_threshold": 1}, &init); e != nil {
		t.Fatal(e)
	}
	if len(init.Keys) != 1 || init.Token == "" {
		t.Fatal("Initialization did not provide recovery material")
	}
	unseal := func() {
		t.Helper()
		if e := c.call(ctx, "POST", "sys/unseal", map[string]string{"key": init.Keys[0]}, nil); e != nil {
			t.Fatal(e)
		}
		for {
			status, e := c.status(ctx)
			if e == nil && status.State == "ready" {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatal("unseal timeout")
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	unseal()
	c.token = init.Token
	c.http.Transport = vaultRuntimeTransport{c.http.Transport, t}
	if e := provisionVault(ctx, c, "runtime-human-password-strong"); e != nil {
		t.Fatal(e)
	}
	if e := c.call(ctx, "GET", "auth/token/lookup-self", nil, nil); e == nil {
		t.Fatal("bootstrap root remained usable")
	}
	if e := setupVaultGuided(&prompter{yes: true}, cfg); e != nil {
		t.Fatalf("Guided ready-vault resume: %v", e)
	}
	backend, e := managedVaultClient()
	if e != nil {
		t.Fatal(e)
	}
	repo := newGitRepo(t)
	if _, e = executeSecret(ctx, secretRequest{Operation: "register", Root: repo.root}); e != nil {
		t.Fatal(e)
	}
	const sentinel = "devkit-runtime-secret-SENTINEL"
	saved, e := executeSecret(ctx, secretRequest{Operation: "set", Root: repo.root, Scope: "project", Key: "PASSWORD", Value: sentinel})
	if e != nil {
		t.Fatal(e)
	}

	// Verify real KV version recovery and the automatic-off policy, not only a fixture.
	runSecret := func(q secretRequest) secretResponse {
		t.Helper()
		q.Root = repo.root
		out, e := executeSecret(ctx, q)
		if e != nil {
			t.Fatalf("%s: %v", q.Operation, e)
		}
		return out
	}
	confirmed := func(q secretRequest) {
		t.Helper()
		preview := runSecret(q)
		if preview.Confirmation == "" {
			t.Fatal("Missing mutation preview")
		}
		q.Confirm = preview.Confirmation
		runSecret(q)
	}
	// Exercise the real CLI/socket path for a paired entry, using protected stdin.
	cli := filepath.Join(root, "devkit-login-test")
	build := exec.Command("go", "build", "-o", cli, "./cmd/devkit")
	build.Dir = kit
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("Build CLI: %v: %s", err, out)
	}
	stopBroker, err := startSecretSocket()
	if err != nil {
		t.Fatal(err)
	}
	defer stopBroker()
	cliRun := func(input string, args ...string) []byte {
		t.Helper()
		command := exec.Command(cli, args...)
		command.Dir = repo.root
		command.Stdin = strings.NewReader(input)
		out, err := command.Output()
		if err != nil {
			t.Fatalf("Login CLI %s failed: %v", args[0], err)
		}
		return out
	}
	const loginUser = "runtime-login-user-SENTINEL"
	generated := cliRun(`{"username":"`+loginUser+`"}`, "secrets", "generate", "dev-admin", "--type", "login", "--scope", "project", "--username-env", "APP_USER", "--password-env", "APP_PASS", "--stdin")
	if bytes.Contains(generated, []byte(loginUser)) {
		t.Fatal("Generation output leaked username")
	}
	cliRun("", "secrets", "rotate", "dev-admin", "--scope", "project", "--expected", "1")
	if strings.TrimSpace(string(cliRun("", "secrets", "get", "dev-admin", "--field", "username", "--reveal"))) != loginUser {
		t.Fatal("CLI username reveal failed")
	}
	loginPassword := strings.TrimSpace(string(cliRun("", "secrets", "get", "dev-admin", "--field", "password", "--reveal")))
	if len(loginPassword) != 32 {
		t.Fatal("Login rotation failed")
	}
	if string(cliRun("", "secrets", "run", "--", "sh", "-c", `test -n "$APP_USER" && test "${#APP_PASS}" -eq 32 && printf success`)) != "success" {
		t.Fatal("CLI login injection failed")
	}
	confirmed(secretRequest{Operation: "delete", Scope: "project", Key: "dev-admin"})
	confirmed(secretRequest{Operation: "restore", Scope: "project", Key: "dev-admin"})
	if got := runSecret(secretRequest{Operation: "get", Scope: "project", Key: "dev-admin", Field: "password", Reveal: true}); got.Value != loginPassword {
		t.Fatal("Login restore lost password")
	}
	t.Log("Verified paired login CLI stdin, rotation, field reveals, child injection and recoverable deletion")
	// External applications use the same managed secret path through HTTPS.
	tokenFile := filepath.Join(root, "runtime-api-token")
	issued := cliRun("", "secrets", "tokens", "create", "runtime-api", "--scope", "worktree", "--permissions", "read,write,cleanup", "--output", tokenFile)
	var apiIdentity secretAPIToken
	if json.Unmarshal(issued, &apiIdentity) != nil || apiIdentity.ID == "" || apiIdentity.Hash != "" {
		t.Fatal("Invalid public token metadata")
	}
	tokenBytes, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	bearer := strings.TrimSpace(string(tokenBytes))
	if bytes.Contains(issued, []byte(bearer)) {
		t.Fatal("Token leaked to ordinary CLI output")
	}
	tokenInfo, err := os.Stat(tokenFile)
	if err != nil || tokenInfo.Mode().Perm() != 0600 {
		t.Fatal("Token output file is not private")
	}
	apiServer := httptest.NewTLSServer(http.HandlerFunc(secretsAPIHandler))
	defer apiServer.Close()
	apiCall := func(q secretRequest, want int) secretResponse {
		t.Helper()
		body, _ := json.Marshal(q)
		request, err := http.NewRequestWithContext(ctx, "POST", apiServer.URL+"/api/secrets/v1", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+bearer)
		response, err := apiServer.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("API %s status %d, want %d", q.Operation, response.StatusCode, want)
		}
		var out secretResponse
		if want == 200 && json.NewDecoder(response.Body).Decode(&out) != nil {
			t.Fatal("Invalid API response")
		}
		return out
	}
	if got := apiCall(secretRequest{Operation: "get", Key: "PASSWORD", Reveal: true}, 200).Value; got != sentinel {
		t.Fatal("API inherited read failed")
	}
	apiCall(secretRequest{Operation: "generate", Key: "EXTERNAL_PASSWORD", Length: 32}, 200)
	if got := apiCall(secretRequest{Operation: "get", Key: "EXTERNAL_PASSWORD", Reveal: true}, 200).Value; len(got) != 32 {
		t.Fatal("API generation failed")
	}
	apiCall(secretRequest{Operation: "set", Scope: "project", Key: "ESCAPE", Value: "wrong"}, 403)
	apiDelete := apiCall(secretRequest{Operation: "delete", Key: "EXTERNAL_PASSWORD"}, 200)
	if apiDelete.Confirmation == "" {
		t.Fatal("API deletion lacked preview")
	}
	apiCall(secretRequest{Operation: "delete", Key: "EXTERNAL_PASSWORD", Confirm: apiDelete.Confirmation}, 200)
	apiRestore := apiCall(secretRequest{Operation: "restore", Key: "EXTERNAL_PASSWORD"}, 200)
	apiCall(secretRequest{Operation: "restore", Key: "EXTERNAL_PASSWORD", Confirm: apiRestore.Confirmation}, 200)
	cliRun("", "secrets", "tokens", "revoke", apiIdentity.ID)
	apiCall(secretRequest{Operation: "list"}, 401)
	tokenStore, err := os.ReadFile(filepath.Join(vaultDir(), "api-tokens.json"))
	if err != nil || bytes.Contains(tokenStore, []byte(bearer)) {
		t.Fatal("Token persistence leaked bearer")
	}
	t.Log("Verified external HTTPS token CLI issuance, inherited reads, generation, context refusal, confirmed delete/restore and immediate revocation")

	runSecret(secretRequest{Operation: "set", Scope: "worktree", Key: "TEMP_PASSWORD", Value: "temporary-v1"})
	runSecret(secretRequest{Operation: "update", Scope: "worktree", Key: "TEMP_PASSWORD", Value: "temporary-v2", Expected: 1})
	confirmed(secretRequest{Operation: "delete", Scope: "worktree", Key: "TEMP_PASSWORD"})
	temporaryPath := "projects/" + saved.Context.Project + "/worktrees/" + saved.Context.Worktree + "/TEMP_PASSWORD"
	var metadata struct {
		Data struct {
			Versions map[string]struct {
				Deleted string `json:"deletion_time"`
			} `json:"versions"`
		} `json:"data"`
	}
	if e := backend.call(ctx, "GET", "kv/metadata/"+temporaryPath, nil, &metadata); e != nil || len(metadata.Data.Versions) != 2 {
		t.Fatal("Version metadata unavailable after deletion")
	}
	for _, version := range metadata.Data.Versions {
		if version.Deleted == "" {
			t.Fatal("Deletion left an owned version readable")
		}
	}
	confirmed(secretRequest{Operation: "restore", Scope: "worktree", Key: "TEMP_PASSWORD"})
	if restored := runSecret(secretRequest{Operation: "get", Scope: "worktree", Key: "TEMP_PASSWORD", Reveal: true}); restored.Value != "temporary-v2" {
		t.Fatal("Restoration lost latest version")
	}
	policy := defaultCleanupConfig()
	policy.Automatic = false
	confirmed(secretRequest{Operation: "cleanup-config", Cleanup: &policy})
	confirmed(secretRequest{Operation: "cleanup-retire", Resource: saved.Context.Worktree})
	if e := runSecretCleanup(ctx, time.Now().Add(365*24*time.Hour)); e != nil {
		t.Fatal(e)
	}
	registry, e := loadSecretRegistry()
	if e != nil {
		t.Fatal(e)
	}
	if registry.Resources[saved.Context.Worktree].State != "retired" || registry.Keys[secretKeyID(saved.Context.Worktree, "TEMP_PASSWORD")] == nil {
		t.Fatal("Automatic-off lost retired secrets")
	}
	confirmed(secretRequest{Operation: "cleanup-restore", Resource: saved.Context.Worktree})
	if restored := runSecret(secretRequest{Operation: "get", Scope: "worktree", Key: "TEMP_PASSWORD", Reveal: true}); restored.Value != "temporary-v2" {
		t.Fatal("Scope restoration failed with automatic cleanup off")
	}
	confirmed(secretRequest{Operation: "cleanup-retire", Resource: saved.Context.Worktree})
	confirmed(secretRequest{Operation: "cleanup-purge", Resource: saved.Context.Worktree})
	if e := backend.call(ctx, "GET", "kv/metadata/"+temporaryPath, nil, nil); e == nil {
		t.Fatal("Confirmed purge retained owned version metadata")
	}
	t.Log("Verified real version deletion/restoration, cleanup off past expiry, explicit scope recovery/purge and parent preservation")
	path := "projects/" + saved.Context.Project + "/shared/PASSWORD"
	req, _ := http.NewRequestWithContext(ctx, "GET", cfg.Address+"/v1/sys/storage/raft/snapshot", nil)
	req.Header.Set("X-Vault-Token", backend.token)
	response, e := backend.http.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := io.ReadAll(io.LimitReader(response.Body, 64<<20))
	response.Body.Close()
	if e != nil || response.StatusCode != 200 || len(snapshot) == 0 {
		t.Fatal("snapshot capture failed")
	}
	if e = exec.Command("docker", "compose", "-p", project, "-f", file, "restart").Run(); e != nil {
		t.Fatal(e)
	}
	status, e := waitVaultVersion(ctx, c, strings.TrimPrefix(cfg.Image, "openbao/openbao:"))
	if e != nil || status.State != "sealed" {
		t.Fatal("restart did not preserve sealed initialized storage")
	}
	sealedLogin, sealedErr := executeSecret(ctx, secretRequest{Operation: "get", Root: repo.root, Scope: "project", Key: "dev-admin", Field: "username", Reveal: true})
	if sealedErr == nil || sealedLogin.Value != "" {
		t.Fatal("Sealed login reveal did not fail closed")
	}
	unseal()
	value, _, e := backend.read(ctx, path)
	if e != nil || value != sentinel {
		t.Fatal("bind mount did not preserve secret")
	}
	if got := runSecret(secretRequest{Operation: "get", Scope: "project", Key: "dev-admin", Field: "password", Reveal: true}); got.Value != loginPassword {
		t.Fatal("Restart lost paired login")
	}
	// Restore the snapshot into this disposable instance after changing its value.
	if _, e = backend.write(ctx, path, "changed-after-snapshot", 1); e != nil {
		t.Fatal(e)
	}
	var login struct {
		Auth struct {
			Token string `json:"client_token"`
		} `json:"auth"`
	}
	if e = c.call(ctx, "POST", "auth/userpass/login/admin", map[string]string{"password": "runtime-human-password-strong"}, &login); e != nil {
		t.Fatal(e)
	}
	req, _ = http.NewRequestWithContext(ctx, "POST", cfg.Address+"/v1/sys/storage/raft/snapshot-force", bytes.NewReader(snapshot))
	req.Header.Set("X-Vault-Token", login.Auth.Token)
	response, e = backend.http.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 204 && response.StatusCode != 200 {
		t.Fatal("snapshot restore rejected")
	}
	for {
		value, _, e = backend.read(ctx, path)
		if e == nil && value == sentinel {
			break
		}
		status, _ = c.status(ctx)
		if status.State == "sealed" {
			unseal()
		}
		select {
		case <-ctx.Done():
			t.Fatal("restore did not recover original value")
		case <-time.After(200 * time.Millisecond):
		}
	}
	if got := runSecret(secretRequest{Operation: "get", Scope: "project", Key: "dev-admin", Field: "username", Reveal: true}); got.Value != loginUser {
		t.Fatal("Snapshot recovery lost paired login username")
	}
	if got := runSecret(secretRequest{Operation: "get", Scope: "project", Key: "dev-admin", Field: "password", Reveal: true}); got.Value != loginPassword {
		t.Fatal("Snapshot recovery lost paired login password")
	}
	for _, name := range []string{file, filepath.Join(vaultDir(), "openbao", "server.json"), filepath.Join(secretStateDir(), "registry.json"), filepath.Join(secretStateDir(), "audit.jsonl"), filepath.Join(secretStateDir(), "openbao", "data", "audit.jsonl")} {
		b, e := os.ReadFile(name)
		if e != nil {
			t.Fatal(e)
		}
		if bytes.Contains(b, []byte(loginUser)) || bytes.Contains(b, []byte(loginPassword)) || bytes.Contains(b, []byte(sentinel)) || bytes.Contains(b, []byte(init.Token)) || bytes.Contains(b, []byte(init.Keys[0])) || bytes.Contains(b, []byte("runtime-human-password-strong")) {
			t.Fatal("plaintext in managed metadata")
		}
	}
	inspect, e := exec.Command("docker", "compose", "-p", project, "-f", file, "ps", "-q").Output()
	if e != nil {
		t.Fatal(e)
	}
	b, e := exec.Command("docker", "inspect", strings.TrimSpace(string(inspect))).Output()
	if e != nil {
		t.Fatal(e)
	}
	var objects []map[string]any
	if json.Unmarshal(b, &objects) != nil {
		t.Fatal("inspect unavailable")
	}
	if bytes.Contains(b, []byte(sentinel)) {
		t.Fatal("plaintext in container configuration")
	}
	t.Log("Verified real OpenBao provisioning, root retirement, private bind storage, restart/manual unseal, snapshot recovery and metadata redaction")
	if os.Getenv("DEVKIT_OPENBAO_ENCRYPTED_TEST") == "1" || os.Getenv("DEVKIT_OPENBAO_UPGRADE_TEST") == "1" {
		t.Run("EncryptedBackupAndACMERecovery", func(t *testing.T) {
			vaultRuntimeEncryptedRecovery(t, cfg, backend, init.Keys[0], path, sentinel)
		})
	}
}
