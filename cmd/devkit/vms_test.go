package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQEMUHeadlessAndSPICE(t *testing.T) {
	head := parseQEMU(42, []string{"qemu-system-x86_64", "-name", "native-test", "-display", "none", "-smp", "2", "-m", "2048", "-netdev", "user,id=n,hostfwd=tcp:127.0.0.1:22238-:22", "-drive", "if=pflash,file=/private/fw", "-drive", "if=virtio,format=qcow2,file=/private/disk", "-serial", "file:/private/serial"})
	if head.Display != "headless" || head.VCPUs != 2 || head.MemoryBytes != 2<<30 || len(head.Connections) != 1 || head.Connections[0].Kind != "ssh" {
		t.Fatalf("headless: %+v", head)
	}
	if len(head.disks) != 1 {
		t.Fatalf("firmware counted: %+v", head.disks)
	}
	gui := parseQEMU(43, []string{"qemu-system-x86_64", "-name", "guest=desktop,debug-threads=on", "-display", "none", "-spice", "addr=127.0.0.1,port=5931,password=PRIVATE", "-device", "qxl-vga", "-m", "size=4G", "-smp", "cpus=4,maxcpus=8"})
	if gui.Name != "desktop" || gui.Display != "spice" || gui.MemoryBytes != 4<<30 || gui.VCPUs != 4 || len(gui.Connections) != 1 || len(gui.Graphics) != 1 {
		t.Fatalf("graphical: %+v", gui)
	}
	addVMCommands(&gui, "mads", "100.126.112.0")
	if !strings.Contains(gui.Connections[0].Tunnel, "127.0.0.1:5931:127.0.0.1:5931") || gui.Connections[0].Command != "remote-viewer spice://127.0.0.1:5931" {
		t.Fatalf("connection: %+v", gui.Connections)
	}
	for _, vm := range []virtualMachine{head, gui} {
		b, _ := json.Marshal(vm)
		for _, secret := range []string{"PRIVATE", "/private/", "password=", "debug-threads"} {
			if strings.Contains(string(b), secret) {
				t.Fatalf("metadata leak: %s", b)
			}
		}
	}
}

func TestVMUnsupportedEndpointsAndShellSafety(t *testing.T) {
	for _, value := range []string{"port=0", "tls-port=5932", "port=5931,tls-port=5932,tls-channel=main", "addr=evil.example,port=5931", "addr=192.0.2.1,port=5931", "unix=on,addr=/tmp/spice", "port=70000"} {
		vm := parseQEMU(1, []string{"qemu-kvm", "-spice", value})
		addVMCommands(&vm, "mads", "100.64.0.1")
		if len(vm.Connections) != 0 || vm.ConnectionNote == "" {
			t.Fatalf("unsafe or unexplained %s: %+v", value, vm)
		}
	}
	vm := parseQEMU(1, []string{"qemu-kvm", "-nographic", "-netdev", "user,hostfwd=udp::2222-:22,hostfwd=tcp:127.0.0.1:2223-:80,hostfwd=tcp::2224-:22"})
	addVMCommands(&vm, "bad;user", "100.64.0.1")
	if len(vm.Connections) != 1 || vm.Connections[0].Tunnel != "" {
		t.Fatalf("invalid user accepted: %+v", vm)
	}
}

func TestLibvirtMetadataAndDedup(t *testing.T) {
	xml := `<domain><name>test</name><uuid>11111111-1111-1111-1111-111111111111</uuid><memory unit="KiB">4194304</memory><vcpu current="2">4</vcpu><devices><graphics type="spice" port="5931" listen="127.0.0.1" passwd="PRIVATE"/><video><model type="qxl"/></video><disk device="disk"><driver type="qcow2"/><source file="/private/disk"/></disk></devices></domain>`
	vm, err := parseDomain(xml, "qemu:///session")
	if err != nil {
		t.Fatal(err)
	}
	if vm.MemoryBytes != 4<<30 || vm.VCPUs != 2 || vm.Display != "spice" {
		t.Fatalf("domain: %+v", vm)
	}
	proc := parseQEMU(42, []string{"qemu-kvm", "-uuid", vm.UUID, "-name", "duplicate", "-display", "none"})
	proc.RSSBytes = 123
	rows := mergeVMs([]virtualMachine{proc}, []virtualMachine{vm})
	if len(rows) != 1 || rows[0].PID != 42 || rows[0].Name != "test" || rows[0].RSSBytes != 123 {
		t.Fatalf("dedup: %+v", rows)
	}
	b, _ := json.Marshal(rows)
	if strings.Contains(string(b), "PRIVATE") || strings.Contains(string(b), "/private") {
		t.Fatalf("leaked XML: %s", b)
	}
}

