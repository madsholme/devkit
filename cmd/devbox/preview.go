package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// restartLive makes live previews come back after a reboot; devbox stop still keeps them stopped.
// Services may override this, e.g. a one-shot seed service with restart: "no".
func restartLive(service map[string]any) {
	if _, ok := service["restart"]; !ok {
		service["restart"] = "unless-stopped"
	}
}

// up starts a saved preview, adding the restart policy to live previews saved without it.
func up(path string) error {
	saved, err := readCompose(path)
	if err != nil {
		return err
	}
	services := object(saved, "services")
	missing := false
	for _, s := range services {
		service := asObject(s)
		if _, has := service["restart"]; !has && str(object(service, "labels"), "devbox.mode") == "live" {
			missing = true
		}
	}
	if missing {
		for _, s := range services {
			restartLive(asObject(s))
		}
		if err := writeCompose(path, saved); err != nil {
			return err
		}
	}
	return composeRun(path, "up", "-d", "--wait", "--wait-timeout", "120")
}

var composeRun = func(path string, args ...string) error {
	dir := filepath.Dir(path)
	_, err := runCommand(append([]string{"docker", "compose", "--project-directory", dir,
		"-f", path, "-p", filepath.Base(dir)}, args...), runOpts{})
	return err
}

func envValue(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if k, v, _ := strings.Cut(env[i], "="); k == key {
			return v
		}
	}
	return ""
}

func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

func resolve(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	abs, _ := filepath.Abs(path)
	return abs
}

var render = func(root string, cfg projectConfig, mode string, env []string, metadata map[string]string) (map[string]any, error) {
	filename := cfg.Compose[mode]
	if filename == "" {
		filename = "compose." + mode + ".yaml"
	}
	source := resolve(filepath.Join(root, filename))
	if !within(source, resolve(root)) {
		return nil, fail("Compose file must be inside the worktree.")
	}
	preview := envValue(env, "DEVBOX_PREVIEW")
	out, err := runCommand([]string{"docker", "compose", "--project-directory", root, "-f", source,
		"-p", preview, "config", "--format", "json"}, runOpts{dir: root, env: env, capture: true})
	if err != nil {
		return nil, err
	}
	result, err := decodeObject([]byte(out))
	if err != nil {
		return nil, err
	}
	services := object(result, "services")
	web := cfg.webService()
	if _, ok := services[web]; !ok {
		return nil, fail("Compose file has no %q web service.", web)
	}
	for _, name := range sortedKeys(services) {
		service := asObject(services[name])
		if truthy(service["container_name"]) || truthy(service["ports"]) || truthy(service["network_mode"]) {
			return nil, fail("Task services must not set container_name, published ports, or network_mode.")
		}
		if mode == "review" {
			bind := false
			volumes, _ := service["volumes"].([]any)
			for _, v := range volumes {
				bind = bind || str(asObject(v), "type") == "bind"
			}
			if truthy(service["build"]) || bind {
				return nil, fail("Review services must use built images and must not bind-mount host files.")
			}
		}
		if mode == "live" {
			restartLive(service)
		}
		labels := ensureObject(service, "labels")
		for k, v := range metadata {
			labels[k] = v
		}
		labels["traefik.enable"] = "false"
	}
	for _, v := range object(result, "volumes") {
		volume := asObject(v)
		if truthy(volume["external"]) || !strings.HasPrefix(str(volume, "name"), preview+"_") {
			return nil, fail("Task volumes must be Compose-scoped; shared/external volumes are not allowed.")
		}
	}
	for key, n := range object(result, "networks") {
		network := asObject(n)
		if key != "proxy" && (truthy(network["external"]) || !strings.HasPrefix(str(network, "name"), preview+"_")) {
			return nil, fail("Only the proxy network may be shared between previews.")
		}
	}
	front := asObject(services[web])
	ensureObject(front, "networks")["proxy"] = nil
	ensureObject(result, "networks")["proxy"] = map[string]any{"external": true, "name": "devbox-proxy"}
	router := preview
	labels := object(front, "labels")
	for k, v := range map[string]string{
		"traefik.enable": "true", "traefik.docker.network": "devbox-proxy",
		"traefik.http.routers." + router + ".rule":                      fmt.Sprintf("Host(`%s`)", envValue(env, "DEVBOX_HOST")),
		"traefik.http.routers." + router + ".entrypoints":               "websecure",
		"traefik.http.routers." + router + ".tls":                       "true",
		"traefik.http.routers." + router + ".service":                   router,
		"traefik.http.services." + router + ".loadbalancer.server.port": cfg.webPort(),
	} {
		labels[k] = v
	}
	if mode == "review" {
		out, err := capture("docker", "image", "inspect", envValue(env, "DEVBOX_IMAGE"))
		if err != nil {
			return nil, err
		}
		var images []struct {
			ID string `json:"Id"`
		}
		if err := json.Unmarshal([]byte(out), &images); err != nil || len(images) == 0 {
			return nil, fail("Could not inspect the built image %s.", envValue(env, "DEVBOX_IMAGE"))
		}
		// Run precisely the built web image even if its convenient tag is later changed.
		front["image"] = images[0].ID
		front["pull_policy"] = "never"
		labels["devbox.image"] = images[0].ID
	}
	return result, nil
}

