package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const vaultMaxBody = 2 << 20

var vaultSegment = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]{0,127}$`)

type vaultClient struct {
	address, token string
	http           *http.Client
}
type vaultStatus struct {
	State       string `json:"state"`
	Initialized bool   `json:"initialized"`
	Sealed      bool   `json:"sealed"`
	Version     string `json:"version,omitempty"`
	Progress    int    `json:"progress,omitempty"`
	Threshold   int    `json:"t,omitempty"`
}
type vaultError struct{ Code int }

var errVaultMountInitializing = errors.New("OpenBao KV engine is initializing")

func (e vaultError) Error() string {
	switch e.Code {
	case 403:
		return "OpenBao access denied"
	case 404:
		return "OpenBao value not found"
	case 503:
		return "OpenBao is sealed or unavailable"
	case 400:
		return "OpenBao rejected the request (check version or configuration)"
	}
	return fmt.Sprintf("OpenBao request failed (HTTP %d)", e.Code)
}
func newVaultClient(address, token string) (*vaultClient, error) {
	u, e := url.Parse(address)
	if e != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fail("Invalid OpenBao address")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "http" || ip == nil || !ip.IsLoopback() || u.Port() == "" {
		return nil, fail("Managed OpenBao must use an explicit loopback HTTP address/port")
	}
	return &vaultClient{strings.TrimRight(address, "/"), token, &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func validVaultPath(path string) bool {
	if len(path) > 1024 {
		return false
	}
	for _, s := range strings.Split(path, "/") {
		if !vaultSegment.MatchString(s) || s == "." || s == ".." {
			return false
		}
	}
	return true
}
func (c *vaultClient) call(ctx context.Context, method, path string, in, out any) error {
	if !validVaultPath(path) {
		return fail("Invalid OpenBao path")
	}
	var body io.Reader
	if in != nil {
		b, e := json.Marshal(in)
		if e != nil {
			return fail("Invalid OpenBao request")
		}
		body = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(ctx, method, c.address+"/v1/"+path, body)
	if e != nil {
		return fail("Invalid OpenBao request")
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("X-Vault-Token", c.token)
	}
	resp, e := c.http.Do(req)
	if e != nil {
		return fail("OpenBao unavailable; check devkit vault status")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// OpenBao briefly returns 400 while a newly mounted KV v2 engine initializes.
		// Recognize only its fixed readiness messages; never expose arbitrary API bodies.
		if resp.StatusCode == http.StatusBadRequest && path == "kv/config" {
			var result struct {
				Errors []string `json:"errors"`
			}
			b, readErr := io.ReadAll(io.LimitReader(resp.Body, 65537))
			if readErr == nil && len(b) <= 65536 && json.Unmarshal(b, &result) == nil && len(result.Errors) == 1 {
				switch result.Errors[0] {
				case "Upgrading from non-versioned to versioned data. This backend will be unavailable for a brief period and will resume service shortly.",
					"Waiting for the primary to upgrade from non-versioned to versioned data. This backend will be unavailable for a brief period and will resume service when the primary is finished.":
					return errVaultMountInitializing
				}
			}
		}
		return vaultError{resp.StatusCode}
	}
	if out == nil {
		_, e = io.Copy(io.Discard, io.LimitReader(resp.Body, vaultMaxBody))
		return e
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, vaultMaxBody+1))
	if e != nil || len(b) > vaultMaxBody || json.Unmarshal(b, out) != nil {
		return fail("Invalid or oversized OpenBao response")
	}
	return nil
}
func (c *vaultClient) status(ctx context.Context) (vaultStatus, error) {
	var s vaultStatus
	e := c.call(ctx, "GET", "sys/seal-status", nil, &s)
	switch {
	case e != nil:
		s.State = "unavailable"
	case !s.Initialized:
		s.State = "uninitialized"
	case s.Sealed:
		s.State = "sealed"
	default:
		s.State = "ready"
	}
	return s, e
}
func (c *vaultClient) read(ctx context.Context, path string) (string, int, error) {
	if !validVaultPath(path) {
		return "", 0, fail("Invalid secret path")
	}
	var out struct {
		Data struct {
			Data struct {
				Value string `json:"value"`
			} `json:"data"`
			Metadata struct {
				Version int `json:"version"`
			} `json:"metadata"`
		} `json:"data"`
	}
	e := c.call(ctx, "GET", "kv/data/"+path, nil, &out)
	return out.Data.Data.Value, out.Data.Metadata.Version, e
}
func (c *vaultClient) write(ctx context.Context, path, value string, cas int) (int, error) {
	if !validVaultPath(path) || cas < 0 || len(value) > 64<<10 {
		return 0, fail("Invalid secret value, path or expected version")
	}
	var out struct {
		Data struct {
			Version int `json:"version"`
		} `json:"data"`
	}
	e := c.call(ctx, "POST", "kv/data/"+path, map[string]any{"options": map[string]int{"cas": cas}, "data": map[string]string{"value": value}}, &out)
	return out.Data.Version, e
}
