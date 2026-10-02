package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

// Media ports are an explicit exception to the no-published-ports project contract.
// Projects declare services/protocols; only devbox chooses the host IP and ports.
type mediaPort struct {
	Name     string `json:"name"`
	Service  string `json:"service"`
	Protocol string `json:"protocol"`
	Target   int    `json:"target,omitempty"` // zero means the allocated host port
}

const mediaPortFirst, mediaPortLast = 20000, 29999

var mediaPortName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
var tailscaleRange = netip.MustParsePrefix("100.64.0.0/10")

func validateMediaPorts(ports []mediaPort) error {
	if len(ports) > 16 {
		return fail("devbox.json supports at most 16 media_ports per preview.")
	}
	names, targets := map[string]bool{}, map[string]bool{}
	for _, p := range ports {
		if !mediaPortName.MatchString(p.Name) || names[p.Name] {
			return fail("media_ports names must be unique lowercase letters, digits and underscores (1–32 characters, starting with a letter).")
		}
		names[p.Name] = true
		if !serviceName.MatchString(p.Service) || (p.Protocol != "udp" && p.Protocol != "tcp") {
			return fail("media_ports %s needs a Compose service and protocol udp or tcp.", p.Name)
		}
		if p.Target < 0 || p.Target > 65535 {
			return fail("media_ports %s target must be a port number, or omitted to use the allocated port.", p.Name)
		}
		key := fmt.Sprintf("%s/%s/%d", p.Service, p.Protocol, p.Target)
		if p.Target != 0 && targets[key] {
			return fail("media_ports repeats target %s.", key)
		}
		targets[key] = true
	}
	return nil
}

func (p mediaPort) envKey() string { return "DEVBOX_MEDIA_" + strings.ToUpper(p.Name) + "_PORT" }

// mediaHostIP only accepts the local Tailscale IPv4, never a public/wildcard bind.
var mediaHostIP = func() (string, error) {
	out, err := capture("tailscale", "ip", "-4")
	if err != nil {
		return "", fail("media_ports requires a running local Tailscale connection: %v", err)
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(out))
	if err != nil || !tailscaleRange.Contains(ip) {
		return "", fail("tailscale ip -4 did not return a Tailscale IPv4 address.")
	}
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}
	for _, address := range addresses {
		if prefix, err := netip.ParsePrefix(address.String()); err == nil && prefix.Addr() == ip {
			return ip.String(), nil
		}
	}
	return "", fail("Tailscale IPv4 %s is not assigned to a local interface.", ip)
}

// Probe both protocols so each allocated number belongs to just one preview/mapping.
// Docker still validates the final bind if an unrelated host process races creation.
var mediaPortAvailable = func(ip string, port int) bool {
	address := net.JoinHostPort(ip, strconv.Itoa(port))
	tcp, err := net.Listen("tcp4", address)
	if err != nil {
		return false
	}
	defer tcp.Close()
	udp, err := net.ListenPacket("udp4", address)
	if err != nil {
		return false
	}
	udp.Close()
	return true
}

// Include unmanaged/stopped Docker containers: Docker can publish with iptables
// without a listening host socket, and stopped previews must retain their ports.
var mediaPublishedPorts = func(ip string) (map[int]bool, error) {
	ids, err := capture("docker", "ps", "-aq")
	if err != nil {
		return nil, err
	}
	containers, err := inspect(strings.Fields(ids)...)
	if err != nil {
		return nil, err
	}
	used := map[int]bool{}
	for _, c := range containers {
		for _, bindings := range c.HostConfig.PortBindings {
			for _, b := range bindings {
				if b.HostIP == ip || b.HostIP == "" || b.HostIP == "0.0.0.0" || b.HostIP == "::" {
					if port, err := strconv.Atoi(b.HostPort); err == nil {
						used[port] = true
					}
				}
			}
		}
	}
	return used, nil
}

type mediaLease struct {
	Compose string         `json:"compose"`
	IP      string         `json:"ip"`
	Ports   map[string]int `json:"ports"`
}

func mediaLeaseDir() string { return filepath.Join(stateDir(), "media-ports") }
func mediaLeasePath(compose string) string {
	return filepath.Join(mediaLeaseDir(), digest(resolve(compose))+".json")
}

