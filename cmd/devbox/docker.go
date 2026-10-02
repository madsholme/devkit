package main

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// container is the part of docker inspect output devbox uses.
type container struct {
	ID           string `json:"Id"`
	Name         string
	Image        string
	Created      string
	RestartCount int
	Config       struct {
		Image        string
		Labels       map[string]string
		Env          []string
		ExposedPorts map[string]struct{}
		Healthcheck  *struct{ Test []string }
		User         string
		WorkingDir   string
		Entrypoint   []string
		Cmd          []string
	}
	HostConfig struct {
		RestartPolicy struct{ Name string }
	}
	State           containerState
	NetworkSettings struct {
		Networks map[string]struct {
			IPAddress string
			Aliases   []string
		}
	}
	Mounts []struct {
		Type, Name, Source, Destination string
		RW                              bool
	}
}

type containerState struct {
	Status     string
	Running    bool
	StartedAt  string
	FinishedAt string
	ExitCode   int
	Health     *struct {
		Status        string
		FailingStreak int
		Log           []struct {
			Output   string
			ExitCode int
		}
	}
}

func inspect(ids ...string) ([]container, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	out, err := capture(append([]string{"docker", "inspect"}, ids...)...)
	if err != nil {
		return nil, err
	}
	var containers []container
	return containers, json.Unmarshal([]byte(out), &containers)
}

// dockerContainers returns every devbox-managed container, running or not.
var dockerContainers = func() ([]container, error) {
	ids, err := capture("docker", "ps", "-aq", "--filter", "label="+managedLabel+"=true")
	if err != nil {
		return nil, err
	}
	return inspect(strings.Fields(ids)...)
}

func containersFor(name string) ([]container, error) {
	all, err := dockerContainers()
	var matches []container
	for _, c := range all {
		if c.Config.Labels["devbox.preview"] == name {
			matches = append(matches, c)
		}
	}
	return matches, err
}

var assertOwner = func(name, common string) error {
	containers, err := containersFor(name)
	if err != nil {
		return err
	}
	for _, c := range containers {
		if c.Config.Labels["devbox.repository"] != common {
			return fail("Preview name is used by another clone. Give clones distinct project names.")
		}
	}
	return nil
}

// containerStatus reports a stopped container as stopped, not by the last health result it keeps.
func containerStatus(state containerState) string {
	if state.Running && state.Health != nil && state.Health.Status != "" {
		return state.Health.Status
	}
	return state.Status
}

type port struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Routed   bool   `json:"routed"`
}

// containerPorts lists container-internal ports, marking the one Traefik routes the preview URL to.
// Previews never publish host ports, so these are only reachable inside Docker networks.
func containerPorts(c container) []port {
	routed := ""
	for _, key := range sortedKeys(c.Config.Labels) {
		if strings.HasPrefix(key, "traefik.http.services.") && strings.HasSuffix(key, ".loadbalancer.server.port") {
			routed = c.Config.Labels[key]
			break
		}
	}
	protocols := map[string]string{}
	for key := range c.Config.ExposedPorts {
		number, protocol, _ := strings.Cut(key, "/")
		protocols[number] = protocol
	}
	if _, ok := protocols[routed]; routed != "" && !ok {
		protocols[routed] = "tcp"
	}
	ports := []port{}
	for number, protocol := range protocols {
		n, err := strconv.Atoi(number)
		if err == nil {
			ports = append(ports, port{n, protocol, number == routed})
		}
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i].Port < ports[j].Port })
	return ports
}

// isWeb tells whether a container is its preview's web service. Previews saved before routes existed
// have no devbox.web label; their only Traefik-enabled container is the web service.
func isWeb(labels map[string]string) bool {
	if web, ok := labels["devbox.web"]; ok {
		return web == "true"
	}
	return labels["traefik.enable"] == "true"
}

var (
	ruleHost      = regexp.MustCompile("Host\\(`([^`]+)`\\)")
	ruleSubdomain = regexp.MustCompile("HostRegexp\\(`\\^\\[a-z0-9-\\]\\+\\\\\\.([^`]+)\\$`\\)")
	rulePath      = regexp.MustCompile("PathPrefix\\(`([^`]+)`\\)")
)

// routeURLs lists where Traefik sends a container's traffic, read from its router rules: the preview's
// host or, for a route taking every subdomain of it, "*.<host>", followed by the route's path prefixes.
func routeURLs(labels map[string]string) []string {
	urls := []string{}
	for _, key := range sortedKeys(labels) {
		if !strings.HasPrefix(key, "traefik.http.routers.") || !strings.HasSuffix(key, ".rule") {
			continue
		}
		rule := labels[key]
		host := ""
		if m := ruleSubdomain.FindStringSubmatch(rule); m != nil {
			host = "*." + strings.ReplaceAll(m[1], `\`, "")
		} else if m := ruleHost.FindStringSubmatch(rule); m != nil {
			host = m[1]
		} else {
			continue
		}
		paths := rulePath.FindAllStringSubmatch(rule, -1)
		if len(paths) == 0 {
			urls = append(urls, "https://"+host)
		}
		for _, p := range paths {
			urls = append(urls, "https://"+host+p[1])
		}
	}
	return urls
}
