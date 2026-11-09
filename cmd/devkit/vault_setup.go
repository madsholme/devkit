package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func managedVaultClient() (*vaultClient, error) {
	cfg, e := loadVaultConfig()
	if e != nil {
		return nil, e
	}
	if cfg.Provider != "openbao" {
		return nil, fail("Managed secrets are disabled; select OpenBao in setup")
	}
	token, e := readPrivate(filepath.Join(vaultDir(), "backend-token"), 4096)
	if e != nil {
		return nil, fail("OpenBao backend identity is missing or unsafe; run devkit vault provision")
	}
	if strings.TrimSpace(string(token)) == "" {
		return nil, fail("OpenBao backend identity is empty")
	}
	return newVaultClient(cfg.Address, strings.TrimSpace(string(token)))
}
func readVaultReference(ref string) (string, error) {
	path, ok := strings.CutPrefix(ref, "openbao://")
	if !ok || !validVaultPath(path) || (!strings.HasPrefix(path, "devkit/system/") && !strings.HasPrefix(path, "devkit/integrations/")) {
		return "", fail("Invalid Devkit OpenBao reference")
	}
	c, e := managedVaultClient()
	if e != nil {
		return "", e
	}
	v, _, e := c.read(context.Background(), path)
	return v, e
}
func startVault(cfg vaultConfig) error {
	if e := validateVaultConfig(cfg); e != nil {
		return e
	}
	dir := filepath.Join(vaultDir(), "openbao")
	data := filepath.Join(secretStateDir(), "openbao", "data")
	for _, d := range []string{vaultDir(), secretStateDir(), filepath.Dir(data), data, dir} {
		if e := privateDir(d); e != nil {
			return e
		}
	}
	entries, e := os.ReadDir(data)
	if e != nil {
		return e
	}
	marker := filepath.Join(dir, "initialized-storage-owner")
	if len(entries) == 0 {
		if _, e = os.Lstat(marker); errors.Is(e, os.ErrNotExist) {
			if e = writePrivate(marker, []byte(data)); e != nil {
				return e
			}
		}
	}
	compose, configuration := vaultFiles(cfg, dir)
	for name, value := range map[string]any{"compose.json": compose, "server.json": configuration} {
		b, e := json.MarshalIndent(value, "", "  ")
		if e != nil {
			return e
		}
		if e = writePrivate(filepath.Join(dir, name), b); e != nil {
			return e
		}
	}
	_, e = runCommand([]string{"docker", "compose", "-f", filepath.Join(dir, "compose.json"), "up", "-d"}, runOpts{capture: true, quiet: true})
	if e != nil {
		return fail("Could not start managed OpenBao; check Docker and devkit vault health")
	}
	return nil
}
func checkVaultInitStorage() error {
	data := filepath.Join(secretStateDir(), "openbao", "data")
	b, e := readPrivate(filepath.Join(vaultDir(), "openbao", "initialized-storage-owner"), 4096)
	if e != nil || string(b) != data {
		return fail("Refusing initialization of unknown storage; use the existing recovery procedure")
	}
	entries, e := os.ReadDir(data)
	if e != nil {
		return e
	}
	for _, entry := range entries {
		if !((entry.Name() == "raft" && entry.IsDir()) || (entry.Name() == "vault.db" && entry.Type().IsRegular())) {
			return fail("Refusing initialization of unexpected populated storage")
		}
	}
	return nil
}

