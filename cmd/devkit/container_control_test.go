package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func stopContainerFixture(t *testing.T) *container {
	t.Helper()
	c := &container{ID: strings.Repeat("a", 64), Name: "/outside"}
	c.State.Running = true
	c.State.Status = "running"
	swap(t, &hostContainers, func(context.Context) ([]container, error) { return []container{*c}, nil })
	return c
}
func TestContainerTurnOffExactTargetAndRetainedData(t *testing.T) {
	c := stopContainerFixture(t)
	var calls []string
	swap(t, &containerStopRun, func(ctx context.Context, args []string) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded stop")
		}
		calls = args
		c.State.Running = false
		c.State.Status = "exited"
		return nil
	})
	if err := stopContainer("outside"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, " ") != "docker stop --timeout -1 "+c.ID {
		t.Fatal(calls)
	}
	calls = nil
	if err := stopContainer(c.ID); err != nil || len(calls) > 0 {
		t.Fatalf("stopped target: %v %v", err, calls)
	}
}
func TestContainerTurnOffGuardsAndConfirmation(t *testing.T) {
	c := stopContainerFixture(t)
	for _, target := range []string{"", c.ID[:12], "--all", "missing"} {
		if _, err := resolveStopContainer(context.Background(), target); err == nil {
			t.Fatal(target)
		}
	}
	req := actionRequest{Action: "container-stop", Container: c.ID}
	if _, err := planAction(req); err == nil {
		t.Fatal("unconfirmed action")
	}
	req.Confirm = c.ID
	p, err := planAction(req)
	if err != nil || strings.Join(p.args, " ") != "stop --container "+c.ID {
		t.Fatalf("plan: %+v %v", p, err)
	}
	req.Container = "outside"
	req.Confirm = "outside"
	if _, err := planAction(req); err == nil {
		t.Fatal("dashboard accepted mutable name")
	}
	c.HostConfig.AutoRemove = true
	if err := stopContainer(c.ID); err == nil {
		t.Fatal("auto-remove could delete data")
	}
	c.HostConfig.AutoRemove = false
	c.State.Status = "paused"
	if err := stopContainer(c.ID); err == nil {
		t.Fatal("paused stop")
	}
}
func TestContainerTurnOffFailureAndChangedTarget(t *testing.T) {
	c := stopContainerFixture(t)
	swap(t, &containerStopRun, func(context.Context, []string) error { return errors.New("failed") })
	if err := stopContainer(c.ID); err == nil || !c.State.Running {
		t.Fatal("failed stop claimed success")
	}
	n := 0
	swap(t, &hostContainers, func(context.Context) ([]container, error) {
		n++
		if n == 1 {
			return []container{*c}, nil
		}
		return nil, nil
	})
	called := false
	swap(t, &containerStopRun, func(context.Context, []string) error { called = true; return nil })
	if err := stopContainer("outside"); err == nil || called {
		t.Fatal("stale target reached Docker")
	}
}

func TestContainerTurnOffRefusesConfiguredKillSignal(t *testing.T) {
	c := stopContainerFixture(t)
	for _, signal := range []string{"SIGKILL", "KILL", "9"} {
		c.Config.StopSignal = signal
		if _, err := resolveStopContainer(context.Background(), c.ID); err == nil {
			t.Fatalf("%s would force-kill", signal)
		}
	}
	c.Config.StopSignal = "SIGTERM"
	if _, err := resolveStopContainer(context.Background(), c.ID); err != nil {
		t.Fatal(err)
	}
}
