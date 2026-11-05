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

func stopContainer(target string) error { return controlContainer("stop", target) }

func controlContainer(action, target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	resolve := resolveStopContainer
	if action == "start" {
		resolve = resolveStartContainer
	}
	c, err := resolve(ctx, target)
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
	c, err = resolve(ctx, c.ID)
	if err != nil {
		return err
	}
	if (action == "stop" && !c.State.Running) || (action == "start" && c.State.Running) {
		fmt.Fprintln(stdout, "Container is already in the requested state.")
		return nil
	}
	// Docker -1 disables the daemon's timeout/SIGKILL fallback. Killing a timed-out
	// CLI client does not force-kill the container; a late graceful exit is possible.
	if action == "start" {
		err = containerStartRun(ctx, []string{"docker", "start", c.ID})
	} else {
		err = containerStopRun(ctx, []string{"docker", "stop", "--timeout", "-1", c.ID})
	}
	if err != nil {
		return err
	}
	after, err := resolve(ctx, c.ID)
	if err != nil {
		return err
	}
	if after.State.Running != (action == "start") {
		return fail("Container did not remain in the requested state; refresh before retrying.")
	}
	happened("container-"+action, c.ID, strings.TrimPrefix(c.Name, "/"))
	fmt.Fprintln(stdout, "Container "+action+" completed; existing container and storage retained.")
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

func resolveStartContainer(ctx context.Context, target string) (container, error) {
	c, err := registeredContainer(ctx, target)
	if err != nil {
		return c, err
	}
	if note := containerStopNote(c); note != "" {
		return c, fail("%s", note)
	}
	if !c.State.Running && c.State.Status != "exited" && c.State.Status != "created" {
		return c, fail("Container state does not allow start; refresh first.")
	}
	return c, nil
}

var containerStartRun = func(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.WaitDelay = 250 * time.Millisecond
	if err := cmd.Run(); err != nil {
		return fail("Docker could not start this container; refresh its state and check Docker access.")
	}
	return nil
}

func planContainerStart(req actionRequest) (plannedAction, error) {
	if !fullContainerID.MatchString(req.Container) {
		return plannedAction{}, fail("Use an exact registered container ID.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	c, err := resolveStartContainer(ctx, req.Container)
	if err != nil {
		return plannedAction{}, err
	}
	if c.State.Running {
		return plannedAction{}, fail("Container is already running; refresh the inventory.")
	}
	return plannedAction{args: []string{"start", "--container", c.ID}, target: "container " + c.ID, title: "Start container " + strings.TrimPrefix(c.Name, "/")}, nil
}