func preparePreview(mode, ref string) (err error) {
	root, common, err := repository()
	if err != nil {
		return err
	}
	source := root
	cfg, err := loadConfig(root)
	if err != nil {
		return err
	}
	if ref == "" {
		ref = "HEAD"
	}
	commit, err := git(root, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return err
	}
	branch, identity := "", commit
	if mode == "live" {
		if branch, identity, err = liveIdentity(root); err != nil {
			return err
		}
	}
	// A review is named after the current branch when the commit is on it; one commit has one review.
	label := branch
	if mode == "review" && exec.Command("git", "-C", root, "merge-base", "--is-ancestor", commit, "HEAD").Run() == nil {
		if label, _, err = liveIdentity(root); err != nil {
			return err
		}
	}
	saved, err := savedPreviews(common)
	if err != nil {
		return err
	}
	name := findSaved(saved, cfg.Project, mode, identity)
	if name == "" {
		if name, err = newPreviewName(common, cfg.Project, mode, label, commit, identity); err != nil {
			return err
		}
	}
	state := runDir(common, name)
	path := filepath.Join(state, "compose.json")
	if err := assertOwner(name, common); err != nil {
		return err
	}
	exists := isFile(path)
	previous := ""
	if exists {
		previous = saved[name]["devbox.worktree"]
	}
	if mode == "live" && previous != "" && previous != root {
		trees, err := worktreesOf(root)
		if err != nil {
			return err
		}
		if _, ok := branchOf(trees, previous); ok {
			return fail("Preview %s belongs to the worktree %s.", name, previous)
		}
		// The branch moved to a new worktree: re-render the source mount, keep the data volumes.
		fmt.Fprintf(os.Stderr, "Rebinding %s from removed worktree %s.\n", name, previous)
	} else if exists {
		// Reusing a preview never reseeds or rebuilds it. Destroy explicitly first.
		if err := up(path); err != nil {
			return err
		}
		project, err := readCompose(path)
		if err != nil {
			return err
		}
		for _, s := range object(project, "services") {
			if url := str(object(asObject(s), "labels"), "devbox.url"); url != "" {
				fmt.Fprintln(stdout, url)
				break
			}
		}
		return nil
	}
	if err := os.MkdirAll(state, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(state, 0o700); err != nil {
		return err
	}
	checkout := ""
	defer func() {
		if checkout == "" {
			return
		}
		if _, statErr := os.Stat(checkout); statErr == nil {
			// Never force removal of an altered worktree.
			if _, rmErr := runCommand([]string{"git", "--git-dir", common, "worktree", "remove", checkout}, runOpts{}); rmErr != nil && err == nil {
				err = rmErr
			}
		}
	}()
	if mode == "review" {
		target := filepath.Join(common, "devbox", "checkouts", name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if _, statErr := os.Stat(target); !errors.Is(statErr, fs.ErrNotExist) {
			return fail("Review checkout already exists: %s. Inspect it before retrying.", target)
		}
		if _, err := runCommand([]string{"git", "-C", root, "worktree", "add", "--detach", target, commit}, runOpts{}); err != nil {
			return err
		}
		checkout = target
		atCommit, err := loadConfig(checkout)
		if err != nil {
			return err
		}
		if atCommit.Project != cfg.Project {
			return fail("Selected commit has a different project name.")
		}
		cfg, root = atCommit, checkout
	}
	dom, err := domain()
	if err != nil {
		return err
	}
	host := name + "." + dom
	env := withEnv(os.Environ(), map[string]string{
		"DEVBOX_PREVIEW": name, "DEVBOX_HOST": host, "DEVBOX_URL": "https://" + host,
		"DEVBOX_COMMIT": commit, "DEVBOX_MODE": mode, "DEVBOX_IMAGE": "devbox/" + cfg.Project + ":" + commit,
		"COMPOSE_PROJECT_NAME": name,
	})
	metadata := map[string]string{
		managedLabel: "true", "devbox.preview": name, "devbox.mode": mode,
		"devbox.project": cfg.Project, "devbox.repository": common,
		"devbox.commit": commit, "devbox.branch": branch, "devbox.worktree": source,
		"devbox.identity": identity, "devbox.url": "https://" + host,
	}
	if mode == "review" {
		if _, err := hook(root, cfg, "prepare", env); err != nil {
			return err
		}
		if _, err := hook(root, cfg, "check", env); err != nil {
			return err
		}
		status, err := git(root, "status", "--porcelain", "--untracked-files=all")
		if err != nil {
			return err
		}
		if status != "" {
			return fail("prepare/check changed the review checkout. Keep build inputs committed.")
		}
		built, err := hook(root, cfg, "build", env)
		if err != nil {
			return err
		}
		if !built {
			return fail("devbox.json has no build hook; review needs one to produce $DEVBOX_IMAGE.")
		}
	} else if _, err := hook(root, cfg, "prepare", env); err != nil {
		return err
	}
	resolved, err := render(root, cfg, mode, env, metadata)
	if err != nil {
		return err
	}
	if err := writeCompose(path, resolved); err != nil {
		return err
	}
	if err := composeRun(path, "up", "-d", "--wait", "--wait-timeout", "120"); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "https://"+host)
	fmt.Fprintf(stdout, "preview=%s mode=%s commit=%s\n", name, mode, commit)
	return nil
}

var destroyPreview = func(path string) error {
	if err := composeRun(path, "down", "--volumes", "--remove-orphans"); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return os.Remove(filepath.Dir(path))
}

var previewID = regexp.MustCompile(`^[a-z0-9-]{1,63}$`)

func lifecycle(action, name string) error {
	root, common, err := repository()
	if err != nil {
		return err
	}
	if name == "" {
		if name, err = currentPreview(root, common); err != nil {
			return err
		}
	} else if !previewID.MatchString(name) {
		return fail("Use an exact preview ID from devbox ls.")
	}
	if err := assertOwner(name, common); err != nil {
		return err
	}
	path := filepath.Join(runDir(common, name), "compose.json")
	if !isFile(path) {
		return fail("No saved preview configuration in this repository.")
	}
	switch action {
	case "stop":
		err = composeRun(path, "stop")
	case "start":
		err = up(path)
	default:
		err = destroyPreview(path)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s: %s\n", action, name)
	return nil
}

// gc destroys previews whose source worktree no longer exists.
func gc(apply bool) error {
	root, common, err := repository()
	if err != nil {
		return err
	}
	trees, err := worktreesOf(root)
	if err != nil {
		return err
	}
	saved, err := savedPreviews(common)
	if err != nil {
		return err
	}
	type stalePreview struct{ name, mode, source string }
	var stale []stalePreview
	var unknown []string
	for _, name := range sortedKeys(saved) {
		labels := saved[name]
		path, exists, ok := sourceWorktree(labels, trees)
		if !ok {
			unknown = append(unknown, name)
		} else if !exists {
			mode := labels["devbox.mode"]
			if mode == "" {
				mode = "?"
			}
			stale = append(stale, stalePreview{name, mode, path})
		}
	}
	if len(unknown) > 0 {
		fmt.Fprintln(stdout, "Kept (source worktree not recorded; destroy explicitly if unneeded):")
		for _, name := range unknown {
			fmt.Fprintf(stdout, "  %s\n", name)
		}
	}
	if len(stale) == 0 {
		fmt.Fprintln(stdout, "No previews with a removed worktree.")
		return nil
	}
	if apply {
		fmt.Fprintln(stdout, "Destroying:")
	} else {
		fmt.Fprintln(stdout, "Would destroy (worktree no longer exists):")
	}
	for _, s := range stale {
		fmt.Fprintf(stdout, "  %s\t%s\t%s\n", s.name, s.mode, s.source)
	}
	if !apply {
		fmt.Fprintln(stdout, "Run devbox gc --yes to remove their containers, networks, volumes and run dirs.")
		return nil
	}
	for _, s := range stale {
		if err := assertOwner(s.name, common); err != nil {
			return err
		}
		if err := destroyPreview(filepath.Join(runDir(common, s.name), "compose.json")); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "Destroyed %d preview(s).\n", len(stale))
	return nil
}

// liveHead is the current commit of a live preview's worktree; its code is the worktree, not the start commit.
func liveHead(labels map[string]string, treesByRepo map[string][]worktree) string {
	repo := labels["devbox.repository"]
	if labels["devbox.mode"] != "live" || repo == "" {
		return ""
	}
	if _, ok := treesByRepo[repo]; !ok {
		trees, _ := worktreesOf(repo)
		treesByRepo[repo] = trees
	}
	path, exists, ok := sourceWorktree(labels, treesByRepo[repo])
	if !ok || !exists {
		return ""
	}
	head, _ := git(path, "rev-parse", "HEAD")
	return head
}

func listing() error {
	containers, err := dockerContainers()
	if err != nil {
		return err
	}
	seen, trees := map[string]bool{}, map[string][]worktree{}
	fmt.Fprintln(stdout, "PREVIEW\tMODE\tSTATUS\tCOMMIT\tURL")
	for _, c := range containers {
		labels := c.Config.Labels
		if labels["traefik.enable"] != "true" || seen[labels["devbox.preview"]] {
			continue
		}
		seen[labels["devbox.preview"]] = true
		commit := liveHead(labels, trees)
		if commit == "" {
			commit = labels["devbox.commit"]
		}
		fmt.Fprintln(stdout, strings.Join([]string{labels["devbox.preview"], labels["devbox.mode"],
			containerStatus(c.State), commit[:min(12, len(commit))], labels["devbox.url"]}, "\t"))
	}
	return nil
}

func publish(ref string) error {
	root, common, err := repository()
	if err != nil {
		return err
	}
	cfg, err := loadConfig(root)
	if err != nil {
		return err
	}
	commit, err := git(root, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return err
	}
	saved, err := savedPreviews(common)
	if err != nil {
		return err
	}
	name := findSaved(saved, cfg.Project, "review", commit)
	if name == "" {
		return fail("Build a local review first. publish requires its existing reviewed image.")
	}
	containers, err := containersFor(name)
	if err != nil {
		return err
	}
	var images []string
	for _, c := range containers {
		if image := c.Config.Labels["devbox.image"]; image != "" {
			images = append(images, image)
		}
	}
	if len(images) != 1 {
		return fail("Build a local review first. publish requires its existing reviewed image.")
	}
	if err := assertOwner(name, common); err != nil {
		return err
	}
	env := withEnv(os.Environ(), map[string]string{
		"DEVBOX_COMMIT": commit, "DEVBOX_IMAGE_ID": images[0], "DEVBOX_PREVIEW": name})
	// An explicit per-project adapter owns registry and Dokploy configuration.
	published, err := hook(root, cfg, "publish", env)
	if err != nil {
		return err
	}
	if !published {
		return fail("Team publishing is not configured: add a publish hook to devbox.json.")
	}
	return nil
}

// liveWeb returns the running web container of the current worktree's live preview.
func liveWeb(root string) (string, error) {
	cfg, err := loadConfig(root)
	if err != nil {
		return "", err
	}
	containers, err := dockerContainers()
	if err != nil {
		return "", err
	}
	for _, c := range containers {
		labels := c.Config.Labels
		if c.State.Running && labels["devbox.mode"] == "live" && labels["devbox.worktree"] == root &&
			labels["com.docker.compose.service"] == cfg.webService() {
			return c.ID, nil
		}
	}
	return "", fail("No running live preview for this worktree. Run devbox live first.")
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// execLive runs a command in the live web container and returns its exit code.
func execLive(command []string) (int, error) {
	root, _, err := repository()
	if err != nil {
		return 1, err
	}
	id, err := liveWeb(root)
	if err != nil {
		return 1, err
	}
	// Run as the host user so files written into the bind-mounted source stay yours.
	args := []string{"exec", "-i", "-u", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())}
	if isTerminal(os.Stdin) && isTerminal(os.Stdout) {
		args = append(args, "-t")
	}
	cmd := exec.Command("docker", append(append(args, id), command...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}

func logs(follow bool) error {
	root, _, err := repository()
	if err != nil {
		return err
	}
	id, err := liveWeb(root)
	if err != nil {
		return err
	}
	args := []string{"docker", "logs", "--tail", "200"}
	if follow {
		args = append(args, "--follow")
	}
	_, err = runCommand(append(args, id), runOpts{})
	return err
}
