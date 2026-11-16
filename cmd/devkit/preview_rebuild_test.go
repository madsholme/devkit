package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPreviewLockKeepsBusyExpiredShare(t *testing.T) {
	name := "expiry-busy"
	swap(t, &dockerContainers, withContainers(shareContainer(name, "expiry.share.test", "tunnel", "dns", time.Now().Add(-time.Hour))))
	swap(t, &removeContainer, func(string) error { t.Fatal("busy expiry removed connector"); return nil })
	unlock, err := lockPreview(name)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	removed, err := reapExpired(time.Now())
	if err == nil || len(removed) != 0 {
		t.Fatal(removed, err)
	}
	if _, found, _ := shareFor(name); !found {
		t.Fatal("busy share lost")
	}
}

func runtimeFixture(t *testing.T, op rebuildOperation) []container {
	t.Helper()
	rows := []container{}
	for name, v := range object(op.Old, "services") {
		service := asObject(v)
		c := container{ID: strings.Repeat("a", 64), Image: "sha256:" + strings.Repeat("b", 64)}
		c.Config.Labels = stringMap(object(service, "labels"))
		c.Config.Labels["com.docker.compose.service"] = name
		c.State.Running = true
		c.State.Status = "running"
		mounts := []map[string]any{}
		for _, v := range service["volumes"].([]any) {
			m := asObject(v)
			mount := map[string]any{"Type": str(m, "type"), "Destination": str(m, "target"), "RW": !truthy(m["read_only"])}
			if str(m, "type") == "volume" {
				mount["Name"] = str(object(object(op.Old, "volumes"), str(m, "source")), "name")
			} else {
				mount["Source"] = str(m, "source")
			}
			mounts = append(mounts, mount)
		}
		b, _ := json.Marshal(mounts)
		json.Unmarshal(b, &c.Mounts)
		rows = append(rows, c)
	}
	return rows
}
func TestPreviewRebuildPlanExactOwnerAndNoWrites(t *testing.T) {
	requireCompose(t)
	repo := newGitRepo(t)
	t.Chdir(repo.root)
	cfg, _ := loadConfig(repo.root)
	name := "plan-fixture"
	branch, identity, _ := liveIdentity(repo.root)
	labels := map[string]any{managedLabel: "true", "devkit.web": "true", "devkit.url": "https://plan-fixture.devkit.test"}
	seed := map[string]any{"services": map[string]any{"web": map[string]any{"labels": labels}}}
	model, _, err := rebuildModel(repo.root, repo.common, name, cfg, seed, repo.commit, identity, branch)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(runDir(repo.common, name), "compose.json")
	os.MkdirAll(filepath.Dir(path), 0700)
	writeCompose(path, model)
	op := rebuildOperation{Preview: name, Root: repo.root, Common: repo.common, Identity: identity, Old: model}
	rows := runtimeFixture(t, op)
	swap(t, &rebuildInventory, func(string) ([]container, error) { return rows, nil })
	swap(t, &rebuildVolumes, func(string, map[string]any) error { return nil })
	swap(t, &dockerContainers, func() ([]container, error) { return nil, nil })
	before, _ := os.ReadFile(path)
	code, r := validationCLI(t, "rebuild", name, "--json")
	if code != 0 {
		t.Fatal(r)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("plan changed saved config")
	}
	if _, err := os.Stat(rebuildDir(name)); !os.IsNotExist(err) {
		t.Fatal("plan created operation state")
	}
	rows[0].Config.Labels["devkit.worktree"] = "/other"
	code, _ = validationCLI(t, "rebuild", name, "--json")
	if code != 1 {
		t.Fatal("accepted different worktree")
	}
}
func TestPreviewRebuildRuntimeStorageAndPartialRecovery(t *testing.T) {
	old, _ := rebuildModels()
	labels := map[string]any{managedLabel: "true", "devkit.preview": "demo", "devkit.repository": "/repo", "devkit.worktree": "/repo/tree", "devkit.identity": "main"}
	object(object(old, "services"), "web")["labels"] = labels
	op := rebuildOperation{Preview: "demo", Common: "/repo", Root: "/repo/tree", Identity: "main", Old: old}
	rows := runtimeFixture(t, op)
	if _, err := validateRebuildRuntime(op, rows); err != nil {
		t.Fatal(err)
	}
	rows[0].Mounts[0].Name = "foreign"
	if _, err := validateRebuildRuntime(op, rows); err == nil {
		t.Fatal("accepted foreign volume")
	}
	if _, err := validateRebuildRuntime(op, nil); err == nil {
		t.Fatal("accepted missing runtime")
	}
	op.Built = true
	op.Phase = "applying"
	if _, err := validateRebuildRuntime(op, nil); err != nil {
		t.Fatal("partial recovery must permit recreation with verified owned volumes", err)
	}
}

