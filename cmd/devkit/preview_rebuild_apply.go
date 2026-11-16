package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Output can contain build arguments and credentials. Keep it off both report and
// history surfaces. Bound execution, including child processes left by the CLI.
var rebuildCompose = func(op rebuildOperation, path string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	argv := append([]string{"compose", "--project-directory", op.Root, "-f", path, "-p", op.Preview}, args...)
	cmd := exec.CommandContext(ctx, "docker", argv...)
	cmd.Dir = op.Root
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	return cmd.Run()
}
var rebuildImageID = func(image string) (string, error) {
	out, err := validationCommand([]string{"docker", "image", "inspect", "--format", "{{.Id}}", image}, runOpts{capture: true, quiet: true})
	id := strings.TrimSpace(out)
	if err != nil || len(id) != 71 || !strings.HasPrefix(id, "sha256:") {
		return "", fail("Built image identity unavailable")
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(id, "sha256:")); err != nil {
		return "", fail("Invalid image identity")
	}
	return id, nil
}
var revalidateRebuild = func(op rebuildOperation, recovery bool) error {
	root, common, err := repository()
	if err != nil || root != op.Root || common != op.Common {
		return fail("Checkout ownership changed")
	}
	branch, identity, err := liveIdentity(root)
	if err != nil || identity != op.Identity {
		return fail("Checkout identity changed")
	}
	head, err := git(root, "rev-parse", "HEAD")
	if err != nil || head != op.Head {
		return fail("Source HEAD changed")
	}
	if err = unchangedInputs(op.Inputs); err != nil {
		return err
	}
	saved, err := readCompose(filepath.Join(runDir(common, op.Preview), "compose.json"))
	if err != nil || (!sameModel(saved, op.Old) && !(recovery && sameModel(saved, op.Candidate))) {
		return fail("Saved configuration changed")
	}
	cfg, err := loadConfig(root)
	if err != nil {
		return fail("Recipe changed")
	}
	model, inputs, err := rebuildModel(root, common, op.Preview, cfg, op.Old, op.Head, op.Identity, branch)
	if err != nil || !sameModel(model, op.Model) || !sameModel(inputs, op.Inputs) {
		return fail("Rendered inputs or environment changed")
	}
	if err = localRebuildBuilds(root, model); err != nil {
		return err
	}
	if op.Built && !recordedCandidateMatches(op) {
		return fail("Recorded candidate differs from source model")
	}
	if err = supportedRebuild(op.Old, model); err != nil {
		return err
	}
	rows, err := rebuildInventory(op.Preview)
	if err != nil {
		return fail("Complete runtime inventory unavailable")
	}
	running, err := validateRebuildRuntime(op, rows)
	if err != nil {
		return err
	}
	if !recovery {
		views := []previewContainerView{}
		for _, c := range rows {
			views = append(views, projectContainerView(c))
		}
		if running == op.Stopped || !sameModel(sortedRuntime(views), sortedRuntime(op.Runtime)) {
			return fail("Runtime changed during the build")
		}
	} else {
		for _, c := range rows {
			s := c.Config.Labels["com.docker.compose.service"]
			if c.Image != op.Images[s] && c.Image != str(object(object(op.Candidate, "services"), s), "image") {
				return fail("Runtime uses an unrelated image; manual recovery required")
			}
		}
	}
	if err = rebuildVolumes(op.Preview, op.Old); err != nil {
		return fail("Storage ownership changed")
	}
	if _, shared, err := shareFor(op.Preview); err != nil || shared {
		return fail("Sharing state changed or is unavailable")
	}
	return nil
}

var verifyRebuildApplied = func(op rebuildOperation) error {
	rows, err := rebuildInventory(op.Preview)
	if err != nil {
		return err
	}
	op.Phase = "built" // Require the complete topology after Compose reported success.
	running, err := validateRebuildRuntime(op, rows)
	if err != nil || !running {
		return fail("Candidate topology is incomplete")
	}
	for _, c := range rows {
		if c.Image != str(object(object(op.Candidate, "services"), c.Config.Labels["com.docker.compose.service"]), "image") || c.State.Health == nil || c.State.Health.Status != "healthy" {
			return fail("Candidate identity or health changed")
		}
	}
	return rebuildVolumes(op.Preview, op.Old)
}

