package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Only allowlisted metadata crosses the API boundary. Paths and raw arguments stay private.
type vmConnection struct {
	Kind    string `json:"kind"`
	Port    int    `json:"port,omitempty"`
	Tunnel  string `json:"tunnel,omitempty"`
	Command string `json:"command,omitempty"`
	host    string
}

type vmDisk struct{ path, format string }

type virtualMachine struct {
	ControlID      string         `json:"control_id,omitempty"`
	Actions        []string       `json:"actions,omitempty"`
	ControlNote    string         `json:"control_note,omitempty"`
	Project        string         `json:"project"`
	ProjectNote    string         `json:"project_note,omitempty"`
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	UUID           string         `json:"uuid,omitempty"`
	Source         string         `json:"source"`
	State          string         `json:"state"`
	PID            int            `json:"pid,omitempty"`
	Display        string         `json:"display"`
	MemoryBytes    uint64         `json:"memory_bytes,omitempty"`
	VCPUs          int            `json:"vcpus,omitempty"`
	DiskBytes      *uint64        `json:"disk_bytes"`
	Graphics       []string       `json:"graphics"`
	CPUStatus      string         `json:"cpu_status"`
	CPUPercent     *float64       `json:"cpu_percent"`
	RSSBytes       uint64         `json:"rss_bytes,omitempty"`
	Connections    []vmConnection `json:"connections"`
	ConnectionNote string         `json:"connection_note,omitempty"`
	cwd            string
	diskTargets    []string
	disks          []vmDisk
	ticks, start   uint64
}

type vmSnapshot struct {
	VMs      []virtualMachine `json:"vms"`
	Warnings []string         `json:"warnings"`
}

type vmCPU struct {
	ticks, start uint64
	at           time.Time
}

type vmInventory struct {
	mu       sync.Mutex
	at       time.Time
	data     vmSnapshot
	previous map[string]vmCPU
}

func (v *vmInventory) invalidate() { v.mu.Lock(); defer v.mu.Unlock(); v.at = time.Time{} }

func newVMInventory() *vmInventory { return &vmInventory{previous: map[string]vmCPU{}} }

// Output and elapsed time are bounded even for unavailable libvirt daemons or disks.
type vmOutput struct{ bytes.Buffer }

func (b *vmOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 2<<20 {
		return 0, fmt.Errorf("VM probe output too large")
	}
	return b.Buffer.Write(p)
}

var vmProbe = func(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	cmd.WaitDelay = 100 * time.Millisecond
	var out vmOutput
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	err := cmd.Run()
	return out.String(), err
}
var vmProcRoot = "/proc"
var vmUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var vmUser = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.-]*[$]?$`)
var vmHost = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]*$`)

func qemuOptions(s string) map[string]string {
	// QEMU escapes a literal comma as two commas.
	s = strings.ReplaceAll(s, ",,", "\x00")
	m := map[string]string{}
	for _, p := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(p, "=")
		if ok {
			m[k] = strings.ReplaceAll(v, "\x00", ",")
		}
	}
	return m
}

func vmBytes(s string, base uint64) uint64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	switch s[len(s)-1] {
	case 'k', 'K':
		base = 1 << 10
		s = s[:len(s)-1]
	case 'm', 'M':
		base = 1 << 20
		s = s[:len(s)-1]
	case 'g', 'G':
		base = 1 << 30
		s = s[:len(s)-1]
	case 't', 'T':
		base = 1 << 40
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil || n > ^uint64(0)/base {
		return 0
	}
	return n * base
}

func vmPort(s string) int {
	n, _ := strconv.Atoi(s)
	if n < 1 || n > 65535 {
		return 0
	}
	return n
}

func vmListener(s string) (string, bool) {
	if s == "" || s == "0.0.0.0" {
		return "127.0.0.1", true
	}
	if s == "::" {
		return "::1", true
	}
	ip := net.ParseIP(s)
	return s, ip != nil && ip.IsLoopback()
}

