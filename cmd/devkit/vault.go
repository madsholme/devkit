package main

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"syscall"
)

const defaultOpenBaoImage = "openbao/openbao:2.7.2"

type vaultConfig struct {
	Provider string            `json:"provider"`
	Address  string            `json:"address"`
	Image    string            `json:"image"`
	Backup   vaultBackupConfig `json:"backup"`
}

type vaultBackupConfig struct {
	Recipient string `json:"recipient,omitempty"`
	Directory string `json:"directory,omitempty"`
	Daily     bool   `json:"daily"`
	Retain    int    `json:"retain"`
}

func defaultVaultConfig() vaultConfig {
	c := vaultConfig{Provider: "openbao", Address: "http://127.0.0.1:8210", Image: defaultOpenBaoImage}
	c.Backup.Daily = true
	c.Backup.Retain = 30
	return c
}
func vaultDir() string       { return filepath.Join(configDir(), "secrets") }
func secretStateDir() string { return filepath.Join(stateDir(), "secrets") }
func loadVaultConfig() (vaultConfig, error) {
	cfg := defaultVaultConfig()
	b, e := os.ReadFile(configPath())
	if errors.Is(e, os.ErrNotExist) {
		cfg.Provider = "local"
		return cfg, nil
	}
	if e != nil {
		return cfg, e
	}
	var raw struct {
		Secrets json.RawMessage `json:"secrets"`
	}
	if e = json.Unmarshal(b, &raw); e != nil {
		return cfg, e
	}
	if len(raw.Secrets) == 0 {
		cfg.Provider = "local"
		return cfg, nil
	}
	if e = json.Unmarshal(raw.Secrets, &cfg); e != nil {
		return cfg, fail("Invalid secrets configuration")
	}
	return cfg, validateVaultConfig(cfg)
}

var pinnedVaultImage = regexp.MustCompile(`^(openbao/openbao|ghcr.io/openbao/openbao):[0-9]+\.[0-9]+\.[0-9]+(@sha256:[a-f0-9]{64})?$`)

func validateVaultConfig(c vaultConfig) error {
	if c.Provider != "local" && c.Provider != "openbao" {
		return fail("Secrets provider must be openbao or local")
	}
	if _, e := newVaultClient(c.Address, ""); e != nil {
		return e
	}
	u, _ := url.Parse(c.Address)
	port, _ := strconv.Atoi(u.Port())
	if port < 1 || port > 65534 {
		return fail("OpenBao API port must leave room for its adjacent cluster port")
	}
	if !pinnedVaultImage.MatchString(c.Image) {
		return fail("OpenBao image must be an exact official version, optionally with a digest")
	}
	if c.Backup.Retain < 1 || c.Backup.Retain > 3650 {
		return fail("Backup retention must be 1–3650 copies")
	}
	return nil
}
func saveVaultConfig(c vaultConfig) error {
	if e := validateVaultConfig(c); e != nil {
		return e
	}
	raw, e := readConfigFile()
	if e != nil {
		return e
	}
	raw["secrets"] = c
	return writeConfigFile(raw)
}
func vaultFiles(c vaultConfig, dir string) (map[string]any, map[string]any) {
	u, _ := url.Parse(c.Address)
	port, _ := strconv.Atoi(u.Port())
	cluster := net.JoinHostPort(u.Hostname(), strconv.Itoa(port+1))
	config := map[string]any{"ui": true, "disable_mlock": true, "api_addr": c.Address, "cluster_addr": "https://" + cluster, "storage": map[string]any{"raft": map[string]string{"path": "/bao/data", "node_id": "devkit"}}, "listener": map[string]any{"tcp": map[string]any{"address": u.Host, "tls_disable": true, "cluster_address": cluster}}, "log_level": "warn"}
	config["audit"] = []any{map[string]any{"file": map[string]any{"devkit": map[string]any{"description": "Private Devkit audit", "options": map[string]string{"file_path": "/bao/data/audit.jsonl", "mode": "0600", "log_raw": "false"}}}}}
	compose := map[string]any{"name": "devkit-vault", "services": map[string]any{"openbao": map[string]any{
		"image": c.Image, "restart": "unless-stopped", "network_mode": "host", "mem_limit": "512m", "memswap_limit": "512m", "cpu_shares": 512,
		"ulimits": map[string]any{"core": map[string]int{"soft": 0, "hard": 0}}, "cap_drop": []string{"ALL"}, "security_opt": []string{"no-new-privileges:true"}, "entrypoint": []string{"bao"}, "command": []string{"server", "-config=/bao/config/server.json"},
		"user":    strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid()),
		"volumes": []string{dir + "/server.json:/bao/config/server.json:ro", filepath.Join(secretStateDir(), "openbao", "data") + ":/bao/data"},
	}}}
	return compose, config
}

// privateDir rejects shared/symlinked storage. Same-user code remains trusted.
func privateDir(path string) error {
	if e := os.MkdirAll(path, 0700); e != nil {
		return e
	}
	st, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm()&0077 != 0 {
		return fail("Secrets directory must be an owner-only real directory")
	}
	if stat, ok := st.Sys().(*syscall.Stat_t); !ok || int(stat.Uid) != os.Getuid() {
		return fail("Secrets directory has unexpected ownership")
	}
	return nil
}
func readPrivate(path string, limit int64) ([]byte, error) {
	fd, e := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return nil, e
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	if !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || !ok || int(stat.Uid) != os.Getuid() {
		return nil, fail("Secret file must be a private regular file owned by this user")
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(b)) > limit {
		return nil, fail("Secret file is too large")
	}
	return b, e
}
func writePrivate(path string, b []byte) error {
	if e := privateDir(filepath.Dir(path)); e != nil {
		return e
	}
	if st, e := os.Lstat(path); e == nil && (!st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0) {
		return fail("Refusing unsafe secret file")
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".secret-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	d, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
