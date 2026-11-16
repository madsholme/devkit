package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVMValidationStoppedOpaqueAndPlain(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	os.WriteFile(filepath.Join(root, "devkit.json"), []byte(`{"project":"vm-test","vms":[{"name":"guest","control":{"start":"SECRET-start","stop":"SECRET-stop"}},{"name":"plain"}]}`), 0600)
	swap(t, &vmObservation, func(context.Context) ([]virtualMachine, []projectConfig, []string) {
		cfg, _ := loadConfig(root)
		return nil, []projectConfig{cfg}, nil
	})
	swap(t, &vmControlRun, func(context.Context, []string, runOpts) error { t.Fatal("executed VM action"); return nil })
	code, report := validationCLI(t, "project", "validate", "--json")
	if code != 0 || strings.Contains(string(mustJSON(t, report)), "SECRET") {
		t.Fatal(code, report)
	}
	code, report = validationCLI(t, "restart", "--vm", "guest", "--dry-run", "--json")
	if code != 1 {
		t.Fatal(code, report)
	}
}
func TestVMRestartPlanRunningAndIncomplete(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	cfg := projectConfig{root: root, Project: "guest", VMs: []vmSelector{{Name: "guest", Control: &vmControl{Start: "secret", Stop: "secret"}}}}
	rows := []virtualMachine{{ID: "qemu-1", Name: "guest", State: "running", Source: "qemu"}}
	warnings := []string{}
	swap(t, &vmObservation, func(context.Context) ([]virtualMachine, []projectConfig, []string) {
		return rows, []projectConfig{cfg}, warnings
	})
	code, r := validationCLI(t, "restart", "--vm", "guest", "--dry-run", "--json")
	if code != 0 {
		t.Fatal(r)
	}
	warnings = []string{"secret error"}
	code, r = validationCLI(t, "restart", "--vm", "guest", "--dry-run", "--json")
	if code != 1 || r["complete"] != false || strings.Contains(string(mustJSON(t, r)), "secret") {
		t.Fatal(r)
	}
}

func TestVMValidationAuthorityAndConnections(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	cfg := projectConfig{root: root, Project: "test", VMs: []vmSelector{{Name: "guest", Control: &vmControl{Start: "opaque", Stop: "opaque"}}}}
	for _, state := range []string{"paused", "unknown"} {
		t.Run(state, func(t *testing.T) {
			swap(t, &vmObservation, func(context.Context) ([]virtualMachine, []projectConfig, []string) {
				return []virtualMachine{{Name: "guest", Source: "qemu", State: state}}, []projectConfig{cfg}, nil
			})
			code, _ := validationCLI(t, "restart", "--vm", "guest", "--dry-run", "--json")
			if code != 1 {
				t.Fatal("invalid state allowed restart")
			}
		})
	}
	for _, listen := range []string{"0.0.0.0", "127.0.0.1", "203.0.113.2", "192.168.1.2"} {
		r := newReport("test", nil)
		validateVMConnections(&r, virtualMachine{ID: "test", Connections: []vmConnection{{Kind: "spice", Listen: listen, Port: 5900}}})
		if listen == "203.0.113.2" && r.exitCode() != 1 {
			t.Fatal(r)
		}
		if listen != "203.0.113.2" && r.exitCode() != 0 {
			t.Fatal(r)
		}
	}
	duplicate := cfg
	duplicate.root = t.TempDir()
	swap(t, &vmObservation, func(context.Context) ([]virtualMachine, []projectConfig, []string) {
		return []virtualMachine{{Name: "guest", State: "running", Source: "qemu"}}, []projectConfig{cfg, duplicate}, nil
	})
	code, _ := validationCLI(t, "restart", "--vm", "guest", "--dry-run", "--json")
	if code != 1 {
		t.Fatal("ambiguous target permitted")
	}
}
