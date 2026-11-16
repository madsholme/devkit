package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVaultRestoreLoginReportsSafeHTTPStatus(t *testing.T) {
	for _, status := range []int{400, 403, 404, 429, 500, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/auth/userpass/login/admin" {
					t.Error("unexpected login path")
				}
				w.WriteHeader(status)
				fmt.Fprint(w, `{"errors":["private-response-sentinel"]}`)
			}))
			defer server.Close()
			client, e := newVaultClient(server.URL, "")
			if e != nil {
				t.Fatal(e)
			}
			input := &restoreCredentials{Login: func() (string, string, error) { return "admin", "private-password-sentinel", nil }}
			e = restoreLogin(context.Background(), client, input)
			if e == nil || !strings.Contains(e.Error(), fmt.Sprintf("HTTP %d", status)) {
				t.Fatalf("HTTP status missing: %v", e)
			}
			if strings.Contains(e.Error(), "private-") {
				t.Fatal("login error exposed private data")
			}
		})
	}
}

func TestVaultRestoreLoginUsesSelectedUsername(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/auth/userpass/login/custom-operator" {
			t.Error("selected username was not used for userpass login")
		}
		if r.Header.Get("X-Vault-Token") != "" {
			t.Error("username/password login unexpectedly used a token")
		}
		var request map[string]string
		if json.NewDecoder(r.Body).Decode(&request) != nil || request["password"] != "synthetic-password" {
			t.Error("password was not supplied in the login body")
		}
		fmt.Fprint(w, `{"auth":{"client_token":"synthetic-session-token"}}`)
	}))
	defer server.Close()
	client, e := newVaultClient(server.URL, "")
	if e != nil {
		t.Fatal(e)
	}
	input := &restoreCredentials{Login: func() (string, string, error) { return "custom-operator", "synthetic-password", nil }}
	if e = restoreLogin(context.Background(), client, input); e != nil {
		t.Fatal(e)
	}
	if client.token != "synthetic-session-token" {
		t.Fatal("successful userpass login did not establish its session")
	}
}
