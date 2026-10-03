package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type worktreeInfo struct {
	Head      string `json:"head"`
	Changes   int    `json:"changes"`
	Upstream  string `json:"upstream"`
	Ahead     int    `json:"ahead"`
	Behind    int    `json:"behind"`
	Subject   string `json:"subject"`
	Committed *int64 `json:"committed"`
}

// worktreeState reads the current commit, uncommitted changes, upstream distance and last commit of one worktree.
func worktreeState(path string) worktreeInfo {
	var state worktreeInfo
	out, err := git(path, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return state
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		switch {
		case strings.HasPrefix(line, "# branch.oid ") && len(fields) > 2:
			if fields[2] != "(initial)" {
				state.Head = fields[2]
			}
		case strings.HasPrefix(line, "# branch.upstream "):
			state.Upstream = strings.SplitN(line, " ", 3)[2]
		case strings.HasPrefix(line, "# branch.ab ") && len(fields) > 3:
			state.Ahead, _ = strconv.Atoi(strings.TrimPrefix(fields[2], "+"))
			behind, _ := strconv.Atoi(fields[3])
			state.Behind = -behind
		case line != "" && !strings.HasPrefix(line, "#"):
			state.Changes++
		}
	}
	if state.Head != "" {
		if out, err := git(path, "log", "-1", "--format=%ct%x00%s", state.Head); err == nil {
			committed, subject, _ := strings.Cut(out, "\x00")
			if t, err := strconv.ParseInt(committed, 10, 64); err == nil {
				state.Committed, state.Subject = &t, subject
			}
		}
	}
	return state
}

// commitsBetween counts the commits head has that base lacks; nil if either is unknown.
func commitsBetween(root, base, head string) *int {
	if base == "" || head == "" {
		return nil
	}
	count := 0
	if base != head {
		out, err := git(root, "rev-list", "--count", base+".."+head)
		if err != nil {
			return nil
		}
		if count, err = strconv.Atoi(out); err != nil {
			return nil
		}
	}
	return &count
}

// watchedRepositories finds the Git common dirs of the harness's worktrees (its "worktrees" glob,
// e.g. ~/.t3/worktrees/*/* for T3).
func watchedRepositories() []string {
	cfg, err := loadHostConfig()
	if err != nil || cfg.Harness == nil || cfg.Harness.Worktrees == "" {
		return nil
	}
	trees, _ := filepath.Glob(expandHome(cfg.Harness.Worktrees))
	sort.Strings(trees)
	var found []string
	for _, tree := range trees {
		if _, err := os.Stat(filepath.Join(tree, ".git")); err == nil {
			if common, err := git(tree, "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil {
				found = append(found, common)
			}
		}
	}
	return found
}

type service struct {
	Name    string   `json:"name"`
	Status  string   `json:"status"`
	Web     bool     `json:"web"`
	ID      string   `json:"id"`
	Image   string   `json:"image"`
	Ports   []port   `json:"ports"`
	URLs    []string `json:"urls"`
	Started *string  `json:"started"`
}

type shareView struct {
	URL     string `json:"url"`
	Host    string `json:"host"`
	Expires string `json:"expires"`
	Fixed   bool   `json:"fixed"`
}

type preview struct {
	Name          string     `json:"name"`
	Mode          string     `json:"mode"`
	Project       string     `json:"project"`
	Branch        string     `json:"branch"`
	Commit        string     `json:"commit"`
	Worktree      string     `json:"worktree"`
	URL           string     `json:"url"`
	Repository    string     `json:"repository"`
	Services      []service  `json:"services"`
	Status        string     `json:"status"`
	Started       *string    `json:"started"`
	Head          *string    `json:"head"`
	Changes       *int       `json:"changes"`
	Behind        *int       `json:"behind"`
	SourceBranch  *string    `json:"source_branch"`
	Share         *shareView `json:"share"`
	statusFromWeb bool
}

type orphan struct {
	Name     string `json:"name"`
	Mode     string `json:"mode"`
	Worktree string `json:"worktree"`
}

type worktreeRow struct {
	Path     string   `json:"path"`
	Branch   string   `json:"branch"`
	Previews []string `json:"previews"`
	worktreeInfo
}

type repositoryRow struct {
	Path      string        `json:"path"`
	Orphans   []orphan      `json:"orphans"`
	Worktrees []worktreeRow `json:"worktrees"`
}

