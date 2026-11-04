package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Cleanup edits registrations, never guessed disk paths. Purge is explicitly
// delegated to a trusted project adapter after the guest is confirmed stopped.
type vmCleanupTarget struct {
	vmTarget
	problem string
}
type vmCleanupRecipe struct {
	root, path string
	data       []byte
	info       os.FileInfo
}

func vmCleanupCatalog(ctx context.Context) ([]vmCleanupTarget, error) {
	cfg, err := loadHostConfig()
	if err != nil {
		return nil, err
	}
	if _, warnings := projectDirectories(cfg); len(warnings) > 0 {
		return nil, fail("Project directory discovery is incomplete; fix projects_dir before cleanup.")
	}
	for _, root := range cfg.VMProjects {
		if info, err := os.Stat(expandHome(root)); err != nil || !info.IsDir() {
			return nil, fail("A vm_projects directory is unavailable; fix it before cleanup.")
		}
	}
	rows, warnings := vmControlProcesses()
	if len(warnings) > 0 {
		return nil, fail("QEMU inventory is incomplete; refusing cleanup.")
	}
	if _, err := exec.LookPath("virsh"); err == nil {
		domains, _ := libvirtVMs(ctx)
		rows = mergeVMs(rows, domains)
	}
	roots, _ := vmProjectRoots(ctx, cfg, rows)
	projects, problems := readVMProjects(roots)
	if len(problems) > 0 {
		return nil, fail("VM recipe discovery is incomplete; fix invalid or unreadable recipes before cleanup.")
	}
	controls := 0
	for _, cfg := range projects {
		for _, selector := range cfg.VMs {
			if selector.Control != nil {
				controls++
			}
		}
	}
	if controls > 128 {
		return nil, fail("Too many managed VMs; limit discovered control definitions to 128 before cleanup.")
	}
	var targets []vmCleanupTarget
	for _, cfg := range projects {
		for _, s := range cfg.VMs {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			id := "vm-" + digest(cfg.root, s.key())
			t := vmCleanupTarget{vmTarget: vmTarget{
				root: cfg.root, project: cfg.Project, selector: s,
				vm: virtualMachine{ID: id, ControlID: id, Name: firstOf(s.Name, s.UUID), State: "stopped", Source: "qemu"},
			}}
			var matches []virtualMachine
			for _, row := range rows {
				if s.matches(row) {
					matches = append(matches, row)
				}
			}
			if len(matches) == 1 {
				t.vm = matches[0]
				t.vm.ControlID = id
			} else if len(matches) > 1 {
				t.problem = "Multiple active VMs match this selector; use a unique name or UUID."
			}
			for _, other := range projects {
				for _, claim := range other.VMs {
					if other.root == cfg.root && claim.key() == s.key() {
						continue
					}
					if claim.key() == s.key() || (len(matches) == 1 && claim.matches(matches[0])) {
						t.problem = "Multiple recipes claim this VM; resolve the conflicting registrations."
					}
				}
			}
			if (s.Control != nil && s.Control.LibvirtURI != "") || t.vm.Source != "qemu" {
				t.problem = "Cleanup supports standalone QEMU only; manage libvirt definitions with their existing manager."
			}
			targets = append(targets, t)
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].root != targets[j].root {
			return targets[i].root < targets[j].root
		}
		return targets[i].selector.key() < targets[j].selector.key()
	})
	return targets, nil
}

func cleanupBlocker(t vmCleanupTarget, purge bool) string {
	if t.problem != "" {
		return t.problem
	}
	switch t.vm.State {
	case "stopped":
	case "running", "paused":
		if t.selector.Control == nil {
			return "Running VM has no configured stop adapter; add lifecycle controls or stop it with its existing manager first."
		}
	default:
		return "VM state is unavailable or changing; refresh before cleanup."
	}
	if purge && (t.selector.Control == nil || t.selector.Control.Purge == "") {
		return "Purge requires an explicit control.purge adapter for this VM."
	}
	return ""
}