func addVMEndpoint(vm *virtualMachine, kind, host, port string) {
	h, ok := vmListener(host)
	p := vmPort(port)
	if ok && p != 0 {
		vm.Connections = append(vm.Connections, vmConnection{Kind: kind, Port: p, host: h})
	} else {
		vm.ConnectionNote = "Display uses an unsupported address, socket or TLS-only port; use its existing client configuration."
	}
}

func parseQEMU(pid int, args []string) virtualMachine {
	vm := virtualMachine{ID: fmt.Sprintf("pid-%d", pid), PID: pid, Name: fmt.Sprintf("QEMU %d", pid), Source: "qemu", State: "running", Display: "unknown", Graphics: []string{}, Connections: []vmConnection{}}
	headless := false
	for i := 1; i < len(args); i++ {
		flag := args[i]
		if flag == "-nographic" {
			headless = true
			continue
		}
		if i+1 >= len(args) {
			continue
		}
		v := args[i+1]
		o := qemuOptions(v)
		switch flag {
		case "-name":
			vm.Name = strings.Split(v, ",")[0]
			if o["guest"] != "" {
				vm.Name = o["guest"]
			}
		case "-uuid":
			if vmUUID.MatchString(v) {
				vm.UUID = strings.ToLower(v)
			}
		case "-m":
			if o["size"] != "" {
				v = o["size"]
			}
			vm.MemoryBytes = vmBytes(v, 1<<20)
		case "-smp":
			n := strings.Split(v, ",")[0]
			if o["cpus"] != "" {
				n = o["cpus"]
			}
			vm.VCPUs, _ = strconv.Atoi(n)
			if vm.VCPUs < 0 {
				vm.VCPUs = 0
			}
		case "-display":
			if v == "none" {
				headless = true
			} else {
				vm.Display = "local"
			}
		case "-spice":
			vm.Display = "spice"
			if vmPort(o["tls-port"]) != 0 {
				vm.ConnectionNote = "SPICE uses TLS; use its existing viewer and certificate configuration."
			} else if o["unix"] == "on" {
				vm.ConnectionNote = "SPICE uses a Unix socket; use its existing client configuration."
			} else {
				addVMEndpoint(&vm, "spice", o["addr"], o["port"])
			}
		case "-vnc":
			if v == "none" {
				continue
			}
			vm.Display = "vnc"
			address := strings.Split(v, ",")[0]
			host, display, ok := strings.Cut(address, ":")
			if strings.HasPrefix(address, "[") {
				host, display, _ = net.SplitHostPort(address)
				ok = display != ""
			}
			n, err := strconv.Atoi(display)
			if ok && err == nil && n >= 0 && n <= 59635 {
				addVMEndpoint(&vm, "vnc", host, strconv.Itoa(5900+n))
			} else {
				vm.ConnectionNote = "VNC uses an unsupported address or socket; use its existing client configuration."
			}
		case "-vga":
			if v != "none" {
				vm.Graphics = append(vm.Graphics, v)
			}
		case "-device":
			model := strings.Split(v, ",")[0]
			if strings.Contains(model, "vga") || strings.Contains(model, "VGA") || strings.HasPrefix(model, "virtio-gpu") || model == "qxl" || model == "bochs-display" || model == "ramfb" {
				vm.Graphics = append(vm.Graphics, model)
			}
			if model == "vfio-pci" {
				vm.Graphics = append(vm.Graphics, pciGraphics(o["host"]))
			}
		case "-drive":
			if o["media"] != "cdrom" && o["if"] != "pflash" {
				vm.disks = append(vm.disks, vmDisk{o["file"], o["format"]})
			}
		case "-hda", "-hdb", "-hdc", "-hdd":
			vm.disks = append(vm.disks, vmDisk{v, ""})
		case "-blockdev":
			vm.disks = append(vm.disks, vmDisk{}) // advanced graphs: capacity deliberately unavailable
		case "-netdev", "-nic", "-net":
			for _, part := range strings.Split(v, ",") {
				if !strings.HasPrefix(part, "hostfwd=tcp:") {
					continue
				}
				bind, guest, ok := strings.Cut(strings.TrimPrefix(part, "hostfwd=tcp:"), "-")
				if !ok || !strings.HasSuffix(guest, ":22") {
					continue
				}
				h, p, err := net.SplitHostPort(bind)
				if err == nil {
					if _, ok := vmListener(h); ok && vmPort(p) != 0 {
						addVMEndpoint(&vm, "ssh", h, p)
					}
				}
			}
		default:
			continue
		}
		i++
	}
	if vm.Display == "unknown" && headless {
		vm.Display = "headless"
	}
	return vm
}