func TestPreviewRebuildUnbuiltImageReferenceAndCandidateIntegrity(t *testing.T) {
	old, next := rebuildModels()
	a, b := object(object(old, "services"), "web"), object(object(next, "services"), "web")
	delete(a, "build")
	delete(b, "build")
	a["image"] = "sha256:" + strings.Repeat("a", 64)
	a["labels"] = map[string]any{"devkit.rebuild-source-image": "demo:v1"}
	if err := supportedRebuild(old, next); err != nil {
		t.Fatal("unchanged source reference rejected after pinned rebuild", err)
	}
	candidate, _ := cloneModel(next)
	service := object(object(candidate, "services"), "web")
	service["image"] = "sha256:" + strings.Repeat("b", 64)
	service["pull_policy"] = "never"
	op := rebuildOperation{Model: next, Candidate: candidate}
	if !recordedCandidateMatches(op) {
		t.Fatal("valid pinned candidate rejected")
	}
	service["environment"] = map[string]any{"UNEXPECTED": "value"}
	if recordedCandidateMatches(op) {
		t.Fatal("corrupted candidate accepted")
	}
}

func TestPreviewRebuildRecoveryStopRequiresObservation(t *testing.T) {
	old, _ := rebuildModels()
	name := "recovery-stop"
	object(object(old, "services"), "web")["labels"] = map[string]any{managedLabel: "true", "devkit.preview": name, "devkit.repository": "/repo", "devkit.worktree": "/repo/tree", "devkit.identity": "main"}
	op := rebuildOperation{Preview: name, Common: "/repo", Root: "/repo/tree", Identity: "main", Old: old, Phase: "failed", Built: true}
	if err := saveRebuildOperation(op); err != nil {
		t.Fatal(err)
	}
	rows := runtimeFixture(t, op)
	swap(t, &rebuildInventory, func(string) ([]container, error) { return rows, nil })
	if recordRecoveryStop(name) == nil {
		t.Fatal("recorded running preview as stopped")
	}
	rows[0].State.Running = false
	rows[0].State.Status = "exited"
	if err := recordRecoveryStop(name); err != nil {
		t.Fatal(err)
	}
	saved, _ := loadRebuildOperation(name)
	if !saved.Stopped || saved.Phase != "failed" || len(saved.Runtime) != 1 {
		t.Fatal(saved)
	}
}

func rebuildModels() (map[string]any, map[string]any) {
	model := map[string]any{"name": "demo", "services": map[string]any{"web": map[string]any{"image": "demo:v1", "build": map[string]any{"context": "/app"}, "volumes": []any{map[string]any{"type": "volume", "source": "data", "target": "/data"}}, "healthcheck": map[string]any{"test": []any{"CMD", "true"}}}}, "volumes": map[string]any{"data": map[string]any{"name": "demo_data"}}, "networks": map[string]any{}}
	b, _ := cloneModel(model)
	return model, b
}
func TestPreviewRebuildPlanStorageBoundaries(t *testing.T) {
	cases := map[string]func(map[string]any){
		"volume name":   func(m map[string]any) { object(object(m, "volumes"), "data")["name"] = "other" },
		"service added": func(m map[string]any) { object(m, "services")["db"] = map[string]any{} },
		"mount": func(m map[string]any) {
			object(object(m, "services"), "web")["volumes"] = []any{map[string]any{"type": "bind", "source": "/other", "target": "/data"}}
		},
		"network": func(m map[string]any) { object(m, "networks")["other"] = map[string]any{} },
		"credentials": func(m map[string]any) {
			object(object(m, "services"), "web")["environment"] = map[string]any{"DB_PASSWORD": "synthetic"}
		},
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			old, next := rebuildModels()
			edit(next)
			if err := supportedRebuild(old, next); err == nil {
				t.Fatal("unsafe edit accepted")
			}
		})
	}
	old, next := rebuildModels()
	object(object(next, "services"), "web")["command"] = []any{"new-app"}
	if err := supportedRebuild(old, next); err != nil {
		t.Fatal(err)
	}
	old, next = rebuildModels()
	delete(object(object(old, "services"), "web"), "build")
	delete(object(object(next, "services"), "web"), "build")
	object(object(next, "services"), "web")["image"] = "demo:new"
	if supportedRebuild(old, next) == nil {
		t.Fatal("unbuilt image upgrade accepted")
	}
}
func TestPreviewLockNonblockingAndRecoveryGuard(t *testing.T) {
	unlock, err := lockPreview("lock-test")
	if err != nil {
		t.Fatal(err)
	}
	if second, err := lockPreview("lock-test"); err == nil {
		second()
		t.Fatal("overlapping lock")
	}
	unlock()
	unlock, err = lockPreview("lock-test")
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	op := rebuildOperation{Preview: "lock-test", Phase: "applying"}
	if err := saveRebuildOperation(op); err != nil {
		t.Fatal(err)
	}
	if previewRecoveryGuard("lock-test", "start") == nil {
		t.Fatal("stale saved config can start")
	}
	if err := previewRecoveryGuard("lock-test", "stop"); err != nil {
		t.Fatal(err)
	}
}
func TestPreviewRebuildInputsDetectDrift(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "model.yaml")
	os.WriteFile(path, []byte("services: {}"), 0600)
	inputs := composeInputs{}
	inputs.read(root, path)
	if err := unchangedInputs(inputs); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte("changed"), 0600)
	if unchangedInputs(inputs) == nil {
		t.Fatal("did not detect edit")
	}
}

