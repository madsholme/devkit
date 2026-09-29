package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPasswordHashesVerifyOnlyTheRightPassword(t *testing.T) {
	hash, err := hashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "pbkdf2-sha256$600000$") || strings.Contains(hash, "correct horse") {
		t.Errorf("hash %q", hash)
	}
	if !checkPassword("correct horse battery", hash) || checkPassword("correct horse batterY", hash) {
		t.Error("password check is wrong")
	}
	for _, bad := range []string{"", "plain", "pbkdf2-sha256$x$y$z", "md5$1$a$b"} {
		if checkPassword("anything", bad) {
			t.Errorf("accepted stored value %q", bad)
		}
	}
}

func TestSessionsExpireAndEndWithAPasswordChange(t *testing.T) {
	secret := bytes.Repeat([]byte{7}, 32)
	now := time.Now()
	value := newSession(secret, "hash-1", now)
	if !validSession(secret, "hash-1", value, now) {
		t.Fatal("fresh session rejected")
	}
	if validSession(secret, "hash-1", value, now.Add(sessionLife+time.Minute)) {
		t.Error("expired session accepted")
	}
	if validSession(secret, "hash-2", value, now) {
		t.Error("session survived a password change")
	}
	if validSession(bytes.Repeat([]byte{8}, 32), "hash-1", value, now) {
		t.Error("session signed with another secret accepted")
	}
	if validSession(secret, "hash-1", "x"+value, now) || validSession(secret, "", value, now) {
		t.Error("tampered session, or a session without a password, accepted")
	}
}

func TestRepeatedWrongPasswordsLockAnAddressOut(t *testing.T) {
	l := &loginLimiter{failures: map[string][]time.Time{}}
	now := time.Now()
	for i := 0; i < loginFailures; i++ {
		if l.blocked("100.64.0.1", now) {
			t.Fatalf("blocked after %d failures", i)
		}
		l.fail("100.64.0.1", now)
	}
	if !l.blocked("100.64.0.1", now) || l.blocked("100.64.0.2", now) {
		t.Error("lockout should hit only the failing address")
	}
	if l.blocked("100.64.0.1", now.Add(loginWindow+time.Second)) {
		t.Error("lockout did not end")
	}
}

// webHost prepares a host config with a dashboard password, one preview and one known worktree.
func webHost(t *testing.T) (*httptest.Server, chan plannedAction, chan struct{}) {
	dir := t.TempDir()
	t.Setenv("DEVBOX_CONFIG_DIR", filepath.Join(dir, "config"))
	t.Setenv("HOME", dir)
	os.MkdirAll(filepath.Join(dir, "config"), 0o700)
	hash, _ := hashPassword("dashboard-password")
	os.WriteFile(filepath.Join(dir, "config", "config.json"), []byte(`{"web": {"password_hash": "`+hash+`"}}`), 0o600)
	web := runningWeb("demo-main")
	web.Config.Labels["devbox.repository"], web.Config.Labels["devbox.worktree"] = "/repo/.git", "/repo"
	swap(t, &dockerContainers, withContainers(web))
	swap(t, &worktreesOf, func(string) ([]worktree, error) {
		return []worktree{{"/repo", "main"}, {"/wt/feature", "feature"}}, nil
	})
	started, release := make(chan plannedAction, 10), make(chan struct{})
	swap(t, &runAction, func(p plannedAction, out *job) error {
		started <- p
		out.Write([]byte("working\n"))
		<-release
		return nil
	})
	server := httptest.NewServer(newActionHandler())
	t.Cleanup(server.Close)
	return server, started, release
}

