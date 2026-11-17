package main

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func checkFixture(t *testing.T, status int) (observedPreviewTarget, *atomic.Int32) {
	t.Helper()
	c := observedFixture(t)
	c.State.Running = true
	c.State.Status = "running"
	c.Config.Labels["devkit.url"] = "https://example.com"
	prefix := "traefik.http.routers.demo-main."
	c.Config.Labels["traefik.enable"] = "true"
	c.Config.Labels["traefik.docker.network"] = "devkit-proxy"
	c.Config.Labels[prefix+"rule"] = "Host(`example.com`)"
	c.Config.Labels[prefix+"tls"] = "true"
	c.Config.Labels[prefix+"service"] = "demo-main"
	c.Config.Labels["traefik.http.services.demo-main.loadbalancer.server.port"] = "8000"
	requests := &atomic.Int32{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Host != "example.com" || r.TLS.ServerName != "example.com" || r.URL.Path != "/" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("unexpected request authority, path or credentials")
		}
		w.Header().Set("Location", "https://redirect.invalid/SECRET-SENTINEL")
		w.Header().Set("X-Secret", "SECRET-SENTINEL")
		w.WriteHeader(status)
		w.Write([]byte("SECRET-SENTINEL"))
	}))
	t.Cleanup(server.Close)
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	swap(t, &previewRootCAs, func() *x509.CertPool { return pool })
	swap(t, &previewDial, func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "192.168.1.20:443" {
			t.Fatalf("wrong dial %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	})
	swap(t, &previewLookup, func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("192.168.1.20")}, nil
	})
	swap(t, &previewAllowedIPs, func(context.Context) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("192.168.1.20")}, nil
	})
	swap(t, &observeContainers, func(context.Context) ([]container, error) { return []container{c}, nil })
	return observedPreviewTarget{ID: "demo-main", CommonDir: "/repo/.git", Mode: "live", URL: c.Config.Labels["devkit.url"], Containers: []container{c}}, requests
}
func TestPreviewCheckPrimaryRouteAndHealth(t *testing.T) {
	target, requests := checkFixture(t, 200)
	r := checkPreview(context.Background(), target)
	b, _ := json.Marshal(r)
	if r.exitCode() != 0 || !r.Complete || requests.Load() != 1 || !strings.Contains(string(b), `"vantage":"host"`) || strings.Contains(string(b), "SENTINEL") {
		t.Fatal(string(b), requests.Load())
	}
	for _, state := range []string{"exited", "paused", "restarting"} {
		target.Containers[0].State.Status = state
		target.Containers[0].State.Running = state != "exited"
		r = checkPreview(context.Background(), target)
		if r.exitCode() != 1 {
			t.Fatal(state, r)
		}
	}
}
func TestPreviewCheckSavedObservedDrift(t *testing.T) {
	target, requests := checkFixture(t, 200)
	target.Containers[0].Config.Labels["traefik.http.routers.demo-main.rule"] = "Host(`other.test`)"
	r := checkPreview(context.Background(), target)
	if r.exitCode() != 1 || requests.Load() != 0 {
		t.Fatal(r, requests.Load())
	}
}
func TestPreviewCheckDNSPinningAndPrivatePeers(t *testing.T) {
	for _, ip := range []string{"8.8.8.8", "192.168.1.99", "169.254.169.254", "127.0.0.1", "0.0.0.0"} {
		t.Run(ip, func(t *testing.T) {
			target, requests := checkFixture(t, 200)
			swap(t, &previewLookup, func(context.Context, string) ([]netip.Addr, error) { return []netip.Addr{netip.MustParseAddr(ip)}, nil })
			r := checkPreview(context.Background(), target)
			if r.exitCode() != 1 || requests.Load() != 0 {
				t.Fatal(r, requests.Load())
			}
		})
	}
	t.Run("pin", func(t *testing.T) {
		target, requests := checkFixture(t, 200)
		lookups := 0
		swap(t, &previewLookup, func(context.Context, string) ([]netip.Addr, error) {
			lookups++
			if lookups > 1 {
				t.Fatal("second DNS lookup")
			}
			return []netip.Addr{netip.MustParseAddr("192.168.1.20")}, nil
		})
		t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
		r := checkPreview(context.Background(), target)
		if r.exitCode() != 0 || requests.Load() != 1 {
			t.Fatal(r)
		}
	})
}
func TestPreviewCheckTLSVerification(t *testing.T) {
	for _, wrongHost := range []bool{false, true} {
		t.Run(map[bool]string{true: "wrong-host", false: "untrusted"}[wrongHost], func(t *testing.T) {
			target, requests := checkFixture(t, 200)
			if wrongHost {
				target.URL = "https://wrong.example"
				target.Containers[0].Config.Labels["devkit.url"] = target.URL
				target.Containers[0].Config.Labels["traefik.http.routers.demo-main.rule"] = "Host(`wrong.example`)"
			} else {
				swap(t, &previewRootCAs, func() *x509.CertPool { return x509.NewCertPool() })
			}
			r := checkPreview(context.Background(), target)
			if r.exitCode() != 1 || requests.Load() != 0 {
				t.Fatal(r, requests.Load())
			}
		})
	}
}
func TestPreviewCheckHTTPClassification(t *testing.T) {
	for _, code := range []int{200, 204, 302, 401, 403, 404, 500} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			target, requests := checkFixture(t, code)
			r := checkPreview(context.Background(), target)
			want := 0
			if code >= 400 && code != 401 && code != 403 {
				want = 1
			}
			if r.exitCode() != want || requests.Load() != 1 {
				t.Fatal(r, requests.Load())
			}
		})
	}
}
func TestPreviewCheckTotalDeadline(t *testing.T) {
	target, _ := checkFixture(t, 200)
	swap(t, &previewDial, func(ctx context.Context, _, _ string) (net.Conn, error) { <-ctx.Done(); return nil, ctx.Err() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	r := checkPreview(ctx, target)
	if r.Complete || r.exitCode() != 1 {
		t.Fatal(r)
	}
	for _, duration := range []string{"0s", "61s", "bogus"} {
		code, _ := checkCLI([]string{"--timeout", duration})
		if code != 2 {
			t.Fatal(duration, code)
		}
	}
}
func TestPreviewCheckRecoveryReadOnly(t *testing.T) {
	freshHistory(t)
	target, _ := checkFixture(t, 200)
	if err := saveRebuildOperation(rebuildOperation{Preview: target.ID, Phase: "failed"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(rebuildDir(target.ID), "operation.json")
	before, _ := os.ReadFile(path)
	r := checkPreview(context.Background(), target)
	after, _ := os.ReadFile(path)
	b, _ := json.Marshal(r)
	if string(before) != string(after) || !strings.Contains(string(b), "preview.recovery") {
		t.Fatal(string(b))
	}
}

func TestPreviewCheckConfiguredPrivateListeners(t *testing.T) {
	t.Setenv("DEVKIT_CONFIG_DIR", t.TempDir())
	if err := writeConfigFile(map[string]any{"tailscale_ip": "100.64.0.7", "proxy": map[string]any{"listen": []string{"0.0.0.0"}}}); err != nil {
		t.Fatal(err)
	}
	swap(t, &hostAddresses, func() []string { return []string{"100.64.0.7", "192.168.1.20", "127.0.0.1", "8.8.8.8", "169.254.1.1"} })
	swap(t, &observationCommand, func(context.Context, ...string) (string, error) {
		t.Fatal("configured Tailscale IP should not need a subprocess")
		return "", nil
	})
	ips, err := privatePreviewListeners(context.Background())
	if err != nil || len(ips) != 2 {
		t.Fatal(ips, err)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			t.Fatal(ip)
		}
	}
}
func TestPreviewCheckAmbiguousRouter(t *testing.T) {
	target, requests := checkFixture(t, 200)
	other := labelled(map[string]string{"com.docker.compose.service": "other", "traefik.http.routers.demo-main.rule": "Host(`example.com`)"})
	other.ID = strings.Repeat("b", 64)
	other.State.Running = true
	target.Containers = append(target.Containers, other)
	r := checkPreview(context.Background(), target)
	if r.exitCode() != 1 || requests.Load() != 0 {
		t.Fatal(r, requests.Load())
	}
}
func TestPreviewCheckAddressLimitAndPartialPeers(t *testing.T) {
	t.Run("limit", func(t *testing.T) {
		target, requests := checkFixture(t, 200)
		swap(t, &previewLookup, func(context.Context, string) ([]netip.Addr, error) {
			ips := make([]netip.Addr, 9)
			for i := range ips {
				ips[i] = netip.MustParseAddr("192.168.1.20")
			}
			return ips, nil
		})
		r := checkPreview(context.Background(), target)
		if r.exitCode() != 1 || requests.Load() != 0 {
			t.Fatal(r)
		}
	})
	t.Run("partial", func(t *testing.T) {
		target, _ := checkFixture(t, 200)
		ips := []netip.Addr{netip.MustParseAddr("192.168.1.20"), netip.MustParseAddr("192.168.1.21")}
		swap(t, &previewAllowedIPs, func(context.Context) ([]netip.Addr, error) { return ips, nil })
		swap(t, &previewLookup, func(context.Context, string) ([]netip.Addr, error) { return ips, nil })
		dial := previewDial
		swap(t, &previewDial, func(ctx context.Context, n, a string) (net.Conn, error) {
			if strings.HasPrefix(a, "192.168.1.21:") {
				return nil, fail("SENTINEL")
			}
			return dial(ctx, n, a)
		})
		r := checkPreview(context.Background(), target)
		if r.exitCode() != 0 || !r.Complete {
			t.Fatal(r)
		}
		found := false
		for _, c := range r.Checks {
			if c.Code == "preview.tcp" && c.Status == "warn" {
				found = true
			}
		}
		if !found {
			t.Fatal(r)
		}
	})
}
