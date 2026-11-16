package main

import (
	"context"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A lightweight observation deliberately omits diskCapacity/qemu-img and CPU sampling.
var vmObservation = func(ctx context.Context) ([]virtualMachine, []projectConfig, []string) {
	host, err := loadHostConfig()
	if err != nil {
		return nil, nil, []string{"host configuration unavailable"}
	}
	rows, warnings := vmControlProcesses()
	roots, rootWarnings := vmDeclaredProjectRoots(ctx, host, rows)
	warnings = append(warnings, rootWarnings...)
	// Explicit host roots plus their linked Git checkouts do not depend on Docker.
	seenRoots := map[string]bool{}
	for _, root := range append([]string{}, roots...) {
		root = resolve(expandHome(root))
		if seenRoots[root] {
			continue
		}
		seenRoots[root] = true
		if len(seenRoots) > 128 || ctx.Err() != nil {
			warnings = append(warnings, "VM root discovery limit reached")
			break
		}
		if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
			trees, err := worktreesOf(root)
			if err != nil {
				warnings = append(warnings, "Git worktree discovery unavailable")
			} else {
				for _, tree := range trees {
					roots = append(roots, tree.Path)
				}
			}
		}
	}
	projects, problems := readVMProjects(roots)
	warnings = append(warnings, problems...)
	needLibvirt := false
	for _, cfg := range projects {
		for _, s := range cfg.VMs {
			needLibvirt = needLibvirt || (s.Control != nil && s.Control.LibvirtURI != "")
		}
	}
	if _, err := exec.LookPath("virsh"); err == nil {
		domains, problems := libvirtVMs(ctx)
		rows = mergeVMs(rows, domains)
		warnings = append(warnings, problems...)
	} else if needLibvirt {
		warnings = append(warnings, "libvirt tools unavailable")
	}
	hostAddress := host.TailscaleIP
	if hostAddress == "" {
		if value, err := vmProbe(ctx, "tailscale", "ip", "-4"); err == nil && net.ParseIP(strings.TrimSpace(value)) != nil {
			hostAddress = strings.TrimSpace(value)
		}
	}
	if hostAddress == "" {
		hostAddress, _ = os.Hostname()
	}
	username := ""
	if u, err := user.Current(); err == nil {
		username = u.Username
	}
	for i := range rows {
		addVMCommands(&rows[i], username, hostAddress)
	}
	assignVMProjects(rows, projects)
	return rows, projects, warnings
}

type vmOperationView struct {
	ID          string         `json:"id"`
	ControlID   string         `json:"control_id,omitempty"`
	Name        string         `json:"name"`
	UUID        string         `json:"uuid,omitempty"`
	Provider    string         `json:"provider"`
	State       string         `json:"state"`
	Authority   string         `json:"authority"`
	Actions     []string       `json:"actions"`
	Connections []vmConnection `json:"connections"`
}

func vmView(v virtualMachine, authority string) vmOperationView {
	actions := append([]string{}, v.Actions...)
	connections := append([]vmConnection{}, v.Connections...)
	return vmOperationView{v.ID, v.ControlID, v.Name, v.UUID, v.Source, firstOf(v.State, "unknown"), authority, actions, connections}
}
func observedVMTargets(ctx context.Context) ([]vmTarget, []virtualMachine, []projectConfig, []string) {
	rows, projects, warnings := vmObservation(ctx)
	targets := managedVMs(ctx, rows, projects, warnings)
	if len(warnings) > 0 {
		for i := range targets {
			targets[i].vm.Actions = []string{}
		}
	}
	return targets, rows, projects, warnings
}

