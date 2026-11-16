package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validationCLI(t *testing.T, args ...string) (int, map[string]any) {
	t.Helper()
	var out bytes.Buffer
	swap(t, &stdout, io.Writer(&out))
	code, err := dispatch(args)
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("report: %v %s", err, out.String())
	}
	if report["schema_version"] != float64(1) {
		t.Fatal(report)
	}
	return code, report
}

func TestProjectValidationTemplateWithoutRuntime(t *testing.T) {
	requireCompose(t)
	repo := newGitRepo(t)
	t.Chdir(repo.root)
	before := repo.git("status", "--porcelain")
	code, report := validationCLI(t, "project", "validate", "--runtime", "containers", "--json")
	if code != 0 || report["complete"] != true {
		t.Fatal(code, report)
	}
	if repo.git("status", "--porcelain") != before {
		t.Fatal("validation changed checkout")
	}
	if _, err := os.Stat(filepath.Join(repo.common, "devkit")); !os.IsNotExist(err) {
		t.Fatal("validation created runtime state", err)
	}
}

func TestProjectValidationRejectsUnsafeInputsBeforeCompose(t *testing.T) {
	for _, body := range []string{"include: https://example.test/secret\n", "include:\n- ../outside.yaml\n", "include:\n  - ../outside.yaml\n", "services:\n  web:\n    env_file: ../secret.env\n", "\"include\": remote.yaml\n", "include: [https://example.test/x]\n"} {
		t.Run(body, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			os.WriteFile(filepath.Join(root, "devkit.json"), []byte(`{"project":"test","compose":{"live":"compose.live.yaml"}}`), 0600)
			os.WriteFile(filepath.Join(root, "compose.live.yaml"), []byte(body), 0600)
			swap(t, &validationCommand, func([]string, runOpts) (string, error) { t.Fatal("unsafe input reached Compose"); return "", nil })
			code, _ := validationCLI(t, "project", "validate", "--runtime", "containers", "--json")
			if code != 1 {
				t.Fatal(code)
			}
		})
	}
}

func TestProjectValidationRedactsErrorsAndMissingHealth(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	os.WriteFile(filepath.Join(root, "devbox.json"), []byte(`{"project":"test"}`), 0600)
	os.WriteFile(filepath.Join(root, "compose.live.yaml"), []byte("services:\n  web:\n    image: example\n"), 0600)
	swap(t, &validationCommand, func([]string, runOpts) (string, error) { return "", fail("secret-SENTINEL") })
	code, report := validationCLI(t, "project", "validate", "--json")
	b, _ := json.Marshal(report)
	if code != 1 || strings.Contains(string(b), "SENTINEL") {
		t.Fatal(code, string(b))
	}
	validationCommand = func([]string, runOpts) (string, error) { return `{"services":{"web":{"image":"test"}}}`, nil }
	code, report = validationCLI(t, "project", "validate", "--json")
	if code != 1 || !strings.Contains(string(mustJSON(t, report)), "compose.health") {
		t.Fatal(code, report)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestProjectValidationLocalIncludeAndMissingInterpolation(t *testing.T) {
	requireCompose(t)
	root := t.TempDir()
	t.Chdir(root)
	os.WriteFile(filepath.Join(root, "devkit.json"), []byte(`{"project":"demo"}`), 0600)
	os.WriteFile(filepath.Join(root, "compose.live.yaml"), []byte("include:\n  - web.yaml\n"), 0600)
	model := "services:\n  web:\n    image: python:3.13-slim\n    env_file: app.env\n    healthcheck:\n      test: [CMD, \"true\"]\n"
	os.WriteFile(filepath.Join(root, "web.yaml"), []byte(model), 0600)
	os.WriteFile(filepath.Join(root, "app.env"), []byte("APP_TEST=synthetic-secret\n"), 0600)
	code, r := validationCLI(t, "project", "validate", "--json")
	if code != 0 {
		t.Fatal(r)
	}
	os.WriteFile(filepath.Join(root, "web.yaml"), []byte(strings.ReplaceAll(model, "python:3.13-slim", "${DEVKIT_MISSING_TEST_INPUT:?required}")), 0600)
	t.Setenv("DEVKIT_MISSING_TEST_INPUT", "")
	code, r = validationCLI(t, "project", "validate", "--json")
	if code != 1 || strings.Contains(string(mustJSON(t, r)), "synthetic-secret") {
		t.Fatal(r)
	}
}

func TestOperationUsageJSON(t *testing.T) {
	for _, args := range [][]string{{"context", "--unknown", "--json"}, {"project", "validate", "--runtime", "invalid", "--json"}, {"rebuild", "--recover", "--json"}} {
		code, r := validationCLI(t, args...)
		if code != 2 || r["complete"] != false {
			t.Fatal(code, r)
		}
	}
}