func TestPreviewRebuildApplyFailureAndStopped(t *testing.T) {
	for _, buildFailure := range []bool{true, false} {
		t.Run(map[bool]string{true: "build failure", false: "stopped stages"}[buildFailure], func(t *testing.T) {
			root := t.TempDir()
			common := filepath.Join(root, ".git")
			name := "apply-test"
			os.MkdirAll(runDir(common, name), 0700)
			old, next := rebuildModels()
			writeCompose(filepath.Join(runDir(common, name), "compose.json"), old)
			op := rebuildOperation{Preview: name, Root: root, Common: common, Old: old, Model: next, Candidate: next, Stopped: true, Images: map[string]string{"web": "sha256:old"}}
			swap(t, &revalidateRebuild, func(rebuildOperation, bool) error { return nil })
			calls := []string{}
			swap(t, &rebuildCompose, func(op rebuildOperation, path string, args ...string) error {
				calls = append(calls, args[0])
				if args[0] != "build" {
					t.Fatal("stopped preview started")
				}
				if buildFailure {
					return fail("SECRET")
				}
				return nil
			})
			swap(t, &rebuildImageID, func(string) (string, error) { return "sha256:new", nil })
			err := applyRebuild(&op, false)
			if buildFailure {
				if err == nil {
					t.Fatal("build unexpectedly succeeded")
				}
				saved, _ := readCompose(filepath.Join(runDir(common, name), "compose.json"))
				if !sameModel(saved, old) {
					t.Fatal("build failure altered saved configuration")
				}
			} else if err != nil || op.Phase != "complete" {
				t.Fatal(err, op.Phase)
			}
			if len(calls) != 1 {
				t.Fatal(calls)
			}
		})
	}
}
func TestPreviewRebuildRecoveryDoesNotRebuild(t *testing.T) {
	swap(t, &verifyRebuildApplied, func(rebuildOperation) error { return nil })
	root := t.TempDir()
	common := filepath.Join(root, ".git")
	name := "recover-test"
	os.MkdirAll(runDir(common, name), 0700)
	old, next := rebuildModels()
	writeCompose(filepath.Join(runDir(common, name), "compose.json"), old)
	op := rebuildOperation{Preview: name, Root: root, Common: common, Old: old, Model: next, Candidate: next, Phase: "failed", Built: true}
	swap(t, &revalidateRebuild, func(rebuildOperation, bool) error { return nil })
	calls := 0
	swap(t, &rebuildCompose, func(_ rebuildOperation, _ string, args ...string) error {
		calls++
		if args[0] != "up" {
			t.Fatal(args)
		}
		if calls == 1 {
			return fail("SECRET")
		}
		return nil
	})
	if err := applyRebuild(&op, true); err == nil || op.Phase != "failed" {
		t.Fatal(err, op.Phase)
	}
	if err := previewRecoveryGuard(name, "start"); err == nil {
		t.Fatal("allowed stale start")
	}
	if err := applyRebuild(&op, true); err != nil || op.Phase != "complete" || calls != 2 {
		t.Fatal(err, op.Phase, calls)
	}
}