func post(t *testing.T, server *httptest.Server, path, origin, cookie string, body any) (*http.Response, string) {
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", server.URL+path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if cookie != "" {
		req.Header.Set("Cookie", sessionCookie+"="+cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	text, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(text)
}

func TestActionsNeedTheDashboardsOwnPageAndASignIn(t *testing.T) {
	server, started, release := webHost(t)
	defer close(release)
	origin := server.URL
	if resp, _ := post(t, server, "/api/action", "", "", actionRequest{Action: "stop", Preview: "demo-main"}); resp.StatusCode != 403 {
		t.Errorf("no Origin: %d", resp.StatusCode)
	}
	if resp, _ := post(t, server, "/api/action", "https://evil.example", "", actionRequest{Action: "stop", Preview: "demo-main"}); resp.StatusCode != 403 {
		t.Errorf("foreign Origin: %d", resp.StatusCode)
	}
	if resp, _ := post(t, server, "/api/action", origin, "", actionRequest{Action: "stop", Preview: "demo-main"}); resp.StatusCode != 401 {
		t.Errorf("not signed in: %d", resp.StatusCode)
	}
	if resp, _ := post(t, server, "/api/login", origin, "", map[string]string{"password": "wrong"}); resp.StatusCode != 401 {
		t.Errorf("wrong password: %d", resp.StatusCode)
	}
	resp, _ := post(t, server, "/api/login", origin, "", map[string]string{"password": "dashboard-password"})
	var cookie string
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			cookie = c.Value
			if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
				t.Errorf("cookie flags %+v", c)
			}
		}
	}
	if resp.StatusCode != 200 || cookie == "" {
		t.Fatalf("login: %d, cookie %q", resp.StatusCode, cookie)
	}
	resp, text := post(t, server, "/api/action", origin, cookie, actionRequest{Action: "stop", Preview: "demo-main"})
	if resp.StatusCode != 200 {
		t.Fatalf("stop: %d %s", resp.StatusCode, text)
	}
	p := <-started
	if strings.Join(p.args, " ") != "stop demo-main" || p.dir != "/repo" {
		t.Errorf("planned %v in %s", p.args, p.dir)
	}
	// A second action on the same preview waits for the first.
	if resp, _ := post(t, server, "/api/action", origin, cookie, actionRequest{Action: "start", Preview: "demo-main"}); resp.StatusCode != 409 {
		t.Errorf("busy preview: %d", resp.StatusCode)
	}
	var job struct{ ID string }
	json.Unmarshal([]byte(text), &job)
	get, _ := http.NewRequest("GET", server.URL+"/api/job?id="+job.ID, nil)
	get.Header.Set("Cookie", sessionCookie+"="+cookie)
	jr, err := http.DefaultClient.Do(get)
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Output string
		Done   bool
	}
	json.NewDecoder(jr.Body).Decode(&state)
	jr.Body.Close()
	if !strings.Contains(state.Output, "$ devbox stop demo-main") || !strings.Contains(state.Output, "working") || state.Done {
		t.Errorf("job state %+v", state)
	}
}

func TestActionsAcceptOnlyKnownTargetsAndConfirmedDestruction(t *testing.T) {
	webHost(t)
	cases := []struct {
		req  actionRequest
		args string
		dir  string
	}{
		{actionRequest{Action: "destroy", Preview: "demo-main"}, "", ""},
		{actionRequest{Action: "destroy", Preview: "demo-main", Confirm: "demo-main"}, "destroy demo-main", "/repo"},
		{actionRequest{Action: "stop", Preview: "demo-other"}, "", ""},
		{actionRequest{Action: "stop", Preview: "../x"}, "", ""},
		{actionRequest{Action: "share", Preview: "demo-main", Host: "sso-1", Hours: 4}, "share demo-main --hours 4 --host sso-1", "/repo"},
		{actionRequest{Action: "share", Preview: "demo-main", Host: "x --y"}, "", ""},
		{actionRequest{Action: "live", Worktree: "/wt/feature"}, "live", "/wt/feature"},
		{actionRequest{Action: "review", Worktree: "/wt/feature"}, "review HEAD", "/wt/feature"},
		{actionRequest{Action: "live", Worktree: "/etc"}, "", ""},
		{actionRequest{Action: "gc", Repo: "/repo/.git"}, "", ""},
		{actionRequest{Action: "gc", Repo: "/repo/.git", Confirm: "remove"}, "gc --yes", "/repo"},
		{actionRequest{Action: "exec", Preview: "demo-main"}, "", ""},
	}
	for _, c := range cases {
		p, err := planAction(c.req)
		got := strings.Join(p.args, " ")
		if c.args == "" && err == nil {
			t.Errorf("%+v was accepted as %q", c.req, got)
		}
		if c.args != "" && (err != nil || got != c.args || p.dir != c.dir) {
			t.Errorf("%+v: %q in %q, %v", c.req, got, p.dir, err)
		}
	}
}

func TestSessionReportsSignInAndShareOptions(t *testing.T) {
	server, _, release := webHost(t)
	defer close(release)
	resp, err := http.Get(server.URL + "/api/session")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	json.NewDecoder(resp.Body).Decode(&state)
	resp.Body.Close()
	if state["password_set"] != true || state["signed_in"] != false || state["share"] != nil {
		t.Errorf("session %v", state)
	}
}

func TestWebPasswordKeepsTheRestOfTheConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DEVBOX_CONFIG_DIR", dir)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"share": {"domain": "share.test", "max_hours": 72}}`), 0o600)
	in, w, _ := os.Pipe()
	w.WriteString("a long enough password\na long enough password\n")
	w.Close()
	swap(t, &os.Stdin, in)
	swap[io.Writer](t, &stdout, &bytes.Buffer{})
	if err := setWebPassword(); err != nil {
		t.Fatal(err)
	}
	cfg, _ := readConfigFile()
	if str(object(cfg, "share"), "domain") != "share.test" || fmtNumber(object(cfg, "share")["max_hours"]) != "72" {
		t.Errorf("share section changed: %v", cfg)
	}
	if !checkPassword("a long enough password", passwordHash()) {
		t.Error("stored hash does not match")
	}
	if info, _ := os.Stat(filepath.Join(dir, "config.json")); info.Mode().Perm() != 0o600 {
		t.Errorf("config mode %v", info.Mode().Perm())
	}
}

func fmtNumber(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}
