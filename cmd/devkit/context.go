package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type previewView struct {
	ID          string                 `json:"id"`
	Mode        string                 `json:"mode"`
	URL         string                 `json:"url"`
	SavedCommit string                 `json:"saved_commit"`
	SourceHead  *string                `json:"source_head"`
	Saved       bool                   `json:"saved"`
	Containers  []previewContainerView `json:"containers"`
}
type previewContainerView struct {
	ID      string `json:"id"`
	Service string `json:"service"`
	State   string `json:"state"`
	ImageID string `json:"image_id"`
}

func previewProjection(id string, l map[string]string) previewView {
	return previewView{ID: id, Mode: l["devkit.mode"], URL: l["devkit.url"], SavedCommit: l["devkit.commit"], Containers: []previewContainerView{}}
}
func projectContainerView(c container) previewContainerView {
	return previewContainerView{c.ID, c.Config.Labels["com.docker.compose.service"], containerStatus(c.State), c.Image}
}
func listingJSON() (int, error) {
	r := newReport("ls", map[string]any{"previews": []previewView{}})
	rows, err := dockerContainers()
	if err != nil {
		r.unavailable("containers.inventory", "docker", "Docker preview inventory is unavailable.")
		return r.write(true)
	}
	found := map[string]previewView{}
	trees := map[string][]worktree{}
	for _, c := range rows {
		l := c.Config.Labels
		id := l["devkit.preview"]
		if !isWeb(l) || id == "" {
			continue
		}
		view := previewProjection(id, l)
		if head := liveHead(l, trees); head != "" {
			view.SourceHead = &head
		}
		found[id] = view
	}
	for _, c := range rows {
		id := c.Config.Labels["devkit.preview"]
		view, ok := found[id]
		if !ok || c.Config.Labels["devkit.mode"] != view.Mode {
			continue
		}
		view.Containers = append(view.Containers, projectContainerView(c))
		found[id] = view
	}
	views := []previewView{}
	for _, id := range sortedKeys(found) {
		v := found[id]
		sort.Slice(v.Containers, func(i, j int) bool { return v.Containers[i].ID < v.Containers[j].ID })
		views = append(views, v)
	}
	r.Data = map[string]any{"previews": views}
	return r.write(true)
}
func contextCLI(args []string) (int, error) {
	p, v, e := splitArgs(args, nil, map[string]bool{"--json": true})
	if e != nil || len(p) > 0 {
		return 2, usageError("Use context [--json]")
	}
	_, asJSON := v["--json"]
	root := currentRecipeRoot()
	data := map[string]any{"checkout": root, "recipe": nil, "git": nil, "previews": []previewView{}, "vms": []vmOperationView{}}
	r := newReport("context", data)
	cfg, err := loadConfig(root)
	if err != nil {
		r.add("recipe.invalid", "fail", "recipe", "No valid recipe is available.")
	} else {
		data["recipe"] = configFile(root)
	}
	gitRoot, common, gitErr := repository()
	found := map[string]previewView{}
	if gitErr == nil {
		head, herr := git(gitRoot, "rev-parse", "HEAD")
		branch, identity, ierr := liveIdentity(gitRoot)
		dirty, derr := sourceStatus(gitRoot)
		if herr != nil || ierr != nil || derr != nil {
			r.unavailable("git.identity", "checkout", "Git identity or worktree status is unavailable.")
		} else {
			data["git"] = map[string]any{"head": head, "branch": branch, "identity": identity, "dirty": dirty != ""}
		}
		saved, serr := savedPreviews(common)
		trees, terr := worktreesOf(gitRoot)
		if serr != nil || terr != nil {
			r.unavailable("previews.saved", "checkout", "Saved preview associations are unavailable.")
		} else {
			for id, l := range saved {
				path, _, ok := sourceWorktree(l, trees)
				if !ok || resolve(path) != resolve(gitRoot) || l["devkit.repository"] != common {
					continue
				}
				view := previewProjection(id, l)
				view.Saved = true
				if view.Mode == "live" && herr == nil {
					view.SourceHead = &head
				}
				found[id] = view
			}
		}
	}
	rows, err := dockerContainers()
	if err != nil {
		r.unavailable("containers.inventory", "docker", "Docker inventory is unavailable; saved metadata is not runtime evidence.")
	} else {
		for _, c := range rows {
			l := c.Config.Labels
			if gitErr != nil || l["devkit.repository"] != common || resolve(l["devkit.worktree"]) != resolve(gitRoot) {
				continue
			}
			id := l["devkit.preview"]
			view, ok := found[id]
			if !ok {
				view = previewProjection(id, l)
			}
			view.Containers = append(view.Containers, projectContainerView(c))
			found[id] = view
		}
	}
	previews := []previewView{}
	for _, id := range sortedKeys(found) {
		v := found[id]
		sort.Slice(v.Containers, func(i, j int) bool { return v.Containers[i].ID < v.Containers[j].ID })
		previews = append(previews, v)
	}
	data["previews"] = previews
	if len(cfg.VMs) > 0 {
		validateProjectVMs(&r, root, cfg, "", false)
	}
	return r.write(asJSON)
}

