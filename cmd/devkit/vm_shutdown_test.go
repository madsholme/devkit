package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveVMShutdownIdentityAndCapabilities(t *testing.T) {
	v := parseQEMU(os.Getpid(), []string{"qemu-system-x86_64", "-qmp", "unix:/tmp/guest-qmp,server=on,wait=off"})
	v.start = 42
	if v.qmp != "/tmp/guest-qmp" {
		t.Fatal("missing private QMP endpoint")
	}
	id := liveVMControlID(v)
	if !strings.HasPrefix(id, "live-") {
		t.Fatal(id)
	}
	v.start++
	if liveVMControlID(v) == id {
		t.Fatal("reused PID retained authority")
	}
	v.start--
	addLiveVMControls(&v)
	if v.ControlID != id || strings.Join(v.Actions, ",") != "stop" {
		t.Fatalf("missing stop: %+v", v)
	}
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), "guest-qmp") {
		t.Fatal("socket path leaked")
	}
	for _, kind := range []string{"missing", "paused", "managed"} {
		c := v
		c.ControlID = ""
		c.Actions = nil
		switch kind {
		case "missing":
			c.qmp = ""
		case "paused":
			c.State = "paused"
		case "managed":
			c.ControlID = "vm-existing"
			c.Actions = []string{"restart"}
		}
		addLiveVMControls(&c)
		if kind == "managed" {
			if strings.Join(c.Actions, ",") != "restart" {
				t.Fatal("overrode adapter")
			}
		} else if len(c.Actions) > 0 || c.ControlNote == "" {
			t.Fatalf("unsafe %s capability: %+v", kind, c)
		}
	}
}

// The server really uses a Unix socket; peer credentials must belong to this
// test process, not to a PID or path supplied by the client.
func fakeQMP(t *testing.T, status string, reject bool) (virtualMachine, <-chan []string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "qmp")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	calls := make(chan []string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		enc, dec := json.NewEncoder(conn), json.NewDecoder(conn)
		enc.Encode(map[string]any{"QMP": map[string]any{}})
		var seen []string
		defer func() { calls <- seen }()
		for {
			var req struct {
				Execute string `json:"execute"`
				ID      string `json:"id"`
			}
			if dec.Decode(&req) != nil {
				return
			}
			seen = append(seen, req.Execute)
			enc.Encode(map[string]any{"event": "TEST"})
			if reject {
				enc.Encode(map[string]any{"id": req.ID, "error": map[string]string{"desc": "PRIVATE ERROR"}})
				return
			}
			result := map[string]any{}
			if req.Execute == "query-status" {
				result["status"] = status
			}
			enc.Encode(map[string]any{"id": req.ID, "return": result})
			if req.Execute == "system_powerdown" {
				return
			}
		}
	}()
	return virtualMachine{PID: os.Getpid(), start: 42, qmp: path, Source: "qemu", State: "running"}, calls
}

func TestQMPPowerdownProtocolAndPeerChecks(t *testing.T) {
	for _, tc := range []struct {
		name, status   string
		badPID, reject bool
		want           string
	}{
		{name: "power button", status: "running", want: "qmp_capabilities,query-status,system_powerdown"},
		{name: "paused", status: "paused", want: "qmp_capabilities,query-status"},
		{name: "wrong peer", status: "running", badPID: true, want: ""},
		{name: "error", status: "running", reject: true, want: "qmp_capabilities"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, calls := fakeQMP(t, tc.status, tc.reject)
			if tc.badPID {
				v.PID++
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := qmpPowerdown(ctx, v, func() error { return nil })
			if (err == nil) != (tc.name == "power button") {
				t.Fatalf("err=%v", err)
			}
			if err != nil && strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("leaked QMP error")
			}
			if got := strings.Join(<-calls, ","); got != tc.want {
				t.Fatalf("commands=%s want=%s", got, tc.want)
			}
		})
	}
}

func TestQMPRevalidatesBeforePowerdown(t *testing.T) {
	v, calls := fakeQMP(t, "running", false)
	err := qmpPowerdown(context.Background(), v, func() error { return fail("identity changed") })
	if err == nil || strings.Join(<-calls, ",") != "qmp_capabilities,query-status" {
		t.Fatal("stale target was shut down")
	}
}

