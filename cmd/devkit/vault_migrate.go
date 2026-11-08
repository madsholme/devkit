package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type vaultMigration struct{ Source, Hash, Target string }

var verifyImportedCredential = func(kind, value string) error {
	host, e := loadHostConfig()
	if e != nil {
		return e
	}
	if kind == "session" {
		decoded, e := base64.RawStdEncoding.DecodeString(value)
		if e != nil || len(decoded) < 32 {
			return fail("Invalid signing key")
		}
		return nil
	}
	client := cloudflareClient()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c := &cloudflare{token: value, client: client}
	domain := host.Domain
	if kind == "share" {
		raw, e := readConfigFile()
		if e != nil {
			return e
		}
		b, _ := json.Marshal(raw["share"])
		var cfg shareConfig
		if json.Unmarshal(b, &cfg) != nil || cfg.AccountID == "" {
			return fail("Share account configuration required")
		}
		domain = cfg.Domain
		if e = c.call("GET", "/accounts/"+url.PathEscape(cfg.AccountID)+"/cfd_tunnel", nil, nil, nil); e != nil {
			return fail("Share token cannot read configured account tunnels")
		}
	}
	if _, e = dnsZone(c, domain); e != nil {
		return fail("Credential cannot read its configured DNS zone")
	}
	return nil
}

func migrateVaultCredential(kind, confirm string, remove bool) (secretResponse, error) {
	out := secretResponse{Records: []secretRecord{}}
	if kind != "dns" && kind != "share" && kind != "session" {
		return out, fail("Migration source must be dns, share or session")
	}
	unlock, e := lockSecretState()
	if e != nil {
		return out, e
	}
	defer unlock()
	client, e := managedVaultClient()
	if e != nil {
		return out, e
	}
	raw, e := readConfigFile()
	if e != nil {
		return out, e
	}
	original, _ := json.Marshal(raw)
	host, e := loadHostConfig()
	if e != nil {
		return out, e
	}
	var source secretSource
	target := "devkit/integrations/cloudflare/" + kind
	switch kind {
	case "dns":
		if host.Proxy == nil || host.Proxy.DNSProvider != "cloudflare" {
			return out, fail("Verified DNS import currently supports Cloudflare")
		}
		source = host.Proxy.DNSEnv["CF_DNS_API_TOKEN"]
	case "share":
		b, _ := json.Marshal(raw["share"])
		var cfg shareConfig
		if json.Unmarshal(b, &cfg) != nil {
			return out, fail("Invalid share configuration")
		}
		if cfg.Token != nil {
			source = *cfg.Token
		} else {
			source.File = firstOf(cfg.TokenFile, filepath.Join(configDir(), "cloudflare-token"))
		}
	case "session":
		source.File = filepath.Join(configDir(), "web-secret")
		target = "devkit/system/web-session-signing-key"
	}
	journalPath := filepath.Join(vaultDir(), "migration-"+kind+".json")
	var journal vaultMigration
	if source.OpenBao != "" {
		b, e := readPrivate(journalPath, 65536)
		if e != nil || json.Unmarshal(b, &journal) != nil || source.OpenBao != "openbao://"+journal.Target {
			return out, fail("Source is already managed; no pending source retirement")
		}
		source = secretSource{File: journal.Source}
		target = journal.Target
	}
	if source.File == "" || source.Env != "" || source.Command != "" {
		return out, fail("Verified import needs a private source file; configure env/command values explicitly through protected stdin")
	}
	file := expandHome(source.File)
	before, e := readPrivate(file, 64<<10)
	if e != nil {
		return out, e
	}
	hash := skillHash(before)
	if journal.Hash != "" && journal.Hash != hash {
		return out, fail("Original source changed; preserving it")
	}
	value := strings.TrimSpace(string(before))
	if kind == "session" {
		value = base64.RawStdEncoding.EncodeToString(before)
	}
	if value == "" {
		return out, fail("Empty migration source")
	}
	out.Confirmation = "migrate:" + digest(kind+"\n"+file+"\n"+hash+"\n"+string(original)+fmt.Sprint(remove))
	out.Message = fmt.Sprintf("Import %s from %s, verify private readback and consumer read access, then switch its reference. Remove unchanged original: %t. DNS issuance/tunnel creation still need runtime validation.", kind, file, remove)
	if confirm != out.Confirmation {
		return out, nil
	}
	if e = verifyImportedCredential(kind, value); e != nil {
		return out, e
	}
	_, version, e := client.read(context.Background(), target)
	if e != nil {
		if ve, ok := e.(vaultError); !ok || ve.Code != 404 {
			return out, e
		}
	}
	if _, e = client.write(context.Background(), target, value, version); e != nil {
		return out, e
	}
	got, _, e := client.read(context.Background(), target)
	if e != nil || got != value {
		return out, fail("Imported credential readback failed; original is preserved")
	}
	current, e := readPrivate(file, 64<<10)
	if e != nil || !bytes.Equal(before, current) {
		return out, fail("Original credential changed; refusing reference switch")
	}
	currentConfig, e := readConfigFile()
	if e != nil {
		return out, e
	}
	encoded, _ := json.Marshal(currentConfig)
	if !bytes.Equal(original, encoded) {
		return out, fail("Configuration changed; retry migration")
	}
	journal = vaultMigration{file, hash, target}
	b, _ := json.Marshal(journal)
	if e = writePrivate(journalPath, b); e != nil {
		return out, e
	}
	ref := secretSource{OpenBao: "openbao://" + target}
	if kind == "dns" {
		host.Proxy.DNSEnv["CF_DNS_API_TOKEN"] = ref
		raw["proxy"] = host.Proxy
	}
	if kind == "share" {
		share, ok := raw["share"].(map[string]any)
		if !ok {
			return out, fail("Share configuration missing")
		}
		share["token"] = ref
		delete(share, "token_file")
	}
	if e = writeConfigFile(raw); e != nil {
		return out, e
	}
	if remove {
		current, e = readPrivate(file, 64<<10)
		if e != nil || !bytes.Equal(before, current) {
			return out, fail("Reference switched; original changed and was preserved")
		}
		if e = os.Remove(file); e != nil {
			return out, e
		}
		if e = os.Remove(journalPath); e != nil {
			return out, e
		}
	}
	happened("vault migrate", kind, "verified private readback; reference switched")
	out.Message = "Credential imported and verified. Restart the affected consumer; existing plaintext backups are not removed."
	out.Confirmation = ""
	return out, nil
}
func vaultMigrateCLI(args []string) (int, error) {
	pos, v, e := splitArgs(args, map[string]bool{"--source": true, "--confirm": true}, map[string]bool{"--remove-source": true, "--json": true})
	if e != nil || len(pos) != 0 {
		return 2, fail("usage: devkit vault migrate --source dns|share|session [--remove-source] [--confirm PREVIEW_ID]")
	}
	_, remove := v["--remove-source"]
	out, e := migrateVaultCredential(v["--source"], v["--confirm"], remove)
	if e != nil {
		return 1, e
	}
	return 0, json.NewEncoder(stdout).Encode(out)
}
