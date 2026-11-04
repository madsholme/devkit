package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func TestShutdownActionAuthentication(t *testing.T) {
	c := stopContainerFixture(t)
	v := virtualMachine{PID: 42, start: 10, qmp: "/private/qmp", Source: "qemu", State: "running"}
	swap(t, &liveVMScan, func(context.Context) ([]virtualMachine, error) { return []virtualMachine{v}, nil })
	hash, err := hashPassword("shutdown-test")
	if err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]any{"web": map[string]string{"password_hash": hash}})
	hostWith(t, string(config))
	server := httptest.NewServer(newActionHandler())
	defer server.Close()
	requests := []actionRequest{{Action: "container-stop", Container: c.ID, Confirm: c.ID}, {Action: "vm-stop", VM: liveVMControlID(v), Confirm: liveVMControlID(v)}}
	for _, req := range requests {
		resp, _ := post(t, server, "/api/action", server.URL, "", req)
		if resp.StatusCode != 401 {
			t.Fatal(resp.StatusCode)
		}
		resp, _ = post(t, server, "/api/action", "https://elsewhere.invalid", "", req)
		if resp.StatusCode != 403 {
			t.Fatal(resp.StatusCode)
		}
	}
	resp, _ := post(t, server, "/api/login", server.URL, "", map[string]string{"password": "shutdown-test"})
	cookie := ""
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			cookie = c.Value
		}
	}
	started := make(chan plannedAction, 2)
	swap(t, &runAction, func(p plannedAction, j *job) error { started <- p; return nil })
	for _, req := range requests {
		resp, body := post(t, server, "/api/action", server.URL, cookie, req)
		if resp.StatusCode != 200 {
			t.Fatalf("%d %s", resp.StatusCode, body)
		}
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("action did not run")
		}
	}
}
