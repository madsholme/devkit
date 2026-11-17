package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

var previewLookup = func(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}
var previewDial = func(ctx context.Context, network, address string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, network, address)
}
var previewRootCAs = func() *x509.CertPool { return nil }
var previewAllowedIPs = privatePreviewListeners

func privatePreviewListeners(ctx context.Context) ([]netip.Addr, error) {
	cfg, err := loadHostConfig()
	if err != nil || cfg.Proxy == nil {
		return nil, fail("Private proxy configuration unavailable.")
	}
	ts := cfg.TailscaleIP
	if ts == "" {
		ts, err = observationCommand(ctx, "tailscale", "ip", "-4")
		if err != nil {
			return nil, fail("Tailscale address unavailable.")
		}
		ts, _, _ = strings.Cut(strings.TrimSpace(ts), "\n")
	}
	tsIP, err := netip.ParseAddr(strings.TrimSpace(ts))
	if err != nil || !tailscaleRange.Contains(tsIP) {
		return nil, fail("Private Tailscale address unavailable.")
	}
	local := map[netip.Addr]bool{}
	for _, s := range hostAddresses() {
		if ip, e := netip.ParseAddr(s); e == nil {
			local[ip.Unmap()] = true
		}
	}
	if !local[tsIP] {
		return nil, fail("Configured Tailscale address is not local.")
	}
	allowed := map[netip.Addr]bool{}
	for _, s := range listenAddresses(cfg.Proxy, ts) {
		ip, err := netip.ParseAddr(s)
		if err != nil || ip.Zone() != "" {
			return nil, fail("Private listener evidence is invalid.")
		}
		ip = ip.Unmap()
		if ip.IsUnspecified() {
			for candidate := range local {
				if (ip.Is6() || candidate.Is4()) && (candidate.IsPrivate() || candidate == tsIP) && !candidate.IsLinkLocalUnicast() {
					allowed[candidate] = true
				}
			}
			continue
		}
		if !local[ip] || !(ip.IsPrivate() || ip.IsLoopback() || ip == tsIP) {
			return nil, fail("Listener is unavailable or is not a verified private local address.")
		}
		allowed[ip] = true
	}
	result := []netip.Addr{}
	for ip := range allowed {
		result = append(result, ip)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Less(result[j]) })
	if len(result) == 0 {
		return nil, fail("No verified private listeners.")
	}
	return result, nil
}

type previewEndpoint struct {
	Host string
	IPs  []netip.Addr
}
type endpointAttempt struct {
	IP         string  `json:"ip"`
	TCP        string  `json:"tcp"`
	TLS        string  `json:"tls"`
	HTTP       string  `json:"http"`
	HTTPStatus *int    `json:"http_status"`
	Elapsed    float64 `json:"elapsed_seconds"`
}
type endpointObservation struct {
	Attempts   []endpointAttempt `json:"attempts"`
	Peer       string            `json:"peer,omitempty"`
	HTTPStatus *int              `json:"http_status"`
}

