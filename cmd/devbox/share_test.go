package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeCloudflare is an in-memory stand-in for the parts of the Cloudflare API devbox uses.
type fakeCloudflare struct {
	mu      sync.Mutex
	calls   []string
	tunnels map[string]tunnel
	routes  map[string]any
	records map[string]dnsRecord
	next    int
}

func newFakeCloudflare(t *testing.T) *fakeCloudflare {
	f := &fakeCloudflare{tunnels: map[string]tunnel{}, routes: map[string]any{}, records: map[string]dnsRecord{}}
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	swap(t, &cloudflareAPI, server.URL)
	return f
}

func (f *fakeCloudflare) id(prefix string) string {
	f.next++
	return fmt.Sprintf("%s-%d", prefix, f.next)
}

func (f *fakeCloudflare) called(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (f *fakeCloudflare) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	reply := func(result any) {
		json.NewEncoder(w).Encode(map[string]any{"success": true, "errors": []any{}, "result": result})
	}
	var body map[string]any
	data, _ := io.ReadAll(r.Body)
	json.Unmarshal(data, &body)
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case r.URL.Path == "/zones":
		reply([]map[string]string{{"id": "zone-1"}})
	case len(parts) == 3 && parts[2] == "cfd_tunnel" && r.Method == "POST":
		t := tunnel{ID: f.id("tun"), Name: body["name"].(string), Status: "inactive", CreatedAt: time.Now().UTC().Format(time.RFC3339)}
		f.tunnels[t.ID] = t
		reply(t)
	case len(parts) == 3 && parts[2] == "cfd_tunnel" && r.Method == "GET":
		list := []tunnel{}
		for _, t := range f.tunnels {
			if strings.HasPrefix(t.Name, r.URL.Query().Get("include_prefix")) {
				list = append(list, t)
			}
		}
		reply(list)
	case len(parts) == 4 && parts[2] == "cfd_tunnel" && r.Method == "GET":
		t := f.tunnels[parts[3]]
		t.Status = "healthy"
		reply(t)
	case len(parts) == 4 && parts[2] == "cfd_tunnel" && r.Method == "DELETE":
		delete(f.tunnels, parts[3])
		reply(map[string]any{})
	case len(parts) == 5 && parts[4] == "token":
		reply("token-for-" + parts[3])
	case len(parts) == 5 && parts[4] == "configurations":
		f.routes[parts[3]] = body["config"]
		reply(map[string]any{})
	case len(parts) == 5 && parts[4] == "connections":
		reply(map[string]any{})
	case len(parts) == 3 && parts[2] == "dns_records" && r.Method == "GET":
		list := []dnsRecord{}
		for _, rec := range f.records {
			q := r.URL.Query()
			if (q.Get("name") == "" || q.Get("name") == rec.Name) && (q.Get("comment.exact") == "" || q.Get("comment.exact") == rec.Comment) {
				list = append(list, rec)
			}
		}
		reply(list)
	case len(parts) == 3 && parts[2] == "dns_records" && r.Method == "POST":
		rec := dnsRecord{ID: f.id("dns"), Type: body["type"].(string), Name: body["name"].(string),
			Content: body["content"].(string), Comment: body["comment"].(string), CreatedOn: time.Now().UTC().Format(time.RFC3339)}
		f.records[rec.ID] = rec
		reply(rec)
	case len(parts) == 4 && parts[2] == "dns_records" && r.Method == "PUT":
		rec := f.records[parts[3]]
		rec.Content, rec.Comment = body["content"].(string), body["comment"].(string)
		f.records[rec.ID] = rec
		reply(rec)
	case len(parts) == 4 && parts[2] == "dns_records" && r.Method == "DELETE":
		delete(f.records, parts[3])
		reply(map[string]any{})
	default:
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []map[string]any{{"code": 404, "message": "no route " + r.URL.Path}}})
	}
}

