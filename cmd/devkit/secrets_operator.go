package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

func operatorSecretOperation(op string) bool {
	return strings.HasPrefix(op, "cleanup-") || strings.HasPrefix(op, "vault-") || strings.HasPrefix(op, "integration-") || strings.HasPrefix(op, "agents-")
}
func executeSecretOperator(ctx context.Context, q secretRequest) (secretResponse, error) {
	out := secretResponse{Records: []secretRecord{}, Management: map[string]any{}}
	cfg, e := loadVaultConfig()
	if e != nil {
		return out, e
	}
	switch q.Operation {
	case "agents-status", "agents-install", "agents-repair":
		result, e := managedAgentSkills(strings.TrimPrefix(q.Operation, "agents-"))
		out.Management["agents"] = result
		if e == nil && q.Operation != "agents-status" {
			happened(q.Operation, "both", "user edits preserved")
		}
		return out, e

	case "vault-status":
		status := vaultStatus{State: "disabled"}
		if cfg.Provider == "openbao" {
			c, e := newVaultClient(cfg.Address, "")
			if e != nil {
				return out, e
			}
			status, _ = c.status(ctx)
		}
		out.Management["status"] = status
		out.Management["config"] = cfg
		out.Management["maintenance"] = secretsMaintenanceStatus()
		if domain, e := domain(); e == nil {
			out.Management["url"] = "https://vault.devkit." + domain + "/ui/"
		}
		return out, nil
	case "vault-setup":
		if cfg.Provider != "openbao" {
			cfg.Provider = "openbao"
			if e = saveVaultConfig(cfg); e != nil {
				return out, e
			}
		}
		if e = startVault(cfg); e != nil {
			return out, e
		}
		out.Message = "OpenBao started. Complete human initialization/unseal/provision in your terminal, then refresh."
		return out, nil
	case "vault-backup":
		id, e := createVaultBackup()
		out.Message = "Encrypted backup: " + id
		return out, e
	case "vault-backup-config":
		if q.Backup == nil {
			return out, fail("Provide backup configuration")
		}
		cfg.Backup = *q.Backup
		if e = validateBackupDestination(cfg); e != nil {
			return out, e
		}
		if e = saveVaultConfig(cfg); e != nil {
			return out, e
		}
		out.Message = "Backup configuration saved"
		return out, nil
	case "vault-update":
		out.Confirmation = "vault-update:" + digest(cfg.Image+"\n"+q.Image)
		out.Message, e = applyVaultUpdate(cfg, q.Image, q.Confirm == out.Confirmation)
		if e == nil && q.Confirm == out.Confirmation {
			out.Confirmation = ""
		}
		return out, e
	case "integration-list":
		raw, e := readConfigFile()
		if e != nil {
			return out, e
		}
		entries := []map[string]string{}
		if integrations, ok := raw["integrations"].(map[string]any); ok {
			for service, keys := range integrations {
				if m, ok := keys.(map[string]any); ok {
					for key := range m {
						entries = append(entries, map[string]string{"service": service, "key": key, "source": "OpenBao"})
					}
				}
			}
		}
		host, e := loadHostConfig()
		if e != nil {
			return out, e
		}
		if host.Proxy != nil {
			for key, source := range host.Proxy.DNSEnv {
				kind := "Local"
				if source.OpenBao != "" {
					kind = "OpenBao"
				}
				entries = append(entries, map[string]string{"service": "dns", "key": key, "source": kind})
			}
		}
		out.Management["integrations"] = entries
		return out, nil
	case "integration-set":
		if q.Service == "" || q.Value == "" || len(q.Value) > 64<<10 {
			return out, fail("Integration and bounded nonempty value required")
		}
		source, e := setIntegration(q.Service, q.Key, q.Value)
		if e != nil {
			return out, e
		}
		if q.Service == "cloudflare" && q.Key == "token" {
			raw, e := readConfigFile()
			if e != nil {
				return out, e
			}
			host, e := loadHostConfig()
			if e != nil {
				return out, e
			}
			if host.Proxy == nil {
				return out, fail("Configure the proxy before binding its DNS token")
			}
			host.Proxy.DNSEnv = map[string]secretSource{"CF_DNS_API_TOKEN": source}
			raw["proxy"] = host.Proxy
			if e = writeConfigFile(raw); e != nil {
				return out, e
			}
		}
		out.Message = "Credential saved in OpenBao; reload its consumer to use the new value. Remote token issuance/revocation is separate."
		return out, nil
	case "integration-get":
		path, e := integrationPath(q.Service, q.Key)
		if e != nil {
			return out, e
		}
		c, e := managedVaultClient()
		if e != nil {
			return out, e
		}
		value, version, e := c.read(ctx, path)
		if e != nil {
			return out, e
		}
		out.Management["version"] = version
		if q.Reveal {
			if e = secretAudit("integration-reveal", q.Service, q.Key); e != nil {
				return out, e
			}
			out.Value = value
		}
		return out, nil
	}
	return out, fail("Unknown operator operation")
}

// Export only recovery instructions. Root/unseal material is handled by the human terminal.
func vaultRecoveryInstructions() string {
	return fmt.Sprintf("Use devkit vault init --shares N --threshold N, vault unseal, and vault provision on this host. Data is preserved at %s.", filepath.Join(secretStateDir(), "openbao", "data"))
}
