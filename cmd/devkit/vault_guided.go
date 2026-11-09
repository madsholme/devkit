package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Keep recovery material in the existing controlling-terminal commands. This
// coordinator sees only readiness metadata and non-secret share counts.
func guideVault(p *prompter, status func() (vaultStatus, error), ready func() (bool, error), run func([]string) (int, error)) error {
	s, err := status()
	if err != nil {
		return err
	}
	invoke := func(args ...string) error {
		if p.yes {
			return fail("OpenBao needs human input; rerun devkit setup without --yes in your terminal")
		}
		_, err := run(args)
		return err
	}
	if !s.Initialized {
		if p.yes {
			return fail("OpenBao needs initialization; rerun devkit setup without --yes in your terminal")
		}
		shares, err := p.ask("Recovery shares (store outside this host)", "1")
		if err != nil {
			return err
		}
		threshold, err := p.ask("Shares required to unseal", "1")
		if err != nil {
			return err
		}
		n, e1 := strconv.Atoi(shares)
		t, e2 := strconv.Atoi(threshold)
		if e1 != nil || e2 != nil || t < 1 || n < t || n > 10 {
			return fail("Use 1 <= threshold <= shares <= 10")
		}
		if err = invoke("init", "--shares", shares, "--threshold", threshold); err != nil {
			return err
		}
		saved, err := p.confirm("Have you saved the recovery shares and bootstrap token outside this host?", false)
		if err != nil {
			return err
		}
		if !saved {
			return fail("Save the recovery material, then rerun devkit setup; the existing vault will not be reinitialized")
		}
		s, err = status()
		if err != nil {
			return err
		}
	}
	for s.Sealed {
		if err = invoke("unseal"); err != nil {
			return err
		}
		s, err = status()
		if err != nil {
			return err
		}
	}
	ok, err := ready()
	if err != nil {
		return err
	}
	if !ok {
		if err = invoke("provision"); err != nil {
			return err
		}
		ok, err = ready()
		if err != nil {
			return err
		}
		if !ok {
			return fail("OpenBao backend is not ready after provisioning; rerun setup to resume")
		}
	}
	fmt.Fprintln(stdout, "OpenBao ready. Continuing Devkit setup.")
	return nil
}

func setupVaultGuided(p *prompter, cfg vaultConfig) error {
	c, err := newVaultClient(cfg.Address, "")
	if err != nil {
		return err
	}
	// Compose returns before the API necessarily starts listening. Wait only
	// before human input; later operation failures remain explicit.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := waitVaultSetupAPI(ctx, c); err != nil {
		return err
	}
	status := func() (vaultStatus, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return c.status(ctx)
	}
	ready := func() (bool, error) {
		backend, err := managedVaultClient()
		if err != nil {
			return false, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err = backend.call(ctx, "GET", "auth/token/lookup-self", nil, nil)
		var ve vaultError
		if errors.As(err, &ve) && ve.Code == 403 {
			return false, nil
		}
		return err == nil, err
	}
	return guideVault(p, status, ready, vaultCLI)
}

func waitVaultSetupAPI(ctx context.Context, c *vaultClient) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := c.status(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fail("OpenBao API did not become available; rerun setup after checking Docker")
		case <-ticker.C:
		}
	}
}