func pciGraphics(address string) string {
	if !strings.Contains(address, "0000:") && len(address) == 7 {
		address = "0000:" + address
	}
	if !regexp.MustCompile(`^[0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}\.[0-7]$`).MatchString(address) {
		return "PCI passthrough (type unavailable)"
	}
	class, err := os.ReadFile(filepath.Join("/sys/bus/pci/devices", address, "class"))
	if err == nil && strings.HasPrefix(string(class), "0x03") {
		return "GPU passthrough " + address
	}
	return "PCI passthrough " + address + " (GPU unconfirmed)"
}

func processVMs() ([]virtualMachine, []string) {
	rows := []virtualMachine{}
	warnings := []string{}
	entries, err := os.ReadDir(vmProcRoot)
	if err != nil {
		return rows, []string{"QEMU process inventory is unavailable; check /proc access."}
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		dir := filepath.Join(vmProcRoot, e.Name())
		exe, err := os.Readlink(filepath.Join(dir, "exe"))
		if err != nil {
			if comm, e := os.ReadFile(filepath.Join(dir, "comm")); e == nil && (strings.HasPrefix(string(comm), "qemu-system-") || strings.TrimSpace(string(comm)) == "qemu-kvm") {
				if len(warnings) == 0 {
					warnings = append(warnings, "Some QEMU process metadata is unreadable; inventory may be incomplete.")
				}
			}
			continue
		}
		name := strings.TrimSuffix(filepath.Base(exe), " (deleted)")
		if !strings.HasPrefix(name, "qemu-system-") && name != "qemu-kvm" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, "cmdline"))
		if err != nil {
			warnings = append(warnings, "A QEMU process is unreadable; inventory may be incomplete.")
			continue
		}
		vm := parseQEMU(pid, strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00"))
		stat, err := os.ReadFile(filepath.Join(dir, "stat"))
		if err != nil {
			if !os.IsNotExist(err) {
				warnings = append(warnings, "A QEMU process state is unreadable; inventory may be incomplete.")
			}
			continue
		}
		end := strings.LastIndex(string(stat), ")")
		if end < 0 {
			warnings = append(warnings, "A QEMU process state is unsupported; inventory may be incomplete.")
			continue
		}
		f := strings.Fields(string(stat)[end+1:])
		if len(f) < 22 {
			warnings = append(warnings, "A QEMU process state is unsupported; inventory may be incomplete.")
			continue
		}
		if f[0] == "Z" {
			continue
		}
		u, _ := strconv.ParseUint(f[11], 10, 64)
		s, _ := strconv.ParseUint(f[12], 10, 64)
		vm.ticks = u + s
		vm.start, _ = strconv.ParseUint(f[19], 10, 64)
		rss, _ := strconv.ParseUint(f[21], 10, 64)
		vm.RSSBytes = rss * uint64(os.Getpagesize())
		if f[0] == "T" || f[0] == "t" {
			vm.State = "paused"
		}
		vm.cwd, _ = os.Readlink(filepath.Join(dir, "cwd"))
		for j := range vm.disks {
			if p := vm.disks[j].path; p != "" && !filepath.IsAbs(p) {
				cwd, err := os.Readlink(filepath.Join(dir, "cwd"))
				if err == nil {
					vm.disks[j].path = filepath.Join(cwd, p)
				} else {
					vm.disks[j].path = ""
				}
			}
		}
		rows = append(rows, vm)
	}
	return rows, warnings
}