func TestVMProviderFailureAndBudget(t *testing.T) {
	old := vmProbe
	defer func() { vmProbe = old }()
	vmProbe = func(ctx context.Context, args ...string) (string, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("no deadline")
		}
		return "sensitive raw error", errors.New("private path")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	rows, warnings := libvirtVMs(ctx)
	if len(rows) != 0 || len(warnings) != 2 || strings.Contains(strings.Join(warnings, " "), "private") {
		t.Fatalf("provider isolation: %v %v", rows, warnings)
	}
}

func TestVMCPURespectsProcessIdentity(t *testing.T) {
	c := newVMInventory()
	now := time.Now()
	rows := []virtualMachine{{ID: "pid-1", ticks: 100, start: 10, RSSBytes: 1}}
	c.sampleCPU(rows, now)
	if rows[0].CPUPercent != nil {
		t.Fatal("first sample must be unknown")
	}
	rows[0].ticks = 200
	c.sampleCPU(rows, now.Add(time.Second))
	if rows[0].CPUPercent == nil || *rows[0].CPUPercent != 100 {
		t.Fatalf("cpu: %+v", rows[0])
	}
	rows[0].start = 20
	rows[0].CPUPercent = nil
	c.sampleCPU(rows, now.Add(2*time.Second))
	if rows[0].CPUPercent != nil {
		t.Fatal("reused PID inherited CPU")
	}
}

func TestVMProcessScanAndDiskCapacity(t *testing.T) {
	root := t.TempDir()
	swap(t, &vmProcRoot, root)
	dir := filepath.Join(root, "123")
	os.Mkdir(dir, 0700)
	os.Symlink("/usr/bin/qemu-system-x86_64", filepath.Join(dir, "exe"))
	os.WriteFile(filepath.Join(dir, "cmdline"), []byte("qemu-system-x86_64\x00-name\x00test\x00-nographic\x00"), 0600)
	fields := make([]string, 22)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0] = "S"
	fields[11] = "100"
	fields[12] = "25"
	fields[19] = "999"
	fields[21] = "12"
	os.WriteFile(filepath.Join(dir, "stat"), []byte("123 (qemu (test)) "+strings.Join(fields, " ")), 0600)
	rows, w := processVMs()
	if len(rows) != 1 || len(w) != 0 || rows[0].ticks != 125 || rows[0].start != 999 || rows[0].RSSBytes != uint64(12*os.Getpagesize()) {
		t.Fatalf("process stat: %+v %v", rows, w)
	}
	disk := filepath.Join(root, "disk")
	os.WriteFile(disk, []byte("fixture"), 0600)
	swap(t, &vmProbe, func(ctx context.Context, args ...string) (string, error) {
		if strings.Join(args, " ") != "qemu-img info --force-share --output=json -f qcow2 "+disk {
			t.Fatalf("unsafe probe: %v", args)
		}
		return `{"virtual-size":1048576,"filename":"PRIVATE"}`, nil
	})
	if n := diskCapacity(context.Background(), []vmDisk{{disk, "qcow2"}}); n == nil || *n != 1048576 {
		t.Fatalf("capacity: %v", n)
	}
	if diskCapacity(context.Background(), []vmDisk{{"https://example/disk", "qcow2"}}) != nil {
		t.Fatal("network disk accepted")
	}
	if diskCapacity(context.Background(), []vmDisk{{disk, "qcow2"}, {"", ""}}) != nil {
		t.Fatal("partial total presented as complete")
	}
}

