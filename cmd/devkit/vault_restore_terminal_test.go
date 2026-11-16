package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"testing"
)

// Driven through a real controlling PTY by vault_terminal_acceptance.py.
func TestVaultRestoreTerminalInput(t *testing.T) {
	mode := os.Getenv("DEVKIT_VAULT_RESTORE_TERMINAL_TEST")
	if mode == "" {
		t.Skip("PTY runner only")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	input, close, e := restoreTerminalCredentials(ctx, "admin")
	if mode == "no-terminal" {
		if e == nil {
			close()
			t.Fatal("accepted missing controlling terminal")
		}
		return
	}
	if e != nil {
		t.Fatal(e)
	}
	defer close()
	for i := 0; i < 2; i++ {
		value, e := input.Share(vaultStatus{State: "sealed", Threshold: 2, Progress: i})
		if mode == "cancel" {
			if e == nil {
				t.Fatal("cancel accepted input")
			}
			return
		}
		if e != nil || value != "synthetic-restore-share" {
			t.Fatal("hidden share input failed")
		}
	}
	user, password, e := input.Login()
	if e != nil || user != "admin" || password != "synthetic-restore-password" {
		t.Fatal("hidden login input failed")
	}
}