type overviewData struct {
	Previews     []*preview      `json:"previews"`
	Repositories []repositoryRow `json:"repositories"`
	Host         any             `json:"host"`
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func startedAt(state containerState) *string {
	if !state.Running {
		return nil
	}
	started := state.StartedAt
	return &started
}

// overview gathers everything the web overview shows, read live from Docker labels and Git.
func overview() (overviewData, error) {
	containers, err := dockerContainers()
	if err != nil {
		return overviewData{}, err
	}
	previews := map[string]*preview{}
	for _, c := range containers {
		labels := c.Config.Labels
		name := labels["devbox.preview"]
		if name == "" {
			continue
		}
		p := previews[name]
		if p == nil {
			p = &preview{Name: name, Mode: orDefault(labels["devbox.mode"], "?"), Project: orDefault(labels["devbox.project"], "?"),
				Branch: labels["devbox.branch"], Commit: labels["devbox.commit"], Worktree: labels["devbox.worktree"],
				URL: labels["devbox.url"], Repository: labels["devbox.repository"], Services: []service{}}
			previews[name] = p
		}
		status := containerStatus(c.State)
		web := isWeb(labels)
		if s, ok := shareOf(c); ok {
			p.Share = &shareView{s.URL, s.Host, s.Expires.Format(time.RFC3339), s.Fixed}
		}
		serviceLabel := orDefault(labels["com.docker.compose.service"], labels["devbox.service"])
		p.Services = append(p.Services, service{Name: orDefault(serviceLabel, "?"), Status: status,
			Web: web, ID: c.ID[:min(12, len(c.ID))], Image: c.Config.Image, Ports: containerPorts(c), URLs: routeURLs(c.Config.Labels),
			Started: startedAt(c.State)})
		if web {
			// The web container speaks for the preview, whichever container Docker listed first.
			p.Status, p.Started, p.statusFromWeb = status, startedAt(c.State), true
			p.Branch, p.Commit, p.Worktree, p.URL = labels["devbox.branch"], labels["devbox.commit"], labels["devbox.worktree"], labels["devbox.url"]
		}
	}
	commons := map[string]bool{}
	for _, p := range previews {
		if p.Repository != "" {
			commons[p.Repository] = true
		}
	}
	for _, common := range watchedRepositories() {
		commons[common] = true
	}
	repos := []repositoryRow{}
	for _, common := range sortedKeys(commons) {
		trees, _ := worktreesOf(common)
		states := map[string]worktreeInfo{}
		for _, t := range trees {
			states[t.Path] = worktreeState(t.Path)
		}
		saved, _ := savedPreviews(common)
		orphans := []orphan{}
		for _, name := range sortedKeys(saved) {
			labels := saved[name]
			if path, exists, ok := sourceWorktree(labels, trees); ok && !exists {
				orphans = append(orphans, orphan{name, orDefault(labels["devbox.mode"], "?"), path})
			}
		}
		byTree := map[string][]string{}
		for _, name := range sortedKeys(previews) {
			p := previews[name]
			if p.Repository != common {
				continue
			}
			path, exists, ok := sourceWorktree(map[string]string{
				"devbox.worktree": p.Worktree, "devbox.mode": p.Mode, "devbox.branch": p.Branch}, trees)
			if !ok || !exists {
				continue
			}
			byTree[path] = append(byTree[path], p.Name)
			state := states[path]
			branch, _ := branchOf(trees, path)
			p.SourceBranch = &branch
			if p.Mode == "live" {
				// Live code is the worktree itself; the label only records the start commit.
				head, changes := state.Head, state.Changes
				p.Head, p.Changes = &head, &changes
			} else {
				p.Behind = commitsBetween(common, p.Commit, state.Head)
			}
		}
		rows := []worktreeRow{}
		for _, t := range trees {
			rows = append(rows, worktreeRow{t.Path, t.Branch, append([]string{}, byTree[t.Path]...), states[t.Path]})
		}
		repos = append(repos, repositoryRow{common, orphans, rows})
	}
	list := make([]*preview, 0, len(previews))
	for _, p := range previews {
		// The share is listed after the preview's own services.
		sort.SliceStable(p.Services, func(i, j int) bool { return p.Services[i].Name != "share" && p.Services[j].Name == "share" })
		if !p.statusFromWeb {
			p.Status = "missing"
		}
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.Project != b.Project {
			return a.Project < b.Project
		}
		if a.Mode != b.Mode {
			return a.Mode < b.Mode
		}
		return a.Name < b.Name
	})
	host, err := hostFactsFn()
	if err != nil {
		return overviewData{}, err
	}
	return overviewData{list, repos, host}, nil
}

