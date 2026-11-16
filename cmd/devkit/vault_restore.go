package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

const vaultRestoreUsage = "usage: vault backup restore ID [--test|--yes] --identity PRIVATE_FILE [--admin-user NAME] [--installation ID]; restore --rollback OPERATION --yes"

func vaultRestoreCLI(args []string) (int, error) {
	pos, v, e := splitArgs(args, map[string]bool{"--identity": true, "--rollback": true, "--admin-user": true, "--installation": true}, map[string]bool{"--test": true, "--yes": true})
	if e != nil {
		return 2, fail(vaultRestoreUsage)
	}
	_, test := v["--test"]
	_, apply := v["--yes"]
	rollback := v["--rollback"]
	if rollback != "" {
		if len(pos) != 0 || !apply || test || v["--identity"] != "" || v["--installation"] != "" || !restoreOperationPattern.MatchString(rollback) {
			return 2, fail(vaultRestoreUsage)
		}
	} else if len(pos) != 1 || test && apply || (test || apply) && v["--identity"] == "" || !test && !apply && (v["--identity"] != "" || v["--installation"] != "" || v["--admin-user"] != "") || test && v["--installation"] != "" {
		return 2, fail(vaultRestoreUsage)
	}
	user := firstOf(v["--admin-user"], "admin")
	if !vaultSegment.MatchString(user) {
		return 2, fail("Invalid administrator name")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if rollback != "" {
		input, close, e := restoreTerminalCredentials(ctx, user)
		if e != nil {
			return 1, e
		}
		defer close()
		if e = rollbackVaultRestore(ctx, rollback, input); e != nil {
			return 1, e
		}
		fmt.Fprintln(stdout, "Previous vault restored. Replacement data retained; app tokens revoked and automatic cleanup off. Start devkit-web explicitly after inspection.")
		return 0, nil
	}
	cfg, e := loadVaultConfig()
	if e != nil {
		return 1, e
	}
	if cfg.Provider != "openbao" {
		return 1, fail("Restore requires configured managed OpenBao")
	}
	b, e := inspectRestoreBackup(cfg, pos[0])
	if e != nil {
		return 1, e
	}
	if !test && !apply {
		fmt.Fprintf(stdout, "Target: %s, data %s.\n", cfg.Address, restoreDataPath())
		fmt.Fprintf(stdout, "Restore backup %s for installation %s using %s. An isolated restore test is mandatory before replacement. Use --test --identity PRIVATE_FILE to rehearse; --yes repeats the test before replacement. Stop devkit-web before --yes. Same-host recovery preserves current TLS certificates, revokes app tokens and leaves automatic cleanup off. Original unseal shares and native admin login are required.\n", b.Manifest.ID, b.Manifest.Installation, b.Manifest.Image)
		return 0, nil
	}
	if e = vaultRestoreGuard(); e != nil {
		return 1, e
	}
	input, close, e := restoreTerminalCredentials(ctx, user)
	if e != nil {
		return 1, e
	}
	defer close()
	operation, e := runVaultRestore(ctx, cfg, b, v["--identity"], v["--installation"], apply, input)
	if e != nil {
		return 1, e
	}
	if test {
		fmt.Fprintln(stdout, "Isolated restore test passed: snapshot unsealed, administrator/backend authenticated, registry and versions verified; configured vault unchanged.")
	} else {
		fmt.Fprintf(stdout, "Restore complete; rollback operation %s. Previous data retained, app tokens revoked, browser sessions invalidated, automatic cleanup off. Start devkit-web explicitly after inspection.\n", operation)
	}
	return 0, nil
}
func runVaultRestore(ctx context.Context, cfg vaultConfig, b restoreBackup, identity, installation string, apply bool, input *restoreCredentials) (operation string, err error) {
	defer func() {
		if err != nil && operation != "" {
			err = fail("Recovery operation %s: %v. Preserve its data; rollback is vault backup restore --rollback %s --yes", operation, err, operation)
		}
	}()
	if e := vaultRestoreGuard(); e != nil {
		return "", e
	}
	contents, e := decryptRestoreBackup(ctx, b, identity)
	if e != nil {
		return "", e
	}
	defer func() {
		if e := os.RemoveAll(contents.Root); e != nil {
			err = fail("Decrypted recovery storage cleanup failed")
		}
	}()
	parent := ""
	if apply {
		if e = restoreWebStopped(ctx); e != nil {
			return "", e
		}
		if e = verifySameHostRestore(cfg, b, contents, installation); e != nil {
			return "", e
		}
		parent = filepath.Dir(restoreDataPath())
		if e = restorePrivateDirectory(parent); e != nil {
			return "", e
		}
		if e = restoreRealPath(parent); e != nil {
			return "", e
		}
	}
	candidate, e := rehearseVaultRestore(ctx, b, contents, input, parent)
	if e != nil {
		return "", e
	}
	defer func() {
		if e := candidate.close(operation == "" || err == nil); e != nil {
			err = e
		}
	}()
	if !apply {
		return "", nil
	}
	operation, e = applyVaultRestore(ctx, cfg, b, contents, candidate, input, installation)
	return operation, e
}