func TestLiveVMStopWaitsForExitAndRefusesReplacements(t *testing.T) {
	for _, mode := range []string{"exit", "timeout", "replacement", "managed"} {
		t.Run(mode, func(t *testing.T) {
			v, calls := fakeQMP(t, "running", false)
			v.Name = "guest"
			reads := 0
			swap(t, &liveVMScan, func(context.Context) ([]virtualMachine, error) {
				reads++
				if mode == "managed" {
					v.ControlID = "vm-existing"
					return []virtualMachine{v}, nil
				}
				if reads > 2 {
					if mode == "exit" {
						return nil, nil
					}
					if mode == "replacement" {
						v.start++
					}
				}
				return []virtualMachine{v}, nil
			})
			swap(t, &liveStopTimeout, 50*time.Millisecond)
			err := stopLiveVM(liveVMControlID(v))
			if (err == nil) != (mode == "exit") {
				t.Fatalf("%s: %v", mode, err)
			}
			if mode != "managed" {
				if got := strings.Join(<-calls, ","); got != "qmp_capabilities,query-status,system_powerdown" {
					t.Fatal(got)
				}
			}
		})
	}
}

func TestLiveVMActionRequiresCurrentIdentityAndConfirmation(t *testing.T) {
	v := virtualMachine{PID: 42, start: 10, qmp: "/tmp/qmp", Source: "qemu", State: "running"}
	swap(t, &liveVMScan, func(context.Context) ([]virtualMachine, error) { return []virtualMachine{v}, nil })
	id := liveVMControlID(v)
	for _, r := range []actionRequest{{Action: "vm-stop", VM: id}, {Action: "vm-start", VM: id, Confirm: id}, {Action: "vm-stop", VM: "live-stale", Confirm: "live-stale"}} {
		if _, err := planAction(r); err == nil {
			t.Fatal(r)
		}
	}
	p, err := planAction(actionRequest{Action: "vm-stop", VM: id, Confirm: id})
	if err != nil || strings.Join(p.args, " ") != "stop --vm "+id || p.target != "vm lifecycle" {
		t.Fatalf("%+v %v", p, err)
	}
	v.start++
	if _, err = planAction(actionRequest{Action: "vm-stop", VM: id, Confirm: id}); err == nil {
		t.Fatal("reused PID accepted")
	}
}

func TestQMPUnsupportedEndpointsAndMalformedOrSilentPeer(t *testing.T) {
	for _, endpoint := range []string{"tcp:127.0.0.1:4444,server=on", "unix:relative.sock,server=on", "unix:/tmp/client.sock,server=off", "unix:@abstract,server=on"} {
		v := parseQEMU(42, []string{"qemu-system-x86_64", "-qmp", endpoint})
		v.start = 1
		if liveVMControlID(v) != "" {
			t.Fatalf("unsupported endpoint: %s", endpoint)
		}
	}
	for _, greeting := range []string{"not json\n", "{}\n", "{\"QMP\":{}}\n"} {
		t.Run(greeting, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "qmp")
			listener, err := net.Listen("unix", path)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(time.Second))
				conn.Write([]byte(greeting))
				buf := make([]byte, 4096)
				for {
					n, err := conn.Read(buf)
					if strings.Contains(string(buf[:n]), "system_powerdown") {
						t.Error("powerdown after malformed/silent peer")
					}
					if err != nil {
						return
					}
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			if err = qmpPowerdown(ctx, virtualMachine{PID: os.Getpid(), qmp: path}, func() error { return nil }); err == nil {
				t.Fatal("invalid peer accepted")
			}
			<-done
		})
	}
}

func TestNativeLibvirtShutdownUsesExactLocalDomain(t *testing.T) {
	v := virtualMachine{Source: "qemu:///session", UUID: "11111111-1111-1111-1111-111111111111", State: "running", Name: "domain"}
	stopped := false
	swap(t, &liveVMScan, func(context.Context) ([]virtualMachine, error) {
		if stopped {
			return nil, nil
		}
		return []virtualMachine{v}, nil
	})
	swap(t, &vmControlRun, func(ctx context.Context, args []string, o runOpts) error {
		if strings.Join(args, " ") != "virsh --connect qemu:///session shutdown "+v.UUID {
			t.Fatal(args)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded libvirt shutdown")
		}
		stopped = true
		return nil
	})
	if err := stopLiveVM(liveVMControlID(v)); err != nil || !stopped {
		t.Fatal(err)
	}
	v.Source = "qemu+ssh://remote/system"
	if liveVMControlID(v) != "" {
		t.Fatal("remote URI accepted")
	}
}
