package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

var fullContainerID = regexp.MustCompile(`^[0-9a-f]{64}$`)

func containerStopNote(c container) string {
	if signal := strings.ToUpper(c.Config.StopSignal); signal == "SIGKILL" || signal == "KILL" || signal == "9" {
		return "This container's stop signal is SIGKILL; configure a graceful stop signal with its existing manager."
	}
	if c.HostConfig.AutoRemove {
		return "This container auto-removes on exit; use its existing manager to avoid unintended data deletion."
	}
	if c.State.Status == "paused" {
		return "Resume this container with its existing manager before graceful shutdown."
	}
	if c.State.Status == "restarting" || c.State.Status == "removing" || c.State.Status == "dead" {
		return "Container is changing state or unavailable; refresh before turning it off."
	}
	return ""
}
func resolveStopContainer(ctx context.Context, target string) (container, error) {
	if target == "" || strings.HasPrefix(target, "-") {
		return container{}, fail("Use an exact container name or full ID.")
	}
	rows, err := hostContainers(ctx)
	if err != nil {
		return container{}, fail("Cannot read container inventory; check Docker access.")
	}
	matches := []container{}
	for _, c := range rows {
		if c.ID == target || strings.TrimPrefix(c.Name, "/") == target {
			matches = append(matches, c)
		}
	}
	if len(matches) != 1 || !fullContainerID.MatchString(matches[0].ID) {
		return container{}, fail("Use one exact container name or full ID from this host.")
	}
	c := matches[0]
	if note := containerStopNote(c); note != "" {
		return container{}, fail("%s", note)
	}
	return c, nil
}

var containerStopRun = func(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.WaitDelay = 250 * time.Millisecond
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fail("Timed out waiting for graceful stop. Docker may still be waiting for the container to exit; no timeout kill was requested.")
		}
		return fail("Docker could not stop this container; refresh its state and check Docker access.")
	}
	return nil
}

func stopContainer(target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	c, err := resolveStopContainer(ctx, target)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(stateDir(), 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(stateDir(), "container-"+c.ID+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fail("This container has another lifecycle operation in progress.")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	c, err = resolveStopContainer(ctx, c.ID)
	if err != nil {
		return err
	}
	if !c.State.Running {
		fmt.Fprintln(stdout, "Container is already stopped.")
		return nil
	}
	// Docker -1 disables the daemon's timeout/SIGKILL fallback. Killing a timed-out
	// CLI client does not force-kill the container; a late graceful exit is possible.
	if err = containerStopRun(ctx, []string{"docker", "stop", "--timeout", "-1", c.ID}); err != nil {
		return err
	}
	after, err := resolveStopContainer(ctx, c.ID)
	if err != nil {
		return err
	}
	if after.State.Running {
		return fail("Container is still running or restarted; refresh before retrying.")
	}
	happened("container-stop", c.ID, strings.TrimPrefix(c.Name, "/"))
	fmt.Fprintln(stdout, "Container stopped; its data was retained.")
	return nil
}

func planContainerStop(req actionRequest) (plannedAction, error) {
	if !fullContainerID.MatchString(req.Container) || req.Confirm != req.Container {
		return plannedAction{}, fail("Confirm turning off this exact container ID.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	c, err := resolveStopContainer(ctx, req.Container)
	if err != nil {
		return plannedAction{}, err
	}
	if !c.State.Running {
		return plannedAction{}, fail("Container is already stopped; refresh the inventory.")
	}
	return plannedAction{args: []string{"stop", "--container", c.ID}, target: "container " + c.ID, title: "Turn off container " + strings.TrimPrefix(c.Name, "/")}, nil
}
