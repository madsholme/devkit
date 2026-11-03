package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestVMControlConfigValidation(t *testing.T) {
	root := t.TempDir()
	for _, entry := range []string{
		`{"name":"worker","control":{}}`,
		`{"name":"worker","control":{"start":"go run ."}}`,
		`{"name":"worker","control":{"libvirt_uri":"qemu:///system"}}`,
		`{"uuid":"aaaaaaaa-1111-1111-1111-111111111111","control":{"libvirt_uri":"qemu+ssh://other/system"}}`,
		`{"uuid":"aaaaaaaa-1111-1111-1111-111111111111","control":{"libvirt_uri":"qemu:///system","start":"true","stop":"true"}}`,
		`{"name":"worker","control":{"start":"true","stop":"true","timeout_seconds":-1}}`,
		`{"name":"worker","control":{"start":"true","stop":"true","timeout_seconds":601}}`,
	} {
		os.WriteFile(filepath.Join(root, "devkit.json"), []byte(`{"project":"demo","vms":[`+entry+`]}`), 0600)
		if _, err := loadConfig(root); err == nil {
			t.Errorf("unsafe control accepted: %s", entry)
		}
	}
}

func TestVMControlStoppedInventoryAndPrivacy(t *testing.T) {
	cfg := projectConfig{root: "/private/project", Project: "demo", VMs: []vmSelector{{Name: "worker", Control: &vmControl{Start: "secret-start", Stop: "secret-stop"}}}}
	swap(t, &vmControlProcesses, func() ([]virtualMachine, []string) { return nil, nil })
	targets := managedVMs(context.Background(), nil, []projectConfig{cfg}, nil)
	if len(targets) != 1 || targets[0].vm.State != "stopped" || strings.Join(targets[0].vm.Actions, ",") != "start" {
		t.Fatalf("stopped VM lost: %+v", targets)
	}
	raw, _ := json.Marshal(targets[0].vm)
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "/private") {
		t.Fatalf("leaked: %s", raw)
	}
	// Same project across worktrees is okay for grouping, not for choosing launch commands.
	other := cfg
	other.root = "/other/worktree"
	targets = managedVMs(context.Background(), nil, []projectConfig{cfg, other}, nil)
	for _, target := range targets {
		if len(target.vm.Actions) != 0 || target.vm.ControlNote == "" {
			t.Fatal("conflicting launchers actionable")
		}
	}
}

func TestVMControlRejectsIncompleteAndDuplicateProcessState(t *testing.T) {
	target := vmTarget{selector: vmSelector{Name: "worker", Control: &vmControl{Start: "start", Stop: "stop"}}}
	for _, fixture := range []struct {
		rows     []virtualMachine
		warnings []string
	}{
		{nil, []string{"unreadable"}},
		{[]virtualMachine{{Name: "worker", Source: "qemu"}, {Name: "worker", Source: "qemu"}}, nil},
	} {
		swap(t, &vmControlProcesses, func() ([]virtualMachine, []string) { return fixture.rows, fixture.warnings })
		if _, err := vmControlState(context.Background(), target); err == nil {
			t.Fatal("unsafe state accepted")
		}
	}
}

