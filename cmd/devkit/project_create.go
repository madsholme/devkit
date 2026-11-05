package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	projecttemplate "github.com/steenfeldt/devkit/project-template"
)

const maxAgentNotes = 16 << 10

var scpRemote = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*@[a-zA-Z0-9][a-zA-Z0-9.-]*:[^\s-][^\s]*$`)

type createProjectOptions struct{ name, remote, notes, expectedParent string }

func validateProjectCreation(opts createProjectOptions) error {
	if !projectSlug.MatchString(opts.name) {
		return fail("Use a project name of 1–32 lowercase letters, digits or hyphens, starting with a letter or digit.")
	}
	if len(opts.notes) > maxAgentNotes || !utf8.ValidString(opts.notes) || strings.ContainsFunc(opts.notes, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }) {
		return fail("Agent instructions must be valid text of at most 16 KiB.")
	}
	if opts.remote == "" {
		return nil
	}
	invalid := func() error {
		return fail("Use an HTTPS or SSH Git remote without passwords, tokens, query strings or fragments.")
	}
	if len(opts.remote) > 2048 || strings.ContainsFunc(opts.remote, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return invalid()
	}
	if scpRemote.MatchString(opts.remote) && !strings.ContainsAny(opts.remote, "?#") {
		return nil
	}
	u, err := url.Parse(opts.remote)
	if err != nil || (u.Scheme != "https" && u.Scheme != "ssh") || (u.Hostname() == "" || strings.HasPrefix(u.Hostname(), "-")) || u.Path == "" || u.Path == "/" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return invalid()
	}
	if u.User != nil {
		if _, password := u.User.Password(); password || u.Scheme != "ssh" || strings.HasPrefix(u.User.Username(), "-") || strings.ContainsFunc(u.User.Username(), func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) {
			return invalid()
		}
	}
	return nil
}

func creationParent(expected string) (string, error) {
	cfg, err := loadHostConfig()
	if err != nil {
		return "", err
	}
	if cfg.ProjectsDir == "" {
		return "", fail("Configure projects_dir on this host before creating a project.")
	}
	parent := resolve(expandHome(cfg.ProjectsDir))
	if expected != "" && expected != parent {
		return "", fail("The configured projects directory changed; refresh and try again.")
	}
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return "", fail("The configured projects directory is unavailable; create it on the host first.")
	}
	return parent, nil
}

// Preflight is repeated by the CLI at execution; the dashboard never chooses a parent path.
func planProjectCreate(req actionRequest) (plannedAction, error) {
	opts := createProjectOptions{name: req.Name, remote: req.Remote, notes: req.AgentNotes}
	if err := validateProjectCreation(opts); err != nil {
		return plannedAction{}, err
	}
	parent, err := creationParent("")
	if err != nil {
		return plannedAction{}, err
	}
	if _, err := os.Lstat(filepath.Join(parent, opts.name)); !os.IsNotExist(err) {
		return plannedAction{}, fail("A file or folder named %s already exists, or the destination cannot be inspected. Nothing was changed.", opts.name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := creationIdentity(ctx, parent); err != nil {
		return plannedAction{}, err
	}
	args := []string{"project", "create", opts.name, "--projects-dir", parent, "--agents-file", "-"}
	if opts.remote != "" {
		args = append(args, "--remote", opts.remote)
	}
	return plannedAction{args: args, dir: parent, target: "create project " + filepath.Join(parent, opts.name), title: "Create project " + opts.name, stdin: opts.notes}, nil
}

func projectCreateCLI(args []string) (int, error) {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		return 0, helpRequested{}
	}
	if len(args) == 0 || args[0] != "create" {
		return 2, usageError("usage: devkit project create NAME [--remote URL] [--agents-file FILE|-]")
	}
	positional, values, err := splitArgs(args[1:], map[string]bool{"--remote": true, "--agents-file": true, "--projects-dir": true}, nil)
	if err != nil {
		return 2, err
	}
	if len(positional) != 1 {
		return 2, usageError("project create needs exactly one project name")
	}
	opts := createProjectOptions{name: positional[0], remote: values["--remote"], expectedParent: values["--projects-dir"]}
	if path, ok := values["--agents-file"]; ok {
		var reader io.Reader = os.Stdin
		if path != "-" {
			f, e := os.Open(path)
			if e != nil {
				return 1, fail("Cannot read the agent instructions file.")
			}
			defer f.Close()
			reader = f
		}
		data, e := io.ReadAll(io.LimitReader(reader, maxAgentNotes+1))
		if e != nil {
			return 1, fail("Cannot read agent instructions.")
		}
		opts.notes = string(data)
	}
	return 0, noted("project-create", opts.name, createProject(opts))
}

// Clear caller-selected Git repositories/indexes/config injection. Identity is read only
// from global configuration; creation then runs without inherited global/system config.
func creationGitEnv(identity bool) []string {
	env := []string{}
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if strings.HasPrefix(key, "GIT_") {
			if identity && key == "GIT_CONFIG_GLOBAL" {
				env = append(env, item)
			}
			continue
		}
		env = append(env, item)
	}
	env = append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never")
	if !identity {
		env = append(env, "GIT_CONFIG_GLOBAL="+os.DevNull)
	}
	return env
}

var creationGit = func(ctx context.Context, dir string, identity bool, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = creationGitEnv(identity)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 250 * time.Millisecond
	out, err := cmd.Output()
	if err != nil {
		return "", fail("Git initialization or initial commit failed; check Git installation, disk space and permissions.")
	}
	return strings.TrimSpace(string(out)), nil
}

func creationIdentity(ctx context.Context, parent string) (string, string, error) {
	author, err := creationGit(ctx, parent, true, "config", "--global", "--get", "user.name")
	if err != nil || author == "" {
		return "", "", fail("Configure git config --global user.name and user.email on the host before creating a project.")
	}
	email, err := creationGit(ctx, parent, true, "config", "--global", "--get", "user.email")
	if err != nil || email == "" || strings.ContainsAny(author+email, "\r\n\x00") {
		return "", "", fail("Configure valid git config --global user.name and user.email on the host before creating a project.")
	}
	return author, email, nil
}

func createProject(opts createProjectOptions) (result error) {
	if err := validateProjectCreation(opts); err != nil {
		return err
	}
	parent, err := creationParent(opts.expectedParent)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return fail("Cannot open the configured projects directory.")
	}
	defer root.Close()
	if _, err := root.Lstat(opts.name); !os.IsNotExist(err) {
		return fail("A file or folder named %s already exists, or the destination cannot be inspected. Nothing was changed.", opts.name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	author, email, err := creationIdentity(ctx, parent)
	if err != nil {
		return err
	}
	// Mkdir is the no-clobber reservation across all CLI/dashboard processes. No
	// failure path recursively deletes it: concurrent user edits must survive.
	if err := root.Mkdir(opts.name, 0755); err != nil {
		return fail("Cannot create %s: the destination already exists or is not writable. Nothing was overwritten.", opts.name)
	}
	destination := filepath.Join(parent, opts.name)
	defer func() {
		if result != nil {
			result = fail("%v Partial project retained at %s; inspect it before retrying. Existing files were not overwritten.", result, destination)
		}
	}()
	info, err := root.Lstat(opts.name)
	if err != nil || !info.IsDir() {
		return fail("The new project directory changed during creation.")
	}
	child, err := root.OpenRoot(opts.name)
	if err != nil {
		return fail("Cannot open the new project directory.")
	}
	defer child.Close()
	directory, err := child.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	pinned, err := directory.Stat()
	if err != nil || !os.SameFile(info, pinned) {
		return fail("The new project directory changed during creation.")
	}
	// Linux-only devkit: use the open directory through the parent process's fd,
	// so Git cannot be redirected by a rename/symlink replacing the visible path.
	gitDir := fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), directory.Fd())
	err = fs.WalkDir(projecttemplate.Files, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "." {
			return nil
		}
		if entry.IsDir() {
			return child.Mkdir(path, 0755)
		}
		data, e := projecttemplate.Files.ReadFile(path)
		if e != nil {
			return e
		}
		if path == "devkit.json" {
			var cfg map[string]json.RawMessage
			if e = json.Unmarshal(data, &cfg); e != nil {
				return e
			}
			cfg["project"], _ = json.Marshal(opts.name)
			data, e = json.MarshalIndent(cfg, "", "  ")
			if e != nil {
				return e
			}
			data = append(data, '\n')
		}
		if path == "AGENTS.md" && strings.TrimSpace(opts.notes) != "" {
			data = append(data, []byte("\n## Project-specific instructions\n\n"+opts.notes+"\n")...)
		}
		f, e := child.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if e != nil {
			return fail("Cannot copy template file %s without overwriting an existing file.", path)
		}
		_, e = f.Write(data)
		closeErr := f.Close()
		if e != nil {
			return e
		}
		return closeErr
	})
	if err != nil {
		return err
	}
	if err := child.Mkdir(".git", 0755); err != nil {
		return fail("Cannot reserve a new Git repository; a .git entry already exists or is not writable.")
	}
	git := func(args ...string) (string, error) {
		prefix := []string{"-c", "core.hooksPath=" + os.DevNull, "-c", "commit.gpgSign=false", "-c", "user.name=" + author, "-c", "user.email=" + email}
		return creationGit(ctx, gitDir, false, append(prefix, args...)...)
	}
	if _, err := git("init", "--template=", "--initial-branch=main"); err != nil {
		return err
	}
	if opts.remote != "" {
		if _, err := git("remote", "add", "origin", opts.remote); err != nil {
			return err
		}
	}
	if _, err := git("add", "--", "."); err != nil {
		return err
	}
	if _, err := git("commit", "--no-verify", "-m", "Initialize project from devkit template"); err != nil {
		return err
	}
	current, err := root.Lstat(opts.name)
	if err != nil || !os.SameFile(pinned, current) {
		return fail("The project path changed during creation; the initialized directory was retained.")
	}
	pinnedParent, pinErr := root.Stat(".")
	visibleParent, visibleErr := os.Stat(parent)
	if pinErr != nil || visibleErr != nil || !os.SameFile(pinnedParent, visibleParent) {
		return fail("The projects directory moved during creation.")
	}
	parentNow, err := creationParent(parent)
	if err != nil || parentNow != parent {
		return fail("The configured projects directory changed during creation.")
	}
	happened("project-create", opts.name, "Created project from template with an initial main commit.")
	fmt.Fprintf(stdout, "Created project %s at %s\nInitialized main with an initial commit.\n", opts.name, destination)
	if opts.remote != "" {
		fmt.Fprintln(stdout, "Configured origin locally; nothing was fetched or pushed.")
	}
	return nil
}