func TestVMVNCAndHeadlessConsole(t *testing.T) {
	vm := parseQEMU(7, []string{"qemu-kvm", "-vnc", "127.0.0.1:31,password=on"})
	addVMCommands(&vm, "mads", "100.64.0.1")
	if vm.Display != "vnc" || len(vm.Connections) != 1 || vm.Connections[0].Command != "remote-viewer vnc://127.0.0.1:5931" {
		t.Fatalf("vnc: %+v", vm)
	}
	vm, err := parseDomain(`<domain><name>headless</name><uuid>11111111-1111-1111-1111-111111111111</uuid><memory unit="MiB">1024</memory><vcpu>2</vcpu></domain>`, "qemu:///system")
	if err != nil {
		t.Fatal(err)
	}
	addVMCommands(&vm, "mads", "100.64.0.1")
	if vm.Display != "headless" || len(vm.Connections) != 1 || vm.Connections[0].Kind != "console" || !strings.Contains(vm.Connections[0].Command, "virsh --connect qemu:///system console") {
		t.Fatalf("console: %+v", vm)
	}
}

func TestVMProviderReadsActiveDomains(t *testing.T) {
	id := "11111111-1111-1111-1111-111111111111"
	swap(t, &vmProbe, func(ctx context.Context, args ...string) (string, error) {
		if len(args) < 5 || args[0] != "virsh" || args[1] != "--readonly" {
			t.Fatalf("not read-only: %v", args)
		}
		if args[3] == "qemu:///system" {
			return "", nil
		}
		switch args[4] {
		case "list":
			return id, nil
		case "dumpxml":
			return `<domain><name>demo</name><uuid>` + id + `</uuid><devices><disk device="disk"><target dev="vda"/><source file="/inaccessible/disk"/></disk><disk device="cdrom"><target dev="sda"/></disk></devices></domain>`, nil
		case "domstats":
			return "state.state=3\ncpu.time=2000000000\nballoon.rss=1024\nblock.count=2\nblock.0.name=vda\nblock.0.capacity=10737418240\nblock.1.name=sda\nblock.1.capacity=1024", nil
		}
		t.Fatalf("unexpected probe: %v", args)
		return "", nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	rows, w := libvirtVMs(ctx)
	if len(rows) != 1 || len(w) != 0 || rows[0].State != "paused" || rows[0].ticks != 200 || rows[0].RSSBytes != 1<<20 || rows[0].DiskBytes == nil || *rows[0].DiskBytes != 10<<30 {
		t.Fatalf("domain stats: %+v %v", rows, w)
	}
}

func TestVMIPv6TunnelIsShellQuoted(t *testing.T) {
	vm := parseQEMU(1, []string{"qemu-kvm", "-spice", "addr=::1,port=5931"})
	addVMCommands(&vm, "machine$", "fd7a:115c:a1e0::1")
	if len(vm.Connections) != 1 || !strings.Contains(vm.Connections[0].Tunnel, "-L '127.0.0.1:5931:[::1]:5931' 'machine$@[fd7a:115c:a1e0::1]'") {
		t.Fatalf("unquoted shell metacharacters: %+v", vm.Connections)
	}
}

func TestLibvirtTLSDoesNotOfferPlaintextViewer(t *testing.T) {
	vm, err := parseDomain(`<domain><name>tls</name><uuid>11111111-1111-1111-1111-111111111111</uuid><devices><graphics type="spice" port="5931" tlsPort="5932" listen="127.0.0.1"/></devices></domain>`, "qemu:///system")
	if err != nil {
		t.Fatal(err)
	}
	addVMCommands(&vm, "demo", "100.64.0.1")
	if len(vm.Connections) != 0 || !strings.Contains(vm.ConnectionNote, "TLS") {
		t.Fatalf("TLS lost: %+v", vm)
	}
}
