package main

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestDoctorDNSAcceptsOnlyListeningAddresses(t *testing.T) {
	for _, tc := range []struct {
		name     string
		listen   []string
		resolved string
		want     string
	}{
		{"tailnet", nil, "100.64.0.9", "ok"},
		{"LAN not enabled", nil, "192.168.1.20", "fail"},
		{"split DNS", []string{"192.168.1.20"}, "192.168.1.20", "ok"},
		{"unrelated LAN host", []string{"192.168.1.20"}, "192.168.1.21", "fail"},
		{"all IPv4 interfaces", []string{"0.0.0.0"}, "192.168.1.20", "ok"},
		{"all interfaces exclude other hosts", []string{"0.0.0.0"}, "192.168.1.21", "fail"},
		{"IPv4 listener excludes IPv6", []string{"0.0.0.0"}, "fd00::20", "fail"},
		{"dual stack listener", []string{"::"}, "fd00::20", "ok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := proxyHost()
			cfg.Harness = nil
			cfg.Proxy.Listen = tc.listen
			data, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			hostWith(t, string(data))
			t.Setenv("DEV_DOMAIN", "")
			t.Setenv("X", "")
			swap(t, &tailscaleIP, func() (string, error) { return "100.64.0.9", nil })
			swap(t, &hostAddresses, func() []string { return []string{"100.64.0.9", "192.168.1.20", "fd00::20"} })
			swap(t, &lookupHost, func(string) ([]string, error) { return []string{tc.resolved}, nil })
			// This DNS check needs no Docker daemon, systemd, or credential commands.
			swap(t, &runCommand, func([]string, runOpts) (string, error) { return "", errors.New("unavailable in DNS test") })
			for _, result := range doctorChecks() {
				if result.Name == "dns" {
					if result.Status != tc.want {
						t.Fatalf("want %s, got %+v", tc.want, result)
					}
					return
				}
			}
			t.Fatal("doctor omitted DNS check")
		})
	}
}
