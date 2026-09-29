package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// shareConfig is the "share" section of the host config: where public shares live and how to reach Cloudflare.
type shareConfig struct {
	Domain       string   `json:"domain"`        // share domain on Cloudflare, e.g. "example-share.com"
	AccountID    string   `json:"account_id"`    // Cloudflare account ID
	ZoneID       string   `json:"zone_id"`       // optional; looked up from the domain when empty
	TokenFile    string   `json:"token_file"`    // API token (Tunnel Write + DNS Write); must not be readable by others
	Prefix       *string  `json:"prefix"`        // put before the preview ID in its share name; default "preview-"
	Hosts        []string `json:"hosts"`         // fixed names under the domain, assignable with --host
	Image        string   `json:"image"`         // cloudflared image
	DefaultHours int      `json:"default_hours"` // share lifetime when --hours is not given
	MaxHours     int      `json:"max_hours"`     // longest lifetime --hours may ask for
	token        string
}

const defaultCloudflared = "cloudflare/cloudflared:2026.9.3"

func configDir() string {
	if dir := os.Getenv("DEVBOX_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "devbox")
}

func expandHome(path string) string {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, rest)
	}
	return path
}

const shareConfigHelp = `Public shares need the "share" section in %s, e.g.
  {"share": {"domain": "example-share.com", "account_id": "<Cloudflare account ID>",
             "hosts": ["sso-1", "sso-2"], "default_hours": 8, "max_hours": 72}}
and an API token with Tunnel Write and DNS Write in %s (chmod 600).`

// loadShareConfig reads the share settings and the Cloudflare token; ok is false when sharing is not configured.
func loadShareConfig() (cfg shareConfig, ok bool, err error) {
	path := filepath.Join(configDir(), "config.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, false, nil
	}
	if err != nil {
		return cfg, false, err
	}
	var file struct {
		Share *shareConfig `json:"share"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return cfg, false, fail("%s: %v", path, err)
	}
	if file.Share == nil {
		return cfg, false, nil
	}
	cfg = *file.Share
	cfg.Domain = strings.TrimRight(strings.ToLower(cfg.Domain), ".")
	if cfg.TokenFile == "" {
		cfg.TokenFile = filepath.Join(configDir(), "cloudflare-token")
	}
	cfg.TokenFile = expandHome(cfg.TokenFile)
	if cfg.Prefix == nil {
		prefix := "preview-"
		cfg.Prefix = &prefix
	}
	if *cfg.Prefix != "" && !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,19}$`).MatchString(*cfg.Prefix) {
		return cfg, false, fail("%s: share prefix %q must be lowercase letters, digits and dashes (at most 20).", path, *cfg.Prefix)
	}
	if cfg.Image == "" {
		cfg.Image = defaultCloudflared
	}
	if cfg.DefaultHours <= 0 {
		cfg.DefaultHours = 8
	}
	if cfg.MaxHours <= 0 {
		cfg.MaxHours = 72
	}
	parts := strings.Split(cfg.Domain, ".")
	valid := len(parts) >= 2
	for _, part := range parts {
		valid = valid && domainLabel.MatchString(part)
	}
	if !valid || cfg.AccountID == "" {
		return cfg, false, fail("%s: share needs a domain and an account_id.", path)
	}
	for _, host := range cfg.Hosts {
		if !domainLabel.MatchString(host) {
			return cfg, false, fail("%s: share host %q must be a single DNS label.", path, host)
		}
	}
	info, err := os.Stat(cfg.TokenFile)
	if err != nil {
		return cfg, false, fail("Cloudflare token file %s is missing.", cfg.TokenFile)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return cfg, false, fail("Cloudflare token file %s must not be readable by others (chmod 600).", cfg.TokenFile)
	}
	token, err := os.ReadFile(cfg.TokenFile)
	if err != nil {
		return cfg, false, err
	}
	cfg.token = strings.TrimSpace(string(token))
	if cfg.token == "" {
		return cfg, false, fail("Cloudflare token file %s is empty.", cfg.TokenFile)
	}
	return cfg, true, nil
}

// requireShareConfig is loadShareConfig for commands that cannot work without it.
func requireShareConfig() (shareConfig, error) {
	cfg, ok, err := loadShareConfig()
	if err == nil && !ok {
		err = fail(shareConfigHelp, filepath.Join(configDir(), "config.json"), filepath.Join(configDir(), "cloudflare-token"))
	}
	return cfg, err
}
