package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// stdout receives command output; tests replace it.
var stdout io.Writer = os.Stdout

// userError is a failure the user can act on; it is printed without a stack or command dump.
type userError string

func (e userError) Error() string { return string(e) }

func fail(format string, args ...any) error { return userError(fmt.Sprintf(format, args...)) }

// commandError reports a failed external command, like Python's CalledProcessError.
type commandError struct {
	args []string
	err  error
}

func (e *commandError) Error() string {
	var exit *exec.ExitError
	if errors.As(e.err, &exit) {
		return fmt.Sprintf("command %q returned non-zero exit status %d", strings.Join(e.args, " "), exit.ExitCode())
	}
	return fmt.Sprintf("command %q failed: %v", strings.Join(e.args, " "), e.err)
}

func (e *commandError) Unwrap() error { return e.err }

type runOpts struct {
	dir     string
	env     []string // nil inherits the environment
	capture bool     // return stdout instead of passing it through
	quiet   bool     // discard stderr: for probes whose failure the caller reports
}

// runCommand runs a command with stderr passed through; tests replace it.
var runCommand = func(args []string, o runOpts) (string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir, cmd.Env = o.dir, o.env
	cmd.Stdin, cmd.Stderr = os.Stdin, os.Stderr
	if o.quiet {
		cmd.Stderr = nil
	}
	var out bytes.Buffer
	if o.capture {
		cmd.Stdout = &out
	} else {
		cmd.Stdout = os.Stdout
	}
	if err := cmd.Run(); err != nil {
		return "", &commandError{args, err}
	}
	return strings.TrimSpace(out.String()), nil
}

func capture(args ...string) (string, error) { return runCommand(args, runOpts{capture: true}) }

// probe is capture without stderr, for checks that report a failure themselves.
func probe(args ...string) (string, error) {
	return runCommand(args, runOpts{capture: true, quiet: true})
}

func git(root string, args ...string) (string, error) {
	return capture(append([]string{"git", "-C", root}, args...)...)
}

// withEnv returns base with the given variables set, replacing earlier values. Each DEVKIT_*
// variable also gets its DEVBOX_* twin for projects written before devbox became devkit.
func withEnv(base []string, vars map[string]string) []string {
	set := map[string]string{}
	for key, value := range vars {
		set[key] = value
		if rest, ok := strings.CutPrefix(key, "DEVKIT_"); ok {
			set["DEVBOX_"+rest] = value
		}
	}
	env := make([]string, 0, len(base)+len(set))
	for _, item := range base {
		key, _, _ := strings.Cut(item, "=")
		if _, replaced := set[key]; !replaced {
			env = append(env, item)
		}
	}
	for _, key := range sortedKeys(set) {
		env = append(env, key+"="+set[key])
	}
	return env
}
