package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// A binding contains metadata only. Tokens always come from project-scoped KV.
type gitCredentialBinding struct{ Root, Project, URL, Username, Key, Helper string }

var gitSecretRequest = callSecretBroker

func parseGitCredentialURL(raw string) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path == "" || u.Path == "/" || strings.ContainsAny(raw, "\r\n\x00\t ") || u.Opaque != "" {
		return nil, fail("Bind an exact HTTPS repository URL without credentials, query or fragment")
	}
	return u, nil
}
func gitCredentialResponse(b gitCredentialBinding, op string, in io.Reader) (string, error) {
	if op == "store" || op == "erase" {
		return "", nil
	}
	if op != "get" {
		return "", fail("Unsupported Git credential operation")
	}
	u, e := parseGitCredentialURL(b.URL)
	if e != nil {
		return "", e
	}
	fields := map[string]string{}
	scanner := bufio.NewScanner(io.LimitReader(in, 65537))
	scanner.Buffer(make([]byte, 1024), 65536)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			break
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || fields[k] != "" {
			return "", fail("Invalid Git credential request")
		}
		fields[k] = v
	}
	if scanner.Err() != nil || fields["protocol"] != "https" || fields["host"] != u.Host || fields["path"] != strings.TrimPrefix(u.Path, "/") || (fields["username"] != "" && fields["username"] != b.Username) {
		return "", fail("Git credential request does not match its exact binding")
	}
	out, e := gitSecretRequest(secretRequest{Operation: "get", Root: b.Root, Project: b.Project, Scope: "project", Key: b.Key, Reveal: true})
	if e != nil {
		return "", e
	}
	if out.Value == "" || strings.ContainsAny(out.Value, "\r\n\x00") || b.Username == "" || strings.ContainsAny(b.Username, "\r\n\x00") {
		return "", fail("Credential is not valid for Git HTTPS")
	}
	return "username=" + b.Username + "\npassword=" + out.Value + "\n\n", nil
}
func gitBindingPath(root, remote string) string {
	return filepath.Join(secretStateDir(), "git", digest(root+"\n"+remote)+".json")
}
func shellQuoteGit(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func gitCredentialsCLI(args []string) (int, error) {
	if len(args) == 0 {
		return 2, fail("usage: devkit integrations git bind|unbind|status|helper --url HTTPS_URL [--key KEY --username USER]")
	}
	op := args[0]
	pos, v, e := splitArgs(args[1:], map[string]bool{"--url": true, "--key": true, "--username": true, "--root": true}, map[string]bool{"--yes": true, "--json": true})
	if e != nil {
		return 2, e
	}
	if _, e = parseGitCredentialURL(v["--url"]); e != nil {
		return 2, e
	}
	root := v["--root"]
	if root == "" {
		root, e = os.Getwd()
		if e != nil {
			return 1, e
		}
	}
	checkout, e := inspectSecretCheckout(root)
	if e != nil {
		return 1, e
	}
	root = checkout.Root
	path := gitBindingPath(root, v["--url"])
	if op == "helper" {
		// quit=true prevents Git from consulting later helpers or prompting after failure.
		if len(pos) != 1 {
			fmt.Fprint(stdout, "quit=true\n\n")
			return 1, fail("Git helper requires its operation")
		}
		data, e := readPrivate(path, 65536)
		var b gitCredentialBinding
		if e != nil || json.Unmarshal(data, &b) != nil || b.Root != root || b.URL != v["--url"] {
			fmt.Fprint(stdout, "quit=true\n\n")
			return 1, fail("Git credential binding is unavailable")
		}
		out, e := gitCredentialResponse(b, pos[0], os.Stdin)
		if e != nil {
			fmt.Fprint(stdout, "quit=true\n\n")
			return 1, fail("Git credential unavailable; check Devkit vault and binding")
		}
		fmt.Fprint(stdout, out)
		return 0, nil
	}
	if len(pos) != 0 {
		return 2, fail("Unexpected Git credential argument")
	}
	prefix := "credential." + v["--url"] + "."
	if op == "status" {
		data, e := readPrivate(path, 65536)
		if e != nil {
			return 1, e
		}
		fmt.Fprintln(stdout, string(data))
		return 0, nil
	}
	if op == "unbind" {
		data, e := readPrivate(path, 65536)
		var b gitCredentialBinding
		if e != nil || json.Unmarshal(data, &b) != nil {
			return 1, fail("Binding is unavailable")
		}
		helpers, _ := git(root, "config", "--local", "--get-all", prefix+"helper")
		httpPath, _ := git(root, "config", "--local", "--get", prefix+"useHttpPath")
		if strings.TrimSpace(helpers) != b.Helper || httpPath != "true" {
			return 1, fail("Git settings changed; preserve them and resolve the binding manually")
		}
		if _, yes := v["--yes"]; !yes {
			fmt.Fprintln(stdout, "Remove only this Devkit URL binding; existing global helpers will apply again. Repeat with --yes.")
			return 0, nil
		}
		if _, e = git(root, "config", "--local", "--unset-all", prefix+"helper"); e != nil {
			return 1, e
		}
		if _, e = git(root, "config", "--local", "--unset-all", prefix+"useHttpPath"); e != nil {
			return 1, e
		}
		if e = os.Remove(path); e != nil {
			return 1, e
		}
		happened("git credentials unbind", root, v["--url"])
		return 0, nil
	}
	if op != "bind" {
		return 2, fail("Unknown Git credential command")
	}
	if v["--key"] == "" || v["--username"] == "" || strings.ContainsAny(v["--username"], "\r\n\x00") {
		return 2, fail("Bind requires --key PROJECT_SECRET and --username USER")
	}
	context, e := gitSecretRequest(secretRequest{Operation: "get", Root: root, Scope: "project", Key: v["--key"]})
	if e != nil {
		return 1, e
	}
	if exists(path) {
		return 1, fail("Binding already exists; unbind explicitly before replacing it")
	}
	// Refuse existing URL-specific settings, including inherited ones. Other URLs/SSH remain untouched.
	for _, key := range []string{"helper", "useHttpPath", "username"} {
		if _, e := git(root, "config", "--get-all", prefix+key); e == nil {
			return 1, fail("URL-specific Git settings already exist; resolve them before binding")
		}
	}
	binary, e := os.Executable()
	if e != nil {
		return 1, e
	}
	b := gitCredentialBinding{Root: root, Project: context.Context.Project, URL: v["--url"], Username: v["--username"], Key: v["--key"]}
	b.Helper = "!" + shellQuoteGit(binary) + " integrations git helper --root " + shellQuoteGit(root) + " --url " + shellQuoteGit(b.URL)
	if _, yes := v["--yes"]; !yes {
		fmt.Fprintf(stdout, "Bind %s to project secret %s for user %s; reset helpers for this URL only. Repeat with --yes.\n", b.URL, b.Key, b.Username)
		return 0, nil
	}
	data, _ := json.MarshalIndent(b, "", "  ")
	if e = writePrivate(path, data); e != nil {
		return 1, e
	}
	// Keep metadata on partial failure so status and explicit repair remain possible.
	for _, args := range [][]string{{"config", "--local", "--add", prefix + "helper", ""}, {"config", "--local", "--add", prefix + "helper", b.Helper}, {"config", "--local", prefix + "useHttpPath", "true"}} {
		if _, e = git(root, args...); e != nil {
			return 1, fail("Binding settings incomplete; inspect repository credential settings before retrying")
		}
	}
	happened("git credentials bind", root, b.URL)
	return 0, nil
}
