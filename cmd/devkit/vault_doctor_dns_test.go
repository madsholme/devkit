package main

import (
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVaultDoctorPrivateSplitDNS(t *testing.T) {
	for _, tc := range []struct {
		name            string
		listen, answers []string
		lookupErr       error
		warning         bool
	}{
		{"tailnet", nil, []string{"100.64.0.9"}, nil, false},
		{"LAN split DNS", []string{"192.168.1.20"}, []string{"192.168.1.20"}, nil, false},
		{"both private routes", []string{"192.168.1.20"}, []string{"192.168.1.20", "100.64.0.9"}, nil, false},
		{"unconfigured LAN", nil, []string{"192.168.1.20"}, nil, true},
		{"unrelated host", []string{"192.168.1.20"}, []string{"192.168.1.21"}, nil, true},
		{"public listener excluded", []string{"203.0.113.20"}, []string{"203.0.113.20"}, nil, true},
		{"mixed wrong answer", []string{"192.168.1.20"}, []string{"192.168.1.20", "203.0.113.20"}, nil, true},
		{"wildcard has no vault route", []string{"0.0.0.0"}, []string{"192.168.1.20"}, nil, true},
		{"empty", nil, nil, nil, true},
		{"lookup failure", nil, nil, errors.New("no DNS"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"initialized":true,"sealed":false}`)) }))
			defer server.Close()
			cfg := proxyHost()
			cfg.Proxy.Listen = tc.listen
			b, e := json.Marshal(cfg)
			if e != nil {
				t.Fatal(e)
			}
			hostWith(t, string(b))
			t.Setenv("DEV_DOMAIN", "")
			vault := defaultVaultConfig()
			vault.Address = server.URL
			if e := saveVaultConfig(vault); e != nil {
				t.Fatal(e)
			}
			swap(t, &tailscaleIP, func() (string, error) { return "100.64.0.9", nil })
			swap(t, &lookupHost, func(string) ([]string, error) { return tc.answers, tc.lookupErr })
			swap(t, &servedCertificate, func(string, string) (*x509.Certificate, error, error) { return nil, nil, nil })
			warning := false
			for _, c := range vaultDoctorChecks() {
				if c.Name == "vault-dns" && c.Status == "warn" {
					warning = true
				}
			}
			if warning != tc.warning {
				t.Fatalf("DNS warning=%t, want %t", warning, tc.warning)
			}
		})
	}
}