// Safe messages are selected at check creation, independent of untrusted details.
func safeDoctorMessage(name, status string) string {
	subject := map[string]string{"config": "Host configuration", "docker": "Docker and Compose access", "tailscale": "Tailscale address", "dns": "Preview DNS", "network": "Proxy network", "traefik": "Traefik configuration and runtime", "dashboard": "Dashboard service", "linger": "User service persistence", "harness": "Agent harness", "share": "Public sharing configuration and resources", "history": "History storage", "vault": "Managed secrets service", "agent-context": "Project secret context", "agent-guidance": "Project agent guidance", "agent-skills": "Bundled agent skills", "agent-cli": "Devkit CLI"}[name]
	if subject == "" {
		subject = "Host diagnostic"
	}
	switch status {
	case "ok":
		return subject + " check passed."
	case "warn":
		return subject + " needs attention; inspect the local text diagnostic."
	default:
		return subject + " check failed or is unavailable; inspect the local text diagnostic."
	}
}
func doctorReport(checks []check, agents bool) operationReport {
	command := "doctor"
	if agents {
		command += " --agents"
	}
	r := newReport(command, nil)
	for _, c := range checks {
		status := c.Status
		if agents && status != "ok" {
			status = "fail"
		}
		r.add(c.Code, status, c.Name, c.SafeMessage)
		if c.Status == "fail" {
			r.Complete = false
		}
	}
	return r
}
func doctorJSON(agents bool) (int, error) {
	if !agents {
		return doctorReport(append(doctorChecks(), vaultDoctorChecks()...), false).write(true)
	}
	checks := []check{}
	home, err := os.UserHomeDir()
	if err != nil {
		checks = append(checks, bad("agent-skills", "Home unavailable"))
	} else {
		for _, agent := range []string{".agents", ".claude"} {
			for _, skill := range bundledAgentSkills() {
				status := agentSkillStatus(filepath.Join(home, agent, "skills", skill.name), skill.version, skill.content)
				c := ok("agent-skills", "current")
				if status != "current" {
					c = warn("agent-skills", "Needs repair")
				}
				c.Code = "agents." + strings.TrimPrefix(agent, ".") + "." + skill.name
				c.SafeMessage = "Bundled skill status: " + status
				checks = append(checks, c)
			}
		}
	}
	if _, err := exec.LookPath("devkit"); err != nil {
		checks = append(checks, bad("agent-cli", "Missing"))
	} else {
		checks = append(checks, ok("agent-cli", "Available"))
	}
	if root, err := os.Getwd(); err == nil {
		checks = append(checks, agentProjectChecks(root)...)
	}
	return doctorReport(checks, true).write(true)
}

func sourceStatus(root string) (string, error) {
	return git(root, "--no-optional-locks", "-c", "core.fsmonitor=false", "status", "--porcelain", "--ignore-submodules=all")
}
