package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"time"
)

func vaultDoctorChecks() []check {
	cfg, e := loadVaultConfig()
	if e != nil {
		return []check{bad("vault", "Invalid secrets configuration")}
	}
	if cfg.Provider != "openbao" {
		return nil
	}
	var checks []check
	c, _ := newVaultClient(cfg.Address, "")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	status, e := c.status(ctx)
	if e != nil {
		checks = append(checks, bad("vault", "Unavailable; run devkit vault setup"))
	} else if status.State != "ready" {
		checks = append(checks, warn("vault", "%s; human init/unseal may be needed", status.State))
	} else {
		checks = append(checks, ok("vault", "%s, %s", status.Version, status.State))
	}
	data := filepath.Join(secretStateDir(), "openbao", "data")
	st, e := os.Lstat(data)
	if e != nil || !st.IsDir() || st.Mode().Perm()&0077 != 0 {
		checks = append(checks, bad("vault-data", "Private bind directory unavailable: %s", data))
	} else {
		checks = append(checks, ok("vault-data", "Bind mount: %s; keep it when replacing containers", data))
	}
	if cfg.Backup.Directory == "" || cfg.Backup.Recipient == "" {
		checks = append(checks, warn("vault-backup", "Configure encrypted off-host backups with devkit vault backup configure"))
	} else if _, e = exec.LookPath("age"); e != nil {
		checks = append(checks, bad("vault-backup", "Install age to encrypt backups"))
	} else {
		checks = append(checks, ok("vault-backup", "%s; %d copies; daily=%t", cfg.Backup.Directory, cfg.Backup.Retain, cfg.Backup.Daily))
	}
	host, e := loadHostConfig()
	if e != nil || host.Proxy == nil {
		return checks
	}
	dom, e := domain()
	if e != nil {
		return checks
	}
	ip, e := tailscaleIP()
	if e != nil {
		return checks
	}
	name := "vault.devkit." + dom
	if addresses, e := lookupHost(name); e != nil || !slices.Contains(addresses, ip) {
		checks = append(checks, warn("vault-dns", "Ensure %s resolves to this host on Tailscale; LAN clients need split DNS to the configured LAN listener", name))
	}
	for _, address := range listenAddresses(host.Proxy, ip) {
		if len(vaultPrivateEntrypoints([]string{address})) == 0 {
			continue
		}
		_, verify, e := servedCertificate(address, name)
		if e != nil || verify != nil {
			checks = append(checks, warn("vault-https", "Exact certificate/listener for %s at %s not verified", name, address))
		} else {
			checks = append(checks, ok("vault-https", "%s at %s", name, address))
		}
	}
	return checks
}
