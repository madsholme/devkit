package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Control is opt-in. A project mapping by itself never authorizes a mutation.
type vmControl struct {
	Start          string `json:"start,omitempty"`
	Stop           string `json:"stop,omitempty"`
	LibvirtURI     string `json:"libvirt_uri,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

func (c *vmControl) validate(s vmSelector) error {
	if c == nil {
		return nil
	}
	if c.TimeoutSeconds < 0 || c.TimeoutSeconds > 600 {
		return fail("VM control timeout_seconds must be 1–600 (or omitted).")
	}
	if c.LibvirtURI != "" {
		if (c.LibvirtURI != "qemu:///session" && c.LibvirtURI != "qemu:///system") || s.UUID == "" || c.Start != "" || c.Stop != "" {
			return fail("VM libvirt control needs a UUID and local qemu:///session or qemu:///system URI, without launcher commands.")
		}
	} else if strings.TrimSpace(c.Start) == "" || strings.TrimSpace(c.Stop) == "" || len(c.Start) > 8192 || len(c.Stop) > 8192 || strings.ContainsRune(c.Start+c.Stop, 0) {
		return fail("VM launcher control needs both start and stop commands (at most 8192 bytes each, no NUL).")
	}
	return nil
}

func (c *vmControl) timeout() time.Duration {
	if c.TimeoutSeconds == 0 {
		return 120 * time.Second
	}
	return time.Duration(c.TimeoutSeconds) * time.Second
}

type vmTarget struct {
	root, project string
	liveID        string
	selector      vmSelector
	vm            virtualMachine
}

func (s vmSelector) matches(vm virtualMachine) bool {
	if s.UUID != "" {
		return strings.EqualFold(s.UUID, vm.UUID)
	}
	return s.Name == vm.Name
}
func (s vmSelector) key() string {
	if s.UUID != "" {
		return "uuid:" + strings.ToLower(s.UUID)
	}
	return "name:" + s.Name
}

var vmControlProcesses = processVMs

func processControlState(s vmSelector, rows []virtualMachine, warnings []string) (string, error) {
	if len(warnings) > 0 {
		return "", fail("QEMU inventory is incomplete; refusing lifecycle control.")
	}
	matches := []virtualMachine{}
	for _, row := range rows {
		if s.matches(row) {
			matches = append(matches, row)
		}
	}
	if len(matches) > 1 {
		return "", fail("Multiple VMs match this selector; use a unique UUID.")
	}
	if len(matches) == 0 {
		return "stopped", nil
	}
	if matches[0].Source != "qemu" {
		return "", fail("This VM is managed by libvirt; configure libvirt_uri with its UUID.")
	}
	return matches[0].State, nil
}

func vmControlState(ctx context.Context, t vmTarget) (string, error) {
	c := t.selector.Control
	if c.LibvirtURI == "" {
		rows, warnings := vmControlProcesses()
		return processControlState(t.selector, rows, warnings)
	}
	out, err := vmProbe(ctx, "virsh", "--readonly", "--connect", c.LibvirtURI, "domstate", strings.ToLower(t.selector.UUID))
	if err != nil {
		return "", fail("Cannot read the configured libvirt domain; check its UUID, URI and permissions.")
	}
	switch strings.TrimSpace(out) {
	case "shut off":
		return "stopped", nil
	case "running", "idle", "blocked":
		return "running", nil
	case "paused", "pmsuspended":
		return "paused", nil
	case "in shutdown":
		return "shutting down", nil
	default:
		return "", fail("The configured libvirt domain has an unsupported state.")
	}
}

// Return configured guests even while stopped. Resolve authority separately from
// the more permissive grouping rules: two worktrees must not choose each other's scripts.
func managedVMs(ctx context.Context, rows []virtualMachine, projects []projectConfig, warnings []string) []vmTarget {
	targets := []vmTarget{}
	for _, cfg := range projects {
		for _, s := range cfg.VMs {
			if s.Control == nil {
				continue
			}
			id := "vm-" + digest(cfg.root, s.key())
			t := vmTarget{root: cfg.root, project: cfg.Project, selector: s, vm: virtualMachine{ID: id, ControlID: id, Project: cfg.Project, Name: firstOf(s.Name, s.UUID), UUID: s.UUID, Source: firstOf(s.Control.LibvirtURI, "qemu"), State: "unavailable", Display: "unknown", Graphics: []string{}, Connections: []vmConnection{}, Actions: []string{}}}
			matches := []virtualMachine{}
			for _, row := range rows {
				if s.matches(row) {
					matches = append(matches, row)
				}
			}
			if len(matches) == 1 {
				t.vm = matches[0]
				t.liveID = t.vm.ID
				t.vm.ID = id
				t.vm.ControlID = id
				t.vm.Actions = []string{}
			}
			conflict := len(matches) > 1
			for _, other := range projects {
				for _, claim := range other.VMs {
					if other.root == cfg.root && claim.key() == s.key() {
						continue
					}
					if claim.key() == s.key() || (len(matches) == 1 && claim.matches(matches[0])) {
						conflict = true
					}
				}
			}
			switch {
			case conflict:
				t.vm.ControlNote = "Multiple recipes or VMs claim this target; give each controlled VM one unique definition."
			case len(matches) == 1 && t.vm.Source != firstOf(s.Control.LibvirtURI, "qemu"):
				t.vm.ControlNote = "The observed VM provider does not match its configured control provider."
			default:
				var state string
				var err error
				if s.Control.LibvirtURI != "" {
					state, err = vmControlState(ctx, t)
				} else {
					state, err = processControlState(s, rows, warnings)
				}
				if err != nil {
					t.vm.State = "unavailable"
					t.vm.ControlNote = err.Error()
				} else {
					t.vm.State = state
					switch state {
					case "stopped":
						t.vm.Actions = []string{"start"}
					case "running", "active", "blocked":
						t.vm.Actions = []string{"stop", "restart"}
					case "paused":
						t.vm.Actions = []string{"stop"}
						t.vm.ControlNote = "VM is paused; resume it with its existing manager before a graceful restart."
					default:
						t.vm.ControlNote = "VM is changing state; refresh before acting."
					}
				}
			}
			if t.vm.State == "stopped" || t.vm.State == "unavailable" {
				t.vm.Connections = []vmConnection{}
				t.vm.ConnectionNote = ""
				t.vm.PID = 0
			}
			targets = append(targets, t)
			if len(targets) > 128 {
				for i := range targets {
					targets[i].vm.Actions = []string{}
					targets[i].vm.ControlNote = "Too many managed VMs; limit discovered control definitions to 128."
				}
				return targets
			}
		}
	}
	return targets
}

func appendManagedVMs(rows []virtualMachine, targets []vmTarget) []virtualMachine {
	result := []virtualMachine{}
	for _, row := range rows {
		matched := false
		for _, t := range targets {
			if t.liveID != "" && t.liveID == row.ID {
				matched = true
				break
			}
		}
		if !matched {
			result = append(result, row)
		}
	}
	for _, t := range targets {
		result = append(result, t.vm)
	}
	groupVMProjects(result)
	return result
}

func vmControlCatalog(ctx context.Context) ([]vmTarget, error) {
	cfg, err := loadHostConfig()
	if err != nil {
		return nil, err
	}
	if _, warnings := projectDirectories(cfg); len(warnings) > 0 {
		return nil, fail("Project directory discovery is incomplete; fix projects_dir before controlling VMs.")
	}
	processes, warnings := vmControlProcesses()
	domains := []virtualMachine{}
	if _, err := exec.LookPath("virsh"); err == nil {
		domains, _ = libvirtVMs(ctx)
	}
	rows := mergeVMs(processes, domains)
	roots, _ := vmProjectRoots(ctx, cfg, rows)
	projects, problems := readVMProjects(roots)
	if len(problems) > 0 {
		return nil, fail("A VM project recipe is invalid, unreadable or beyond the discovery limit; fix it before controlling VMs.")
	}
	assignVMProjects(rows, projects)
	return managedVMs(ctx, rows, projects, warnings), nil
}

func resolveVMControl(ctx context.Context, target string) (vmTarget, error) {
	targets, err := vmControlCatalog(ctx)
	if err != nil {
		return vmTarget{}, err
	}
	cwd, _ := os.Getwd()
	current, _ := readVMProjects([]string{cwd})
	local := ""
	if len(current) > 0 {
		local = current[0].root
	}
	matches := []vmTarget{}
	for _, t := range targets {
		if target == t.vm.ControlID {
			matches = []vmTarget{t}
			break
		}
		if (target == t.selector.Name || (t.selector.UUID != "" && strings.EqualFold(target, t.selector.UUID))) && (local == "" || local == t.root) {
			matches = append(matches, t)
		}
	}
	if len(matches) != 1 {
		return vmTarget{}, fail("Use one exact configured VM name/UUID in its project, or a control ID from devkit vms --json.")
	}
	t := matches[0]
	if len(t.vm.Actions) == 0 {
		return vmTarget{}, fail("VM control unavailable: %s", t.vm.ControlNote)
	}
	return t, nil
}

// Nonblocking host-wide lock also protects against separate CLI/dashboard processes.
func lockVMControl() (func(), error) {
	if err := os.MkdirAll(stateDir(), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(stateDir(), "vm-control.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fail("Another VM lifecycle operation is running; try again when it finishes.")
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

// Bound the launcher invocation, but never signal a discovered guest PID. Launch
// scripts must detach guests and redirect their stdio before returning.
var vmControlRun = func(ctx context.Context, args []string, o runOpts) error {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir, cmd.Env = o.dir, o.env
	// Detached QEMU guests leave this group. Cancel the entire launcher group so
	// timed-out shell children cannot mutate a VM after the lifecycle lock is released.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.Stdout, cmd.Stderr = stdout, os.Stderr
	cmd.WaitDelay = 250 * time.Millisecond
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fail("VM command timed out; inspect the guest state before retrying.")
		}
		return fail("VM command failed; inspect its output and the guest state.")
	}
	return nil
}

func vmStep(ctx context.Context, t vmTarget, action string) error {
	ctx, cancel := context.WithTimeout(ctx, t.selector.Control.timeout())
	defer cancel()
	state, err := vmControlState(ctx, t)
	if err != nil {
		return err
	}
	want := "running"
	if action == "stop" {
		want = "stopped"
	}
	if state == want {
		fmt.Fprintln(stdout, "VM is already", want)
		return nil
	}
	if action == "start" && state != "stopped" {
		return fail("VM is %s; refusing to start a second instance.", state)
	}
	c := t.selector.Control
	args := []string{}
	opts := runOpts{dir: t.root}
	if c.LibvirtURI != "" {
		verb := "start"
		if action == "stop" {
			verb = "shutdown"
		}
		args = []string{"virsh", "--connect", c.LibvirtURI, verb, strings.ToLower(t.selector.UUID)}
	} else {
		command := c.Start
		if action == "stop" {
			command = c.Stop
		}
		args = []string{"sh", "-eu", "-c", command}
		opts.env = withEnv(os.Environ(), map[string]string{"DEVKIT_PROJECT": t.project, "DEVKIT_VM_NAME": t.selector.Name, "DEVKIT_VM_UUID": t.selector.UUID})
	}
	fmt.Fprintln(stdout, action, "VM", firstOf(t.selector.Name, t.selector.UUID))
	if err := vmControlRun(ctx, args, opts); err != nil {
		return err
	}
	for {
		if ctx.Err() != nil {
			return fail("Timed out waiting for VM to become %s; no forced shutdown was attempted.", want)
		}
		state, err = vmControlState(ctx, t)
		if err != nil {
			return err
		}
		if state == want {
			return nil
		}
		select {
		case <-ctx.Done():
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func operateVM(ctx context.Context, t vmTarget, action string) error {
	switch action {
	case "start", "stop":
		return vmStep(ctx, t, action)
	case "restart":
		if err := vmStep(ctx, t, "stop"); err != nil {
			return err
		}
		return vmStep(ctx, t, "start")
	default:
		return fail("Unsupported VM action.")
	}
}

func controlVM(action, target string) error {
	unlock, err := lockVMControl()
	if err != nil {
		return err
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	t, err := resolveVMControl(ctx, target)
	cancel()
	if err != nil {
		return err
	}
	if t.vm.State == "paused" && action != "stop" {
		return fail("Resume the paused VM with its existing manager first.")
	}
	if err := operateVM(context.Background(), t, action); err != nil {
		return err
	}
	happened("vm-"+action, t.vm.ControlID, t.project+" / "+firstOf(t.selector.Name, t.selector.UUID))
	return nil
}
