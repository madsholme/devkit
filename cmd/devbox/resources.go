package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	sampleInterval = 5 * time.Second
	sampleHistory  = 120 // ten minutes
	clockTicks     = 100 // USER_HZ; 100 on every mainstream Linux build
)

type hostSample struct {
	T         float64    `json:"t"`
	CPU       float64    `json:"cpu"`
	Load      [3]float64 `json:"load"`
	MemUsed   uint64     `json:"mem_used"`
	MemTotal  uint64     `json:"mem_total"`
	SwapUsed  uint64     `json:"swap_used"`
	SwapTotal uint64     `json:"swap_total"`
	DiskUsed  uint64     `json:"disk_used"`
	DiskTotal uint64     `json:"disk_total"`
}

type containerUsage struct {
	Name       string  `json:"name"`
	Preview    string  `json:"preview"`
	Service    string  `json:"service"`
	CPU        float64 `json:"cpu"`
	Mem        string  `json:"mem"`
	MemPercent float64 `json:"mem_percent"`
	Net        string  `json:"net"`
	Block      string  `json:"block"`
	PIDs       string  `json:"pids"`
}

type processUsage struct {
	PID     int     `json:"pid"`
	Name    string  `json:"name"`
	Command string  `json:"command"`
	RSS     uint64  `json:"rss"`
	User    string  `json:"user"`
	CPU     float64 `json:"cpu"`
	uid     uint32
}

// resources samples host and container usage every few seconds, keeping a short history in memory.
type resources struct {
	mu         sync.Mutex
	history    []hostSample
	containers []containerUsage
	processes  []processUsage
	wanted     time.Time
	cpuTotal   uint64
	cpuIdle    uint64
	procTimes  map[int]uint64
	procClock  time.Time
	users      map[uint32]string
	wake       chan struct{}
}

func newResources() *resources {
	r := &resources{containers: []containerUsage{}, processes: []processUsage{}, users: map[uint32]string{},
		wake: make(chan struct{}, 1)}
	go r.loop()
	return r
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

func cpuTimes() (total, idle uint64, err error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, err
	}
	line, _, _ := strings.Cut(string(data), "\n")
	fields := strings.Fields(line)[1:]
	for i, f := range fields {
		v, _ := strconv.ParseUint(f, 10, 64)
		total += v
		if i == 3 || i == 4 { // idle + iowait
			idle += v
		}
	}
	return total, idle, nil
}

func meminfo() (map[string]uint64, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info := map[string]uint64{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, _ := strings.Cut(scanner.Text(), ":")
		if fields := strings.Fields(value); len(fields) > 0 {
			kb, _ := strconv.ParseUint(fields[0], 10, 64)
			info[key] = kb * 1024
		}
	}
	return info, scanner.Err()
}

// sampleHost returns a sample, or ok=false on the first call when CPU usage has no baseline yet.
func (r *resources) sampleHost() (sample hostSample, ok bool, err error) {
	total, idle, err := cpuTimes()
	if err != nil {
		return sample, false, err
	}
	if r.cpuTotal != 0 {
		if dt := total - r.cpuTotal; dt > 0 {
			sample.CPU = round1(100 * (1 - float64(idle-r.cpuIdle)/float64(dt)))
		}
		ok = true
	}
	r.cpuTotal, r.cpuIdle = total, idle
	mem, err := meminfo()
	if err != nil {
		return sample, false, err
	}
	var fs syscall.Statfs_t
	if err := syscall.Statfs("/", &fs); err != nil {
		return sample, false, err
	}
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		for i, f := range strings.Fields(string(data))[:3] {
			sample.Load[i], _ = strconv.ParseFloat(f, 64)
		}
	}
	sample.T = float64(time.Now().UnixNano()) / 1e9
	sample.MemTotal, sample.MemUsed = mem["MemTotal"], mem["MemTotal"]-mem["MemAvailable"]
	sample.SwapTotal, sample.SwapUsed = mem["SwapTotal"], mem["SwapTotal"]-mem["SwapFree"]
	sample.DiskTotal = fs.Blocks * uint64(fs.Frsize)
	sample.DiskUsed = (fs.Blocks - fs.Bfree) * uint64(fs.Frsize)
	return sample, ok, nil
}

func percent(value string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSuffix(value, "%"), 64)
	return v
}

func sampleContainers() ([]containerUsage, error) {
	managed, err := dockerContainers()
	if err != nil {
		return nil, err
	}
	labels := map[string]map[string]string{}
	for _, c := range managed {
		labels[strings.TrimPrefix(c.Name, "/")] = c.Config.Labels
	}
	out, err := capture("docker", "stats", "--no-stream", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	rows := []containerUsage{}
	for _, line := range strings.Split(out, "\n") {
		var d struct{ Name, CPUPerc, MemUsage, MemPerc, NetIO, BlockIO, PIDs string }
		if line == "" || json.Unmarshal([]byte(line), &d) != nil {
			continue
		}
		l := labels[d.Name]
		mem, _, _ := strings.Cut(d.MemUsage, " / ")
		rows = append(rows, containerUsage{d.Name, l["devbox.preview"], l["com.docker.compose.service"],
			percent(d.CPUPerc), mem, percent(d.MemPerc), d.NetIO, d.BlockIO, d.PIDs})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].CPU > rows[j].CPU })
	return rows, nil
}