var certCache = struct {
	sync.Mutex
	values map[string]certResult
}{values: map[string]certResult{}}

type certResult struct {
	checked time.Time
	expires string
	days    *int
}

// certExpiry returns the notAfter of the certificate served for host, cached for an hour.
func certExpiry(host string) (string, *int) {
	certCache.Lock()
	defer certCache.Unlock()
	if cached, ok := certCache.values[host]; ok && time.Since(cached.checked) < time.Hour {
		return cached.expires, cached.days
	}
	result := certResult{checked: time.Now()}
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", host+":443", &tls.Config{ServerName: host})
	if err != nil {
		result.expires = fmt.Sprintf("unavailable (%s)", errorKind(err))
	} else {
		notAfter := conn.ConnectionState().PeerCertificates[0].NotAfter.UTC()
		conn.Close()
		result.expires = notAfter.Format("Jan _2 15:04:05 2006 GMT")
		days := int(math.Floor(time.Until(notAfter).Hours() / 24))
		result.days = &days
	}
	certCache.values[host] = result
	return result.expires, result.days
}

func errorKind(err error) string {
	var certErr *tls.CertificateVerificationError
	var netErr net.Error
	switch {
	case errors.As(err, &certErr):
		return "CertificateVerificationError"
	case errors.As(err, &netErr) && netErr.Timeout():
		return "Timeout"
	}
	return "ConnectionError"
}

type serviceStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	URL    string `json:"url,omitempty"` // where the browser reaches it
}

// harnessStatus reports whether the harness answers at its local URL; any HTTP response counts as up.
func harnessStatus(h harnessConfig, dom string) serviceStatus {
	status := serviceStatus{Name: h.Name, Status: "up", Detail: h.URL}
	if h.Host != "" {
		status.URL = "https://" + h.Host + "." + dom
	}
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(h.URL)
	if err != nil {
		status.Status, status.Detail = "down", errorKind(err)
		return status
	}
	resp.Body.Close()
	return status
}

// edgeStatus reports the Traefik container of the devbox-edge Compose project.
func edgeStatus() serviceStatus {
	ids, err := capture("docker", "ps", "-aq", "--filter", "label=com.docker.compose.project=devbox-edge")
	fields := strings.Fields(ids)
	if err != nil || len(fields) == 0 {
		return serviceStatus{Name: "Traefik", Status: "down", Detail: "no devbox-edge container"}
	}
	containers, err := inspect(fields[0])
	if err != nil || len(containers) == 0 {
		return serviceStatus{Name: "Traefik", Status: "down", Detail: "not inspectable"}
	}
	status := containerStatus(containers[0].State)
	if status == "running" || status == "healthy" {
		return serviceStatus{Name: "Traefik", Status: "up", Detail: status}
	}
	return serviceStatus{Name: "Traefik", Status: "down", Detail: status}
}

type hostInfo struct {
	Domain             string          `json:"domain"`
	CertificateExpires string          `json:"certificate_expires"`
	CertificateDays    *int            `json:"certificate_days"`
	Services           []serviceStatus `json:"services"`
	RunningContainers  int             `json:"running_containers"`
	Disk               []diskRow       `json:"disk"`
}

type diskRow struct {
	Type        string `json:"type"`
	Size        string `json:"size"`
	Reclaimable string `json:"reclaimable"`
}