func probePreviewEndpoint(ctx context.Context, endpoint previewEndpoint) endpointObservation {
	result := endpointObservation{Attempts: []endpointAttempt{}}
	for i, ip := range endpoint.IPs {
		started := time.Now()
		a := endpointAttempt{IP: ip.String(), TCP: "skip", TLS: "skip", HTTP: "skip"}
		budget := 15 * time.Second
		if deadline, ok := ctx.Deadline(); ok {
			budget = time.Until(deadline) / time.Duration(len(endpoint.IPs)-i)
		}
		attemptCtx, cancel := context.WithTimeout(ctx, budget)
		func() {
			conn, err := previewDial(attemptCtx, "tcp", net.JoinHostPort(ip.String(), "443"))
			if err != nil {
				a.TCP = "fail"
				return
			}
			defer conn.Close()
			a.TCP = "ok"
			secure := tls.Client(conn, &tls.Config{ServerName: endpoint.Host, RootCAs: previewRootCAs(), MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}})
			if err := secure.HandshakeContext(attemptCtx); err != nil {
				a.TLS = "fail"
				return
			}
			a.TLS = "ok"
			transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, MaxResponseHeaderBytes: 64 << 10, DialTLSContext: func(context.Context, string, string) (net.Conn, error) { return secure, nil }}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			req, err := http.NewRequestWithContext(attemptCtx, "GET", "https://"+endpoint.Host+"/", nil)
			if err != nil {
				a.HTTP = "fail"
				return
			}
			resp, err := client.Do(req)
			if err != nil {
				a.HTTP = "fail"
				return
			}
			resp.Body.Close()
			a.HTTP = "ok"
			status := resp.StatusCode
			a.HTTPStatus = &status
			if result.HTTPStatus == nil {
				result.Peer = ip.String()
				result.HTTPStatus = &status
			}
		}()
		cancel()
		a.Elapsed = time.Since(started).Seconds()
		result.Attempts = append(result.Attempts, a)
	}
	return result
}
func checkCLI(args []string) (int, error) {
	p, v, err := splitArgs(args, map[string]bool{"--timeout": true}, map[string]bool{"--json": true})
	if err != nil || len(p) > 1 {
		return 2, usageError("Use check [PREVIEW] [--timeout DURATION] [--json]")
	}
	name := ""
	if len(p) == 1 {
		name = p[0]
		if !previewID.MatchString(name) {
			return 2, fail("Use an exact preview ID.")
		}
	}
	timeout := 15 * time.Second
	if s, ok := v["--timeout"]; ok {
		timeout, err = time.ParseDuration(s)
		if err != nil || timeout < time.Second || timeout > 60*time.Second {
			return 2, fail("Timeout must be from 1s to 60s.")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, asJSON := v["--json"]
	target, err := resolveObservedPreview(ctx, name)
	if err != nil {
		r := newReport("check", map[string]any{"vantage": "host", "scope": "primary-route", "application_verified": false})
		r.unavailable("preview.target", "preview", "Owned preview identity is unavailable or ambiguous.")
		return r.write(asJSON)
	}
	return checkPreview(ctx, target).write(asJSON)
}
func checkedPreviewEndpoint(target observedPreviewTarget, web container) (previewEndpoint, error) {
	u, err := url.Parse(target.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" || (u.Port() != "" && u.Port() != "443") || u.Hostname() == "" || net.ParseIP(u.Hostname()) != nil {
		return previewEndpoint{}, fail("Primary HTTPS origin is invalid.")
	}
	host := u.Hostname()
	if !validDomain(host, 253) || strings.ToLower(host) != host {
		return previewEndpoint{}, fail("Primary hostname is invalid.")
	}
	l := web.Config.Labels
	p := "traefik.http.routers." + target.ID + "."
	for _, c := range target.Containers {
		if c.ID != web.ID && c.Config.Labels[p+"rule"] != "" {
			return previewEndpoint{}, fail("Primary router is claimed by multiple services.")
		}
	}
	if l["devkit.url"] != target.URL || l["traefik.enable"] != "true" || l["traefik.docker.network"] != "devkit-proxy" || l[p+"rule"] != fmt.Sprintf("Host(`%s`)", host) || l[p+"tls"] != "true" || l[p+"service"] != target.ID {
		return previewEndpoint{}, fail("Observed primary router does not match the selected preview.")
	}
	port, err := strconv.Atoi(l["traefik.http.services."+target.ID+".loadbalancer.server.port"])
	if err != nil || port < 1 || port > 65535 {
		return previewEndpoint{}, fail("Observed backend port is unavailable.")
	}
	// Additional routers are intentionally outside the checked primary route.
	return previewEndpoint{Host: host}, nil
}
func checkPreview(ctx context.Context, target observedPreviewTarget) operationReport {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
	}
	services := []previewContainerView{}
	data := map[string]any{"vantage": "host", "scope": "primary-route", "application_verified": false, "preview": target.ID, "mode": target.Mode, "services": services, "additional_routes_checked": false}
	r := newReport("check", data)
	networkSkipped := func() {
		for _, code := range []string{"preview.dns", "preview.tcp", "preview.tls", "preview.http"} {
			r.add(code, "skip", target.ID, "Not probed because prerequisite evidence is unavailable.")
		}
	}
	if op, err := loadRebuildOperation(target.ID); err != nil {
		r.unavailable("preview.recovery", target.ID, "Recovery metadata is unreadable.")
	} else if op != nil && op.Phase != "complete" {
		r.add("preview.recovery", "warn", target.ID, "An unfinished rebuild requires explicit recovery; diagnostics do not change it.")
	}
	web, err := selectObservedService(target, "")
	if err != nil {
		r.unavailable("preview.target", target.ID, "Primary web service is ambiguous or unavailable.")
		networkSkipped()
		return r
	}
	for _, c := range target.Containers {
		services = append(services, projectContainerView(c))
		status := containerStatus(c.State)
		kind := "ok"
		msg := "Service is running."
		if !c.State.Running || c.State.Status == "paused" || c.State.Status == "restarting" {
			kind = "warn"
			msg = "Service is not continuously running; intended one-shot behavior is unknown."
			if c.ID == web.ID {
				kind = "fail"
				msg = "Primary web service is not continuously running."
			}
		} else if status == "unhealthy" {
			kind = "fail"
			msg = "Configured service health check is unhealthy."
		} else if c.State.Health == nil || status != "healthy" {
			kind = "warn"
			msg = "Service is running; application health is unknown."
		}
		r.add("preview.service", kind, c.Config.Labels["com.docker.compose.service"], msg)
	}
	data["services"] = services
	endpoint, err := checkedPreviewEndpoint(target, web)
	if err != nil {
		r.unavailable("preview.route", target.ID, "Observed primary route is unsupported or does not match the preview.")
		networkSkipped()
		return r
	}
	data["url"] = "https://" + endpoint.Host + "/"
	r.add("preview.route", "ok", target.ID, "Observed primary router matches the web service.")
	if !web.State.Running || web.State.Status == "paused" {
		networkSkipped()
		return r
	}
	allowed, err := previewAllowedIPs(ctx)
	if err != nil {
		r.unavailable("preview.listeners", target.ID, "Verified private host listener addresses are unavailable.")
		networkSkipped()
		return r
	}
	answers, err := previewLookup(ctx, endpoint.Host)
	if err != nil || len(answers) == 0 || len(answers) > 8 {
		r.unavailable("preview.dns", target.ID, "DNS is unavailable or exceeds the eight-address limit.")
		for _, code := range []string{"preview.tcp", "preview.tls", "preview.http"} {
			r.add(code, "skip", target.ID, "DNS evidence is unavailable.")
		}
		return r
	}
	approved := map[netip.Addr]bool{}
	for _, ip := range allowed {
		approved[ip.Unmap()] = true
	}
	seen := map[netip.Addr]bool{}
	for _, ip := range answers {
		ip = ip.Unmap()
		if !ip.IsValid() || ip.Zone() != "" || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || !approved[ip] {
			r.unavailable("preview.dns", target.ID, "DNS resolves outside verified private host listeners.")
			for _, code := range []string{"preview.tcp", "preview.tls", "preview.http"} {
				r.add(code, "skip", target.ID, "Unsafe DNS destination was not probed.")
			}
			return r
		}
		seen[ip] = true
	}
	for ip := range seen {
		endpoint.IPs = append(endpoint.IPs, ip)
	}
	sort.Slice(endpoint.IPs, func(i, j int) bool { return endpoint.IPs[i].Less(endpoint.IPs[j]) })
	r.add("preview.dns", "ok", target.ID, "DNS destinations match verified private host listeners.")
	observation := probePreviewEndpoint(ctx, endpoint)
	data["endpoint"] = observation
	success := 0
	for _, a := range observation.Attempts {
		for _, stage := range []struct{ name, status string }{{"tcp", a.TCP}, {"tls", a.TLS}, {"http", a.HTTP}} {
			kind := stage.status
			if kind == "fail" && observation.HTTPStatus != nil {
				kind = "warn"
			}
			r.add("preview."+stage.name, kind, a.IP, "Host "+stage.name+" probe: "+stage.status+".")
		}
		if a.HTTPStatus != nil {
			success++
			status := *a.HTTPStatus
			kind, msg := "ok", "Primary endpoint returned a successful HTTP response."
			if status >= 300 && status < 400 || status == 401 || status == 403 {
				kind, msg = "warn", "Endpoint responded; redirect or authentication flow was not checked."
			} else if status < 200 || status >= 400 {
				kind, msg = "fail", "Primary endpoint returned an unsuccessful HTTP response."
			}
			r.add("preview.http_status", kind, a.IP, msg)
		}
	}
	if success == 0 {
		r.Complete = false
	}
	if ctx.Err() != nil {
		r.unavailable("observation.timeout", target.ID, "Connectivity deadline exceeded.")
	}
	return r
}
