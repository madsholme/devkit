package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

type inspectionBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *inspectionBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 4<<20 {
		b.overflow = true
		return 0, fail("Inspection output exceeds limit")
	}
	return b.Buffer.Write(p)
}

type inspectionUnavailable struct{}

func (inspectionUnavailable) Error() string {
	return "Inspection unavailable or interpolation unresolved"
}

// This runner never forwards subprocess output, environment or errors to the terminal.
var validationCommand = func(args []string, o runOpts) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = o.dir
	cmd.Env = o.env
	var out, diagnostic inspectionBuffer
	cmd.Stdout = &out
	cmd.Stderr = &diagnostic
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	// Compose warns and substitutes empty strings for unset interpolations. Treat those as unresolved.
	if err != nil || out.overflow || diagnostic.overflow || strings.Contains(diagnostic.String(), "is not set") {
		return "", inspectionUnavailable{}
	}
	return out.String(), nil
}

// The preflight reader accepts a deliberately bounded YAML subset. Reject alternate key
// spellings, anchors, tags and flow mappings before Compose can dereference input files.
// Compose remains the schema/YAML parser; this gate only controls its file inputs.
var inspectKey = regexp.MustCompile(`^([A-Za-z0-9_.-]+)\s*:\s*(.*)$`)

type composeInputs map[string][32]byte

func (inputs composeInputs) read(root, path string) ([]byte, error) {
	path = resolve(path)
	if !within(path, resolve(root)) || len(inputs) >= 128 {
		return nil, fail("Input must be a bounded local checkout file")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > 2<<20 {
		return nil, fail("Input is not a bounded regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, 2<<20+1))
	if err != nil || len(b) > 2<<20 {
		return nil, fail("Cannot read input")
	}
	inputs[path] = sha256.Sum256(b)
	return b, nil
}
func inputScalar(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && ((s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"')) {
		s = s[1 : len(s)-1]
	}
	if s == "" || strings.ContainsAny(s, "$\\{}[]#&*!\n\r\t") || strings.Contains(s, ":") || filepath.IsAbs(s) {
		return "", fail("Use a literal local input path")
	}
	return s, nil
}
func inspectComposeInputs(root, path string, inputs composeInputs, visiting map[string]bool) error {
	path = resolve(path)
	if visiting[path] {
		return fail("Compose include cycle")
	}
	visiting[path] = true
	defer delete(visiting, path)
	b, err := inputs.read(root, path)
	if err != nil {
		return err
	}
	if envFile := filepath.Join(filepath.Dir(path), ".env"); isFile(envFile) {
		if _, err := inputs.read(root, envFile); err != nil {
			return err
		}
	}
	section := ""
	sectionIndent := -1
	blockIndent := -1
	follow := func(value string, include bool) error {
		name, e := inputScalar(value)
		if e != nil {
			return e
		}
		next := filepath.Join(filepath.Dir(path), name)
		if include {
			return inspectComposeInputs(root, next, inputs, visiting)
		}
		_, e = inputs.read(root, next)
		return e
	}
	for _, line := range strings.Split(string(b), "\n") {
		trim := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		if blockIndent >= 0 && indent > blockIndent {
			continue
		}
		blockIndent = -1
		if strings.ContainsRune(line, '\t') || strings.HasPrefix(trim, "---") || strings.HasPrefix(trim, "...") || strings.HasPrefix(trim, "%") {
			return fail("Unsupported Compose YAML notation")
		}
		if section != "" && indent <= sectionIndent && !(indent == sectionIndent && strings.HasPrefix(trim, "- ")) {
			section = ""
		}
		item := strings.TrimPrefix(trim, "- ")
		m := inspectKey.FindStringSubmatch(item)
		if m == nil {
			if section != "" && strings.HasPrefix(trim, "- ") {
				if err := follow(item, section == "include"); err != nil {
					return err
				}
				continue
			}
			if !strings.HasPrefix(trim, "- ") {
				return fail("Use plain Compose mapping keys")
			}
			continue
		}
		key, value := m[1], strings.TrimSpace(m[2])
		if strings.HasPrefix(value, "&") || strings.HasPrefix(value, "*") || strings.HasPrefix(value, "!") || strings.HasPrefix(value, "{") {
			return fail("Unsupported Compose YAML indirection")
		}
		if value == "|" || value == ">" || value == "|-" || value == ">-" {
			blockIndent = indent
		}
		switch key {
		case "include", "env_file":
			section = key
			sectionIndent = indent
			if value != "" {
				if err := follow(value, key == "include"); err != nil {
					return err
				}
				section = ""
			}
		case "extends":
			return fail("Expand extends into a local Compose model before inspection")
		default:
			if section != "" {
				return fail("Use short-form local include/env_file paths")
			}
		}
	}
	return nil
}

func inspectCompose(root string, cfg projectConfig, mode, preview string, env []string) (map[string]any, composeInputs, error) {
	file := firstOf(cfg.Compose[mode], "compose."+mode+".yaml")
	inputs := composeInputs{}
	if err := inspectComposeInputs(root, filepath.Join(root, file), inputs, map[string]bool{}); err != nil {
		return nil, inputs, err
	}
	if isFile(filepath.Join(root, ".env")) {
		if _, err := inputs.read(root, filepath.Join(root, ".env")); err != nil {
			return nil, inputs, err
		}
	}
	// Ignore host-wide Compose file/env switches; the explicit checkout is the whole input boundary.
	clean := []string{}
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		if !strings.HasPrefix(key, "COMPOSE_") {
			clean = append(clean, item)
		}
	}
	clean = append(clean, "COMPOSE_DISABLE_ENV_FILE=1")
	args := []string{"docker", "compose", "--project-directory", root, "-f", filepath.Join(root, file), "-p", preview}
	if isFile(filepath.Join(root, ".env")) {
		args = append(args, "--env-file", filepath.Join(root, ".env"))
	}
	args = append(args, "config", "--format", "json")
	out, err := validationCommand(args, runOpts{dir: root, env: clean, capture: true, quiet: true})
	if err != nil {
		return nil, inputs, err
	}
	model, err := decodeObject([]byte(out))
	if err != nil {
		return nil, inputs, err
	}
	if err = validateComposeStructure(model, cfg, mode, preview); err != nil {
		return nil, inputs, err
	}
	if err := unchangedInputs(inputs); err != nil {
		return nil, inputs, err
	}
	return model, inputs, nil
}
func validationEnv() []string {
	return withEnv(os.Environ(), map[string]string{"DEVKIT_PREVIEW": "devkit-validation", "DEVKIT_HOST": "devkit-validation.invalid", "DEVKIT_URL": "https://devkit-validation.invalid", "DEVKIT_COMMIT": strings.Repeat("0", 40), "DEVKIT_MODE": "validation", "DEVKIT_IMAGE": "devkit/validation:validation"})
}
func composeHealth(model map[string]any) bool {
	for _, v := range object(model, "services") {
		h := object(asObject(v), "healthcheck")
		test, _ := h["test"].([]any)
		if truthy(h["disable"]) || len(test) == 0 || fmt.Sprint(test[0]) == "NONE" {
			return false
		}
	}
	return len(object(model, "services")) > 0
}

func validationMediaEnv(cfg projectConfig, mode string) []string {
	vars := map[string]string{"DEVKIT_MODE": mode, "DEVKIT_MEDIA_IP": "100.64.0.1"}
	for i, p := range cfg.MediaPorts {
		vars[p.envKey()] = fmt.Sprint(mediaPortFirst + i)
	}
	return withEnv(validationEnv(), vars)
}