// Explain targets even when no mutation is permitted. The execution resolver keeps
// its stricter action guard and re-resolves after a dry run.
func lookupVMTarget(target, root string, targets []vmTarget) (vmTarget, bool) {
	exact := []vmTarget{}
	for _, t := range targets {
		if target == t.vm.ControlID && target != "" {
			exact = append(exact, t)
		}
	}
	if len(exact) == 1 {
		return exact[0], true
	}
	if len(exact) > 1 {
		return vmTarget{}, false
	}
	matches := []vmTarget{}
	for _, t := range targets {
		if target == t.vm.ControlID || ((root == "" || t.root == root) && (target == t.selector.Name || (t.selector.UUID != "" && strings.EqualFold(target, t.selector.UUID)))) {
			matches = append(matches, t)
		}
	}
	if len(matches) != 1 {
		return vmTarget{}, false
	}
	return matches[0], true
}
func validateVMConnections(r *operationReport, v virtualMachine) {
	if len(v.Connections) == 0 {
		r.add("vm.connections", "warn", v.ID, "Listeners and client reachability are unknown; a stopped guest supplies no network evidence.")
		return
	}
	ports := map[string]bool{}
	for _, c := range v.Connections {
		if c.Kind == "console" {
			continue
		}
		ip := net.ParseIP(c.Listen)
		switch {
		case ip == nil || ip.IsUnspecified():
			r.add("vm.listener-guard", "warn", v.ID, "Listener binding or wildcard firewall guard is unverified.")
		case ip.IsLoopback():
			r.add("vm.listener-private", "warn", v.ID, "Loopback listener supports tunnels; direct private access is unavailable.")
		case !vmPrivateIP(ip):
			r.add("vm.listener-public", "fail", v.ID, "Observed listener is outside trusted private address ranges.")
		}
		key := net.JoinHostPort(c.Listen, strconv.Itoa(c.Port))
		if ports[key] {
			r.add("vm.listener-conflict", "fail", v.ID, "Multiple connection definitions use the same listener port.")
		}
		ports[key] = true
		direct, tunnels := map[string]bool{}, map[string]bool{}
		for _, route := range c.Direct {
			direct[route.Network] = true
		}
		for _, route := range c.Tunnels {
			tunnels[route.Network] = true
		}
		if !direct["lan"] || !direct["tailscale"] || !tunnels["lan"] || !tunnels["tailscale"] {
			r.add("vm.connection-routes", "warn", v.ID, "Home LAN and away VPN direct/tunnel routes are not both observed.")
		}
	}
	r.add("vm.reachability", "warn", v.ID, "Host observations do not verify LAN/VPN client reachability or firewall policy.")
}
func validateProjectVMs(r *operationReport, root string, cfg projectConfig, target string, required bool) {
	if len(cfg.VMs) == 0 {
		status := "skip"
		if required {
			status = "fail"
		}
		r.add("vm.absent", status, cfg.Project, "No VM mappings are configured.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	targets, rows, projects, warnings := observedVMTargets(ctx)
	if len(warnings) > 0 {
		r.unavailable("vm.inventory", cfg.Project, "VM discovery is incomplete; capabilities are unavailable.")
	}
	views := []vmOperationView{}
	selected := 0
	for _, s := range cfg.VMs {
		id := "vm-" + digest(root, s.key())
		if target != "" && target != s.Name && !strings.EqualFold(target, s.UUID) && target != id {
			continue
		}
		selected++
		if s.Control != nil {
			t, found := lookupVMTarget(id, root, targets)
			if !found {
				r.unavailable("vm.target", id, "Configured control target is unavailable.")
				continue
			}
			if t.vm.State == "unavailable" {
				r.unavailable("vm.state", id, "Configured provider state is unavailable; check its tool, URI and permissions.")
			} else if len(t.vm.Actions) == 0 && len(warnings) == 0 {
				r.add("vm.authority", "fail", id, "No lifecycle action is available; inspect selector uniqueness, provider, permissions and observed state.")
			} else {
				r.add("vm.authority", "ok", id, "Only the listed exact-target lifecycle actions are available.")
			}
			if s.Control.LibvirtURI == "" {
				r.add("vm.launcher", "warn", id, "Opaque launcher dependencies and future disk/network settings are unknown; no launcher was executed.")
			}
			views = append(views, vmView(t.vm, "configured control"))
			validateVMConnections(r, t.vm)
		} else {
			matches := []virtualMachine{}
			for _, v := range rows {
				if s.matches(v) {
					matches = append(matches, v)
				}
			}
			if len(matches) > 1 {
				r.add("vm.selector", "fail", id, "Selector matches multiple observed guests.")
				continue
			}
			v := virtualMachine{ID: id, Name: firstOf(s.Name, s.UUID), UUID: s.UUID, State: "unknown", Source: "unknown"}
			if len(matches) == 1 {
				v = matches[0]
				claims := 0
				for _, p := range projects {
					for _, claim := range p.VMs {
						if claim.matches(v) {
							claims++
						}
					}
				}
				if claims > 1 {
					r.add("vm.claim", "fail", id, "Multiple recipes claim this guest.")
				}
			}
			// Retain the existing instance-bound native stop capability, never launcher authority.
			v.Actions = []string{}
			if len(warnings) == 0 && len(matches) == 1 {
				addLiveVMControls(&v)
			}
			views = append(views, vmView(v, "mapping only; native stop requires separate exact verification"))
			r.add("vm.mapping", "ok", id, "Mapping alone grants no launcher or restart authority.")
			validateVMConnections(r, v)
		}
	}
	if selected == 0 {
		r.add("vm.target", "fail", "vm", "No exact mapping in this recipe matches the target.")
	}
	sort.Slice(views, func(i, j int) bool { return views[i].ID < views[j].ID })
	if data, ok := r.Data.(map[string]any); ok {
		data["vms"] = views
	}
}
func vmRestartPlanCLI(args []string) (int, error) {
	p, v, e := splitArgs(args, map[string]bool{"--vm": true}, map[string]bool{"--dry-run": true, "--json": true})
	_, dry := v["--dry-run"]
	_, asJSON := v["--json"]
	if e != nil || len(p) > 0 || v["--vm"] == "" || !dry {
		return 2, usageError("Use restart --vm TARGET --dry-run [--json]")
	}
	r := newReport("restart --vm", map[string]any{"sequence": []string{"stop", "start"}, "purge": false, "dry_run": true})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	targets, _, _, warnings := observedVMTargets(ctx)
	if len(warnings) > 0 {
		r.unavailable("vm.inventory", "vm", "VM inventory is incomplete; restart is unavailable.")
	}
	root := currentRecipeRoot()
	if !isFile(configFile(root)) {
		root = ""
	}
	t, ok := lookupVMTarget(v["--vm"], root, targets)
	if !ok {
		r.add("vm.target", "fail", "vm", "Use one exact configured name, UUID or control ID.")
	} else {
		r.Data.(map[string]any)["vm"] = vmView(t.vm, "configured control")
		if !slices.Contains(t.vm.Actions, "restart") {
			r.add("vm.restart", "fail", t.vm.ID, "Restart is unavailable for this identity, provider or observed state.")
		} else {
			r.add("vm.restart", "ok", t.vm.ID, "Graceful stop followed by start is permitted; execution will revalidate.")
		}
		r.add("vm.launcher-effects", "warn", t.vm.ID, "Devkit invokes no purge or disk operation; arbitrary launchers control their own side effects. No action was executed.")
	}
	return r.write(asJSON)
}