type domainXML struct {
	Name   string `xml:"name"`
	UUID   string `xml:"uuid"`
	Memory struct {
		Unit  string `xml:"unit,attr"`
		Value string `xml:",chardata"`
	} `xml:"memory"`
	CurrentMemory struct {
		Unit  string `xml:"unit,attr"`
		Value string `xml:",chardata"`
	} `xml:"currentMemory"`
	VCPU struct {
		Current int `xml:"current,attr"`
		Value   int `xml:",chardata"`
	} `xml:"vcpu"`
	Devices struct {
		Graphics []struct {
			Type      string `xml:"type,attr"`
			Port      string `xml:"port,attr"`
			TLSPort   string `xml:"tlsPort,attr"`
			Listen    string `xml:"listen,attr"`
			Socket    string `xml:"socket,attr"`
			Listeners []struct {
				Type    string `xml:"type,attr"`
				Address string `xml:"address,attr"`
			} `xml:"listen"`
		} `xml:"graphics"`
		Video []struct {
			Model struct {
				Type string `xml:"type,attr"`
			} `xml:"model"`
		} `xml:"video"`
		Disks []struct {
			Device string `xml:"device,attr"`
			Target struct {
				Dev string `xml:"dev,attr"`
			} `xml:"target"`
			Driver struct {
				Type string `xml:"type,attr"`
			} `xml:"driver"`
			Source struct {
				File string `xml:"file,attr"`
			} `xml:"source"`
		} `xml:"disk"`
		Hostdev []struct {
			Type   string `xml:"type,attr"`
			Source struct {
				Address struct {
					Domain   string `xml:"domain,attr"`
					Bus      string `xml:"bus,attr"`
					Slot     string `xml:"slot,attr"`
					Function string `xml:"function,attr"`
				} `xml:"address"`
			} `xml:"source"`
		} `xml:"hostdev"`
	} `xml:"devices"`
}

func domainMemory(value, unit string) uint64 {
	base := uint64(1024)
	switch strings.ToLower(unit) {
	case "b", "bytes":
		base = 1
	case "m", "mib":
		base = 1 << 20
	case "g", "gib":
		base = 1 << 30
	case "kb":
		base = 1000
	case "mb":
		base = 1000000
	case "gb":
		base = 1000000000
	}
	return vmBytes(value, base)
}

func parseDomain(raw, uri string) (virtualMachine, error) {
	var d domainXML
	if err := xml.Unmarshal([]byte(raw), &d); err != nil {
		return virtualMachine{}, err
	}
	if !vmUUID.MatchString(d.UUID) {
		return virtualMachine{}, fmt.Errorf("invalid domain UUID")
	}
	vm := virtualMachine{ID: uri + "/" + strings.ToLower(d.UUID), UUID: strings.ToLower(d.UUID), Name: d.Name, Source: uri, State: "active", Display: "headless", VCPUs: d.VCPU.Value, MemoryBytes: domainMemory(d.Memory.Value, d.Memory.Unit), Graphics: []string{}, Connections: []vmConnection{}}
	if d.CurrentMemory.Value != "" {
		vm.MemoryBytes = domainMemory(d.CurrentMemory.Value, d.CurrentMemory.Unit)
	}
	if d.VCPU.Current > 0 {
		vm.VCPUs = d.VCPU.Current
	}
	for _, g := range d.Devices.Graphics {
		vm.Display = "local"
		if g.Type == "spice" || g.Type == "vnc" {
			vm.Display = g.Type
			listen := g.Listen
			unsupported := g.Socket != "" || vmPort(g.TLSPort) != 0
			for _, l := range g.Listeners {
				if l.Type == "socket" || l.Type == "none" {
					unsupported = true
				}
				if l.Address != "" {
					listen = l.Address
				}
			}
			if unsupported {
				vm.ConnectionNote = "Display uses TLS, a Unix socket or has no listener; use its existing client configuration."
			} else {
				addVMEndpoint(&vm, g.Type, listen, g.Port)
			}
		}
	}
	for _, v := range d.Devices.Video {
		if v.Model.Type != "none" {
			vm.Graphics = append(vm.Graphics, v.Model.Type)
		}
	}
	for _, h := range d.Devices.Hostdev {
		if h.Type == "pci" {
			a := h.Source.Address
			parts := []string{a.Domain, a.Bus, a.Slot, a.Function}
			for i := range parts {
				parts[i] = strings.TrimPrefix(parts[i], "0x")
			}
			vm.Graphics = append(vm.Graphics, pciGraphics(parts[0]+":"+parts[1]+":"+parts[2]+"."+parts[3]))
		}
	}
	for _, d := range d.Devices.Disks {
		if d.Device == "disk" {
			vm.disks = append(vm.disks, vmDisk{d.Source.File, d.Driver.Type})
			vm.diskTargets = append(vm.diskTargets, d.Target.Dev)
		}
	}
	return vm, nil
}