func TestVMLibvirtLifecycleAndFailedShutdown(t *testing.T) {
	target := vmTarget{selector: vmSelector{UUID: "aaaaaaaa-1111-1111-1111-111111111111", Control: &vmControl{LibvirtURI: "qemu:///session", TimeoutSeconds: 1}}}
	state := "shut off"
	mutations := []string{}
	swap(t, &vmProbe, func(ctx context.Context, args ...string) (string, error) {
		if strings.Join(args, " ") != "virsh --readonly --connect qemu:///session domstate aaaaaaaa-1111-1111-1111-111111111111" {
			t.Fatalf("unsafe probe: %v", args)
		}
		return state, nil
	})
	failStop := false
	swap(t, &vmControlRun, func(ctx context.Context, args []string, o runOpts) error {
		if len(args) != 5 || args[0] != "virsh" || args[1] != "--connect" || args[2] != "qemu:///session" || args[4] != target.selector.UUID {
			t.Fatalf("unsafe mutation: %v", args)
		}
		mutations = append(mutations, args[3])
		if args[3] == "shutdown" {
			if failStop {
				return errors.New("denied")
			}
			state = "shut off"
		} else if args[3] == "start" {
			state = "running"
		} else {
			t.Fatalf("destructive verb: %v", args)
		}
		return nil
	})
	for _, action := range []string{"start", "start", "restart", "stop", "stop"} {
		if err := operateVM(context.Background(), target, action); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(mutations, ",") != "start,shutdown,start,shutdown" {
		t.Fatal(mutations)
	}
	state = "running"
	failStop = true
	mutations = nil
	if err := operateVM(context.Background(), target, "restart"); err == nil || strings.Join(mutations, ",") != "shutdown" {
		t.Fatalf("restarted after failed stop: %v %v", err, mutations)
	}
}

func TestVMLauncherEnvironmentAndTimeout(t *testing.T) {
	target := vmTarget{root: t.TempDir(), project: "demo", selector: vmSelector{Name: "worker", Control: &vmControl{Start: "start-command", Stop: "stop-command", TimeoutSeconds: 1}}}
	running := false
	swap(t, &vmControlProcesses, func() ([]virtualMachine, []string) {
		if running {
			return []virtualMachine{{Name: "worker", Source: "qemu", State: "running"}}, nil
		}
		return nil, nil
	})
	swap(t, &vmControlRun, func(ctx context.Context, args []string, o runOpts) error {
		if o.dir != target.root || strings.Join(args, " ") != "sh -eu -c start-command" || envValue(o.env, "DEVKIT_VM_NAME") != "worker" || envValue(o.env, "DEVKIT_PROJECT") != "demo" {
			t.Fatalf("launcher contract: %v %+v", args, o)
		}
		running = true
		return nil
	})
	if err := operateVM(context.Background(), target, "start"); err != nil {
		t.Fatal(err)
	}
	// A launcher that returns successfully but does not stop the guest must not restart it.
	calls := 0
	swap(t, &vmControlRun, func(ctx context.Context, args []string, o runOpts) error { calls++; return nil })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := operateVM(ctx, target, "restart"); err == nil || calls != 1 {
		t.Fatalf("restart did not wait for stop: %v %d", err, calls)
	}
}

func TestVMControlLock(t *testing.T) {
	t.Setenv("DEVKIT_STATE_DIR", t.TempDir())
	release, err := lockVMControl()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if release2, err := lockVMControl(); err == nil {
		release2()
		t.Fatal("concurrent lifecycle operation accepted")
	}
}

func controlFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "devkit.json"), []byte(`{"project":"test","vms":[{"name":"worker","control":{"start":"launch","stop":"shutdown"}}]}`), 0600)
	t.Setenv("DEVKIT_CONFIG_DIR", t.TempDir())
	t.Setenv("DEVKIT_STATE_DIR", t.TempDir())
	os.WriteFile(filepath.Join(os.Getenv("DEVKIT_CONFIG_DIR"), "config.json"), []byte(`{"vm_projects":[`+strconv.Quote(root)+`]}`), 0600)
	swap(t, &vmProbe, func(ctx context.Context, args ...string) (string, error) { return "", nil })
	swap(t, &vmControlProcesses, func() ([]virtualMachine, []string) { return nil, nil })
	return root
}

func TestVMCLIAndDashboardResolveOnlyConfiguredControls(t *testing.T) {
	root := controlFixture(t)
	t.Chdir(root)
	running := false
	launches := 0
	swap(t, &vmControlProcesses, func() ([]virtualMachine, []string) {
		if running {
			return []virtualMachine{{Name: "worker", Source: "qemu", State: "running"}}, nil
		}
		return nil, nil
	})
	swap(t, &vmControlRun, func(ctx context.Context, args []string, o runOpts) error { launches++; running = true; return nil })
	// No Docker installation or running containers may be required to control a VM.
	swap(t, &dockerContainers, func() ([]container, error) { return nil, errors.New("Docker unavailable") })
	for _, cmd := range [][]string{{"up", "--vm", "worker"}, {"start", "--vm", "worker"}} {
		if code, err := dispatch(cmd); err != nil || code != 0 {
			t.Fatalf("%v: %d %v", cmd, code, err)
		}
	}
	if launches != 1 {
		t.Fatalf("duplicate launch: %d", launches)
	}
	targets, err := vmControlCatalog(context.Background())
	if err != nil || len(targets) != 1 {
		t.Fatalf("catalog: %v %v", targets, err)
	}
	id := targets[0].vm.ControlID
	p, err := planAction(actionRequest{Action: "vm-stop", VM: id})
	if err != nil || strings.Join(p.args, " ") != "stop --vm "+id || p.dir != root {
		t.Fatalf("dashboard plan: %+v %v", p, err)
	}
	for _, req := range []actionRequest{{Action: "vm-stop", VM: "worker"}, {Action: "vm-destroy", VM: id}, {Action: "vm-start", VM: "../../tmp"}} {
		if _, err := planAction(req); err == nil {
			t.Fatalf("unsafe request accepted: %+v", req)
		}
	}
	for _, cmd := range [][]string{{"destroy", "--vm", "worker"}, {"up", "--vm", ""}, {"stop", "--vm", "worker", "preview"}, {"restart"}} {
		if _, err := dispatch(cmd); err == nil {
			t.Fatalf("invalid CLI accepted: %v", cmd)
		}
	}
	// Config removal between displayed inventory and action must revoke authority.
	os.WriteFile(filepath.Join(root, "devkit.json"), []byte(`{"project":"test","vms":[{"name":"worker"}]}`), 0600)
	if _, err := planAction(actionRequest{Action: "vm-stop", VM: id}); err == nil {
		t.Fatal("stale action retained control")
	}
}

