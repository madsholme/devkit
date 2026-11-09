package main

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestGuidedVaultFlows(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		initialized, sealed, ready, yes bool
		answers                         string
		want                            []string
		fail                            bool
	}{
		{name: "fresh", answers: "\n\ny\n", want: []string{"init", "unseal", "unseal", "provision"}},
		{name: "sealed existing", initialized: true, sealed: true, ready: true, want: []string{"unseal", "unseal"}},
		{name: "ready rerun", initialized: true, ready: true, yes: true},
		{name: "resume provision", initialized: true, want: []string{"provision"}},
		{name: "noninteractive fresh", yes: true, fail: true},
		{name: "noninteractive sealed", initialized: true, sealed: true, yes: true, fail: true},
		{name: "noninteractive missing backend", initialized: true, yes: true, fail: true},
		{name: "decline recovery custody", answers: "\n\nn\n", want: []string{"init"}, fail: true},
		{name: "invalid shares", answers: "2\n3\n", fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := vaultStatus{Initialized: tc.initialized, Sealed: tc.sealed}
			ready := tc.ready
			var calls []string
			shares := 0
			p := &prompter{in: bufio.NewReader(strings.NewReader(tc.answers)), yes: tc.yes}
			err := guideVault(p, func() (vaultStatus, error) { return s, nil }, func() (bool, error) { return ready, nil }, func(args []string) (int, error) {
				calls = append(calls, args[0])
				switch args[0] {
				case "init":
					s.Initialized = true
					s.Sealed = true
				case "unseal":
					shares++
					s.Sealed = shares < 2
				case "provision":
					ready = true
				}
				return 0, nil
			})
			if (err != nil) != tc.fail || !reflect.DeepEqual(calls, tc.want) {
				t.Fatalf("error=%v calls=%v want=%v", err, calls, tc.want)
			}
		})
	}
}
func TestGuidedVaultStopsOnErrors(t *testing.T) {
	sentinel := errors.New("unavailable")
	for _, stage := range []string{"status", "readiness", "provision"} {
		t.Run(stage, func(t *testing.T) {
			p := &prompter{in: bufio.NewReader(strings.NewReader(""))}
			calls := 0
			err := guideVault(p, func() (vaultStatus, error) {
				if stage == "status" {
					return vaultStatus{}, sentinel
				}
				return vaultStatus{Initialized: true}, nil
			}, func() (bool, error) {
				if stage == "readiness" {
					return false, sentinel
				}
				return false, nil
			}, func([]string) (int, error) { calls++; return 1, sentinel })
			if !errors.Is(err, sentinel) || (stage != "provision" && calls != 0) {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
		})
	}
}

func TestGuidedVaultWaitsForFreshAPI(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte(`{"initialized":false,"sealed":true}`))
	}))
	defer server.Close()
	c, err := newVaultClient(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := waitVaultSetupAPI(ctx, c); err != nil || attempts != 2 {
		t.Fatalf("error=%v attempts=%d", err, attempts)
	}
	expired, stop := context.WithCancel(context.Background())
	stop()
	if err := waitVaultSetupAPI(expired, c); err == nil {
		t.Fatal("missing startup timeout")
	}
}
