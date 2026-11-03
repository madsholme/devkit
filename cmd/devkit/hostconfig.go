package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// hostConfig is ~/.config/devkit/config.json: what devkit knows about this host. devkit setup
// writes it; devkit web password adds the dashboard password.
type hostConfig struct {
	Domain      string         `json:"domain"`       // preview domain, e.g. "dev.example.com"; DEV_DOMAIN overrides it
	TailscaleIP string         `json:"tailscale_ip"` // optional; read from tailscale ip -4 when empty
	Harness     *harnessConfig `json:"harness"`      // the agent tool on this host, if any
	Proxy       *proxyConfig   `json:"proxy"`        // the Traefik proxy started by devkit proxy up
	Web         struct {
		Port int `json:"port"`
	} `json:"web"`
	Prune pruneConfig `json:"prune"` // Docker clean-up by devkit prune and the web service
}

// pruneConfig is how much Docker build output devkit keeps. Every key is optional.
type pruneConfig struct {
	BuildCacheGB int   `json:"build_cache_gb"` // build cache kept so rebuilds stay fast; default 30
	ImageAgeDays int   `json:"image_age_days"` // unused images older than this are removed; default 7
	Daily        *bool `json:"daily"`          // the web service prunes once a day; default true
}

// harnessConfig describes the agent tool (T3 Code, Orca, ...) the dashboard reports on and Traefik routes to.
type harnessConfig struct {
	Name      string `json:"name"`      // shown on the dashboard, e.g. "T3 Code"
	Host      string `json:"host"`      // routed at <host>.<domain>; "" for no route
	URL       string `json:"url"`       // where it answers on this host, e.g. "http://127.0.0.1:3773"
	Worktrees string `json:"worktrees"` // glob of its worktrees, e.g. "~/.t3/worktrees/*/*"
}

// proxyConfig is how Traefik gets the wildcard certificate.
type proxyConfig struct {
	Email       string                  `json:"email"`           // Let's Encrypt account
	DNSProvider string                  `json:"dns_provider"`    // lego DNS provider, e.g. "cloudflare"
	DNSEnv      map[string]secretSource `json:"dns_env"`         // the provider's API credentials, e.g. CF_DNS_API_TOKEN
	Staging     bool                    `json:"staging"`         // use Let's Encrypt's staging CA (untrusted certificates)
	Image       string                  `json:"image,omitempty"` // Traefik image; default defaultTraefik
}

const (
	defaultTraefik = "traefik:v3.7.13"
	defaultWebPort = 3780
)

func configDir() string {
	if dir := os.Getenv("DEVKIT_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "devkit")
}

func expandHome(path string) string {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, rest)
	}
	return path
}

// loadHostConfig reads the host config with its defaults; a missing file is an empty config.
func loadHostConfig() (hostConfig, error) {
	var cfg hostConfig
	data, err := os.ReadFile(configPath())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return cfg, err
	}
	if err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return cfg, fail("%s: %v", configPath(), err)
		}
	}
	cfg.Domain = strings.TrimRight(strings.ToLower(cfg.Domain), ".")
	if cfg.Web.Port == 0 {
		cfg.Web.Port = defaultWebPort
	}
	if cfg.Prune.BuildCacheGB < 0 || cfg.Prune.ImageAgeDays < 0 {
		return cfg, fail("%s: prune build_cache_gb and image_age_days must not be negative.", configPath())
	}
	if cfg.Prune.BuildCacheGB == 0 {
		cfg.Prune.BuildCacheGB = 30
	}
	if cfg.Prune.ImageAgeDays == 0 {
		cfg.Prune.ImageAgeDays = 7
	}
	if cfg.Prune.Daily == nil {
		daily := true
		cfg.Prune.Daily = &daily
	}
	return cfg, nil
}

// validDomain checks a domain name label by label, so it cannot inject into Traefik rules.
func validDomain(value string, maxLength int) bool {
	parts := strings.Split(value, ".")
	valid := len(parts) >= 2 && len(value) <= maxLength
	for _, part := range parts {
		valid = valid && domainLabel.MatchString(part)
	}
	return valid
}

// tailscaleIP is this host's Tailscale IPv4: the configured one, or what tailscale ip -4 reports.
var tailscaleIP = func() (string, error) {
	cfg, err := loadHostConfig()
	if err != nil {
		return "", err
	}
	value := cfg.TailscaleIP
	if value == "" {
		out, err := capture("tailscale", "ip", "-4")
		if err != nil {
			return "", fail("Tailscale is not running here: %v", err)
		}
		value, _, _ = strings.Cut(strings.TrimSpace(out), "\n")
	}
	ip, err := netip.ParseAddr(value)
	if err != nil || !tailscaleRange.Contains(ip) {
		return "", fail("%q is not a Tailscale IPv4 address.", value)
	}
	return ip.String(), nil
}