// Creating the mount can finish before its versioned storage is ready. Retry only
// the explicit initialization response, with both a local bound and caller cancellation.
func configureVaultKV(ctx context.Context, c *vaultClient) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for {
		err := c.call(ctx, "POST", "kv/config", map[string]any{"max_versions": 1000, "cas_required": true, "delete_version_after": "0s"}, nil)
		if !errors.Is(err, errVaultMountInitializing) {
			return err
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func provisionVault(ctx context.Context, root *vaultClient, password string) error {
	if len(password) < 12 {
		return fail("Human administrator password needs at least 12 characters")
	}
	var mounts struct {
		Data map[string]struct {
			Type    string            `json:"type"`
			Options map[string]string `json:"options"`
		} `json:"data"`
	}
	if e := root.call(ctx, "GET", "sys/mounts", nil, &mounts); e != nil {
		return fmt.Errorf("Inspect KV mount: %w", e)
	}
	if m, ok := mounts.Data["kv/"]; ok {
		if m.Type != "kv" || m.Options["version"] != "2" {
			return fail("Existing kv mount is not KV v2; refusing to replace it")
		}
	} else if e := root.call(ctx, "POST", "sys/mounts/kv", map[string]any{"type": "kv", "options": map[string]string{"version": "2"}}, nil); e != nil {
		return e
	}
	if e := configureVaultKV(ctx, root); e != nil {
		return fmt.Errorf("Configure KV engine: %w", e)
	}
	var audit struct {
		Data map[string]struct {
			Type    string            `json:"type"`
			Options map[string]string `json:"options"`
		} `json:"data"`
	}
	if e := root.call(ctx, "GET", "sys/audit", nil, &audit); e != nil {
		return e
	}
	device, ok := audit.Data["devkit/"]
	if !ok || device.Type != "file" || device.Options["file_path"] != "/bao/data/audit.jsonl" || device.Options["log_raw"] == "true" {
		return fail("Private audit device is missing or unsafe; run vault setup and unseal before provisioning")
	}

	policy := `path "kv/data/devkit/*" { capabilities = ["create", "read", "update", "delete"] }
path "kv/metadata/devkit/*" { capabilities = ["read", "list", "delete", "update"] }
path "kv/data/projects/*" { capabilities = ["create", "read", "update", "delete"] }
path "kv/metadata/projects/*" { capabilities = ["read", "list", "delete", "update"] }
path "kv/delete/projects/*" { capabilities = ["update"] }
path "kv/undelete/projects/*" { capabilities = ["update"] }
path "kv/destroy/projects/*" { capabilities = ["update"] }
path "auth/token/renew-self" { capabilities = ["update"] }
path "auth/token/lookup-self" { capabilities = ["read"] }
path "sys/storage/raft/snapshot" { capabilities = ["read"] }`
	if e := root.call(ctx, "PUT", "sys/policies/acl/devkit-backend", map[string]string{"policy": policy}, nil); e != nil {
		return e
	}
	var auth struct {
		Data map[string]any `json:"data"`
	}
	if e := root.call(ctx, "GET", "sys/auth", nil, &auth); e != nil {
		return e
	}
	if _, ok := auth.Data["userpass/"]; !ok {
		if e := root.call(ctx, "POST", "sys/auth/userpass", map[string]string{"type": "userpass"}, nil); e != nil {
			return e
		}
	}
	if e := root.call(ctx, "PUT", "sys/policies/acl/devkit-human-admin", map[string]string{"policy": `path "*" { capabilities = ["create", "read", "update", "delete", "list", "sudo"] }`}, nil); e != nil {
		return e
	}
	if e := root.call(ctx, "POST", "auth/userpass/users/admin", map[string]any{"password": password, "policies": []string{"devkit-human-admin"}}, nil); e != nil {
		return e
	}
	var login struct {
		Auth struct {
			Token string `json:"client_token"`
		} `json:"auth"`
	}
	if e := root.call(ctx, "POST", "auth/userpass/login/admin", map[string]string{"password": password}, &login); e != nil {
		return e
	}
	if login.Auth.Token == "" {
		return fail("Human administrator login verification failed")
	}
	human, _ := newVaultClient(root.address, login.Auth.Token)
	if e := human.call(ctx, "POST", "auth/token/revoke-self", nil, nil); e != nil {
		return e
	}
	var child struct {
		Auth struct {
			Token string `json:"client_token"`
		} `json:"auth"`
	}
	if e := root.call(ctx, "POST", "auth/token/create", map[string]any{"policies": []string{"devkit-backend"}, "no_default_policy": true, "period": "24h", "renewable": true, "no_parent": true}, &child); e != nil {
		return e
	}
	if child.Auth.Token == "" {
		return fail("OpenBao did not create a backend identity")
	}
	if e := writePrivate(filepath.Join(vaultDir(), "backend-token"), []byte(child.Auth.Token)); e != nil {
		return e
	}
	// A new vault session key deliberately invalidates old local browser sessions.
	if _, version, readErr := root.read(ctx, "devkit/system/web-session-signing-key"); version == 0 {
		var ve vaultError
		if readErr != nil && (!errors.As(readErr, &ve) || ve.Code != 404) {
			return readErr
		}
		key := make([]byte, 32)
		if _, e := rand.Read(key); e != nil {
			return e
		}
		if _, e := root.write(ctx, "devkit/system/web-session-signing-key", base64.RawStdEncoding.EncodeToString(key), 0); e != nil {
			return e
		}
	}
	backend, e := newVaultClient(root.address, child.Auth.Token)
	if e != nil {
		return e
	}
	var identity struct {
		Data struct {
			Policies []string `json:"policies"`
		} `json:"data"`
	}
	if e = backend.call(ctx, "GET", "auth/token/lookup-self", nil, &identity); e != nil {
		return fail("Backend identity verification failed; bootstrap token was not revoked")
	}
	if len(identity.Data.Policies) != 1 || identity.Data.Policies[0] != "devkit-backend" {
		return fail("Unexpected backend policies; bootstrap token was not revoked")
	}
	// Provisioning is complete before revocation. Never persist the bootstrap root.
	return root.call(ctx, "POST", "auth/token/revoke-self", nil, nil)
}
func humanTerminal() (*os.File, error) {
	f, e := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if e != nil {
		return nil, fail("This operation needs the human's controlling terminal; recovery material cannot enter jobs or stdout")
	}
	return f, nil
}
func terminalSecret(f *os.File, r *bufio.Reader, prompt string) (string, error) {
	fmt.Fprint(f, prompt+": ")
	cmd := exec.Command("stty", "-echo")
	cmd.Stdin = f
	if e := cmd.Run(); e != nil {
		return "", fail("Cannot disable terminal echo")
	}
	defer func() { cmd := exec.Command("stty", "echo"); cmd.Stdin = f; _ = cmd.Run(); fmt.Fprintln(f) }()
	value, e := r.ReadString('\n')
	if e != nil {
		return "", fail("Secret input interrupted")
	}
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > 64<<10 {
		return "", fail("Secret input is empty or too large")
	}
	return value, nil
}
func vaultCLI(args []string) (int, error) {
	if len(args) == 0 {
		return 2, fail("usage: devkit vault status|health|setup|init|provision|unseal|ui|backup|update")
	}
	op := args[0]
	if op == "migrate" {
		return vaultMigrateCLI(args[1:])
	}
	if op == "backup" {
		return vaultBackupCLI(args[1:])
	}
	if op == "update" {
		return vaultUpdateCLI(args[1:])
	}
	pos, v, e := splitArgs(args[1:], map[string]bool{"--shares": true, "--threshold": true}, map[string]bool{"--json": true})
	if e != nil {
		return 2, e
	}
	if len(pos) > 0 {
		return 2, fail("Unexpected vault argument")
	}
	cfg, e := loadVaultConfig()
	if e != nil {
		return 1, e
	}
	if op == "setup" {
		if cfg.Provider != "openbao" {
			cfg.Provider = "openbao"
			if e = saveVaultConfig(cfg); e != nil {
				return 1, e
			}
		}
		e = startVault(cfg)
		if e == nil {
			fmt.Fprintln(stdout, "OpenBao started. Check status; initialize and unseal through your terminal before collecting credentials.")
		}
		return 0, e
	}
	c, e := newVaultClient(cfg.Address, "")
	if e != nil {
		return 1, e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	switch op {
	case "status", "health":
		if cfg.Provider != "openbao" {
			return 0, json.NewEncoder(stdout).Encode(vaultStatus{State: "disabled"})
		}
		s, e := c.status(ctx)
		_ = json.NewEncoder(stdout).Encode(s)
		return 0, e
	case "ui":
		dom, e := domain()
		if e != nil {
			return 1, e
		}
		fmt.Fprintf(stdout, "https://vault.devkit.%s/ui/\n", dom)
		return 0, nil
	case "init":
		var shares, threshold int
		if _, e = fmt.Sscan(v["--shares"], &shares); e != nil {
			return 2, fail("Choose --shares and --threshold explicitly")
		}
		if _, e = fmt.Sscan(v["--threshold"], &threshold); e != nil || threshold < 1 || shares < threshold || shares > 10 {
			return 2, fail("Use 1 <= threshold <= shares <= 10")
		}
		s, e := c.status(ctx)
		if e != nil {
			return 1, e
		}
		if s.Initialized {
			return 1, fail("OpenBao is already initialized; refusing reinitialization")
		}
		if e = checkVaultInitStorage(); e != nil {
			return 1, e
		}
		f, e := humanTerminal()
		if e != nil {
			return 1, e
		}
		defer f.Close()
		fmt.Fprintln(f, "Store this initialization output outside this host. It is shown only here.")
		var result struct {
			Keys []string `json:"keys_base64"`
			Root string   `json:"root_token"`
		}
		if e = c.call(ctx, "POST", "sys/init", map[string]int{"secret_shares": shares, "secret_threshold": threshold}, &result); e != nil {
			return 1, e
		}
		if e = json.NewEncoder(f).Encode(result); e != nil {
			return 1, fail("Could not deliver recovery material; do not reinitialize storage")
		}
		fmt.Fprintln(stdout, "Initialized. Unseal, then provision the restricted backend and human administrator.")
		return 0, nil
	case "unseal", "provision":
		// Human input time is not part of the API operation deadline.
		cancel()
		f, e := humanTerminal()
		if e != nil {
			return 1, e
		}
		defer f.Close()
		r := bufio.NewReader(io.LimitReader(f, 128<<10))
		if op == "unseal" {
			key, e := terminalSecret(f, r, "Unseal share")
			if e != nil {
				return 1, e
			}
			requestCtx, requestCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer requestCancel()
			var s vaultStatus
			if e = c.call(requestCtx, "POST", "sys/unseal", map[string]string{"key": key}, &s); e != nil {
				return 1, e
			}
			if s.Sealed {
				fmt.Fprintf(stdout, "Still sealed: %d/%d shares.\n", s.Progress, s.Threshold)
			} else {
				fmt.Fprintln(stdout, "OpenBao unsealed.")
			}
			return 0, nil
		}
		root, e := terminalSecret(f, r, "Bootstrap root token (revoked after provisioning)")
		if e != nil {
			return 1, e
		}
		password, e := terminalSecret(f, r, "New native UI password for admin (12+ characters)")
		if e != nil {
			return 1, e
		}
		c.token = root
		requestCtx, requestCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer requestCancel()
		if e = provisionVault(requestCtx, c, password); e != nil {
			return 1, e
		}
		fmt.Fprintln(stdout, "Backend configured; bootstrap root revoked. Native UI login: userpass / admin.")
		return 0, nil
	}
	return 2, fail("Unknown vault operation")
}
