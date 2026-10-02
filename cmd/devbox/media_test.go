package main

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func mediaConfig() projectConfig {
	return projectConfig{Project: "media", MediaPorts: []mediaPort{
		{Name: "sfu_udp", Service: "web", Protocol: "udp"},
		{Name: "ice_tcp", Service: "web", Protocol: "tcp"},
	}}
}

func fakeMediaHost(t *testing.T) {
	t.Setenv("DEVBOX_STATE_DIR", t.TempDir())
	swap(t, &mediaHostIP, func() (string, error) { return "100.69.127.125", nil })
	swap(t, &mediaPublishedPorts, func(string) (map[int]bool, error) { return map[int]bool{}, nil })
	swap(t, &mediaPortAvailable, func(string, int) bool { return true })
}

func TestMediaPortConfigValidation(t *testing.T) {
	valid := mediaConfig().MediaPorts
	if err := validateMediaPorts(valid); err != nil {
		t.Fatal(err)
	}
	for _, ports := range [][]mediaPort{
		{{Name: "bad-name", Service: "web", Protocol: "udp"}},
		{{Name: "bad", Service: "web", Protocol: "sctp"}},
		{{Name: "bad", Service: "web", Protocol: "udp", Target: 65536}},
		{{Name: "bad", Service: "web", Protocol: "udp", Target: -1}},
		{valid[0], valid[0]},
		{{Name: "one", Service: "web", Protocol: "tcp", Target: 80}, {Name: "two", Service: "web", Protocol: "tcp", Target: 80}},
		make([]mediaPort, 17),
	} {
		if err := validateMediaPorts(ports); err == nil {
			t.Errorf("accepted %+v", ports)
		}
	}
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "devbox.json"), []byte(`{"project":"test","media_ports":[{"name":"bad","service":"web","protocol":"udp","target":"123"}]}`), 0o600)
	if _, err := loadConfig(root); err == nil {
		t.Error("accepted non-numeric target")
	}
}

func TestRenderMediaOnlyAddsDeclaredTailnetBindings(t *testing.T) {
	fakeImage(t, `{"services":{"web":{},"db":{}}}`)
	cfg := mediaConfig()
	cfg.MediaPorts[1].Target = 4443
	env := withEnv(composeEnv(), map[string]string{
		"DEVBOX_MEDIA_IP": "100.69.127.125", "DEVBOX_MEDIA_SFU_UDP_PORT": "20010", "DEVBOX_MEDIA_ICE_TCP_PORT": "20011",
	})
	resolved, err := render(kit, cfg, "live", env, nil)
	if err != nil {
		t.Fatal(err)
	}
	services := object(resolved, "services")
	ports := object(services, "web")["ports"].([]any)
	udp, tcp := asObject(ports[0]), asObject(ports[1])
	if udp["host_ip"] != "100.69.127.125" || udp["published"] != "20010" || udp["target"] != 20010 || udp["protocol"] != "udp" {
		t.Fatalf("wrong UDP mapping: %v", udp)
	}
	if tcp["published"] != "20011" || tcp["target"] != 4443 || tcp["protocol"] != "tcp" {
		t.Fatalf("wrong TCP mapping: %v", tcp)
	}
	if object(services, "db")["ports"] != nil {
		t.Fatal("published undeclared database ports")
	}
	clash := mediaConfig()
	clash.MediaPorts[1].Protocol = "udp"
	clash.MediaPorts[1].Target = 20010
	if _, err := render(kit, clash, "live", env, nil); err == nil {
		t.Error("accepted a fixed target that collides with an allocated target")
	}
	for _, ip := range []string{"0.0.0.0", "127.0.0.1", "192.168.1.21", "::", "203.0.113.1", ""} {
		if _, err := render(kit, cfg, "live", withEnv(env, map[string]string{"DEVBOX_MEDIA_IP": ip}), nil); err == nil {
			t.Errorf("accepted non-tailnet IP %q", ip)
		}
	}
	for _, source := range []string{
		`{"services":{"web":{"ports":[{"published":"20000","target":20000,"host_ip":"100.69.127.125"}]}}}`,
		`{"services":{"web":{"network_mode":"host"}}}`,
		`{"services":{"web":{"container_name":"fixed"}}}`,
	} {
		fakeImage(t, source)
		if _, err := render(kit, cfg, "live", env, nil); err == nil {
			t.Errorf("accepted project bypass %s", source)
		}
	}
}

func TestMediaAllocationSkipsReservedAndOccupiedPorts(t *testing.T) {
	swap(t, &mediaPortAvailable, func(_ string, p int) bool { return p != 20001 })
	ports, err := allocateMediaPorts("100.69.127.125", mediaConfig().MediaPorts, map[int]bool{20000: true, 20003: true})
	if err != nil || ports["sfu_udp"] != 20002 || ports["ice_tcp"] != 20004 {
		t.Fatalf("allocation %v, %v", ports, err)
	}
	swap(t, &mediaPortAvailable, func(string, int) bool { return false })
	if _, err := allocateMediaPorts("100.69.127.125", mediaConfig().MediaPorts, map[int]bool{}); err == nil {
		t.Error("accepted an exhausted port range")
	}
}

