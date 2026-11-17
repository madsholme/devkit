package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func resourceFixture(t *testing.T) {
	t.Helper()
	calls := 0
	swap(t, &resourceCPU, func() (uint64, uint64, error) { calls++; return uint64(calls * 100), uint64(calls * 25), nil })
	swap(t, &resourceMemory, func() (map[string]uint64, error) {
		return map[string]uint64{"MemTotal": 100, "MemAvailable": 40, "SwapTotal": 0, "SwapFree": 0}, nil
	})
	swap(t, &resourceDisk, func() (uint64, uint64, error) { return 100, 20, nil })
	swap(t, &resourceReadFile, func(path string) ([]byte, error) {
		if path == "/proc/loadavg" {
			return []byte("0 1 2 1/10 100"), nil
		}
		return []byte("12.5 8"), nil
	})
	swap(t, &resourceWait, func(context.Context, time.Duration) error { return nil })
	swap(t, &observeContainers, func(context.Context) ([]container, error) { return nil, nil })
	swap(t, &observationCommand, func(context.Context, ...string) (string, error) { return "", nil })
}
func TestResourceSampleCPUDelta(t *testing.T) {
	resourceFixture(t)
	s, checks := sampleResourcesOnce(context.Background())
	if len(checks) != 0 || s.CPU == nil || *s.CPU != 75 || s.SwapTotal == nil || *s.SwapTotal != 0 || s.MemUsed == nil || *s.MemUsed != 60 {
		t.Fatal(s, checks)
	}
}
func TestResourcesCLIPartialMetrics(t *testing.T) {
	resourceFixture(t)
	swap(t, &resourceCPU, func() (uint64, uint64, error) { return 0, 0, fail("secret-SENTINEL") })
	r := resourcesReport(context.Background())
	b, _ := json.Marshal(r)
	if r.Complete || !strings.Contains(string(b), `"cpu":null`) || !strings.Contains(string(b), `"mem_used":60`) || strings.Contains(string(b), "SENTINEL") {
		t.Fatal(string(b))
	}
}
func TestResourcesCLIContainerReplacement(t *testing.T) {
	resourceFixture(t)
	c := labelled(map[string]string{managedLabel: "true", "devkit.preview": "old-preview"})
	c.ID = strings.Repeat("a", 64)
	c.Name = "/same"
	c.State.Running = true
	swap(t, &observeContainers, func(context.Context) ([]container, error) { return []container{c}, nil })
	swap(t, &observationCommand, func(context.Context, ...string) (string, error) {
		return `{"ID":"` + strings.Repeat("b", 64) + `","Name":"same","CPUPerc":"1%","MemPerc":"2%"}`, nil
	})
	r := resourcesReport(context.Background())
	b, _ := json.Marshal(r)
	if r.Complete || strings.Contains(string(b), "old-preview") {
		t.Fatal(string(b))
	}
}
func TestResourcesCLIDeadline(t *testing.T) {
	resourceFixture(t)
	swap(t, &resourceWait, func(ctx context.Context, _ time.Duration) error { <-ctx.Done(); return ctx.Err() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	r := resourcesReport(ctx)
	if r.Complete {
		t.Fatal(r)
	}
}
func TestResourcesCLINoProcessArguments(t *testing.T) {
	resourceFixture(t)
	swap(t, &resourceReadFile, func(path string) ([]byte, error) {
		if path != "/proc/loadavg" && path != "/proc/uptime" {
			t.Fatal(path)
		}
		return []byte("0 0 0"), nil
	})
	code, r := validationCLI(t, "resources", "--json")
	if code != 0 {
		t.Fatal(r)
	}
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "processes") {
		t.Fatal(string(b))
	}
}

func TestResourceCPUParsing(t *testing.T) {
	total, idle, err := parseCPUTimes([]byte("cpu  100 20 30 400 50 6 7 8 90 10\ncpu0 1 2 3 4\n"))
	if err != nil || total != 621 || idle != 450 {
		t.Fatalf("guest times were counted twice: %d %d %v", total, idle, err)
	}
	for _, input := range []string{"", "cpu", "cpu 1 2 3", "cpu0 1 2 3 4", "cpu 1 nope 3 4", "cpu 18446744073709551615 1 0 0"} {
		if _, _, err := parseCPUTimes([]byte(input)); err == nil {
			t.Fatalf("accepted invalid CPU counters %q", input)
		}
	}
}