var hostFactsFn = func() (any, error) {
	dom, err := domain()
	if err != nil {
		return nil, err
	}
	out, err := capture("docker", "system", "df", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	disk := []diskRow{}
	for _, line := range strings.Split(out, "\n") {
		var d struct{ Type, Size, Reclaimable string }
		if line != "" && json.Unmarshal([]byte(line), &d) == nil {
			disk = append(disk, diskRow{d.Type, d.Size, d.Reclaimable})
		}
	}
	running, err := capture("docker", "ps", "-q")
	if err != nil {
		return nil, err
	}
	cfg, err := loadHostConfig()
	if err != nil {
		return nil, err
	}
	services := []serviceStatus{}
	if cfg.Harness != nil && cfg.Harness.URL != "" {
		services = append(services, harnessStatus(*cfg.Harness, dom))
	}
	services = append(services, edgeStatus())
	expires, days := certExpiry("devbox." + dom)
	return hostInfo{Domain: dom, CertificateExpires: expires, CertificateDays: days,
		Services: services, RunningContainers: len(strings.Fields(running)), Disk: disk}, nil
}

var serviceName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// previewContainer returns the container ID of a preview's service, its web service by default ("" service).
var previewContainer = func(name string, svc *string) (string, error) {
	if !previewID.MatchString(name) {
		return "", fail("Use an exact preview ID.")
	}
	if svc != nil && !serviceName.MatchString(*svc) {
		return "", fail("Use an exact Compose service name.")
	}
	containers, err := containersFor(name)
	if err != nil {
		return "", err
	}
	for _, c := range containers {
		labels := c.Config.Labels
		if (svc != nil && labels["com.docker.compose.service"] == *svc) || (svc == nil && isWeb(labels)) {
			return c.ID, nil
		}
	}
	which := "web"
	if svc != nil {
		which = *svc
	}
	return "", fail("No %s container for that preview.", which)
}

var secretName = regexp.MustCompile(`(?i)PASS|SECRET|TOKEN|KEY|CREDENTIAL|PRIVATE|DSN|DATABASE_URL`)

type envVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// containerDetails returns a readable subset of docker inspect for one preview container;
// secret-looking environment values are masked.
func containerDetails(name string, svc *string) (map[string]any, error) {
	id, err := previewContainer(name, svc)
	if err != nil {
		return nil, err
	}
	containers, err := inspect(id)
	if err != nil {
		return nil, err
	}
	if len(containers) == 0 {
		return nil, fail("Container %s is gone.", id)
	}
	c := containers[0]
	env := []envVar{}
	for _, item := range c.Config.Env {
		key, value, _ := strings.Cut(item, "=")
		if secretName.MatchString(key) && value != "" {
			value = "••••••"
		}
		env = append(env, envVar{key, value})
	}
	sort.SliceStable(env, func(i, j int) bool { return env[i].Name < env[j].Name })
	health := map[string]any{"status": nil, "failing_streak": nil, "test": []string{}, "last_output": "", "last_exit": nil}
	if c.Config.Healthcheck != nil && c.Config.Healthcheck.Test != nil {
		health["test"] = c.Config.Healthcheck.Test
	}
	if h := c.State.Health; h != nil {
		health["status"], health["failing_streak"] = h.Status, h.FailingStreak
		if len(h.Log) > 0 {
			last := h.Log[len(h.Log)-1]
			health["last_output"], health["last_exit"] = strings.TrimSpace(last.Output), last.ExitCode
		}
	}
	networks := []map[string]any{}
	for _, key := range sortedKeys(c.NetworkSettings.Networks) {
		n := c.NetworkSettings.Networks[key]
		networks = append(networks, map[string]any{"name": key, "ip": n.IPAddress, "aliases": nonNil(n.Aliases)})
	}
	mounts := []map[string]any{}
	for _, m := range c.Mounts {
		mounts = append(mounts, map[string]any{"type": m.Type, "source": orDefault(m.Name, m.Source),
			"destination": m.Destination, "rw": m.RW})
	}
	labels := c.Config.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	return map[string]any{
		"name":           strings.TrimPrefix(c.Name, "/"),
		"id":             c.ID,
		"image":          c.Config.Image,
		"image_id":       c.Image,
		"created":        c.Created,
		"started":        c.State.StartedAt,
		"finished":       c.State.FinishedAt,
		"status":         c.State.Status,
		"exit_code":      c.State.ExitCode,
		"restart_count":  c.RestartCount,
		"restart_policy": orDefault(c.HostConfig.RestartPolicy.Name, "no"),
		"user":           orDefault(c.Config.User, "(image default)"),
		"working_dir":    c.Config.WorkingDir,
		"entrypoint":     nonNil(c.Config.Entrypoint),
		"command":        nonNil(c.Config.Cmd),
		"health":         health,
		"ports":          containerPorts(c),
		"networks":       networks,
		"mounts":         mounts,
		"env":            env,
		"labels":         labels,
	}, nil
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func logsCommand(id string, follow bool) []string {
	args := []string{"docker", "logs", "--tail", "200", "--timestamps"}
	if follow {
		args = append(args, "--follow")
	}
	return append(args, id)
}
