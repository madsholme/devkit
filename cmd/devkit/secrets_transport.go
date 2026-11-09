package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const secretsUsage = `usage: devkit secrets context|list|get KEY [--reveal] [--json]
       devkit secrets generate|set|update|rotate KEY --scope project|branch|worktree [--expected VERSION] [--length N] [--stdin]
       devkit secrets history|delete|restore KEY [--scope SCOPE] [--confirm PREVIEW_ID]
       devkit secrets run [--scope SCOPE] -- COMMAND [ARGS...]
       devkit secrets resources register|list|bind ID|reconcile|retire ID [--confirm PREVIEW_ID]
       devkit secrets cleanup preview|retire|restore|purge ID [--confirm PREVIEW_ID]
       devkit secrets cleanup status|resume [--confirm PREVIEW_ID]
       devkit secrets cleanup config show|set [--automatic on|off] [--branch-retention 7d|never] [--worktree-retention 7d|never] [--project-retention 30d|never] [--confirm PREVIEW_ID]
Login entries: --type login; hidden prompts or --stdin JSON with username/password.
       generate --type login accepts a username and generates its password.
       get KEY --field username|password --reveal returns one field.
       --username-env NAME --password-env NAME maps both fields; --no-env removes mappings.
Selectors: --project ID --branch ID --worktree ID. Values use stdin or hidden prompt, never argv.
Changes to cleanup settings and destructive operations return a preview confirmation first.
`

