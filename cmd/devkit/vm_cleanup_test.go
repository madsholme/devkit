package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cleanupFixture(t *testing.T, entries string) string {
	t.Helper()
	root := controlFixture(t)
	t.Chdir(root)
	data := `{"project":"test","future":{"large":9007199254740993},"hooks":{"check":"keep-me"},"vms":[` + entries + `]}`
	if err := os.WriteFile(filepath.Join(root, "devkit.json"), []byte(data), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "devkit.json"), 0640); err != nil {
		t.Fatal(err)
	}
	return root
}

func cleanupCall(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	old := stdout
	stdout = io.Writer(&out)
	defer func() { stdout = old }()
	_, err := dispatch(append([]string{"cleanup"}, args...))
	return out.String(), err
}

func cleanupEntries(t *testing.T, root string) []vmSelector {
	t.Helper()
	cfg, err := loadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	return cfg.VMs
}

func TestVMCleanupDryRunAndNamedRetention(t *testing.T) {
	root := cleanupFixture(t, `{"name":"worker","control":{"start":"launch","stop":"shutdown"}},{"name":"other","extra":"keep"}`)
	path := filepath.Join(root, "devkit.json")
	before, _ := os.ReadFile(path)
	disk := filepath.Join(root, "guest.qcow2")
	os.WriteFile(disk, []byte("retained data"), 0600)
	running := true
	calls := 0
	swap(t, &vmControlProcesses, func() ([]virtualMachine, []string) {
		if running {
			return []virtualMachine{{Name: "worker", Source: "qemu", State: "running", PID: 42, start: 100}}, nil
		}
		return nil, nil
	})
	swap(t, &vmControlRun, func(ctx context.Context, args []string, o runOpts) error {
		calls++
		if args[len(args)-1] != "shutdown" {
			t.Fatal(args)
		}
		running = false
		return nil
	})
	out, err := cleanupCall(t, "--vm", "worker")
	if err != nil || !strings.Contains(out, "worker") || !strings.Contains(out, "dry run") {
		t.Fatalf("%s %v", out, err)
	}
	after, _ := os.ReadFile(path)
	if calls != 0 || !bytes.Equal(before, after) {
		t.Fatal("dry run mutated")
	}
	out, err = cleanupCall(t, "--vm", "worker", "--yes")
	if err != nil {
		t.Fatalf("%s %v", out, err)
	}
	if running || calls != 1 {
		t.Fatal("guest not stopped exactly once")
	}
	entries := cleanupEntries(t, root)
	if len(entries) != 1 || entries[0].Name != "other" {
		t.Fatal(entries)
	}
	raw, _ := os.ReadFile(path)
	for _, keep := range []string{"9007199254740993", "keep-me", `"extra": "keep"`} {
		if !bytes.Contains(raw, []byte(keep)) {
			t.Fatalf("lost %s: %s", keep, raw)
		}
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0640 {
		t.Fatal(info.Mode())
	}
	got, _ := os.ReadFile(disk)
	if string(got) != "retained data" {
		t.Fatal("disk changed")
	}
	events, _ := readHistory("", 0)
	seen := map[string]bool{}
	for _, e := range events {
		if e.OK {
			seen[e.Action] = true
		}
	}
	if !seen["vm-stop"] || !seen["vm-cleanup"] {
		t.Fatal(events)
	}
}

func TestVMCleanupProjectAndLegacyRecipe(t *testing.T) {
	root := cleanupFixture(t, `{"name":"a"},{"name":"b"}`)
	second := t.TempDir()
	os.WriteFile(filepath.Join(second, "devbox.json"), []byte(`{"project":"test","custom":true,"vms":[{"name":"c"}]}`), 0600)
	third := t.TempDir()
	original := []byte(`{"project":"unrelated","vms":[{"name":"d"}]}`)
	os.WriteFile(filepath.Join(third, "devkit.json"), original, 0600)
	host, _ := json.Marshal(map[string]any{"vm_projects": []string{root, second, third}})
	os.WriteFile(configPath(), host, 0600)
	out, err := cleanupCall(t, "--project", "test", "--yes")
	if err != nil {
		t.Fatalf("%s %v", out, err)
	}
	if len(cleanupEntries(t, root)) != 0 || len(cleanupEntries(t, second)) != 0 {
		t.Fatal("project cleanup incomplete")
	}
	if _, err := os.Stat(filepath.Join(second, "devkit.json")); !os.IsNotExist(err) {
		t.Fatal("legacy recipe replaced by new name")
	}
	got, _ := os.ReadFile(filepath.Join(third, "devkit.json"))
	if !bytes.Equal(got, original) {
		t.Fatal("unrelated recipe changed")
	}
}

func TestVMCleanupPreflightBlocksWholeProject(t *testing.T) {
	for _, purge := range []bool{false, true} {
		t.Run(map[bool]string{false: "running-readonly", true: "missing-purge"}[purge], func(t *testing.T) {
			root := cleanupFixture(t, `{"name":"a","control":{"start":"launch","stop":"shutdown","purge":"purge-a"}},{"name":"z"}`)
			before, _ := os.ReadFile(filepath.Join(root, "devkit.json"))
			calls := 0
			swap(t, &vmControlRun, func(context.Context, []string, runOpts) error { calls++; return nil })
			if !purge {
				swap(t, &vmControlProcesses, func() ([]virtualMachine, []string) {
					return []virtualMachine{{Name: "z", Source: "qemu", State: "running"}}, nil
				})
			}
			args := []string{"--project", "test", "--yes"}
			if purge {
				args = append(args, "--purge")
			}
			out, err := cleanupCall(t, args...)
			if err == nil || !strings.Contains(out, "z") {
				t.Fatalf("%s %v", out, err)
			}
			after, _ := os.ReadFile(filepath.Join(root, "devkit.json"))
			if calls != 0 || !bytes.Equal(before, after) {
				t.Fatal("partially applied blocked batch")
			}
		})
	}
}

func TestVMCleanupPurgeOrderAndFailures(t *testing.T) {
	for _, failAt := range []string{"", "shutdown", "purge"} {
		t.Run("fail-"+failAt, func(t *testing.T) {
			root := cleanupFixture(t, `{"name":"worker","control":{"start":"launch","stop":"shutdown","purge":"purge"}},{"name":"later"}`)
			running := true
			var calls []string
			swap(t, &vmControlProcesses, func() ([]virtualMachine, []string) {
				if running {
					return []virtualMachine{{Name: "worker", Source: "qemu", State: "running"}}, nil
				}
				return nil, nil
			})
			swap(t, &vmControlRun, func(ctx context.Context, args []string, o runOpts) error {
				cmd := args[len(args)-1]
				calls = append(calls, cmd)
				if o.dir != root || envValue(o.env, "DEVKIT_VM_NAME") != "worker" || envValue(o.env, "DEVBOX_VM_NAME") != "worker" {
					t.Fatal("wrong purge/stop environment", o)
				}
				if cmd == failAt {
					return errors.New("adapter failed")
				}
				if cmd == "shutdown" {
					running = false
				}
				if cmd == "purge" && running {
					t.Fatal("purged live guest")
				}
				return nil
			})
			_, err := cleanupCall(t, "--vm", "worker", "--purge", "--yes")
			entries := cleanupEntries(t, root)
			if failAt == "" {
				if err != nil || len(entries) != 1 || strings.Join(calls, ",") != "shutdown,purge" {
					t.Fatal(err, entries, calls)
				}
			} else if err == nil || len(entries) != 2 {
				t.Fatal("failed operation unregistered VM", err, entries)
			}
			if failAt == "shutdown" && len(calls) != 1 {
				t.Fatal("purged after stop failure")
			}
		})
	}
}

func TestVMCleanupPreservesConcurrentRecipeEdit(t *testing.T) {
	root := cleanupFixture(t, `{"name":"worker","control":{"start":"launch","stop":"shutdown","purge":"purge"}}`)
	p := filepath.Join(root, "devkit.json")
	before, _ := os.ReadFile(p)
	edited := append(append([]byte{}, before...), '\n')
	swap(t, &vmControlRun, func(context.Context, []string, runOpts) error { return os.WriteFile(p, edited, 0640) })
	_, err := cleanupCall(t, "--vm", "worker", "--purge", "--yes")
	got, _ := os.ReadFile(p)
	if err == nil || !bytes.Equal(got, edited) {
		t.Fatal("concurrent edit overwritten", err)
	}
	if len(cleanupEntries(t, root)) != 1 {
		t.Fatal("registration lost")
	}
}

func TestVMCleanupRejectsUnsafeInventoryAndRecipes(t *testing.T) {
	for _, kind := range []string{"warnings", "duplicate", "conflict", "symlink", "missing-root", "libvirt"} {
		t.Run(kind, func(t *testing.T) {
			root := cleanupFixture(t, `{"name":"worker"}`)
			switch kind {
			case "warnings":
				swap(t, &vmControlProcesses, func() ([]virtualMachine, []string) { return nil, []string{"unreadable"} })
			case "duplicate":
				swap(t, &vmControlProcesses, func() ([]virtualMachine, []string) {
					return []virtualMachine{{Name: "worker", Source: "qemu"}, {Name: "worker", Source: "qemu"}}, nil
				})
			case "conflict":
				other := t.TempDir()
				os.WriteFile(filepath.Join(other, "devkit.json"), []byte(`{"project":"other","vms":[{"name":"worker"}]}`), 0600)
				host, _ := json.Marshal(map[string]any{"vm_projects": []string{root, other}})
				os.WriteFile(configPath(), host, 0600)
			case "symlink":
				p := filepath.Join(root, "devkit.json")
				os.Rename(p, p+".actual")
				os.Symlink(p+".actual", p)
			case "missing-root":
				host, _ := json.Marshal(map[string]any{"vm_projects": []string{root, filepath.Join(root, "gone")}})
				os.WriteFile(configPath(), host, 0600)
			case "libvirt":
				os.WriteFile(filepath.Join(root, "devkit.json"), []byte(`{"project":"test","vms":[{"uuid":"aaaaaaaa-1111-1111-1111-111111111111","control":{"libvirt_uri":"qemu:///session"}}]}`), 0600)
			}
			if _, err := cleanupCall(t, "--project", "test", "--yes"); err == nil {
				t.Fatal("unsafe cleanup accepted")
			}
		})
	}
}

func TestVMCleanupRejectsChangedProcessBeforeStop(t *testing.T) {
	root := cleanupFixture(t, `{"name":"worker","control":{"start":"launch","stop":"shutdown"}}`)
	samples := 0
	calls := 0
	swap(t, &vmControlProcesses, func() ([]virtualMachine, []string) {
		samples++
		pid := 42
		if samples > 1 {
			pid = 99
		}
		return []virtualMachine{{Name: "worker", Source: "qemu", State: "running", PID: pid, start: uint64(pid)}}, nil
	})
	swap(t, &vmControlRun, func(context.Context, []string, runOpts) error { calls++; return nil })
	if _, err := cleanupCall(t, "--vm", "worker", "--yes"); err == nil || calls != 0 || len(cleanupEntries(t, root)) != 1 {
		t.Fatal("changed identity accepted", err, calls)
	}
}

func TestVMCleanupPartialFailureReportsCompleted(t *testing.T) {
	root := cleanupFixture(t, `{"name":"a","control":{"start":"start","stop":"stop","purge":"purge-a"}},{"name":"b","control":{"start":"start","stop":"stop","purge":"purge-b"}},{"name":"c","control":{"start":"start","stop":"stop","purge":"purge-c"}}`)
	var calls []string
	swap(t, &vmControlRun, func(ctx context.Context, args []string, o runOpts) error {
		calls = append(calls, args[len(args)-1])
		if strings.HasSuffix(args[len(args)-1], "-b") {
			return errors.New("failed b")
		}
		return nil
	})
	out, err := cleanupCall(t, "--project", "test", "--purge", "--yes")
	entries := cleanupEntries(t, root)
	if err == nil || len(entries) != 2 || entries[0].Name != "b" || strings.Join(calls, ",") != "purge-a,purge-b" || !strings.Contains(out, "a") || !strings.Contains(err.Error(), "1") {
		t.Fatal(out, err, entries, calls)
	}
}

func TestVMCleanupArgumentsAndLock(t *testing.T) {
	root := cleanupFixture(t, `{"name":"worker"}`)
	for _, args := range [][]string{{}, {"--yes"}, {"--vm", ""}, {"--project", ""}, {"--vm", "worker", "--project", "test"}, {"--vm", "missing", "--yes"}, {"--project", "missing", "--yes"}, {"--vm", "worker", "extra"}, {"--vm", "worker", "--yes=false"}} {
		if _, err := cleanupCall(t, args...); err == nil {
			t.Fatal("invalid args accepted", args)
		}
	}
	release, err := lockVMControl()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := cleanupCall(t, "--vm", "worker", "--yes"); err == nil {
		t.Fatal("concurrent cleanup accepted")
	}
	if len(cleanupEntries(t, root)) != 1 {
		t.Fatal("registration changed")
	}
}

func TestVMPurgeConfigValidation(t *testing.T) {
	for _, control := range []string{
		`{"start":"s","stop":"s","purge":" "}`,
		`{"start":"s","stop":"s","purge":"bad\u0000command"}`,
		`{"libvirt_uri":"qemu:///session","purge":"delete"}`,
	} {
		root := t.TempDir()
		data := `{"project":"test","vms":[{"uuid":"aaaaaaaa-1111-1111-1111-111111111111","control":` + control + `}]}`
		os.WriteFile(filepath.Join(root, "devkit.json"), []byte(data), 0600)
		if _, err := loadConfig(root); err == nil {
			t.Fatal("invalid purge accepted", control)
		}
	}
}

func TestVMCleanupRejectsChangedDefinition(t *testing.T) {
	root := cleanupFixture(t, `{"name":"worker","control":{"start":"launch","stop":"shutdown","purge":"original"}}`)
	targets, err := vmCleanupCatalog(context.Background())
	if err != nil || len(targets) != 1 {
		t.Fatal(targets, err)
	}
	p := filepath.Join(root, "devkit.json")
	data, _ := os.ReadFile(p)
	os.WriteFile(p, bytes.Replace(data, []byte("original"), []byte("changed"), 1), 0640)
	if _, err := freshCleanupTarget(targets[0], true); err == nil {
		t.Fatal("stale purge definition accepted")
	}
}

func TestVMCleanupPurgeRespawnRetainsRegistration(t *testing.T) {
	root := cleanupFixture(t, `{"name":"worker","control":{"start":"launch","stop":"shutdown","purge":"purge"}}`)
	running := false
	swap(t, &vmControlProcesses, func() ([]virtualMachine, []string) {
		if running {
			return []virtualMachine{{Name: "worker", Source: "qemu", State: "running", PID: 7}}, nil
		}
		return nil, nil
	})
	swap(t, &vmControlRun, func(context.Context, []string, runOpts) error { running = true; return nil })
	if _, err := cleanupCall(t, "--vm", "worker", "--purge", "--yes"); err == nil {
		t.Fatal("active guest unregistered")
	}
	if len(cleanupEntries(t, root)) != 1 {
		t.Fatal("registration lost")
	}
}

func TestVMCleanupStopMustActuallyStop(t *testing.T) {
	root := cleanupFixture(t, `{"name":"worker","control":{"start":"launch","stop":"shutdown","purge":"purge","timeout_seconds":1}}`)
	calls := 0
	swap(t, &vmControlProcesses, func() ([]virtualMachine, []string) {
		return []virtualMachine{{Name: "worker", Source: "qemu", State: "running", PID: 7}}, nil
	})
	swap(t, &vmControlRun, func(context.Context, []string, runOpts) error { calls++; return nil })
	if _, err := cleanupCall(t, "--vm", "worker", "--purge", "--yes"); err == nil {
		t.Fatal("accepted guest still running")
	}
	if calls != 1 || len(cleanupEntries(t, root)) != 1 {
		t.Fatal("purged or removed after failed stop")
	}
}

func TestVMCleanupNameScopeAndUnmappedGuest(t *testing.T) {
	root := cleanupFixture(t, `{"name":"worker"}`)
	other := t.TempDir()
	os.WriteFile(filepath.Join(other, "devkit.json"), []byte(`{"project":"second","vms":[{"name":"different"}]}`), 0600)
	host, _ := json.Marshal(map[string]any{"vm_projects": []string{root, other}})
	os.WriteFile(configPath(), host, 0600)
	if _, err := cleanupCall(t, "--vm", "different", "--yes"); err == nil {
		t.Fatal("cross-recipe name accepted from current project")
	}
	swap(t, &vmControlProcesses, func() ([]virtualMachine, []string) {
		return []virtualMachine{{Name: "unregistered", Source: "qemu", State: "running"}}, nil
	})
	if _, err := cleanupCall(t, "--vm", "unregistered", "--yes"); err == nil {
		t.Fatal("unregistered guest accepted")
	}
	if len(cleanupEntries(t, root)) != 1 || len(cleanupEntries(t, other)) != 1 {
		t.Fatal("unrelated entry removed")
	}
}

func TestVMCleanupKeepsLifecycleDefinitionLimit(t *testing.T) {
	root := cleanupFixture(t, `{"name":"worker","control":{"start":"start","stop":"stop"}}`)
	other := t.TempDir()
	rows := make([]vmSelector, 128)
	for i := range rows {
		rows[i] = vmSelector{Name: fmt.Sprintf("other-%d", i), Control: &vmControl{Start: "start", Stop: "stop"}}
	}
	raw, _ := json.Marshal(projectConfig{Project: "other", VMs: rows})
	os.WriteFile(filepath.Join(other, "devkit.json"), raw, 0600)
	host, _ := json.Marshal(map[string]any{"vm_projects": []string{root, other}})
	os.WriteFile(configPath(), host, 0600)
	if _, err := cleanupCall(t, "--vm", "worker", "--yes"); err == nil {
		t.Fatal("cleanup bypassed the lifecycle catalog limit")
	}
	if len(cleanupEntries(t, root)) != 1 {
		t.Fatal("registration removed")
	}
}