// A host-wide flock protects allocation across separate devbox processes/repos.
// It is held only while rendering/saving configuration, never while building images.
func lockMediaPorts() (func(), error) {
	if err := os.MkdirAll(mediaLeaseDir(), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(stateDir(), "media-ports.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

func loadMediaLeases() ([]mediaLease, error) {
	paths, err := filepath.Glob(filepath.Join(mediaLeaseDir(), "*.json"))
	if err != nil {
		return nil, err
	}
	var leases []mediaLease
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var lease mediaLease
		if err := json.Unmarshal(data, &lease); err != nil || lease.Compose == "" {
			return nil, fail("Invalid media port lease %s; inspect it before retrying.", path)
		}
		if _, err := os.Stat(lease.Compose); errors.Is(err, fs.ErrNotExist) {
			// Failed/crashed creation or an explicitly removed preview. No live
			// allocator can be writing its compose file while this lock is held.
			if err := os.Remove(path); err != nil {
				return nil, err
			}
			continue
		} else if err != nil {
			return nil, err
		}
		leases = append(leases, lease)
	}
	return leases, nil
}

func allocateMediaPorts(ip string, ports []mediaPort, used map[int]bool) (map[string]int, error) {
	allocated := map[string]int{}
	next := mediaPortFirst
	for _, p := range ports {
		for next <= mediaPortLast && (used[next] || !mediaPortAvailable(ip, next)) {
			next++
		}
		if next > mediaPortLast {
			return nil, fail("No free Tailscale media ports in %d–%d.", mediaPortFirst, mediaPortLast)
		}
		allocated[p.Name], used[next] = next, true
		next++
	}
	return allocated, nil
}

func writeMediaJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".media-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(append(data, '\n'))
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

// savePreviewCompose supplies allocation variables only for Compose rendering.
// The saved Compose file is authoritative for restarts and never reallocates ports.
func savePreviewCompose(root string, cfg projectConfig, mode string, env []string, metadata map[string]string, path string) (map[string]any, error) {
	if len(cfg.MediaPorts) == 0 {
		resolved, err := render(root, cfg, mode, env, metadata)
		if err != nil {
			return nil, err
		}
		return resolved, writeCompose(path, resolved)
	}
	if err := validateMediaPorts(cfg.MediaPorts); err != nil {
		return nil, err
	}
	ip, err := mediaHostIP()
	if err != nil {
		return nil, err
	}
	unlock, err := lockMediaPorts()
	if err != nil {
		return nil, err
	}
	defer unlock()
	leases, err := loadMediaLeases()
	if err != nil {
		return nil, err
	}
	used, err := mediaPublishedPorts(ip)
	if err != nil {
		return nil, err
	}
	for _, lease := range leases {
		// Reserve across IP changes as well, keeping allocations simple and stable.
		for _, port := range lease.Ports {
			used[port] = true
		}
	}
	allocated, err := allocateMediaPorts(ip, cfg.MediaPorts, used)
	if err != nil {
		return nil, err
	}
	variables := map[string]string{"DEVBOX_MEDIA_IP": ip}
	for _, p := range cfg.MediaPorts {
		variables[p.envKey()] = strconv.Itoa(allocated[p.Name])
	}
	resolved, err := render(root, cfg, mode, withEnv(env, variables), metadata)
	if err != nil {
		return nil, err
	}
	leasePath := mediaLeasePath(path)
	if err := writeMediaJSON(leasePath, mediaLease{resolve(path), ip, allocated}); err != nil {
		return nil, err
	}
	if err := writeMediaJSON(path, resolved); err != nil {
		os.Remove(leasePath)
		return nil, err
	}
	return resolved, nil
}

func releaseMediaPorts(path string) error {
	lease := mediaLeasePath(path)
	if !isFile(lease) {
		return nil
	}
	unlock, err := lockMediaPorts()
	if err != nil {
		return err
	}
	defer unlock()
	if err := os.Remove(lease); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func exposeMedia(services map[string]any, ports []mediaPort, env []string) error {
	if len(ports) == 0 {
		return nil
	}
	if err := validateMediaPorts(ports); err != nil {
		return err
	}
	ip, err := netip.ParseAddr(envValue(env, "DEVBOX_MEDIA_IP"))
	if err != nil || !tailscaleRange.Contains(ip) {
		return fail("Media ports require the allocated DEVBOX_MEDIA_IP Tailscale address.")
	}
	targets := map[string]bool{}
	for _, p := range ports {
		service := object(services, p.Service)
		if service == nil {
			return fail("Compose file has no %q service for media port %s.", p.Service, p.Name)
		}
		port, err := strconv.Atoi(envValue(env, p.envKey()))
		if err != nil || port < mediaPortFirst || port > mediaPortLast {
			return fail("Missing allocated media port %s.", p.envKey())
		}
		target := p.Target
		if target == 0 {
			target = port
		}
		key := fmt.Sprintf("%s/%s/%d", p.Service, p.Protocol, target)
		if targets[key] {
			return fail("Media port %s repeats container listener %s; use a different target.", p.Name, key)
		}
		targets[key] = true
		bindings, _ := service["ports"].([]any)
		service["ports"] = append(bindings, map[string]any{
			"host_ip": ip.String(), "target": target, "published": strconv.Itoa(port), "protocol": p.Protocol,
		})
		labels := ensureObject(service, "labels")
		labels["devbox.media."+p.Name] = net.JoinHostPort(ip.String(), strconv.Itoa(port)) + "/" + p.Protocol
	}
	return nil
}