func parseSecretsArgs(args []string) (q secretRequest, values map[string]string, child []string, err error) {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		err = fail(secretsUsage)
		return
	}
	q.Operation = args[0]
	rest := args[1:]
	if q.Operation == "run" {
		for i, a := range rest {
			if a == "--" {
				child = rest[i+1:]
				rest = rest[:i]
				break
			}
		}
		if len(child) == 0 {
			err = fail("secrets run needs -- COMMAND")
			return
		}
	}
	if q.Operation == "resources" || q.Operation == "cleanup" {
		if len(rest) == 0 {
			err = fail(secretsUsage)
			return
		}
		group := q.Operation
		q.Operation = rest[0]
		rest = rest[1:]
		if group == "resources" {
			if q.Operation == "list" {
				q.Operation = "resources"
			}
			if q.Operation == "retire" {
				q.Operation = "cleanup-retire"
			}
		} else {
			q.Operation = "cleanup-" + q.Operation
			if q.Operation == "cleanup-config" {
				if len(rest) == 0 || (rest[0] != "show" && rest[0] != "set") {
					err = fail(secretsUsage)
					return
				}
				mode := rest[0]
				rest = rest[1:]
				if mode == "set" {
					cfg, e := loadCleanupConfig()
					if e != nil {
						err = e
						return
					}
					q.Cleanup = &cfg
				}
			}
		}
	}
	var positional []string
	positional, values, err = splitArgs(rest, map[string]bool{"--project": true, "--branch": true, "--worktree": true, "--scope": true, "--expected": true, "--length": true, "--type": true, "--field": true, "--username-env": true, "--password-env": true, "--confirm": true, "--automatic": true, "--branch-retention": true, "--worktree-retention": true, "--project-retention": true}, map[string]bool{"--json": true, "--reveal": true, "--stdin": true, "--no-env": true})
	if err != nil {
		return
	}
	q.Project = values["--project"]
	q.Branch = values["--branch"]
	q.Worktree = values["--worktree"]
	q.Scope = values["--scope"]
	q.Type = values["--type"]
	q.Field = values["--field"]
	q.UsernameEnv = values["--username-env"]
	q.PasswordEnv = values["--password-env"]
	_, q.ClearEnv = values["--no-env"]
	q.Confirm = values["--confirm"]
	_, q.Reveal = values["--reveal"]
	for name, dest := range map[string]*int{"--expected": &q.Expected, "--length": &q.Length} {
		if s, ok := values[name]; ok {
			*dest, err = strconv.Atoi(s)
			if err != nil || *dest < 0 {
				err = fail("Invalid numeric option")
				return
			}
		}
	}
	if q.Cleanup != nil {
		if v, ok := values["--automatic"]; ok {
			if v != "on" && v != "off" {
				err = fail("--automatic must be on or off")
				return
			}
			q.Cleanup.Automatic = v == "on"
		}
		for flag, dest := range map[string]*string{"--branch-retention": &q.Cleanup.Branch, "--worktree-retention": &q.Cleanup.Worktree, "--project-retention": &q.Cleanup.Project} {
			if v, ok := values[flag]; ok {
				*dest = v
			}
		}
		if err = validateCleanupConfig(*q.Cleanup); err != nil {
			return
		}
	}
	switch q.Operation {
	case "get", "set", "update", "generate", "rotate", "delete", "restore", "history":
		if len(positional) != 1 || !vaultSegment.MatchString(positional[0]) || positional[0] == "." || positional[0] == ".." {
			err = fail("Provide one safe secret key name; secret values must not appear in arguments")
			return
		}
		q.Key = positional[0]
	case "bind", "cleanup-preview", "cleanup-retire", "cleanup-restore", "cleanup-purge":
		if len(positional) != 1 {
			err = fail("Provide an exact resource ID")
			return
		}
		q.Resource = positional[0]
	case "context", "list", "register", "resources", "reconcile", "run", "cleanup-config", "cleanup-status", "cleanup-resume", "cleanup-reconcile":
		if len(positional) > 0 {
			err = fail("Unexpected positional argument")
			return
		}
	default:
		err = fail(secretsUsage)
	}
	return
}
func secretsCLI(args []string) (int, error) {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprint(stdout, secretsUsage)
		return 0, nil
	}
	q, flags, child, e := parseSecretsArgs(args)
	if e != nil {
		return 2, e
	}
	q.Root, e = os.Getwd()
	if e != nil {
		return 1, e
	}
	if q.Type == "login" && (q.Operation == "set" || q.Operation == "update" || q.Operation == "generate") {
		if _, fromStdin := flags["--stdin"]; fromStdin {
			if e = readLoginInput(os.Stdin, &q); e != nil {
				return 1, e
			}
		} else {
			input := bufio.NewReader(os.Stdin)
			q.Username, e = readSecret("Username (hidden): ", input)
			if e != nil {
				return 1, e
			}
			if q.Operation != "generate" {
				q.Password, e = readSecret("Password (hidden): ", input)
				if e != nil {
					return 1, e
				}
			}
		}
	} else if q.Operation == "set" || q.Operation == "update" {
		if _, ok := flags["--stdin"]; ok {
			b, e := io.ReadAll(io.LimitReader(os.Stdin, (64<<10)+1))
			if e != nil || len(b) > 64<<10 {
				return 1, fail("Invalid secret input")
			}
			q.Value = strings.TrimRight(string(b), "\r\n")
		} else {
			q.Value, e = readSecret("Secret (hidden): ", bufio.NewReader(os.Stdin))
			if e != nil {
				return 1, e
			}
		}
	}
	if q.Operation == "run" {
		q.Operation = "inject"
		reply, e := callSecretBroker(q)
		if e != nil {
			return 1, e
		}
		command, e := exec.LookPath(child[0])
		if e != nil {
			return 1, e
		}
		// Replace the CLI so the child receives terminal signals and its exact exit status.
		return 1, syscall.Exec(command, child, safeSecretChildEnv(os.Environ(), reply.Values))
	}
	reply, e := callSecretBroker(q)
	if e != nil {
		return 1, e
	}
	if _, asJSON := flags["--json"]; !asJSON && q.Operation == "get" && q.Reveal {
		fmt.Fprintln(stdout, reply.Value)
		return 0, nil
	}
	return 0, json.NewEncoder(stdout).Encode(reply)
}
func safeSecretChildEnv(base []string, values map[string]string) []string {
	var safe []string
	for _, s := range base {
		key, _, _ := strings.Cut(s, "=")
		if strings.HasPrefix(key, "VAULT_") || strings.HasPrefix(key, "BAO_") || (strings.HasPrefix(key, "DEVKIT_") && (strings.Contains(key, "TOKEN") || strings.Contains(key, "SECRET"))) {
			continue
		}
		if _, replace := values[key]; !replace {
			safe = append(safe, s)
		}
	}
	for k, v := range values {
		safe = append(safe, k+"="+v)
	}
	return safe
}
func callSecretBroker(q secretRequest) (secretResponse, error) {
	out := secretResponse{}
	body, e := json.Marshal(q)
	if e != nil {
		return out, e
	}
	client := &http.Client{Timeout: 60 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", filepath.Join(vaultDir(), "broker.sock"))
	}}}
	route := "project"
	if operatorSecretOperation(q.Operation) {
		route = "operator"
	}
	resp, e := client.Post("http://localhost/"+route, "application/json", strings.NewReader(string(body)))
	if e != nil {
		return out, fail("Secrets backend unavailable; start devkit-web and check vault status")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&failure)
		return out, fail("%s", firstOf(failure.Error, "Secrets operation failed"))
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&out); e != nil {
		return out, fail("Invalid secrets backend response")
	}
	return out, nil
}
func secretProtocolHandler(operator bool) http.Handler {
	return secretProtocolHandlerFor(operator, false)
}
func secretProtocolHandlerFor(operator, browser bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != "POST" {
			http.Error(w, "Use POST", 405)
			return
		}
		var q secretRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
		dec.DisallowUnknownFields()
		if dec.Decode(&q) != nil {
			http.Error(w, "Invalid secret request", 400)
			return
		}
		if !operator && operatorSecretOperation(q.Operation) {
			http.Error(w, "Operator operation required", 403)
			return
		}
		if browser && q.Root != "" && !knownGitCheckout(q.Root) {
			http.Error(w, "Checkout is not in the host project inventory", 403)
			return
		}
		out, e := executeSecret(r.Context(), q)
		w.Header().Set("Content-Type", "application/json")
		if e != nil {
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": e.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	})
}
func startSecretSocket() (func(), error) {
	if e := privateDir(vaultDir()); e != nil {
		return nil, e
	}
	path := filepath.Join(vaultDir(), "broker.sock")
	if info, e := os.Lstat(path); e == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fail("Refusing to replace a non-socket broker path")
		}
		if conn, e := net.DialTimeout("unix", path, time.Second); e == nil {
			conn.Close()
			return nil, fail("A secrets backend is already running")
		}
		if e = os.Remove(path); e != nil {
			return nil, e
		}
	}
	listener, e := net.Listen("unix", path)
	if e != nil {
		return nil, e
	}
	if e = os.Chmod(path, 0600); e != nil {
		listener.Close()
		return nil, e
	}
	mux := http.NewServeMux()
	mux.Handle("/project", secretProtocolHandler(false))
	mux.Handle("/operator", secretProtocolHandler(true))
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(listener) }()
	return func() { server.Close(); os.Remove(path) }, nil
}
func secretsWebHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !signedIn(r) {
		sendError(w, http.StatusUnauthorized, "Sign in first.")
		return
	}
	if r.Method != "POST" {
		sendError(w, 405, "Use POST.")
		return
	}
	if !sameOrigin(r) {
		sendError(w, 403, "Cross-site request refused.")
		return
	}
	secretProtocolHandlerFor(true, true).ServeHTTP(w, r)
}
