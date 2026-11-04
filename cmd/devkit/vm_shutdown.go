package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Live controls authorize one observed instance, never a name or arbitrary PID.
func liveVMControlID(v virtualMachine) string {
	if v.Source == "qemu" {
		if v.PID <= 0 || v.start == 0 || v.qmp == "" {
			return ""
		}
	} else if (v.Source != "qemu:///session" && v.Source != "qemu:///system") || !vmUUID.MatchString(v.UUID) {
		return ""
	}
	return "live-" + digest(v.Source, v.UUID, strconv.Itoa(v.PID), strconv.FormatUint(v.start, 10), v.qmp)
}

func addLiveVMControls(v *virtualMachine) {
	if v.ControlID != "" {
		return
	} // Configured adapters and their refusal reasons take precedence.
	if v.State != "running" && v.State != "active" && v.State != "blocked" {
		v.ControlNote = "Resume this VM with its existing manager before requesting a graceful shutdown."
		return
	}
	if id := liveVMControlID(*v); id != "" {
		v.ControlID = id
		v.Actions = []string{"stop"}
	} else {
		v.ControlNote = "Turn off requires an accessible local QMP Unix socket or a configured lifecycle adapter."
	}
}

var liveVMScan = func(ctx context.Context) ([]virtualMachine, error) {
	rows, warnings := vmControlProcesses()
	if len(warnings) > 0 {
		return nil, fail("QEMU inventory is incomplete; refresh before turning off a VM.")
	}
	if _, err := exec.LookPath("virsh"); err == nil {
		domains, w := libvirtVMs(ctx)
		if len(w) > 0 {
			return nil, fail("Libvirt inventory is incomplete; refresh before turning off a VM.")
		}
		rows = mergeVMs(rows, domains)
	}
	cfg, err := loadHostConfig()
	if err != nil {
		return nil, err
	}
	roots, warnings := vmProjectRoots(ctx, cfg, rows)
	projects, problems := readVMProjects(roots)
	if len(warnings) > 0 || len(problems) > 0 {
		return nil, fail("VM recipe discovery is incomplete; refresh before turning off a VM.")
	}
	rows = appendManagedVMs(rows, managedVMs(ctx, rows, projects, nil))
	return rows, ctx.Err()
}

func resolveLiveVM(ctx context.Context, id string) (virtualMachine, error) {
	if !strings.HasPrefix(id, "live-") || len(id) != 69 {
		return virtualMachine{}, fail("Use an exact live VM control ID from the current inventory.")
	}
	rows, err := liveVMScan(ctx)
	if err != nil {
		return virtualMachine{}, err
	}
	for _, v := range rows {
		if v.ControlID == "" && liveVMControlID(v) == id {
			addLiveVMControls(&v)
			if len(v.Actions) == 0 {
				return virtualMachine{}, fail("%s", v.ControlNote)
			}
			return v, nil
		}
	}
	return virtualMachine{}, fail("VM identity changed or disappeared; refresh before turning it off.")
}

// QMP is a local socket protocol. Bind authority to the actual peer process, not
// merely a socket filename, and recheck the process after negotiating capabilities.
func qmpPowerdown(ctx context.Context, v virtualMachine, revalidate func() error) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", v.qmp)
	if err != nil {
		return fail("Cannot connect to this VM's QMP socket; check host permissions.")
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)
	raw, err := conn.(*net.UnixConn).SyscallConn()
	if err != nil {
		return err
	}
	var peer *syscall.Ucred
	var peerErr error
	err = raw.Control(func(fd uintptr) {
		peer, peerErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	})
	if err != nil || peerErr != nil || peer == nil || int(peer.Pid) != v.PID || int(peer.Uid) != os.Geteuid() {
		return fail("QMP peer does not match this user and VM process; refusing shutdown.")
	}
	dec, enc := json.NewDecoder(io.LimitReader(conn, 256<<10)), json.NewEncoder(conn)
	var greeting map[string]json.RawMessage
	if dec.Decode(&greeting) != nil || greeting["QMP"] == nil {
		return fail("Unsupported QMP greeting; no shutdown requested.")
	}
	command := func(name string) (json.RawMessage, error) {
		if err := enc.Encode(map[string]string{"execute": name, "id": name}); err != nil {
			return nil, fail("QMP request failed.")
		}
		for n := 0; n < 128; n++ {
			var reply struct {
				ID     string          `json:"id"`
				Return json.RawMessage `json:"return"`
				Error  json.RawMessage `json:"error"`
			}
			if dec.Decode(&reply) != nil {
				return nil, fail("QMP response failed or timed out; refresh VM state.")
			}
			if reply.ID != name {
				continue
			}
			if len(reply.Error) > 0 {
				return nil, fail("QMP rejected %s; no forced shutdown was attempted.", name)
			}
			if len(reply.Return) == 0 {
				return nil, fail("Malformed QMP response.")
			}
			return reply.Return, nil
		}
		return nil, fail("Too many QMP events; refresh VM state.")
	}
	if _, err = command("qmp_capabilities"); err != nil {
		return err
	}
	status, err := command("query-status")
	if err != nil {
		return err
	}
	var state struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(status, &state) != nil || state.Status != "running" {
		return fail("VM is not running; resume it with its existing manager before graceful shutdown.")
	}
	if err = revalidate(); err != nil {
		return err
	}
	_, err = command("system_powerdown")
	return err
}

var liveStopTimeout = 120 * time.Second

func stopLiveVM(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), liveStopTimeout)
	defer cancel()
	v, err := resolveLiveVM(ctx, id)
	if err != nil {
		return err
	}
	if v.Source == "qemu" {
		err = qmpPowerdown(ctx, v, func() error { _, err := resolveLiveVM(ctx, id); return err })
	} else {
		_, err = resolveLiveVM(ctx, id)
		if err == nil {
			err = vmControlRun(ctx, []string{"virsh", "--connect", v.Source, "shutdown", v.UUID}, runOpts{})
		}
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Requested graceful shutdown of", v.Name)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		rows, err := liveVMScan(ctx)
		if err != nil {
			return fail("Could not verify shutdown; refresh VM state. No force-kill was attempted.")
		}
		found := false
		for _, row := range rows {
			if liveVMControlID(row) == id {
				found = true
			} else if (v.UUID != "" && row.UUID == v.UUID) || (row.Source == v.Source && row.Name == v.Name) {
				return fail("A replacement VM appeared; it was not stopped. Refresh before acting.")
			}
		}
		if !found {
			happened("vm-stop", id, v.Name)
			fmt.Fprintln(stdout, "VM stopped; disks and registration retained.")
			return nil
		}
		select {
		case <-ctx.Done():
			return fail("Timed out waiting for graceful shutdown; the VM was not force-killed. Refresh its state before retrying.")
		case <-ticker.C:
		}
	}
}

func planLiveVMAction(req actionRequest) (plannedAction, error) {
	if req.Action != "vm-stop" || req.Confirm != req.VM {
		return plannedAction{}, fail("Confirm turning off this exact VM.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	v, err := resolveLiveVM(ctx, req.VM)
	if err != nil {
		return plannedAction{}, err
	}
	return plannedAction{args: []string{"stop", "--vm", req.VM}, target: "vm lifecycle", title: "Turn off VM " + v.Name}, nil
}