// sampleProcesses returns the top processes by CPU since the last sample (percent of one core), with resident memory.
func (r *resources) sampleProcesses(limit int) []processUsage {
	page := uint64(os.Getpagesize())
	now := time.Now()
	times := map[int]uint64{}
	var rows []processUsage
	entries, _ := os.ReadDir("/proc")
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		dir := filepath.Join("/proc", entry.Name())
		stat, err1 := os.ReadFile(filepath.Join(dir, "stat"))
		cmdline, err2 := os.ReadFile(filepath.Join(dir, "cmdline"))
		statm, err3 := os.ReadFile(filepath.Join(dir, "statm"))
		info, err4 := os.Stat(dir)
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			continue // The process exited while we looked.
		}
		text := string(stat)
		open, close := strings.Index(text, "("), strings.LastIndex(text, ")")
		fields := strings.Fields(text[min(close+2, len(text)):])
		memFields := strings.Fields(string(statm))
		if open < 0 || close < open || len(fields) < 13 || len(memFields) < 2 {
			continue
		}
		utime, _ := strconv.ParseUint(fields[11], 10, 64)
		stime, _ := strconv.ParseUint(fields[12], 10, 64)
		times[pid] = utime + stime
		resident, _ := strconv.ParseUint(memFields[1], 10, 64)
		name := text[open+1 : close]
		command := strings.TrimSpace(strings.ReplaceAll(string(cmdline), "\x00", " "))
		if runes := []rune(command); len(runes) > 200 {
			command = string(runes[:200])
		}
		if command == "" {
			command = "[" + name + "]"
		}
		rows = append(rows, processUsage{PID: pid, Name: name, Command: command, RSS: resident * page,
			uid: info.Sys().(*syscall.Stat_t).Uid})
	}
	if !r.procClock.IsZero() {
		elapsed := now.Sub(r.procClock).Seconds()
		for i := range rows {
			if previous, ok := r.procTimes[rows[i].PID]; ok && elapsed > 0 {
				rows[i].CPU = round1(100 * float64(times[rows[i].PID]-previous) / clockTicks / elapsed)
			}
		}
	}
	r.procTimes, r.procClock = times, now
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CPU != rows[j].CPU {
			return rows[i].CPU > rows[j].CPU
		}
		return rows[i].RSS > rows[j].RSS
	})
	top := rows[:min(limit, len(rows))]
	for i := range top {
		name, ok := r.users[top[i].uid]
		if !ok {
			name = fmt.Sprint(top[i].uid)
			if u, err := user.LookupId(name); err == nil {
				name = u.Username
			}
			r.users[top[i].uid] = name
		}
		top[i].User = name
	}
	return append([]processUsage{}, top...)
}

func (r *resources) loop() {
	for {
		started := time.Now()
		sample, ok, err := r.sampleHost()
		if err == nil {
			r.mu.Lock()
			watched := time.Since(r.wanted) < time.Minute
			r.mu.Unlock()
			var containers []containerUsage
			var processes []processUsage
			if watched {
				containers, err = sampleContainers()
				processes = r.sampleProcesses(10)
			}
			r.mu.Lock()
			if ok {
				r.history = append(r.history, sample)
				if len(r.history) > sampleHistory {
					r.history = r.history[len(r.history)-sampleHistory:]
				}
			}
			if watched && err == nil {
				r.containers, r.processes = containers, processes
			}
			r.mu.Unlock()
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "devbox web: resource sample failed: %v\n", err)
		}
		select {
		case <-r.wake:
		case <-time.After(max(500*time.Millisecond, sampleInterval-time.Since(started))):
		}
	}
}

func (r *resources) snapshot(full bool) map[string]any {
	uptime := 0.0
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		if fields := strings.Fields(string(data)); len(fields) > 0 {
			uptime, _ = strconv.ParseFloat(fields[0], 64)
		}
	}
	result := map[string]any{"cores": runtime.NumCPU(), "uptime": uptime, "interval": int(sampleInterval.Seconds())}
	r.mu.Lock()
	defer r.mu.Unlock()
	if full {
		// docker stats is comparatively costly; only sample containers while someone is looking.
		if time.Since(r.wanted) > time.Minute {
			r.containers, r.processes = []containerUsage{}, []processUsage{} // Stale: show "collecting" instead.
			select {
			case r.wake <- struct{}{}:
			default:
			}
		}
		r.wanted = time.Now()
	}
	result["history"] = append([]hostSample{}, r.history...)
	if full {
		result["containers"], result["processes"] = r.containers, r.processes
	}
	return result
}
