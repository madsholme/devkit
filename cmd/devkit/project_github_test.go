package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectGitHubRequiresCredentialBeforeReservation(t *testing.T) {
	root := creationHost(t)
	var out bytes.Buffer
	swap(t, &stdout, io.Writer(&out))
	_, err := dispatch([]string{"project", "create", "app", "--github-owner", "octocat", "--github-repo", "demo"})
	if err == nil || !strings.Contains(err.Error(), "GitHub token") {
		t.Fatalf("expected actionable credential error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "app")); !os.IsNotExist(err) {
		t.Fatal("credential failure reserved folder")
	}
}

// The fake replaces only GitHub HTTP; template creation and Git remain real.
func githubFixture(t *testing.T, owner string, status int, response string, public ...bool) *int {
	t.Helper()
	t.Setenv("DEVKIT_TEST_GITHUB_TOKEN", "synthetic-private-token")
	raw, _ := readConfigFile()
	raw["integrations"] = map[string]any{"github": map[string]any{"token": map[string]string{"env": "DEVKIT_TEST_GITHUB_TOKEN"}}}
	if err := writeConfigFile(raw); err != nil {
		t.Fatal(err)
	}
	posts := new(int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-private-token" {
			t.Error("missing credential")
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/user":
			io.WriteString(w, `{"login":"octocat"}`)
		case r.Method == "GET" && r.URL.Path == "/users/team":
			io.WriteString(w, `{"login":"team","type":"Organization"}`)
		case r.Method == "POST":
			*posts++
			want := "/user/repos"
			if owner == "team" {
				want = "/orgs/team/repos"
			}
			if r.URL.Path != want {
				t.Errorf("wrong owner endpoint %s", r.URL.Path)
			}
			var body struct {
				Name     string
				Private  bool
				AutoInit bool `json:"auto_init"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Name != "demo" || body.Private != (len(public) == 0 || !public[0]) || body.AutoInit {
				t.Errorf("unsafe creation body: %+v", body)
			}
			w.WriteHeader(status)
			io.WriteString(w, response)
		default:
			t.Errorf("unexpected GitHub call: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(400)
		}
	}))
	t.Cleanup(server.Close)
	base, _ := url.Parse(server.URL)
	swap(t, &projectGitHubClient, &http.Client{Transport: githubTestTransport{base: base}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }})
	return posts
}

type githubTestTransport struct{ base *url.URL }

func (tr githubTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	u := *r.URL
	copy.URL = &u
	copy.URL.Scheme = tr.base.Scheme
	copy.URL.Host = tr.base.Host
	return http.DefaultTransport.RoundTrip(copy)
}

func TestProjectGitHubCreatesPrivateUserAndOrgWithoutPush(t *testing.T) {
	for _, owner := range []string{"octocat", "team"} {
		t.Run(owner, func(t *testing.T) {
			root := creationHost(t)
			posts := githubFixture(t, owner, 201, `{"name":"demo","owner":{"login":"`+owner+`"},"private":true}`)
			swap(t, &projectGitHubPush, func(context.Context, string, string, string) error { t.Fatal("push without opt-in"); return nil })
			_, err := dispatch([]string{"project", "create", "app", "--github-owner", owner, "--github-repo", "demo"})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "app")
			if *posts != 1 || createdGit(t, path, "remote", "get-url", "origin") != "https://github.com/"+owner+"/demo.git" {
				t.Fatal("repository not connected")
			}
			if createdGit(t, path, "rev-list", "--count", "HEAD") != "1" {
				t.Fatal("initial commit missing")
			}
			data, _ := os.ReadFile(filepath.Join(path, ".git", "config"))
			if bytes.Contains(data, []byte("synthetic-private-token")) {
				t.Fatal("persisted token")
			}
		})
	}
}
func TestProjectGitHubFailureRetainsProjectAndNeverPushes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"exists", 422, `{"message":"synthetic-private-token"}`},
		{"wrong-owner", 201, `{"name":"demo","owner":{"login":"other"},"private":true}`},
		{"wrong-privacy", 201, `{"name":"demo","owner":{"login":"octocat"},"private":false}`},
		{"malformed", 201, `broken`},
		{"redirect", 302, ``},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := creationHost(t)
			posts := githubFixture(t, "octocat", tc.status, tc.body)
			swap(t, &projectGitHubPush, func(context.Context, string, string, string) error { t.Fatal("pushed after failed create"); return nil })
			_, err := dispatch([]string{"project", "create", "app", "--github-owner", "octocat", "--github-repo", "demo", "--github-push"})
			if err == nil || !strings.Contains(err.Error(), "Partial project retained") || strings.Contains(err.Error(), "synthetic-private-token") {
				t.Fatalf("bad error %v", err)
			}
			if *posts != 1 || createdGit(t, filepath.Join(root, "app"), "rev-list", "--count", "HEAD") != "1" {
				t.Fatal("lost local commit or retried POST")
			}
		})
	}
}
func TestProjectGitHubPushFailureRetainsOrigin(t *testing.T) {
	root := creationHost(t)
	githubFixture(t, "octocat", 201, `{"name":"demo","owner":{"login":"octocat"},"private":true}`)
	swap(t, &projectGitHubPush, func(ctx context.Context, dir, remote, token string) error {
		if remote != "https://github.com/octocat/demo.git" || token != "synthetic-private-token" {
			t.Fatal("wrong push target/credential")
		}
		if createdGit(t, dir, "rev-list", "--count", "HEAD") != "1" {
			t.Fatal("missing commit")
		}
		return fmt.Errorf("synthetic-private-token")
	})
	_, err := dispatch([]string{"project", "create", "app", "--github-owner", "octocat", "--github-repo", "demo", "--github-push"})
	if err == nil || !strings.Contains(err.Error(), "git push -u origin main") || strings.Contains(err.Error(), "synthetic-private-token") {
		t.Fatalf("bad recovery message %v", err)
	}
	if createdGit(t, filepath.Join(root, "app"), "remote", "get-url", "origin") != "https://github.com/octocat/demo.git" {
		t.Fatal("origin lost")
	}
}
func TestProjectGitHubRejectsInvalidOptions(t *testing.T) {
	root := creationHost(t)
	for _, args := range [][]string{
		{"--github-owner", "octocat"}, {"--github-repo", "demo"}, {"--github-public"}, {"--github-push"},
		{"--github-owner", "../other", "--github-repo", "demo"},
		{"--github-owner", "octocat", "--github-repo", "../demo"},
		{"--github-owner", "octocat", "--github-repo", "demo", "--remote", "https://github.com/octocat/demo.git"},
	} {
		_, err := dispatch(append([]string{"project", "create", "app"}, args...))
		if err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("invalid options wrote project")
	}
}

func TestProjectGitHubPushCommandIsolatesCredentials(t *testing.T) {
	t.Setenv("GIT_TRACE", "/tmp/forbidden-trace")
	t.Setenv("GIT_CONFIG_COUNT", "9")
	t.Setenv("GIT_DIR", "/unrelated")
	cmd := projectGitHubPushCommand(context.Background(), "/pinned", "https://github.com/octocat/demo.git", "sentinel-token")
	args := strings.Join(cmd.Args, " ")
	for _, forbidden := range []string{"sentinel-token", "--force", "--mirror", "--all"} {
		if strings.Contains(args, forbidden) {
			t.Fatalf("unsafe args %s", args)
		}
	}
	for _, required := range []string{"core.hooksPath=/dev/null", "credential.helper=", "http.followRedirects=false", "protocol.allow=never", "protocol.https.allow=always", "--set-upstream origin main:refs/heads/main"} {
		if !strings.Contains(args, required) {
			t.Fatalf("missing %s", required)
		}
	}
	env := strings.Join(cmd.Env, "\n")
	if strings.Contains(env, "GIT_TRACE=") || strings.Contains(env, "GIT_DIR=") || strings.Contains(env, "GIT_CONFIG_COUNT=9") {
		t.Fatal("inherited Git override")
	}
	if !strings.Contains(env, "GIT_CONFIG_KEY_0=http.https://github.com/octocat/demo.git.extraHeader") || !strings.Contains(env, "GIT_CONFIG_GLOBAL="+os.DevNull) || cmd.Dir != "/pinned" {
		t.Fatal("credential or directory isolation lost")
	}
	if cmd.Stdout != nil || cmd.Stderr != nil {
		t.Fatal("external output can leak")
	}
}

func TestProjectGitHubInitialPushSetsUpstream(t *testing.T) {
	root := creationHost(t)
	githubFixture(t, "octocat", 201, `{"name":"demo","owner":{"login":"octocat"},"private":true}`)
	bare := filepath.Join(t.TempDir(), "remote.git")
	createdGit(t, root, "init", "--bare", bare)
	swap(t, &projectGitHubPush, func(ctx context.Context, dir, remote, token string) error {
		// Exercise the real push command against a disposable bare remote. Only the
		// transport/credential settings change; refspec, hooks and upstream stay real.
		cmd := projectGitHubPushCommand(ctx, dir, remote, token)
		cmd.Args = append(cmd.Args[:1], append([]string{"-c", "url." + bare + ".insteadOf=" + remote, "-c", "protocol.file.allow=always"}, cmd.Args[1:]...)...)
		return cmd.Run()
	})
	_, err := dispatch([]string{"project", "create", "app", "--github-owner", "octocat", "--github-repo", "demo", "--github-push"})
	if err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(root, "app")
	if createdGit(t, local, "rev-parse", "main") != createdGit(t, bare, "rev-parse", "main") {
		t.Fatal("wrong commit pushed")
	}
	if createdGit(t, local, "rev-parse", "--abbrev-ref", "@{upstream}") != "origin/main" {
		t.Fatal("missing upstream")
	}
}

func TestProjectGitHubActionCarriesPublicAndPush(t *testing.T) {
	creationHost(t)
	plan, err := planProjectCreate(actionRequest{Name: "app", GitHubOwner: "team", GitHubRepo: "demo", GitHubPublic: true, GitHubPush: true, AgentNotes: "private notes"})
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(plan.args, " ")
	if !strings.Contains(args, "--github-owner team --github-repo demo --github-public --github-push") || strings.Contains(args, "private notes") || plan.stdin != "private notes" {
		t.Fatalf("bad plan %+v", plan)
	}
}

func TestProjectGitHubPublicVisibilityRequiresExplicitResponse(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		ok         bool
	}{
		{"explicit-public", `{"name":"demo","owner":{"login":"octocat"},"private":false}`, true},
		{"missing-visibility", `{"name":"demo","owner":{"login":"octocat"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			creationHost(t)
			githubFixture(t, "octocat", 201, tc.body, true)
			_, err := dispatch([]string{"project", "create", "app", "--github-owner", "octocat", "--github-repo", "demo", "--github-public"})
			if (err == nil) != tc.ok {
				t.Fatalf("unexpected result: %v", err)
			}
		})
	}
}
func TestProjectGitHubDoesNotCreateRemoteAfterLocalCommitFailure(t *testing.T) {
	creationHost(t)
	posts := githubFixture(t, "octocat", 201, `{"name":"demo","owner":{"login":"octocat"},"private":true}`)
	original := creationGit
	swap(t, &creationGit, func(ctx context.Context, dir string, identity bool, args ...string) (string, error) {
		for _, arg := range args {
			if arg == "commit" {
				return "", fmt.Errorf("disk full")
			}
		}
		return original(ctx, dir, identity, args...)
	})
	_, err := dispatch([]string{"project", "create", "app", "--github-owner", "octocat", "--github-repo", "demo"})
	if err == nil || *posts != 0 {
		t.Fatalf("created remote before local commit: %d %v", *posts, err)
	}
}
