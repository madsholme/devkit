package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"
)

var githubOwnerName = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,37}[a-zA-Z0-9])?$`)
var githubRepositoryName = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,100}$`)

func validateProjectGitHub(opts createProjectOptions) error {
	if opts.githubOwner == "" && opts.githubRepo == "" && !opts.githubPublic && !opts.githubPush {
		return nil
	}
	if !githubOwnerName.MatchString(opts.githubOwner) || strings.Contains(opts.githubOwner, "--") || !githubRepositoryName.MatchString(opts.githubRepo) || opts.githubRepo == "." || opts.githubRepo == ".." || strings.HasSuffix(strings.ToLower(opts.githubRepo), ".git") {
		return fail("GitHub creation requires a valid --github-owner and --github-repo (repository name without .git).")
	}
	if opts.remote != "" {
		return fail("Choose either a Git remote URL or Create GitHub repository, not both.")
	}
	return nil
}

// Only trusted host configuration chooses the secret source. Requests never carry tokens.
func projectGitHubToken() (string, error) {
	raw, err := readConfigFile()
	if err != nil {
		return "", fail("Cannot read the host GitHub token configuration.")
	}
	integrations, _ := raw["integrations"].(map[string]any)
	github, _ := integrations["github"].(map[string]any)
	data, _ := json.Marshal(github["token"])
	var source secretSource
	if json.Unmarshal(data, &source) != nil {
		return "", fail("Configure the host GitHub token before creating a repository.")
	}
	token, err := source.read()
	if err != nil || token == "" || strings.ContainsAny(token, "\r\n\x00") {
		return "", fail("GitHub token unavailable; configure integrations.github.token or unlock its vault before creating a repository.")
	}
	return token, nil
}

var projectGitHubClient = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func projectGitHubRequest(ctx context.Context, token, method, path string, body any, result any) error {
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.github.com"+path, payload)
	if err != nil {
		return fail("Cannot prepare GitHub request.")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := projectGitHubClient.Do(req)
	if err != nil {
		return fail("GitHub request unavailable or timed out.")
	}
	defer resp.Body.Close()
	expected := http.StatusOK
	if method == "POST" {
		expected = http.StatusCreated
	}
	if resp.StatusCode != expected {
		return fail("GitHub rejected the request (HTTP %d); check token permissions, owner access and repository availability.", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 || json.Unmarshal(data, result) != nil {
		return fail("GitHub returned an invalid response.")
	}
	return nil
}

// Preflight reads only. The authenticated user's namespace and organizations are distinct API routes.
func projectGitHubPreflight(ctx context.Context, opts createProjectOptions) (token, endpoint string, err error) {
	token, err = projectGitHubToken()
	if err != nil {
		return
	}
	var user struct{ Login string }
	err = projectGitHubRequest(ctx, token, "GET", "/user", nil, &user)
	if err != nil {
		return
	}
	if strings.EqualFold(user.Login, opts.githubOwner) {
		return token, "/user/repos", nil
	}
	var owner struct{ Login, Type string }
	err = projectGitHubRequest(ctx, token, "GET", "/users/"+opts.githubOwner, nil, &owner)
	if err != nil {
		return
	}
	if !strings.EqualFold(owner.Login, opts.githubOwner) || owner.Type != "Organization" {
		return "", "", fail("GitHub owner must be the authenticated user or an organization that allows this token to create repositories.")
	}
	return token, "/orgs/" + opts.githubOwner + "/repos", nil
}

func createProjectGitHub(ctx context.Context, opts createProjectOptions, token, endpoint string) (string, error) {
	remote := "https://github.com/" + opts.githubOwner + "/" + opts.githubRepo + ".git"
	var repo struct {
		Name    string
		Owner   struct{ Login string }
		Private *bool
	}
	err := projectGitHubRequest(ctx, token, "POST", endpoint, map[string]any{"name": opts.githubRepo, "private": !opts.githubPublic, "auto_init": false}, &repo)
	if err == nil && (!strings.EqualFold(repo.Owner.Login, opts.githubOwner) || repo.Name != opts.githubRepo || repo.Private == nil || *repo.Private != !opts.githubPublic) {
		err = fail("GitHub response did not match the requested owner, name or visibility.")
	}
	if err != nil {
		return "", fail("%v Inspect https://github.com/%s/%s before retrying: repository creation may have succeeded. No push was attempted.", err, opts.githubOwner, opts.githubRepo)
	}
	happened("github-create", opts.name, "Created "+remote)
	fmt.Fprintf(stdout, "Created GitHub repository https://github.com/%s/%s\n", opts.githubOwner, opts.githubRepo)
	return remote, nil
}

// The token is ephemeral process configuration, never an argument or a persisted Git setting.
func projectGitHubPushCommand(ctx context.Context, dir, remote, token string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", "-c", "core.hooksPath=/dev/null", "-c", "credential.helper=", "-c", "http.followRedirects=false", "-c", "protocol.allow=never", "-c", "protocol.https.allow=always", "push", "--porcelain", "--set-upstream", "origin", "main:refs/heads/main")
	cmd.Dir = dir
	cmd.Env = append(creationGitEnv(false), "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http."+remote+".extraHeader", "GIT_CONFIG_VALUE_0=Authorization: Basic "+base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token)))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 250 * time.Millisecond
	return cmd
}

var projectGitHubPush = func(ctx context.Context, dir, remote, token string) error {
	// Discard external output: remote errors and Git tracing must not enter job logs.
	return projectGitHubPushCommand(ctx, dir, remote, token).Run()
}