func TestVMActionAuthenticationAndOrigin(t *testing.T) {
	root := controlFixture(t)
	cfgpath := filepath.Join(os.Getenv("DEVKIT_CONFIG_DIR"), "config.json")
	hash, err := hashPassword("vm-test-password")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"vm_projects": []string{root}, "web": map[string]string{"password_hash": hash}})
	os.WriteFile(cfgpath, raw, 0600)
	targets, err := vmControlCatalog(context.Background())
	if err != nil || len(targets) != 1 {
		t.Fatal(err, targets)
	}
	request := actionRequest{Action: "vm-start", VM: targets[0].vm.ControlID}
	server := httptest.NewServer(newActionHandler())
	defer server.Close()
	for _, fixture := range []struct {
		origin string
		want   int
	}{{server.URL, 401}, {"https://elsewhere.invalid", 403}} {
		resp, _ := post(t, server, "/api/action", fixture.origin, "", request)
		if resp.StatusCode != fixture.want {
			t.Fatal(resp.StatusCode)
		}
	}
	resp, _ := post(t, server, "/api/login", server.URL, "", map[string]string{"password": "vm-test-password"})
	cookie := ""
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("no login cookie")
	}
	started := make(chan plannedAction, 1)
	release := make(chan struct{})
	done := make(chan struct{})
	swap(t, &runAction, func(p plannedAction, j *job) error { started <- p; <-release; close(done); return nil })
	resp, body := post(t, server, "/api/action", server.URL, cookie, request)
	if resp.StatusCode != 200 {
		t.Fatalf("%d: %s", resp.StatusCode, body)
	}
	p := <-started
	if p.dir != root || strings.Join(p.args, " ") != "start --vm "+request.VM {
		t.Fatal(p)
	}
	// Same VM and different VMs share the CLI lock; the dashboard also serializes jobs.
	resp, _ = post(t, server, "/api/action", server.URL, cookie, request)
	if resp.StatusCode != 409 {
		t.Fatal("concurrent request allowed", resp.StatusCode)
	}
	close(release)
	<-done
}

func TestVMLibvirtUnavailableIsNotStopped(t *testing.T) {
	cfg := projectConfig{root: "/test", Project: "demo", VMs: []vmSelector{{UUID: "aaaaaaaa-1111-1111-1111-111111111111", Control: &vmControl{LibvirtURI: "qemu:///system"}}}}
	swap(t, &vmProbe, func(context.Context, ...string) (string, error) { return "", errors.New("denied or missing") })
	targets := managedVMs(context.Background(), nil, []projectConfig{cfg}, nil)
	if targets[0].vm.State != "unavailable" || len(targets[0].vm.Actions) != 0 {
		t.Fatal(targets)
	}
}

func TestVMJobInvalidatesInventoryBeforeReportingCompletion(t *testing.T) {
	inv := newVMInventory()
	inv.at = time.Now()
	swap(t, &runAction, func(plannedAction, *job) error { return nil })
	jobs := &jobs{list: map[string]*job{}, after: func(p plannedAction) { inv.invalidate() }}
	j, err := jobs.start(plannedAction{target: "vm lifecycle"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		_, _, done, _ := j.since(0)
		if done {
			if !inv.at.IsZero() {
				t.Fatal("completed action left stale VM inventory")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("job never completed")
}

func TestVMLauncherTimeoutCancelsItsChildren(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := vmControlRun(ctx, []string{"sh", "-eu", "-c", "(sleep 0.15; touch late-mutation) & wait"}, runOpts{dir: root})
	if err == nil {
		t.Fatal("launcher timeout accepted")
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(root, "late-mutation")); err == nil {
		t.Fatal("timed-out launcher continued mutating after releasing its lock")
	}
}

func TestVMIncompleteInventoryKeepsOneRowWithControlsDisabled(t *testing.T) {
	cfg := projectConfig{root: "/demo", Project: "demo", VMs: []vmSelector{{Name: "worker", Control: &vmControl{Start: "start", Stop: "stop"}}}}
	rows := []virtualMachine{{ID: "pid-42", PID: 42, Name: "worker", Source: "qemu", State: "running"}}
	targets := managedVMs(context.Background(), rows, []projectConfig{cfg}, []string{"another process is unreadable"})
	result := appendManagedVMs(rows, targets)
	if len(result) != 1 || len(result[0].Actions) > 0 {
		t.Fatalf("duplicate or unsafe rows: %+v", result)
	}
}

func TestVMUnreadableProcessStatBlocksControl(t *testing.T) {
	root := t.TempDir()
	swap(t, &vmProcRoot, root)
	dir := filepath.Join(root, "123")
	os.Mkdir(dir, 0700)
	os.Symlink("/usr/bin/qemu-system-x86_64", filepath.Join(dir, "exe"))
	os.WriteFile(filepath.Join(dir, "cmdline"), []byte("qemu-system-x86_64\x00-name\x00worker\x00"), 0600)
	os.WriteFile(filepath.Join(dir, "stat"), []byte("unsupported stat"), 0600)
	rows, warnings := processVMs()
	if _, err := processControlState(vmSelector{Name: "worker"}, rows, warnings); err == nil {
		t.Fatal("unreadable VM was presumed stopped")
	}
}
