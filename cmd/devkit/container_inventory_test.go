package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestContainerInventoryAllowlistAndStoppedState(t *testing.T) {
	managed := runningWeb("sample-main")
	managed.Name = "/web-one"
	managed.Config.Image = "example/web:v1"
	managed.Config.Env = []string{"TOKEN=private-secret"}
	managed.Config.Labels["private.label"] = "private-secret"
	managed.Config.Labels["devkit.project"] = "sample"
	managed.Config.Labels["com.docker.compose.service"] = "web"
	other := managed
	other.Name = "/other"
	other.Config.Labels = map[string]string{"com.docker.compose.project": "outside", "devkit.preview": "spoofed"}
	other.State.Running = false
	other.State.Status = "exited"
	swap(t, &hostContainers, func(context.Context) ([]container, error) { return []container{managed, other}, nil })
	h, err := newHandler()
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/containers", nil))
	if w.Code != 200 {
		t.Fatalf("inventory: %d %s", w.Code, w.Body.String())
	}
	var d containersData
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Containers) != 2 {
		t.Fatal(w.Body.String())
	}
	if d.Containers[0].Name != "other" || d.Containers[0].Status != "exited" || d.Containers[0].Preview != "" || d.Containers[0].Managed {
		t.Fatalf("unmanaged/stopped: %+v", d.Containers[0])
	}
	if d.Containers[1].Project != "sample" || !d.Containers[1].Managed || d.Containers[1].Service != "web" {
		t.Fatalf("managed: %+v", d.Containers[1])
	}
	if strings.Contains(w.Body.String(), "private-secret") || strings.Contains(w.Body.String(), "private.label") {
		t.Fatal("leaked private metadata")
	}
}

func TestContainerInventoryDockerFailure(t *testing.T) {
	swap(t, &hostContainers, func(context.Context) ([]container, error) { return nil, errors.New("secret-bearing diagnostics") })
	_, err := containersOverview(context.Background())
	if err == nil || strings.Contains(err.Error(), "secret-bearing") {
		t.Fatalf("wrong error: %v", err)
	}
}

func TestContainerInventoryPausedAndIPv6Ports(t *testing.T) {
	var c container
	if err := json.Unmarshal([]byte(`{"Name":"/paused","State":{"Status":"paused","Running":true,"Health":{"Status":"healthy"}},"HostConfig":{"PortBindings":{"80/tcp":[{"HostIp":"::1","HostPort":"8080"}]}}}`), &c); err != nil {
		t.Fatal(err)
	}
	swap(t, &hostContainers, func(context.Context) ([]container, error) { return []container{c}, nil })
	d, err := containersOverview(context.Background())
	if err != nil || d.Containers[0].Status != "paused" || d.Containers[0].Ports[0] != "[::1]:8080 → 80/tcp" {
		t.Fatalf("wrong paused inventory: %+v %v", d, err)
	}
}