func libvirtVMs(ctx context.Context) ([]virtualMachine, []string) {
	rows := []virtualMachine{}
	warnings := []string{}
	for _, uri := range []string{"qemu:///session", "qemu:///system"} {
		list, err := vmProbe(ctx, "virsh", "--readonly", "--connect", uri, "list", "--uuid")
		if err != nil {
			warnings = append(warnings, uri+" unavailable; check virsh access and the libvirt socket.")
			continue
		}
		ids := strings.Fields(list)
		if len(ids) > 128 {
			ids = ids[:128]
			warnings = append(warnings, uri+" inventory limited to 128 active domains.")
		}
		for _, id := range ids {
			if ctx.Err() != nil {
				warnings = append(warnings, uri+" scan timed out; inventory is incomplete.")
				break
			}
			if !vmUUID.MatchString(id) {
				continue
			}
			raw, err := vmProbe(ctx, "virsh", "--readonly", "--connect", uri, "dumpxml", id)
			if err != nil {
				warnings = append(warnings, "An active libvirt domain could not be read; it may have stopped.")
				continue
			}
			vm, err := parseDomain(raw, uri)
			if err != nil {
				warnings = append(warnings, "A libvirt domain returned unsupported metadata.")
				continue
			}
			stats, err := vmProbe(ctx, "virsh", "--readonly", "--connect", uri, "domstats", id, "--state", "--cpu-total", "--balloon", "--block", "--raw")
			if err == nil {
				applyDomainStats(&vm, stats)
			}
			rows = append(rows, vm)
		}
	}
	return rows, warnings
}

func applyDomainStats(vm *virtualMachine, raw string) {
	stats := map[string]uint64{}
	values := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			values[k] = strings.Trim(v, "\"'")
			n, err := strconv.ParseUint(v, 10, 64)
			if err == nil {
				stats[k] = n
			}
		}
	}
	if s, ok := stats["state.state"]; ok {
		switch s {
		case 1:
			vm.State = "running"
		case 2:
			vm.State = "blocked"
		case 3:
			vm.State = "paused"
		case 4:
			vm.State = "shutting down"
		case 5:
			vm.State = "shut off"
		case 6:
			vm.State = "crashed"
		default:
			vm.State = "unknown"
		}
	}
	if n, ok := stats["cpu.time"]; ok {
		vm.ticks = n / 10000000
		vm.start = 1
	}
	vm.RSSBytes = stats["balloon.rss"] * 1024
	// libvirt can supply capacity even when the service user cannot read image files.
	if len(vm.diskTargets) > 0 {
		var total uint64
		for _, target := range vm.diskTargets {
			found := false
			for i := uint64(0); i < stats["block.count"] && i < 256; i++ {
				prefix := fmt.Sprintf("block.%d.", i)
				if target != "" && values[prefix+"name"] == target {
					if capacity, ok := stats[prefix+"capacity"]; ok && capacity <= ^uint64(0)-total {
						total += capacity
						found = true
					}
					break
				}
			}
			if !found {
				return
			}
		}
		vm.DiskBytes = &total
	}
}

func mergeVMs(processes, domains []virtualMachine) []virtualMachine {
	used := map[int]bool{}
	for i := range domains {
		for j, p := range processes {
			if p.UUID != "" && p.UUID == domains[i].UUID && !used[j] {
				domains[i].PID = p.PID
				domains[i].cwd = p.cwd
				domains[i].RSSBytes = p.RSSBytes
				domains[i].ticks = p.ticks
				domains[i].start = p.start
				for _, c := range p.Connections {
					if c.Kind == "ssh" {
						domains[i].Connections = append(domains[i].Connections, c)
					}
				}
				used[j] = true
				break
			}
		}
	}
	for j, p := range processes {
		if !used[j] {
			domains = append(domains, p)
		}
	}
	sort.Slice(domains, func(i, j int) bool { return domains[i].Name < domains[j].Name })
	return domains
}