func TestMediaPortProbeDetectsTCPAndUDPListeners(t *testing.T) {
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := tcp.Addr().(*net.TCPAddr).Port
	if mediaPortAvailable("127.0.0.1", port) {
		t.Error("TCP listener was not detected")
	}
	tcp.Close()
	udp, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	if mediaPortAvailable("127.0.0.1", udp.LocalAddr().(*net.UDPAddr).Port) {
		t.Error("UDP listener was not detected")
	}
}

func TestMediaPortDockerBindingsIncludeUnmanagedAndStopped(t *testing.T) {
	swap(t, &runCommand, func(args []string, _ runOpts) (string, error) {
		if strings.Join(args, " ") == "docker ps -aq" {
			return "stopped-unmanaged", nil
		}
		if strings.Join(args, " ") == "docker inspect stopped-unmanaged" {
			return `[{"HostConfig":{"PortBindings":{"5000/udp":[{"HostIp":"100.69.127.125","HostPort":"20000"}],"5001/tcp":[{"HostIp":"0.0.0.0","HostPort":"20001"}],"5002/tcp":[{"HostIp":"127.0.0.1","HostPort":"20002"}]}}}]`, nil
		}
		t.Fatalf("unexpected command %v", args)
		return "", nil
	})
	used, err := mediaPublishedPorts("100.69.127.125")
	if err != nil || !used[20000] || !used[20001] || used[20002] {
		t.Fatalf("ports %v, error %v", used, err)
	}
}

func TestConcurrentMediaAllocationAndLeaseLifecycle(t *testing.T) {
	fakeMediaHost(t)
	swap(t, &render, func(_ string, cfg projectConfig, _ string, env []string, _ map[string]string) (map[string]any, error) {
		services := map[string]any{"web": map[string]any{}}
		if err := exposeMedia(services, cfg.MediaPorts, env); err != nil {
			return nil, err
		}
		return map[string]any{"services": services}, nil
	})
	const count = 8
	paths := make([]string, count)
	errs := make(chan error, count)
	var workers sync.WaitGroup
	for i := range paths {
		paths[i] = filepath.Join(t.TempDir(), "compose.json")
		workers.Add(1)
		go func(path string) {
			defer workers.Done()
			_, err := savePreviewCompose(kit, mediaConfig(), "live", composeEnv(), nil, path)
			errs <- err
		}(paths[i])
	}
	workers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	leases, err := loadMediaLeases()
	if err != nil || len(leases) != count {
		t.Fatalf("leases %d, err %v", len(leases), err)
	}
	seen := map[int]bool{}
	for _, lease := range leases {
		for _, p := range lease.Ports {
			if seen[p] {
				t.Fatalf("duplicate allocation %d", p)
			}
			seen[p] = true
		}
	}
	before, _ := os.ReadFile(paths[0])
	swap(t, &composeRun, func(string, ...string) error { return nil })
	if err := up(paths[0]); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(paths[0])
	if string(before) != string(after) {
		t.Error("restart changed saved port mappings")
	}
	if err := releaseMediaPorts(paths[0]); err != nil {
		t.Fatal(err)
	}
	if isFile(mediaLeasePath(paths[0])) {
		t.Error("destroy did not release allocation")
	}
	// A process killed before saving Compose leaves a stale lease, safely pruned
	// by the next allocator while no other allocator can be writing that file.
	os.Remove(paths[1])
	leases, err = loadMediaLeases()
	if err != nil || len(leases) != count-2 {
		t.Fatalf("stale leases not pruned: %d, %v", len(leases), err)
	}
}

func TestMediaRenderingFailureDoesNotReservePorts(t *testing.T) {
	fakeMediaHost(t)
	swap(t, &render, func(string, projectConfig, string, []string, map[string]string) (map[string]any, error) {
		return nil, errors.New("invalid compose")
	})
	path := filepath.Join(t.TempDir(), "compose.json")
	if _, err := savePreviewCompose(kit, mediaConfig(), "live", composeEnv(), nil, path); err == nil {
		t.Fatal("expected render failure")
	}
	leases, err := loadMediaLeases()
	if err != nil || len(leases) != 0 || isFile(path) {
		t.Fatalf("failed creation left state: %d, %v", len(leases), err)
	}
}

func TestRealComposeInterpolatesMediaEnvironment(t *testing.T) {
	requireCompose(t)
	fakeMediaHost(t)
	root := t.TempDir()
	compose := `services:
  web:
    image: python:3.13-slim
    environment:
      ICE_IP: ${DEVBOX_MEDIA_IP:?}
      UDP_PORT: ${DEVBOX_MEDIA_SFU_UDP_PORT:?}
      TCP_PORT: ${DEVBOX_MEDIA_ICE_TCP_PORT:?}
`
	os.WriteFile(filepath.Join(root, "compose.live.yaml"), []byte(compose), 0o600)
	path := filepath.Join(root, "saved.json")
	resolved, err := savePreviewCompose(root, mediaConfig(), "live", composeEnv(), nil, path)
	if err != nil {
		t.Fatal(err)
	}
	web := object(object(resolved, "services"), "web")
	environment := object(web, "environment")
	if environment["ICE_IP"] != "100.69.127.125" || environment["UDP_PORT"] != strconv.Itoa(mediaPortFirst) {
		t.Fatalf("wrong runtime environment: %v", environment)
	}
	data, _ := os.ReadFile(path)
	var saved map[string]any
	if json.Unmarshal(data, &saved) != nil || strings.Contains(string(data), "${DEVBOX_MEDIA") {
		t.Error("allocation was not saved as resolved Compose")
	}
}