// shareHost writes a host config for sharing under share.test and stubs this machine's name.
func shareHost(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DEVBOX_CONFIG_DIR", dir)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"share": {"domain": "share.test",
		"account_id": "acct", "hosts": ["sso-1"], "default_hours": 8, "max_hours": 24}}`), 0o644)
	os.WriteFile(filepath.Join(dir, "cloudflare-token"), []byte("secret-token\n"), 0o600)
	swap(t, &machine, func() string { return "m5" })
	swap(t, &tunnelWait, time.Duration(0))
	swap[io.Writer](t, &stdout, &bytes.Buffer{})
}

func runningWeb(preview string) container {
	c := labelled(map[string]string{"devbox.managed": "true", "devbox.preview": preview, "devbox.mode": "live",
		"devbox.project": "demo", "devbox.repository": "/repo/.git", "devbox.worktree": "/repo", "traefik.enable": "true",
		"traefik.http.services." + preview + ".loadbalancer.server.port": "8000", "com.docker.compose.service": "web"})
	c.ID, c.Name = preview+"-web-id", "/"+preview+"-web-1"
	c.State.Running, c.State.Status = true, "running"
	return c
}

func shareContainer(preview, host, tunnelID, dnsID string, expires time.Time) container {
	c := labelled(map[string]string{"devbox.managed": "true", "devbox.preview": preview, "devbox.service": "share",
		"devbox.repository": "/repo/.git", "devbox.share.host": host, "devbox.share.url": "https://" + host,
		"devbox.share.tunnel": tunnelID, "devbox.share.dns": dnsID, "devbox.share.fixed": "false",
		"devbox.share.expires": expires.UTC().Format(time.RFC3339)})
	c.ID, c.Name = preview+"-share-id", "/"+preview+"-share"
	c.State.Running, c.State.Status = true, "running"
	return c
}

type recorded struct {
	started   [][]string
	tokens    []string
	removed   []string
	publicURL []string
}

// stubDocker records what share does to containers and to the preview's public URL.
func stubDocker(t *testing.T, rec *recorded, fail error) {
	swap(t, &startCloudflared, func(cfg shareConfig, web container, s share, token string) (string, error) {
		rec.started = append(rec.started, shareLabels(web, s))
		rec.tokens = append(rec.tokens, token)
		return "new-share-container-id", fail
	})
	swap(t, &removeContainer, func(id string) error {
		rec.removed = append(rec.removed, id)
		return nil
	})
	swap(t, &setPublicURL, func(common, preview, url string, apply bool) error {
		rec.publicURL = append(rec.publicURL, fmt.Sprintf("%s %s %s %v", common, preview, url, apply))
		return nil
	})
}

func hasLabel(args []string, label string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--label" && args[i+1] == label {
			return true
		}
	}
	return false
}

func TestShareCreatesTunnelDNSAndConnector(t *testing.T) {
	shareHost(t)
	cf := newFakeCloudflare(t)
	var rec recorded
	stubDocker(t, &rec, nil)
	swap(t, &dockerContainers, withContainers(runningWeb("demo-main")))
	if err := sharePreview("demo-main", "", 2); err != nil {
		t.Fatal(err)
	}
	if len(cf.tunnels) != 1 || len(cf.records) != 1 {
		t.Fatalf("tunnels %v records %v", cf.tunnels, cf.records)
	}
	var tun tunnel
	for _, v := range cf.tunnels {
		tun = v
	}
	if tun.Name != "devbox-m5-demo-main" {
		t.Errorf("tunnel name %q", tun.Name)
	}
	route, _ := json.Marshal(cf.routes[tun.ID])
	if !strings.Contains(string(route), `"hostname":"demo-main.share.test","service":"http://demo-main-web-1:8000"`) ||
		!strings.Contains(string(route), "http_status:404") {
		t.Errorf("route %s", route)
	}
	for _, r := range cf.records {
		if r.Name != "demo-main.share.test" || r.Content != tun.ID+".cfargotunnel.com" || r.Comment != "devbox share m5" {
			t.Errorf("dns record %+v", r)
		}
	}
	if len(rec.tokens) != 1 || rec.tokens[0] != "token-for-"+tun.ID {
		t.Errorf("connector token %v", rec.tokens)
	}
	labels := rec.started[0]
	for _, want := range []string{"devbox.preview=demo-main", "devbox.service=share", "devbox.share.tunnel=" + tun.ID,
		"devbox.share.url=https://demo-main.share.test", "devbox.repository=/repo/.git"} {
		if !hasLabel(labels, want) {
			t.Errorf("missing label %s in %v", want, labels)
		}
	}
	if len(rec.publicURL) != 1 || rec.publicURL[0] != "/repo/.git demo-main https://demo-main.share.test true" {
		t.Errorf("public URL %v", rec.publicURL)
	}
}

func TestShareUsesOnlyConfiguredFixedHostsAndBoundedHours(t *testing.T) {
	shareHost(t)
	cf := newFakeCloudflare(t)
	var rec recorded
	stubDocker(t, &rec, nil)
	swap(t, &dockerContainers, withContainers(runningWeb("demo-main")))
	if err := sharePreview("demo-main", "sso-9", 2); err == nil {
		t.Error("accepted an unconfigured fixed host")
	}
	if err := sharePreview("demo-main", "", 25); err == nil {
		t.Error("accepted more than max_hours")
	}
	if len(cf.tunnels) != 0 {
		t.Errorf("tunnels created: %v", cf.tunnels)
	}
	if err := sharePreview("demo-main", "sso-1", 0); err != nil {
		t.Fatal(err)
	}
	for _, r := range cf.records {
		if r.Name != "sso-1.share.test" {
			t.Errorf("fixed host record %q", r.Name)
		}
	}
	if !hasLabel(rec.started[0], "devbox.share.fixed=true") {
		t.Errorf("fixed label missing: %v", rec.started[0])
	}
}

func TestShareRefusesAForeignDNSRecord(t *testing.T) {
	shareHost(t)
	cf := newFakeCloudflare(t)
	cf.records["x"] = dnsRecord{ID: "x", Type: "A", Name: "demo-main.share.test", Content: "192.0.2.1"}
	var rec recorded
	stubDocker(t, &rec, nil)
	swap(t, &dockerContainers, withContainers(runningWeb("demo-main")))
	if err := sharePreview("demo-main", "", 2); err == nil || !strings.Contains(err.Error(), "did not create") {
		t.Errorf("expected refusal, got %v", err)
	}
	if len(cf.tunnels) != 0 || len(cf.records) != 1 {
		t.Errorf("changed Cloudflare: %v %v", cf.tunnels, cf.records)
	}
}

func TestShareReusesThisMachinesLeftoverRecord(t *testing.T) {
	shareHost(t)
	cf := newFakeCloudflare(t)
	cf.records["old"] = dnsRecord{ID: "old", Type: "CNAME", Name: "sso-1.share.test", Content: "gone.cfargotunnel.com", Comment: "devbox share m5"}
	var rec recorded
	stubDocker(t, &rec, nil)
	swap(t, &dockerContainers, withContainers(runningWeb("demo-main")))
	if err := sharePreview("demo-main", "sso-1", 2); err != nil {
		t.Fatal(err)
	}
	if len(cf.records) != 1 || cf.records["old"].Content == "gone.cfargotunnel.com" {
		t.Errorf("record not repointed: %v", cf.records)
	}
}

func TestShareRollsBackWhenTheConnectorFails(t *testing.T) {
	shareHost(t)
	cf := newFakeCloudflare(t)
	var rec recorded
	stubDocker(t, &rec, errors.New("docker run failed"))
	swap(t, &dockerContainers, withContainers(runningWeb("demo-main")))
	if err := sharePreview("demo-main", "", 2); err == nil {
		t.Fatal("expected failure")
	}
	if len(cf.tunnels) != 0 || len(cf.records) != 0 {
		t.Errorf("left behind: tunnels %v records %v", cf.tunnels, cf.records)
	}
	if len(rec.publicURL) != 2 || !strings.HasSuffix(rec.publicURL[1], "demo-main  true") {
		t.Errorf("public URL not reverted: %v", rec.publicURL)
	}
}

func TestShareRefusesAStoppedPreviewAndAHostInUse(t *testing.T) {
	shareHost(t)
	newFakeCloudflare(t)
	var rec recorded
	stubDocker(t, &rec, nil)
	stopped := runningWeb("demo-main")
	stopped.State.Running = false
	swap(t, &dockerContainers, withContainers(stopped))
	if err := sharePreview("demo-main", "", 2); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Errorf("stopped preview: %v", err)
	}
	swap(t, &dockerContainers, withContainers(runningWeb("demo-main"), runningWeb("demo-other"),
		shareContainer("demo-other", "sso-1.share.test", "tun-x", "dns-x", time.Now().Add(time.Hour))))
	if err := sharePreview("demo-main", "sso-1", 2); err == nil || !strings.Contains(err.Error(), "demo-other") {
		t.Errorf("host in use: %v", err)
	}
}

func TestSharingAgainExtendsWithoutANewTunnel(t *testing.T) {
	shareHost(t)
	cf := newFakeCloudflare(t)
	var rec recorded
	stubDocker(t, &rec, nil)
	swap(t, &dockerContainers, withContainers(runningWeb("demo-main"),
		shareContainer("demo-main", "demo-main.share.test", "tun-1", "dns-1", time.Now().Add(time.Hour))))
	if err := sharePreview("demo-main", "", 5); err != nil {
		t.Fatal(err)
	}
	if cf.called("POST") != 0 {
		t.Errorf("created Cloudflare resources: %v", cf.calls)
	}
	if len(rec.removed) != 1 || rec.removed[0] != "demo-main-share-id" || len(rec.started) != 1 {
		t.Errorf("connector not replaced: removed %v started %d", rec.removed, len(rec.started))
	}
	if !hasLabel(rec.started[0], "devbox.share.tunnel=tun-1") || rec.tokens[0] != "token-for-tun-1" {
		t.Errorf("extended share lost its tunnel: %v", rec.started[0])
	}
	if err := sharePreview("demo-main", "sso-1", 5); err == nil {
		t.Error("switched host without unsharing")
	}
}

func TestReapRemovesOnlyExpiredShares(t *testing.T) {
	shareHost(t)
	cf := newFakeCloudflare(t)
	cf.tunnels["tun-old"] = tunnel{ID: "tun-old", Name: "devbox-m5-demo-old"}
	cf.tunnels["tun-new"] = tunnel{ID: "tun-new", Name: "devbox-m5-demo-new"}
	cf.records["dns-old"] = dnsRecord{ID: "dns-old"}
	cf.records["dns-new"] = dnsRecord{ID: "dns-new"}
	var rec recorded
	stubDocker(t, &rec, nil)
	now := time.Now()
	swap(t, &dockerContainers, withContainers(
		shareContainer("demo-old", "demo-old.share.test", "tun-old", "dns-old", now.Add(-time.Minute)),
		shareContainer("demo-new", "demo-new.share.test", "tun-new", "dns-new", now.Add(time.Hour))))
	removed, err := reapExpired(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != "https://demo-old.share.test" {
		t.Errorf("removed %v", removed)
	}
	if _, ok := cf.tunnels["tun-old"]; ok || cf.tunnels["tun-new"].ID == "" {
		t.Errorf("tunnels %v", cf.tunnels)
	}
	if _, ok := cf.records["dns-old"]; ok || cf.records["dns-new"].ID == "" {
		t.Errorf("records %v", cf.records)
	}
	if len(rec.removed) != 1 || rec.removed[0] != "demo-old-share-id" {
		t.Errorf("containers removed %v", rec.removed)
	}
	if len(rec.publicURL) != 1 || rec.publicURL[0] != "/repo/.git demo-old  true" {
		t.Errorf("public URL %v", rec.publicURL)
	}
}

func TestSweepDeletesOnlyOldLeftoversOfThisMachine(t *testing.T) {
	shareHost(t)
	cf := newFakeCloudflare(t)
	old := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	young := time.Now().UTC().Format(time.RFC3339)
	cf.tunnels = map[string]tunnel{
		"live":   {ID: "live", Name: "devbox-m5-demo-live", CreatedAt: old},
		"orphan": {ID: "orphan", Name: "devbox-m5-demo-crashed", CreatedAt: old},
		"young":  {ID: "young", Name: "devbox-m5-demo-starting", CreatedAt: young},
		"other":  {ID: "other", Name: "devbox-other-box-demo", CreatedAt: old},
	}
	cf.records = map[string]dnsRecord{
		"r-live":   {ID: "r-live", Type: "CNAME", Content: "live.cfargotunnel.com", Comment: "devbox share m5", CreatedOn: old},
		"r-orphan": {ID: "r-orphan", Type: "CNAME", Content: "gone.cfargotunnel.com", Comment: "devbox share m5", CreatedOn: old},
		"r-other":  {ID: "r-other", Type: "CNAME", Content: "gone.cfargotunnel.com", Comment: "devbox share other-box", CreatedOn: old},
		"r-mine":   {ID: "r-mine", Type: "A", Content: "192.0.2.1", CreatedOn: old},
	}
	swap(t, &dockerContainers, withContainers(shareContainer("demo-live", "demo-live.share.test", "live", "r-live", time.Now().Add(time.Hour))))
	cfg, _, err := loadShareConfig()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sweepOrphans(cfg, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"live", "young", "other"} {
		if _, ok := cf.tunnels[id]; !ok {
			t.Errorf("tunnel %s was deleted", id)
		}
	}
	if _, ok := cf.tunnels["orphan"]; ok {
		t.Error("orphan tunnel kept")
	}
	for _, id := range []string{"r-live", "r-other", "r-mine"} {
		if _, ok := cf.records[id]; !ok {
			t.Errorf("record %s was deleted", id)
		}
	}
	if _, ok := cf.records["r-orphan"]; ok {
		t.Error("orphan record kept")
	}
}

func TestSetPublicURLChangesOnlyTheWebService(t *testing.T) {
	common := t.TempDir()
	path := filepath.Join(runDir(common, "demo-main"), "compose.json")
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, []byte(`{"services": {
		"web": {"labels": {"traefik.enable": "true"}, "environment": {"DOMAIN": "https://demo-main.dev"}},
		"db": {"labels": {"traefik.enable": "false"}}}}`), 0o600)
	ups := 0
	swap(t, &composeRun, func(string, ...string) error { ups++; return nil })
	if err := setPublicURL(common, "demo-main", "https://demo-main.share.test", true); err != nil {
		t.Fatal(err)
	}
	project, _ := readCompose(path)
	services := object(project, "services")
	if env := object(object(services, "web"), "environment"); env["DEVBOX_PUBLIC_URL"] != "https://demo-main.share.test" || env["DOMAIN"] != "https://demo-main.dev" {
		t.Errorf("web environment %v", env)
	}
	if _, has := object(services, "db")["environment"]; has {
		t.Error("db got an environment")
	}
	if ups != 1 {
		t.Errorf("up ran %d times", ups)
	}
	setPublicURL(common, "demo-main", "", false)
	project, _ = readCompose(path)
	if _, has := object(object(object(project, "services"), "web"), "environment")["DEVBOX_PUBLIC_URL"]; has || ups != 1 {
		t.Errorf("not removed, or restarted (%d)", ups)
	}
}

func TestShareConfigNeedsAPrivateToken(t *testing.T) {
	shareHost(t)
	os.Chmod(filepath.Join(os.Getenv("DEVBOX_CONFIG_DIR"), "cloudflare-token"), 0o644)
	if _, _, err := loadShareConfig(); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Errorf("readable token accepted: %v", err)
	}
	t.Setenv("DEVBOX_CONFIG_DIR", t.TempDir())
	if _, ok, err := loadShareConfig(); ok || err != nil {
		t.Errorf("missing config: ok=%v err=%v", ok, err)
	}
	if _, err := requireShareConfig(); err == nil || !strings.Contains(err.Error(), "account_id") {
		t.Errorf("missing config should explain the setup: %v", err)
	}
}

func TestOverviewShowsTheShare(t *testing.T) {
	web := runningWeb("demo-main")
	web.Config.Labels["devbox.branch"], web.Config.Labels["devbox.commit"] = "main", "abc123"
	// Docker lists the newest container first, so the share comes before the web container.
	swap(t, &dockerContainers, withContainers(
		shareContainer("demo-main", "demo-main.share.test", "tun-1", "dns-1", time.Now().Add(time.Hour)), web))
	swap(t, &hostFactsFn, func() (any, error) { return map[string]any{}, nil })
	data, err := overview()
	if err != nil {
		t.Fatal(err)
	}
	p := data.Previews[0]
	if p.Share == nil || p.Share.URL != "https://demo-main.share.test" || p.Status != "running" {
		t.Errorf("share %+v status %q", p.Share, p.Status)
	}
	if p.Branch != "main" || p.Commit != "abc123" {
		t.Errorf("preview fields came from the share container: %q %q", p.Branch, p.Commit)
	}
	names := []string{}
	for _, s := range p.Services {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "web,share" {
		t.Errorf("services %v", names)
	}
}

func TestShareArgumentsInAnyOrder(t *testing.T) {
	positional, values, err := splitArgs([]string{"--hours", "4", "demo-main", "--host=sso-1"},
		map[string]bool{"--host": true, "--hours": true}, nil)
	if err != nil || strings.Join(positional, ",") != "demo-main" || values["--hours"] != "4" || values["--host"] != "sso-1" {
		t.Errorf("parsed %v %v %v", positional, values, err)
	}
	if _, _, err := splitArgs([]string{"--hosts", "x"}, map[string]bool{"--host": true}, nil); err == nil {
		t.Error("accepted an unknown option")
	}
}