func readCleanupRecipe(root string) (*vmCleanupRecipe, error) {
	r := &vmCleanupRecipe{root: root, path: configFile(root)}
	info, err := os.Lstat(r.path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fail("Cleanup requires a regular recipe file, not a symlink: %s", r.path)
	}
	r.info = info
	r.data, err = os.ReadFile(r.path)
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(r.data, &raw); err != nil {
		return nil, fail("Cannot safely edit recipe %s: %v", r.path, err)
	}
	return r, r.check()
}

func (r *vmCleanupRecipe) check() error {
	if configFile(r.root) != r.path {
		return fail("Recipe selection changed; rerun cleanup.")
	}
	info, err := os.Lstat(r.path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(r.path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || !os.SameFile(r.info, info) || info.Mode() != r.info.Mode() || !bytes.Equal(r.data, data) {
		return fail("Recipe changed during cleanup; preserving it. Rerun after inspecting the guest state.")
	}
	return nil
}

func (r *vmCleanupRecipe) remove(s vmSelector) error {
	if err := r.check(); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(r.data, &raw); err != nil {
		return err
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw["vms"], &entries); err != nil {
		return err
	}
	kept := []json.RawMessage{}
	removed := 0
	for _, entry := range entries {
		var selector vmSelector
		if err := json.Unmarshal(entry, &selector); err != nil {
			return err
		}
		if selector.key() == s.key() {
			removed++
		} else {
			kept = append(kept, entry)
		}
	}
	if removed != 1 {
		return fail("Recipe no longer contains exactly one selected VM; refusing to edit it.")
	}
	encoded, err := json.Marshal(kept)
	if err != nil {
		return err
	}
	raw["vms"] = encoded
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.CreateTemp(filepath.Dir(r.path), ".devkit-cleanup-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(r.info.Mode().Perm()); err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = r.check(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), r.path); err != nil {
		return err
	}
	r.data = data
	r.info = info
	return nil
}

// Re-read claims and processes before each mutation. The host lock serializes
// devkit callers; external editors/launchers must not race their own adapters.
func freshCleanupTarget(t vmCleanupTarget, purge bool) (vmCleanupTarget, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	targets, err := vmCleanupCatalog(ctx)
	if err != nil {
		return t, err
	}
	for _, fresh := range targets {
		if fresh.vm.ControlID == t.vm.ControlID {
			before, after := t.selector.Control, fresh.selector.Control
			if fresh.project != t.project || fresh.selector.Name != t.selector.Name || fresh.selector.UUID != t.selector.UUID ||
				(before == nil) != (after == nil) || (before != nil && after != nil && *before != *after) {
				return t, fail("VM definition changed during cleanup; inspect it before retrying.")
			}
			if reason := cleanupBlocker(fresh, purge); reason != "" {
				return t, fail("%s", reason)
			}
			return fresh, nil
		}
	}
	return t, fail("VM registration disappeared; rerun cleanup.")
}

func applyVMCleanup(t vmCleanupTarget, r *vmCleanupRecipe, purge bool) error {
	if err := r.check(); err != nil {
		return err
	}
	fresh, err := freshCleanupTarget(t, purge)
	if err != nil {
		return err
	}
	if fresh.vm.PID != t.vm.PID || fresh.vm.start != t.vm.start || fresh.vm.State != t.vm.State {
		return fail("VM identity or state changed before cleanup; rerun after inspecting it.")
	}
	if err := r.check(); err != nil {
		return err
	}
	if t.vm.State != "stopped" {
		if err := vmStep(context.Background(), t.vmTarget, "stop"); err != nil {
			return err
		}
		happened("vm-stop", t.vm.ControlID, t.project+" / "+t.vm.Name+" (cleanup)")
	}
	// Confirm absence again, including conflicts introduced while stopping.
	fresh, err = freshCleanupTarget(t, purge)
	if err != nil {
		return err
	}
	if fresh.vm.State != "stopped" {
		return fail("VM is still active; registration and data retained.")
	}
	if err := r.check(); err != nil {
		return err
	}
	if purge {
		c := t.selector.Control
		ctx, cancel := context.WithTimeout(context.Background(), c.timeout())
		opts := runOpts{dir: t.root, env: withEnv(os.Environ(), map[string]string{
			"DEVKIT_PROJECT": t.project, "DEVKIT_VM_NAME": t.selector.Name, "DEVKIT_VM_UUID": t.selector.UUID,
		})}
		fmt.Fprintln(stdout, "purge VM", t.vm.Name, "using its configured adapter")
		err := vmControlRun(ctx, []string{"sh", "-eu", "-c", c.Purge}, opts)
		cancel()
		if err != nil {
			return err
		}
		happened("vm-purge", t.vm.ControlID, t.project+" / "+t.vm.Name)
		fresh, err = freshCleanupTarget(t, purge)
		if err != nil {
			return err
		}
		if fresh.vm.State != "stopped" {
			return fail("VM became active during purge; retaining its registration. Inspect adapter effects before retrying.")
		}
	}
	if err := r.remove(t.selector); err != nil {
		return err
	}
	happened("vm-cleanup", t.vm.ControlID, t.project+" / "+t.vm.Name)
	return nil
}

func cleanupVMs(name, project string, purge, yes bool) error {
	if yes {
		unlock, err := lockVMControl()
		if err != nil {
			return err
		}
		defer unlock()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	targets, err := vmCleanupCatalog(ctx)
	cancel()
	if err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	current, _ := readVMProjects([]string{cwd})
	local := ""
	if len(current) > 0 {
		local = current[0].root
	}
	var selected []vmCleanupTarget
	for _, t := range targets {
		if project != "" {
			if t.project == project {
				selected = append(selected, t)
			}
		} else if name == t.vm.ControlID || ((name == t.selector.Name || (t.selector.UUID != "" && strings.EqualFold(name, t.selector.UUID))) && (local == "" || local == t.root)) {
			selected = append(selected, t)
		}
	}
	if len(selected) == 0 {
		return fail("No configured VM matches; use an exact name in its recipe or an exact project slug.")
	}
	if name != "" && len(selected) != 1 {
		return fail("VM name is ambiguous; select it from its owning recipe or use a unique control ID.")
	}
	recipes := map[string]*vmCleanupRecipe{}
	blocked := false
	for _, t := range selected {
		reason := cleanupBlocker(t, purge)
		if recipes[t.root] == nil {
			r, err := readCleanupRecipe(t.root)
			if err != nil {
				reason = err.Error()
			} else {
				recipes[t.root] = r
			}
		}
		operation := "unregister; retain disks and other files"
		if purge {
			operation = "run configured purge adapter, then unregister"
		}
		if t.vm.State != "stopped" {
			operation = "stop, then " + operation
		}
		fmt.Fprintf(stdout, "%s / %s [%s] — %s\n", t.project, t.vm.Name, t.vm.State, operation)
		if reason != "" {
			blocked = true
			fmt.Fprintln(stdout, "  blocked:", reason)
		}
	}
	if blocked {
		return fail("Cleanup blocked; no VMs were changed. Resolve every listed blocker before applying.")
	}
	if !yes {
		fmt.Fprintln(stdout, "dry run: no VMs or recipes changed; add --yes to apply.")
		return nil
	}
	completed := 0
	for _, t := range selected {
		if err := applyVMCleanup(t, recipes[t.root], purge); err != nil {
			return fail("Cleanup stopped at %s after %d completed; this and remaining registrations retained. Inspect any stop/purge effects before retrying: %v", t.vm.Name, completed, err)
		}
		completed++
		fmt.Fprintln(stdout, "Removed VM registration:", t.project, "/", t.vm.Name)
	}
	fmt.Fprintf(stdout, "Cleaned up %d VM registration(s).\n", completed)
	return nil
}
