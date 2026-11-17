package main

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var resourceCPU = cpuTimes
var resourceMemory = meminfo
var resourceReadFile = os.ReadFile
var resourceDisk = func() (uint64, uint64, error) {
	var fs syscall.Statfs_t
	err := syscall.Statfs("/", &fs)
	return fs.Blocks * uint64(fs.Frsize), (fs.Blocks - fs.Bfree) * uint64(fs.Frsize), err
}
var resourceWait = func(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type resourceSnapshot struct {
	Timestamp  time.Time           `json:"timestamp"`
	Interval   float64             `json:"interval_seconds"`
	Cores      int                 `json:"cores"`
	CPU        *float64            `json:"cpu"`
	Load       *[3]float64         `json:"load"`
	Uptime     *float64            `json:"uptime_seconds"`
	MemUsed    *uint64             `json:"mem_used"`
	MemTotal   *uint64             `json:"mem_total"`
	SwapUsed   *uint64             `json:"swap_used"`
	SwapTotal  *uint64             `json:"swap_total"`
	DiskUsed   *uint64             `json:"disk_used"`
	DiskTotal  *uint64             `json:"disk_total"`
	Containers []resourceContainer `json:"containers"`
}
type resourceContainer struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Preview    string   `json:"preview,omitempty"`
	Service    string   `json:"service,omitempty"`
	CPU        *float64 `json:"cpu"`
	MemPercent *float64 `json:"mem_percent"`
	Mem        string   `json:"mem"`
	Net        string   `json:"net"`
	Block      string   `json:"block"`
	PIDs       string   `json:"pids"`
}

func resourcesCLI(args []string) (int, error) {
	return reportCLI("resources", args, 10*time.Second, resourcesReport)
}
func resourcesReport(ctx context.Context) operationReport {
	snapshot, checks := sampleResourcesOnce(ctx)
	r := newReport("resources", snapshot)
	r.Checks = checks
	for _, c := range checks {
		if c.Status == "fail" {
			r.Complete = false
		}
	}
	return r
}
func resourceNumber(s string) (float64, bool) {
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0
}
func sampleResourcesOnce(ctx context.Context) (resourceSnapshot, []operationCheck) {
	r := newReport("resources", nil)
	s := resourceSnapshot{Cores: runtime.NumCPU(), Containers: []resourceContainer{}}
	started := time.Now()
	total, idle, e1 := resourceCPU()
	waitErr := resourceWait(ctx, 250*time.Millisecond)
	next, nextIdle, e2 := resourceCPU()
	s.Timestamp = time.Now().UTC()
	s.Interval = time.Since(started).Seconds()
	if e1 == nil && e2 == nil && waitErr == nil && next > total && nextIdle >= idle && nextIdle-idle <= next-total {
		v := round1(100 * (1 - float64(nextIdle-idle)/float64(next-total)))
		s.CPU = &v
	} else {
		r.unavailable("resources.cpu", "host", "CPU delta is unavailable.")
	}
	mem, err := resourceMemory()
	if err == nil {
		if total, ok := mem["MemTotal"]; ok {
			if available, ok := mem["MemAvailable"]; ok && available <= total {
				used := total - available
				s.MemTotal = &total
				s.MemUsed = &used
			}
		}
		if total, ok := mem["SwapTotal"]; ok {
			if free, ok := mem["SwapFree"]; ok && free <= total {
				used := total - free
				s.SwapTotal = &total
				s.SwapUsed = &used
			}
		}
	}
	if s.MemTotal == nil || s.SwapTotal == nil {
		r.unavailable("resources.memory", "host", "Memory or swap metrics are unavailable.")
	}
	if total, used, err := resourceDisk(); err == nil && used <= total {
		s.DiskTotal = &total
		s.DiskUsed = &used
	} else {
		r.unavailable("resources.disk", "/", "Root filesystem usage is unavailable.")
	}
	if b, err := resourceReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(b))
		if len(fields) >= 3 {
			var load [3]float64
			valid := true
			for i := range load {
				v, ok := resourceNumber(fields[i])
				load[i] = v
				valid = valid && ok
			}
			if valid {
				s.Load = &load
			}
		}
	}
	if s.Load == nil {
		r.unavailable("resources.load", "host", "Load averages are unavailable.")
	}
	if b, err := resourceReadFile("/proc/uptime"); err == nil {
		fields := strings.Fields(string(b))
		if len(fields) > 0 {
			v, ok := resourceNumber(fields[0])
			if ok {
				s.Uptime = &v
			}
		}
	}
	if s.Uptime == nil {
		r.unavailable("resources.uptime", "host", "Host uptime is unavailable.")
	}
	if ctx.Err() != nil {
		r.unavailable("observation.timeout", "host", "Resource sampling deadline exceeded.")
		return s, r.Checks
	}
	inventory, err := observeContainers(ctx)
	if err != nil {
		r.unavailable("containers.inventory", "docker", "Container identity is unavailable.")
		return s, r.Checks
	}
	known := map[string]container{}
	for _, c := range inventory {
		if fullContainerID.MatchString(c.ID) {
			known[c.ID] = c
		}
	}
	out, err := observationCommand(ctx, "docker", "stats", "--no-stream", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		r.unavailable("resources.containers", "docker", "Container resource sampling is unavailable.")
		return s, r.Checks
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var d struct{ ID, Container, Name, CPUPerc, MemUsage, MemPerc, NetIO, BlockIO, PIDs string }
		if len(s.Containers) >= 512 || json.Unmarshal([]byte(line), &d) != nil {
			r.unavailable("resources.container_sample", "docker", "Container sample exceeds limits or is invalid.")
			break
		}
		id := firstOf(d.ID, d.Container)
		if !fullContainerID.MatchString(id) || seen[id] {
			r.unavailable("resources.container_identity", "docker", "Container sample identity is invalid or duplicated.")
			continue
		}
		seen[id] = true
		row := resourceContainer{ID: id, Name: d.Name, Net: d.NetIO, Block: d.BlockIO, PIDs: d.PIDs}
		row.Mem, _, _ = strings.Cut(d.MemUsage, " / ")
		if v, ok := resourceNumber(strings.TrimSuffix(d.CPUPerc, "%")); ok {
			row.CPU = &v
		}
		if v, ok := resourceNumber(strings.TrimSuffix(d.MemPerc, "%")); ok && v <= 100 {
			row.MemPercent = &v
		}
		if row.CPU == nil || row.MemPercent == nil {
			r.unavailable("resources.container_metric", id, "Container numeric metrics are unavailable.")
		}
		if c, ok := known[id]; ok {
			if c.Config.Labels[managedLabel] == "true" {
				row.Preview = c.Config.Labels["devkit.preview"]
				row.Service = c.Config.Labels["com.docker.compose.service"]
			}
		} else {
			r.unavailable("resources.container_identity", id, "Container appeared after identity sampling; association is unknown.")
		}
		s.Containers = append(s.Containers, row)
	}
	for _, id := range sortedKeys(known) {
		if known[id].State.Running && !seen[id] {
			r.unavailable("resources.container_missing", id, "Running container has no current resource sample.")
		}
	}
	sort.Slice(s.Containers, func(i, j int) bool { return s.Containers[i].ID < s.Containers[j].ID })
	if ctx.Err() != nil {
		r.unavailable("observation.timeout", "host", "Resource sampling deadline exceeded.")
	}
	return s, r.Checks
}