func writeRebuildCandidate(op rebuildOperation) (string, error) {
	path := filepath.Join(rebuildDir(op.Preview), "candidate.json")
	b, err := json.Marshal(op.Candidate)
	if err != nil {
		return "", err
	}
	return path, writePrivate(path, b)
}
func applyRebuild(op *rebuildOperation, recovery bool) error {
	failed := func(message string) error {
		op.Phase = "failed"
		if err := saveRebuildOperation(*op); err != nil {
			return fail("Rebuild failed and recovery state could not be updated; preserve private artifacts for manual inspection.")
		}
		return fail("%s No rollback was attempted; inspect rebuild --recover and private recovery evidence.", message)
	}
	if err := revalidateRebuild(*op, recovery); err != nil {
		return fail("Fresh rebuild validation failed; inspect a new dry run before applying.")
	}
	if !recovery {
		candidate, err := cloneModel(op.Model)
		if err != nil {
			return fail("Cannot prepare candidate")
		}
		op.Candidate = candidate
		nonce := make([]byte, 16)
		if _, err := rand.Read(nonce); err != nil {
			return fail("Cannot allocate operation identity")
		}
		for name, v := range object(candidate, "services") {
			service := asObject(v)
			if truthy(service["build"]) {
				// Refuse additional mutable tags and external build-export side effects.
				build := asObject(service["build"])
				if truthy(build["tags"]) || truthy(build["cache_to"]) || truthy(build["ssh"]) {
					return fail("Build tags, cache exports or SSH forwarding need separate preparation.")
				}
				service["image"] = "devkit-rebuild/" + op.Preview + ":" + hex.EncodeToString(nonce) + "-" + slug(name, 40)
			} else {
				if op.Images[name] == "" {
					return fail("Existing image identity unavailable")
				}
				service["image"] = op.Images[name]
			}
			service["pull_policy"] = "never"
		}
		op.Phase = "prepared"
		if err := saveRebuildOperation(*op); err != nil {
			return fail("Cannot save private recovery state")
		}
		path, err := writeRebuildCandidate(*op)
		if err != nil {
			return failed("Cannot stage candidate.")
		}
		if hasBuild(candidate) {
			if err := rebuildCompose(*op, path, "build"); err != nil {
				return failed("Candidate build failed; saved configuration and existing containers were retained.")
			}
		}
		for _, v := range object(candidate, "services") {
			service := asObject(v)
			if truthy(service["build"]) {
				id, err := rebuildImageID(str(service, "image"))
				if err != nil {
					return failed("Built image identity is unavailable.")
				}
				service["image"] = id
			}
		}
		op.Built = true
		op.Phase = "built"
		if err := saveRebuildOperation(*op); err != nil {
			return failed("Cannot save built candidate.")
		}
		if err := revalidateRebuild(*op, false); err != nil {
			return failed("Inputs, runtime or storage changed during build; candidate was not applied.")
		}
	} else if !op.Built {
		return fail("Recovery requires an already built candidate")
	}
	path, err := writeRebuildCandidate(*op)
	if err != nil {
		return failed("Cannot stage the recorded candidate.")
	}
	op.Phase = "applying"
	if err := saveRebuildOperation(*op); err != nil {
		return failed("Cannot record applying phase.")
	}
	if !op.Stopped {
		timeout := "120"
		for _, v := range object(op.Candidate, "services") {
			if value := str(object(asObject(v), "labels"), "devkit.wait-timeout"); value != "" {
				timeout = value
			}
		}
		if err := rebuildCompose(*op, path, "up", "-d", "--no-build", "--pull", "never", "--wait", "--wait-timeout", timeout); err != nil {
			return failed("Candidate application or health verification failed; runtime may contain both old and candidate containers.")
		}
	}
	if !op.Stopped {
		if err := verifyRebuildApplied(*op); err != nil {
			return failed("Fresh candidate identity, health or storage verification failed.")
		}
	}
	if err := revalidateRebuild(*op, true); err != nil {
		return failed("Inputs or ownership changed before publication.")
	}
	// Atomic publication is last. An interrupted publish still leaves the operation
	// pending, so ordinary resume cannot accidentally launch the old configuration.
	b, err := json.MarshalIndent(op.Candidate, "", "  ")
	if err != nil {
		return failed("Cannot serialize candidate.")
	}
	if err = writePrivate(filepath.Join(runDir(op.Common, op.Preview), "compose.json"), append(b, '\n')); err != nil {
		return failed("Cannot atomically publish saved configuration.")
	}
	op.Phase = "complete"
	if err = saveRebuildOperation(*op); err != nil {
		return fail("Candidate saved, but completion record failed; retain recovery evidence.")
	}
	happened("rebuild", op.Preview, map[bool]string{true: "staged while stopped; runtime verification pending", false: "healthy candidate applied; storage retained"}[op.Stopped])
	return nil
}