func diskCapacity(ctx context.Context, disks []vmDisk) *uint64 {
	if len(disks) == 0 {
		return nil
	}
	var total uint64
	for _, d := range disks {
		if !filepath.IsAbs(d.path) || (d.format != "" && d.format != "raw" && d.format != "qcow2") {
			return nil
		}
		info, err := os.Stat(d.path)
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		args := []string{"qemu-img", "info", "--force-share", "--output=json"}
		if d.format != "" {
			args = append(args, "-f", d.format)
		}
		args = append(args, d.path)
		out, err := vmProbe(ctx, args...)
		if err != nil {
			return nil
		}
		var size struct {
			Virtual uint64 `json:"virtual-size"`
		}
		if json.Unmarshal([]byte(out), &size) != nil || size.Virtual > ^uint64(0)-total {
			return nil
		}
		total += size.Virtual
	}
	return &total
}

func addVMCommands(vm *virtualMachine, username, host string) {
	if !vmUser.MatchString(username) || (!vmHost.MatchString(host) && net.ParseIP(host) == nil) {
		vm.ConnectionNote = "SSH destination unavailable; use your devkit host's SSH address."
		return
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	dest := "'" + username + "@" + host + "'"
	for i := range vm.Connections {
		c := &vm.Connections[i]
		target := c.host
		if strings.Contains(target, ":") {
			target = "[" + target + "]"
		}
		c.Tunnel = fmt.Sprintf("ssh -N -o ExitOnForwardFailure=yes -L '127.0.0.1:%d:%s:%d' %s", c.Port, target, c.Port, dest)
		if c.Kind == "ssh" {
			c.Command = fmt.Sprintf("printf 'Guest username: '; read -r guest_user; ssh -p %d -l \"$guest_user\" 127.0.0.1", c.Port)
		} else {
			c.Command = fmt.Sprintf("remote-viewer %s://127.0.0.1:%d", c.Kind, c.Port)
		}
	}
	if vm.Display == "headless" && strings.HasPrefix(vm.Source, "qemu:///") && vmUUID.MatchString(vm.UUID) {
		vm.Connections = append(vm.Connections, vmConnection{Kind: "console", Command: fmt.Sprintf("ssh -t %s virsh --connect %s console %s", dest, vm.Source, vm.UUID)})
	}
	if len(vm.Connections) == 0 && vm.ConnectionNote == "" {
		vm.ConnectionNote = "No supported connection discovered. Use the VM's existing SSH address or console; devkit does not change guest networking."
	}
}

func (v *vmInventory) sampleCPU(rows []virtualMachine, now time.Time) {
	next := map[string]vmCPU{}
	for i := range rows {
		vm := &rows[i]
		vm.CPUPercent = nil
		vm.CPUStatus = "unavailable"
		if vm.start == 0 {
			continue
		}
		vm.CPUStatus = "sampling"
		prev, ok := v.previous[vm.ID]
		if seconds := now.Sub(prev.at).Seconds(); ok && prev.start == vm.start && vm.ticks >= prev.ticks && seconds > 0 {
			cpu := round1(float64(vm.ticks-prev.ticks) / float64(clockTicks) / seconds * 100)
			vm.CPUPercent = &cpu
			vm.CPUStatus = "available"
		}
		next[vm.ID] = vmCPU{vm.ticks, vm.start, now}
	}
	v.previous = next
}

func (v *vmInventory) snapshot(ctx context.Context) vmSnapshot {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.at.IsZero() && time.Since(v.at) < 5*time.Second {
		return v.data
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	processes, warnings := vmControlProcesses()
	processWarnings := append([]string{}, warnings...)
	domains := []virtualMachine{}
	if _, err := exec.LookPath("virsh"); err == nil {
		var w []string
		domains, w = libvirtVMs(ctx)
		warnings = append(warnings, w...)
	}
	rows := mergeVMs(processes, domains)
	cfg, configErr := loadHostConfig()
	if configErr != nil {
		warnings = append(warnings, "Host configuration is invalid; check projects_dir, vm_projects and other host settings.")
		cfg = hostConfig{}
	}
	roots, rootWarnings := vmProjectRoots(ctx, cfg, rows)
	projects, problems := readVMProjects(roots)
	warnings = append(warnings, rootWarnings...)
	warnings = append(warnings, problems...)
	assignVMProjects(rows, projects)
	targets := managedVMs(ctx, rows, projects, processWarnings)
	_, directoryWarnings := projectDirectories(cfg)
	if len(problems) > 0 || configErr != nil || len(directoryWarnings) > 0 {
		for i := range targets {
			targets[i].vm.Actions = nil
			targets[i].vm.ControlNote = "VM recipe discovery is incomplete; fix configuration before controlling VMs."
		}
	}
	rows = appendManagedVMs(rows, targets)
	host := cfg.TailscaleIP
	if host == "" {
		if ip, err := vmProbe(ctx, "tailscale", "ip", "-4"); err == nil && net.ParseIP(strings.TrimSpace(ip)) != nil {
			host = strings.TrimSpace(ip)
		}
	}
	if host == "" {
		host, _ = os.Hostname()
	}
	username := ""
	if u, err := user.Current(); err == nil {
		username = u.Username
	}
	for i := range rows {
		if rows[i].DiskBytes == nil {
			rows[i].DiskBytes = diskCapacity(ctx, rows[i].disks)
		}
		if rows[i].State != "stopped" && rows[i].State != "unavailable" {
			addVMCommands(&rows[i], username, host)
		}
	}
	v.sampleCPU(rows, time.Now())
	v.at = time.Now()
	v.data = vmSnapshot{rows, warnings}
	return v.data
}

func listVMs(asJSON bool) error {
	data := newVMInventory().snapshot(context.Background())
	if asJSON {
		return json.NewEncoder(stdout).Encode(data)
	}
	for _, w := range data.Warnings {
		fmt.Fprintln(stdout, "warning:", w)
	}
	if len(data.VMs) == 0 {
		fmt.Fprintln(stdout, "No QEMU or libvirt virtual machines found.")
	}
	lastProject := ""
	for i, vm := range data.VMs {
		if i == 0 || vm.Project != lastProject {
			fmt.Fprintln(stdout, orDefault(vm.Project, "Other VMs"))
			lastProject = vm.Project
		}
		if vm.ControlID != "" {
			fmt.Fprintln(stdout, "  control:", vm.ControlID)
		}
		if vm.ControlNote != "" {
			fmt.Fprintln(stdout, "  "+vm.ControlNote)
		}
		if vm.ProjectNote != "" {
			fmt.Fprintln(stdout, "  "+vm.ProjectNote)
		}
		fmt.Fprintf(stdout, "%s  %s  %s  %s\n", vm.Name, vm.State, vm.Source, vm.Display)
		value := func(n uint64) string {
			if n == 0 {
				return "unavailable"
			}
			return strconv.FormatUint(n, 10)
		}
		fmt.Fprintf(stdout, "  vCPUs: %s  RAM bytes: %s  RSS bytes: %s\n", value(uint64(vm.VCPUs)), value(vm.MemoryBytes), value(vm.RSSBytes))
		if vm.DiskBytes != nil {
			fmt.Fprintf(stdout, "  disk capacity bytes: %d\n", *vm.DiskBytes)
		}
		if len(vm.Graphics) > 0 {
			fmt.Fprintln(stdout, "  graphics:", strings.Join(vm.Graphics, ", "))
		}
		for _, c := range vm.Connections {
			if c.Tunnel != "" {
				fmt.Fprintln(stdout, "  tunnel:", c.Tunnel)
			}
			fmt.Fprintln(stdout, "  connect:", c.Command)
		}
		if vm.ConnectionNote != "" {
			fmt.Fprintln(stdout, "  "+vm.ConnectionNote)
		}
	}
	return nil
}