// secretSource says where a secret comes from: a private file, an environment variable or a
// command such as "op read op://dev/cloudflare/token". Exactly one is set.
type secretSource struct {
	File    string `json:"file,omitempty"`
	Env     string `json:"env,omitempty"`
	Command string `json:"command,omitempty"`
}

func (s secretSource) String() string {
	switch {
	case s.File != "":
		return "file " + expandHome(s.File)
	case s.Env != "":
		return "environment variable " + s.Env
	}
	return "command " + s.Command
}

func (s secretSource) read() (string, error) {
	set := 0
	for _, v := range []string{s.File, s.Env, s.Command} {
		if v != "" {
			set++
		}
	}
	if set != 1 {
		return "", fail("A secret needs exactly one of file, env or command.")
	}
	var value string
	switch {
	case s.File != "":
		path := expandHome(s.File)
		info, err := os.Stat(path)
		if err != nil {
			return "", fail("Secret file %s is missing.", path)
		}
		if info.Mode().Perm()&0o077 != 0 {
			return "", fail("Secret file %s must not be readable by others (chmod 600).", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		value = string(data)
	case s.Env != "":
		value = os.Getenv(s.Env)
	default:
		out, err := runCommand([]string{"sh", "-c", s.Command}, runOpts{capture: true})
		if err != nil {
			return "", fail("Secret command %q failed: %v", s.Command, err)
		}
		value = out
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fail("Secret from %s is empty.", s)
	}
	return value, nil
}

// writeSecretFile stores a secret typed during setup, readable only by this user.
func writeSecretFile(path, value string) error {
	path = expandHome(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// writeConfigFile saves the generic host config privately.
func writeConfigFile(cfg map[string]any) error {
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(configPath(), append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Chmod(configPath(), 0o600)
}

// shareConfig is the "share" section of the host config: where public shares live and how to reach Cloudflare.
type shareConfig struct {
	Domain       string        `json:"domain"`        // share domain on Cloudflare, e.g. "example-share.com"
	AccountID    string        `json:"account_id"`    // Cloudflare account ID
	ZoneID       string        `json:"zone_id"`       // optional; looked up from the domain when empty
	Token        *secretSource `json:"token"`         // API token (Tunnel Write + DNS Write); default the file below
	TokenFile    string        `json:"token_file"`    // older form of token.file
	Prefix       *string       `json:"prefix"`        // put before the preview ID in its share name; default "preview-"
	Hosts        []string      `json:"hosts"`         // fixed names under the domain, assignable with --host
	Image        string        `json:"image"`         // cloudflared image
	DefaultHours int           `json:"default_hours"` // share lifetime when --hours is not given
	MaxHours     int           `json:"max_hours"`     // longest lifetime --hours may ask for
	token        string
}

const defaultCloudflared = "cloudflare/cloudflared:2026.9.3"

const shareConfigHelp = `Public shares need the "share" section in %s, e.g.
  {"share": {"domain": "example-share.com", "account_id": "<Cloudflare account ID>",
             "hosts": ["sso-1", "sso-2"], "default_hours": 8, "max_hours": 72}}
and an API token with Tunnel Write and DNS Write in %s (chmod 600),
or "token": {"env": "..."} or {"command": "..."} to read it from elsewhere.`

// loadShareConfig reads the share settings and the Cloudflare token; ok is false when sharing is not configured.
func loadShareConfig() (cfg shareConfig, ok bool, err error) {
	path := configPath()
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
	if cfg.Token == nil {
		file := cfg.TokenFile
		if file == "" {
			file = filepath.Join(configDir(), "cloudflare-token")
		}
		cfg.Token = &secretSource{File: file}
	}
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
	if !validDomain(cfg.Domain, 253) || cfg.AccountID == "" {
		return cfg, false, fail("%s: share needs a domain and an account_id.", path)
	}
	for _, host := range cfg.Hosts {
		if !domainLabel.MatchString(host) {
			return cfg, false, fail("%s: share host %q must be a single DNS label.", path, host)
		}
	}
	if cfg.token, err = cfg.Token.read(); err != nil {
		return cfg, false, fmt.Errorf("Cloudflare token: %w", err)
	}
	return cfg, true, nil
}

// requireShareConfig is loadShareConfig for commands that cannot work without it.
func requireShareConfig() (shareConfig, error) {
	cfg, ok, err := loadShareConfig()
	if err == nil && !ok {
		err = fail(shareConfigHelp, configPath(), filepath.Join(configDir(), "cloudflare-token"))
	}
	return cfg, err
}
