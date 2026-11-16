package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"syscall"
)

type rebuildOperation struct {
	Preview   string                 `json:"preview"`
	Root      string                 `json:"root"`
	Common    string                 `json:"common"`
	Identity  string                 `json:"identity"`
	Head      string                 `json:"head"`
	Dirty     bool                   `json:"dirty"`
	Phase     string                 `json:"phase"`
	Built     bool                   `json:"built"`
	Stopped   bool                   `json:"stopped"`
	Old       map[string]any         `json:"old"`
	Candidate map[string]any         `json:"candidate"`
	Model     map[string]any         `json:"model"`
	Inputs    composeInputs          `json:"inputs"`
	Images    map[string]string      `json:"images"`
	Runtime   []previewContainerView `json:"runtime"`
}
type rebuildPlan struct {
	op     rebuildOperation
	report operationReport
}

func cloneModel(m map[string]any) (map[string]any, error) {
	b, e := json.Marshal(m)
	if e != nil {
		return nil, e
	}
	return decodeObject(b)
}
func sameModel(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

var credentialField = regexp.MustCompile(`(?i)(password|passwd|secret|token|user|connection|credential|private.?key|api.?key|database.?url|dsn)`)

// Deliberately compare normalized storage definitions, not just their counts.
func supportedRebuild(old, next map[string]any) error {
	for _, key := range []string{"name", "volumes", "networks", "secrets", "configs"} {
		if !sameModel(old[key], next[key]) {
			return fail("Storage, network, secret/config or Compose identity changes need an explicit migration.")
		}
	}
	before, after := object(old, "services"), object(next, "services")
	if !reflect.DeepEqual(sortedKeys(before), sortedKeys(after)) {
		return fail("Adding or removing services requires an explicit migration.")
	}
	for name, value := range before {
		a, b := asObject(value), asObject(after[name])
		for _, key := range []string{"volumes", "volumes_from", "networks", "network_mode", "ports", "devices", "device_cgroup_rules", "tmpfs", "secrets", "configs", "container_name", "privileged", "pid", "ipc", "userns_mode"} {
			if !sameModel(a[key], b[key]) {
				return fail("Mount, network or host access changes need an explicit migration.")
			}
		}
		volumes, _ := a["volumes"].([]any)
		for _, v := range volumes {
			m := asObject(v)
			if (str(m, "type") != "volume" && str(m, "type") != "bind") || str(m, "source") == "" {
				return fail("Anonymous or unsupported storage cannot be rebuilt.")
			}
		}
		if truthy(a["build"]) != truthy(b["build"]) {
			return fail("Changing service build ownership is unsupported.")
		}
		if !truthy(b["build"]) && (firstOf(str(object(a, "labels"), "devkit.rebuild-source-image"), str(a, "image")) != str(b, "image") || !sameModel(a["environment"], b["environment"]) || !sameModel(a["command"], b["command"]) || !sameModel(a["entrypoint"], b["entrypoint"])) {
			return fail("Unbuilt service image or startup changes need an explicit migration.")
		}
		envA, envB := object(a, "environment"), object(b, "environment")
		for _, env := range []map[string]any{envA, envB} {
			for key := range env {
				if credentialField.MatchString(key) && !sameModel(envA[key], envB[key]) {
					return fail("Credential changes require an explicit account migration.")
				}
			}
		}
		labelsA, labelsB := object(a, "labels"), object(b, "labels")
		for _, labels := range []map[string]any{labelsA, labelsB} {
			for key := range labels {
				if (strings.HasPrefix(key, "traefik.") || strings.HasPrefix(key, "devkit.media.")) && !sameModel(labelsA[key], labelsB[key]) {
					return fail("Routing or media changes need a separate migration.")
				}
			}
		}
	}
	for _, v := range object(next, "services") {
		build := object(asObject(v), "build")
		if truthy(build["tags"]) || truthy(build["cache_to"]) || truthy(build["ssh"]) {
			return fail("Build tags, cache exports and SSH forwarding require separate preparation.")
		}
	}
	if !composeHealth(next) {
		return fail("Every service needs an enabled health check before rebuild.")
	}
	return nil
}
func localRebuildBuilds(root string, model map[string]any) error {
	for _, v := range object(model, "services") {
		build := object(asObject(v), "build")
		if len(build) == 0 {
			continue
		}
		contextPath := str(build, "context")
		if contextPath == "" || !filepath.IsAbs(contextPath) || !within(resolve(contextPath), resolve(root)) || truthy(build["additional_contexts"]) {
			return fail("Rebuild requires local build contexts inside the checkout; additional contexts need separate preparation.")
		}
		if dockerfile := str(build, "dockerfile"); dockerfile != "" && !within(resolve(filepath.Join(contextPath, dockerfile)), resolve(root)) {
			return fail("Dockerfile must remain inside the checkout.")
		}
	}
	return nil
}
func recordedCandidateMatches(op rebuildOperation) bool {
	candidate, err := cloneModel(op.Candidate)
	if err != nil {
		return false
	}
	if !reflect.DeepEqual(sortedKeys(object(candidate, "services")), sortedKeys(object(op.Model, "services"))) {
		return false
	}
	for name, v := range object(candidate, "services") {
		service := asObject(v)
		source := object(object(op.Model, "services"), name)
		if !strings.HasPrefix(str(service, "image"), "sha256:") || len(str(service, "image")) != 71 || str(service, "pull_policy") != "never" {
			return false
		}
		if image, ok := source["image"]; ok {
			service["image"] = image
		} else {
			delete(service, "image")
		}
		if policy, ok := source["pull_policy"]; ok {
			service["pull_policy"] = policy
		} else {
			delete(service, "pull_policy")
		}
	}
	return sameModel(candidate, op.Model)
}

func unchangedInputs(inputs composeInputs) error {
	for path, want := range inputs {
		f, e := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if e != nil {
			return fail("A rebuild input is unavailable")
		}
		st, e := f.Stat()
		if e != nil || !st.Mode().IsRegular() || st.Size() > 2<<20 {
			f.Close()
			return fail("Rebuild input is not a bounded regular file")
		}
		var b inspectionBuffer
		_, e = b.ReadFrom(io.LimitReader(f, 2<<20+1))
		f.Close()
		if e != nil || b.Len() > 2<<20 || sha256.Sum256(b.Bytes()) != want {
			return fail("A rebuild input changed; inspect a fresh plan")
		}
	}
	return nil
}
func savedWebLabels(m map[string]any) map[string]string {
	for _, v := range object(m, "services") {
		l := stringMap(object(asObject(v), "labels"))
		if isWeb(l) {
			return l
		}
	}
	return nil
}

func rebuildModel(root, common, name string, cfg projectConfig, old map[string]any, head, identity, branch string) (map[string]any, composeInputs, error) {
	labels := savedWebLabels(old)
	host := strings.TrimPrefix(labels["devkit.url"], "https://")
	if host == "" || strings.ContainsAny(host, "/ ?#@") {
		return nil, nil, fail("Saved preview URL is invalid")
	}
	env := withEnv(os.Environ(), map[string]string{"DEVKIT_PREVIEW": name, "DEVKIT_HOST": host, "DEVKIT_URL": labels["devkit.url"], "DEVKIT_COMMIT": head, "DEVKIT_MODE": "live", "DEVKIT_IMAGE": "devkit/" + cfg.Project + ":" + head})
	extra := composeInputs{}
	if len(cfg.MediaPorts) > 0 {
		leasePath := mediaLeasePath(filepath.Join(runDir(common, name), "compose.json"))
		b, err := readPrivate(leasePath, 1<<20)
		if err != nil {
			return nil, nil, fail("Existing media lease unavailable")
		}
		var lease mediaLease
		if json.Unmarshal(b, &lease) != nil || lease.Compose != resolve(filepath.Join(runDir(common, name), "compose.json")) || len(lease.Ports) != len(cfg.MediaPorts) {
			return nil, nil, fail("Media lease ownership changed")
		}
		ip, err := mediaHostIP()
		if err != nil || ip != lease.IP {
			return nil, nil, fail("Media address changed or unavailable")
		}
		vars := map[string]string{"DEVKIT_MEDIA_IP": ip}
		for _, p := range cfg.MediaPorts {
			port := lease.Ports[p.Name]
			if port < mediaPortFirst || port > mediaPortLast {
				return nil, nil, fail("Media allocation changed")
			}
			vars[p.envKey()] = fmt.Sprint(port)
		}
		env = withEnv(env, vars)
		extra[leasePath] = sha256.Sum256(b)
	}
	model, inputs, err := inspectCompose(root, cfg, "live", name, env)
	if err != nil {
		return nil, inputs, err
	}
	metadata := map[string]string{managedLabel: "true", "devkit.preview": name, "devkit.mode": "live", "devkit.project": cfg.Project, "devkit.repository": common, "devkit.commit": head, "devkit.branch": branch, "devkit.worktree": root, "devkit.identity": identity, "devkit.url": labels["devkit.url"]}
	model, err = decorateCompose(model, cfg, "live", env, metadata)
	if err != nil {
		return nil, inputs, err
	}
	for _, v := range object(model, "services") {
		service := asObject(v)
		ensureObject(service, "labels")["devkit.rebuild-source-image"] = str(service, "image")
	}
	for p, h := range extra {
		inputs[p] = h
	}
	if _, err = inputs.read(root, configFile(root)); err != nil {
		return nil, inputs, err
	}
	return model, inputs, nil
}

// Read all containers with the exact Compose project label, including unmanaged ones.
// This prevents an incomplete managed-only inventory from granting rebuild authority.
var rebuildInventory = func(name string) ([]container, error) {
	out, err := validationCommand([]string{"docker", "ps", "-aq", "--filter", "label=com.docker.compose.project=" + name}, runOpts{capture: true, quiet: true})
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(out)
	if len(ids) == 0 {
		return []container{}, nil
	}
	out, err = validationCommand(append([]string{"docker", "inspect"}, ids...), runOpts{capture: true, quiet: true})
	if err != nil {
		return nil, err
	}
	var rows []container
	err = json.Unmarshal([]byte(out), &rows)
	return rows, err
}

func validateRebuildRuntime(op rebuildOperation, rows []container) (bool, error) {
	services := object(op.Old, "services")
	seen := map[string]bool{}
	running, stopped := false, false
	for _, c := range rows {
		l := c.Config.Labels
		service := l["com.docker.compose.service"]
		if l[managedLabel] != "true" || l["devkit.preview"] != op.Preview || l["devkit.repository"] != op.Common || l["devkit.worktree"] != op.Root || l["devkit.identity"] != op.Identity || services[service] == nil || seen[service] {
			return false, fail("Runtime ownership or service topology is ambiguous")
		}
		seen[service] = true
		running = running || c.State.Running
		stopped = stopped || !c.State.Running
		mounts, _ := asObject(services[service])["volumes"].([]any)
		if len(mounts) != len(c.Mounts) {
			return false, fail("Observed storage differs from saved mounts")
		}
		for _, v := range mounts {
			m := asObject(v)
			matched := false
			for _, actual := range c.Mounts {
				if actual.Destination != str(m, "target") || actual.Type != str(m, "type") || actual.RW == truthy(m["read_only"]) {
					continue
				}
				if actual.Type == "volume" {
					matched = actual.Name == str(object(object(op.Old, "volumes"), str(m, "source")), "name")
				} else {
					matched = actual.Source == str(m, "source")
				}
				if matched {
					break
				}
			}
			if !matched {
				return false, fail("Observed storage does not match the saved preview")
			}
		}
	}
	recovering := op.Built && (op.Phase == "applying" || op.Phase == "failed")
	if !recovering && (len(seen) != len(services) || (running && stopped)) {
		return false, fail("All saved services must be observed in one consistent running/stopped state")
	}
	return running, nil
}

var rebuildVolumes = func(name string, model map[string]any) error {
	for _, v := range object(model, "volumes") {
		volume := asObject(v)
		out, err := validationCommand([]string{"docker", "volume", "inspect", str(volume, "name")}, runOpts{capture: true, quiet: true})
		if err != nil {
			return err
		}
		var rows []struct {
			Name    string
			Driver  string
			Options map[string]string
			Labels  map[string]string
		}
		if json.Unmarshal([]byte(out), &rows) != nil || len(rows) != 1 || rows[0].Name != str(volume, "name") || rows[0].Labels["com.docker.compose.project"] != name {
			return fail("Volume ownership unavailable")
		}
		driver := firstOf(str(volume, "driver"), "local")
		if rows[0].Driver != driver {
			return fail("Volume driver differs")
		}
		want := stringMap(object(volume, "driver_opts"))
		if len(want) != len(rows[0].Options) {
			return fail("Volume options differ")
		}
		for k, v := range want {
			if rows[0].Options[k] != v {
				return fail("Volume options differ")
			}
		}
	}
	return nil
}

func makeRebuildPlan(name string, recover bool) rebuildPlan {
	r := newReport("rebuild", map[string]any{"preview": name, "apply": false, "recover": recover})
	p := rebuildPlan{report: r}
	bad := func(code, message string) rebuildPlan { p.report.add(code, "fail", name, message); return p }
	root, common, err := repository()
	if err != nil {
		return bad("rebuild.checkout", "A verified Git checkout is required.")
	}
	cfg, err := loadConfig(root)
	if err != nil {
		return bad("rebuild.recipe", "Recipe is missing or invalid.")
	}
	_, identity, err := liveIdentity(root)
	if err != nil {
		return bad("rebuild.identity", "Checkout identity is unavailable.")
	}
	if name == "" {
		saved, err := savedPreviews(common)
		if err != nil {
			return bad("rebuild.saved", "Saved metadata is unavailable.")
		}
		matches := []string{}
		for id, l := range saved {
			if l["devkit.mode"] == "live" && l["devkit.worktree"] == root && l["devkit.identity"] == identity {
				matches = append(matches, id)
			}
		}
		if len(matches) != 1 {
			return bad("rebuild.target", "Use one exact saved live preview belonging to this checkout.")
		}
		name = matches[0]
	}
	p.report.Data.(map[string]any)["preview"] = name
	if !previewID.MatchString(name) {
		return bad("rebuild.target", "Use an exact preview ID.")
	}
	old, err := readCompose(filepath.Join(runDir(common, name), "compose.json"))
	if err != nil {
		return bad("rebuild.saved", "Saved configuration is unavailable.")
	}
	l := savedWebLabels(old)
	if l["devkit.mode"] != "live" || l["devkit.repository"] != common || l["devkit.worktree"] != root || l["devkit.identity"] != identity || l["devkit.project"] != cfg.Project || l["devkit.preview"] != name {
		return bad("rebuild.owner", "Only an exact saved live preview in its owning checkout can be rebuilt.")
	}
	previous, err := loadRebuildOperation(name)
	if err != nil {
		return bad("rebuild.recovery", "Recovery state is unreadable.")
	}
	if !recover && previous != nil && previous.Phase != "complete" {
		return bad("rebuild.recovery", "An unfinished rebuild exists; inspect --recover before any new build.")
	}
	if recover && (previous == nil || previous.Phase == "complete" || !previous.Built) {
		return bad("rebuild.recovery", "No built unfinished candidate is available for automatic recovery; inspect private evidence.")
	}
	head, err := git(root, "rev-parse", "HEAD")
	if err != nil {
		return bad("rebuild.head", "Source HEAD is unavailable.")
	}
	dirty, err := sourceStatus(root)
	if err != nil {
		return bad("rebuild.dirty", "Source status is unavailable.")
	}
	branch, _, _ := liveIdentity(root)
	baseline := old
	if recover {
		baseline = previous.Old
	}
	model, inputs, err := rebuildModel(root, common, name, cfg, baseline, head, identity, branch)
	if err != nil {
		return bad("rebuild.model", "Cannot safely render the local model; check inputs, interpolation, Compose access and existing media allocation.")
	}
	if err = localRebuildBuilds(root, model); err != nil {
		return bad("rebuild.build-context", err.Error())
	}
	if err = supportedRebuild(baseline, model); err != nil {
		return bad("rebuild.compatibility", err.Error())
	}
	op := rebuildOperation{Preview: name, Root: root, Common: common, Identity: identity, Head: head, Dirty: dirty != "", Old: old, Model: model, Candidate: model, Inputs: inputs, Images: map[string]string{}}
	if recover {
		if !recordedCandidateMatches(*previous) {
			return bad("rebuild.candidate", "Recorded candidate does not match its source model; preserve evidence for manual review.")
		}
		if previous.Root != root || previous.Common != common || previous.Identity != identity || previous.Head != head || (!sameModel(previous.Old, old) && !sameModel(previous.Candidate, old)) || !sameModel(previous.Model, model) || !sameModel(previous.Inputs, inputs) || unchangedInputs(previous.Inputs) != nil {
			return bad("rebuild.stale", "Recovery inputs or saved ownership changed; manual compatibility review is required.")
		}
		op = *previous
	}
	rows, err := rebuildInventory(name)
	if err != nil {
		p.report.unavailable("rebuild.inventory", name, "Complete Docker inventory is unavailable.")
		return p
	}
	running, err := validateRebuildRuntime(op, rows)
	if err != nil {
		return bad("rebuild.runtime", err.Error())
	}
	if err = rebuildVolumes(name, old); err != nil {
		return bad("rebuild.volumes", "Volume ownership or storage options are unavailable or differ.")
	}
	if _, shared, err := shareFor(name); err != nil || shared {
		return bad("rebuild.share", "Active sharing or unavailable share inventory blocks rebuild; end sharing explicitly first.")
	}
	if !recover {
		op.Stopped = !running
		for _, c := range rows {
			op.Images[c.Config.Labels["com.docker.compose.service"]] = c.Image
			op.Runtime = append(op.Runtime, projectContainerView(c))
		}
	}
	if recover && !running && len(rows) > 0 {
		op.Stopped = true
	}
	p.op = op
	services := sortedKeys(object(model, "services"))
	builds := []string{}
	for _, s := range services {
		if truthy(object(object(model, "services"), s)["build"]) {
			builds = append(builds, s)
		}
	}
	p.report.Data = map[string]any{"preview": name, "url": l["devkit.url"], "apply": false, "recover": recover, "services": services, "build_services": builds, "source_head": head, "dirty": dirty != "", "runtime": map[bool]string{true: "running", false: "stopped"}[running], "phase": firstOf(op.Phase, "planned")}
	p.report.add("rebuild.plan", "ok", name, "Preview identity, observed storage and supported changes are consistent.")
	p.report.add("rebuild.data", "warn", name, "Devkit retains storage; Dockerfiles and new application code can modify data. No project hooks run. A live worktree build is not a reproducible commit build.")
	if !running {
		p.report.add("rebuild.health", "warn", name, "Stopped preview stays stopped; runtime verification waits for an explicit start.")
	}
	return p
}
func rebuildCLI(args []string) (int, error) {
	pos, v, err := splitArgs(args, nil, map[string]bool{"--json": true, "--yes": true, "--recover": true})
	_, asJSON := v["--json"]
	_, apply := v["--yes"]
	_, recover := v["--recover"]
	if err != nil || len(pos) > 1 || (recover && len(pos) != 1) {
		return 2, usageError("Use rebuild [PREVIEW] [--yes] [--json], or rebuild PREVIEW --recover [--yes] [--json]")
	}
	name := ""
	if len(pos) > 0 {
		name = pos[0]
	}
	p := makeRebuildPlan(name, recover)
	if p.report.exitCode() != 0 || !apply {
		return p.report.write(asJSON)
	}
	unlock, err := lockPreview(p.op.Preview)
	if err != nil {
		p.report.add("rebuild.lock", "fail", p.op.Preview, "Another preview lifecycle operation is in progress.")
		return p.report.write(asJSON)
	}
	defer unlock()
	p = makeRebuildPlan(p.op.Preview, recover)
	if p.report.exitCode() == 0 {
		if err := applyRebuild(&p.op, recover); err != nil {
			p.report.add("rebuild.apply", "fail", p.op.Preview, err.Error())
			failed("rebuild", p.op.Preview, err)
		} else {
			p.report.Data.(map[string]any)["apply"] = true
			p.report.Data.(map[string]any)["phase"] = "complete"
		}
	}
	if data, ok := p.report.Data.(map[string]any); ok {
		data["phase"] = firstOf(p.op.Phase, "planned")
		if rows, err := rebuildInventory(p.op.Preview); err == nil {
			observed := []previewContainerView{}
			for _, c := range rows {
				observed = append(observed, projectContainerView(c))
			}
			data["observed_containers"] = sortedRuntime(observed)
		} else {
			data["observed_containers"] = nil
			p.report.Complete = false
		}
	}
	return p.report.write(asJSON)
}

func sortedRuntime(rows []previewContainerView) []previewContainerView {
	out := append([]previewContainerView{}, rows...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
