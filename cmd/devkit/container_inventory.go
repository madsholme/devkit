package main

import (
	"context"
	"encoding/json"
	"net"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// This host inventory is read-only; mutation and detail/log APIs still require
// devkit's existing managed-preview identity checks.
var hostContainers = func(ctx context.Context) ([]container, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	run := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "docker", args...)
		cmd.WaitDelay = 100 * time.Millisecond
		var out vmOutput
		cmd.Stdout = &out
		err := cmd.Run()
		return out.String(), err
	}
	out, err := run("ps", "-aq", "--no-trunc")
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(out)
	if len(ids) > 512 {
		return nil, fail("Container inventory is limited to 512 containers.")
	}
	result := []container{}
	// Limit each inspect response, especially on hosts with large container envs.
	for len(ids) > 0 {
		n := min(len(ids), 32)
		out, err = run(append([]string{"inspect"}, ids[:n]...)...)
		if err != nil {
			return nil, err
		}
		var batch []container
		if err = json.Unmarshal([]byte(out), &batch); err != nil {
			return nil, err
		}
		result = append(result, batch...)
		ids = ids[n:]
	}
	return result, nil
}

type containerRow struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Image   string   `json:"image"`
	Status  string   `json:"status"`
	Project string   `json:"project"`
	Preview string   `json:"preview"`
	Service string   `json:"service"`
	Managed bool     `json:"managed"`
	Ports   []string `json:"ports"`
}
type containersData struct {
	Containers []containerRow `json:"containers"`
}

func containersOverview(ctx context.Context) (containersData, error) {
	data := containersData{Containers: []containerRow{}}
	rows, err := hostContainers(ctx)
	if err != nil {
		return data, fail("Could not read Docker containers. Check Docker access on the host (maximum 512 containers), then refresh.")
	}
	for _, c := range rows {
		labels := c.Config.Labels
		managed := labels[managedLabel] == "true"
		r := containerRow{ID: c.ID, Name: strings.TrimPrefix(c.Name, "/"), Image: c.Config.Image, Status: containerStatus(c.State), Managed: managed, Project: labels["com.docker.compose.project"], Ports: []string{}}
		if c.State.Status == "paused" {
			r.Status = "paused"
		}
		if managed {
			r.Project = firstOf(labels["devkit.project"], r.Project)
			r.Preview = labels["devkit.preview"]
			r.Service = firstOf(labels["com.docker.compose.service"], labels["devkit.service"])
		}
		for _, p := range sortedKeys(c.HostConfig.PortBindings) {
			for _, binding := range c.HostConfig.PortBindings[p] {
				r.Ports = append(r.Ports, net.JoinHostPort(firstOf(binding.HostIP, "0.0.0.0"), binding.HostPort)+" → "+p)
			}
		}
		data.Containers = append(data.Containers, r)
	}
	sort.Slice(data.Containers, func(i, j int) bool { return data.Containers[i].Name < data.Containers[j].Name })
	return data, nil
}
