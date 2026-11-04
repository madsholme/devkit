// Command devkit is a small, single-host preview runner for Git worktrees.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const usage = `usage: devkit <command> [arguments]

Small, single-host preview runner for Git worktrees.

commands:
  setup [--yes] [--domain D] [--email E] [--harness t3|none|custom]
                       configure this host: settings, credentials, DNS, Traefik and the dashboard
  doctor               check this host and say how to fix what is missing
  version              show which devkit release or commit this is
  proxy up             write Traefik's configuration from the host config and start or update it
  up                   alias for live (Docker); use --vm TARGET for a configured VM
  live                 start or resume a preview of working files
  review [commit]      fresh build from a committed version (default HEAD)
  publish [commit]     publish an existing reviewed image through the project publish hook
  exec [-s SERVICE] <command...>
                       run a command in this worktree's live web container (or SERVICE's)
  logs [-s SERVICE] [-f]
                       show this worktree's live web log, or SERVICE's (-f, --follow to follow it)
  vms [--json]          list QEMU/libvirt VMs, including stopped managed guests
  cleanup --vm NAME|--project SLUG [--purge] [--yes]
                       stop and unregister QEMU VMs; dry run, retain disks unless --purge
  git fetch|sync|push   fetch origin, fast-forward from its upstream, or explicitly push
  ls                   list managed previews, including stopped ones
  web [--host H] [--port P]
                       serve the web overview (default 127.0.0.1 and the configured port, 3780)
  web password         set the password that lets the web overview run actions
  gc [--yes]           destroy previews whose worktree no longer exists (dry run by default)
  prune [--yes]        trim Docker build cache and old unused images host-wide (dry run by default)
  share [preview] [--host NAME] [--hours N]
                       make a running preview public through a Cloudflare tunnel until it expires
  unshare [preview]    end a preview's public share (--expired: every expired share, plus leftovers)
  shares               list public shares
  pair [--json]        pair a device with T3 Code: a one-time link and QR code, valid 10 minutes
  history [preview] [-n N]
                       show what devkit did on this host, newest last (default the last 30 events)
  up|start|stop|restart --vm TARGET
                       control a configured VM by name, UUID or control ID; retain disks
  start [preview]      resume a stopped preview; data is retained
  stop [preview]       stop a preview's containers; data is retained
  destroy [preview]    remove a preview's containers, networks and volumes

Without a preview ID, start, stop, destroy, share and unshare act on the current worktree's preview.
stop, destroy and gc end a preview's share first.
`

func main() {
	if filepath.Base(os.Args[0]) == "devbox" {
		fmt.Fprintln(os.Stderr, "devkit: devbox is now called devkit; the old name still works for now.")
	}
	code, err := dispatch(os.Args[1:])
	if err != nil {
		var help helpRequested
		if errors.As(err, &help) {
			fmt.Print(usage)
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "devkit: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

type helpRequested struct{}

func (helpRequested) Error() string { return "help" }

type usageError string

func (e usageError) Error() string { return string(e) + "\n\n" + usage }

// optionalArg parses at most one positional argument.
func optionalArg(command string, args []string) (string, error) {
	if len(args) > 1 {
		return "", usageError(fmt.Sprintf("%s takes at most one argument", command))
	}
	if len(args) == 1 {
		if args[0] == "-h" || args[0] == "--help" {
			return "", helpRequested{}
		}
		return args[0], nil
	}
	return "", nil
}

func flags(command string) *flag.FlagSet {
	set := flag.NewFlagSet(command, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	return set
}

func parse(set *flag.FlagSet, args []string) error {
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return helpRequested{}
		}
		return usageError(err.Error())
	}
	if set.NArg() > 0 {
		return usageError(fmt.Sprintf("%s: unexpected argument %q", set.Name(), set.Arg(0)))
	}
	return nil
}

// splitArgs separates flags from positional arguments in any order; valued flags take the next
// argument or --flag=value.
func splitArgs(args []string, valued, switches map[string]bool) (positional []string, values map[string]string, err error) {
	values = map[string]string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-h" || arg == "--help" {
			return nil, nil, helpRequested{}
		}
		name, value, hasValue := strings.Cut(arg, "=")
		switch {
		case valued[name] && hasValue:
			values[name] = value
		case valued[name]:
			if i+1 >= len(args) {
				return nil, nil, usageError(name + " needs a value")
			}
			i++
			values[name] = args[i]
		case switches[arg]:
			values[arg] = ""
		case strings.HasPrefix(arg, "-"):
			return nil, nil, usageError(fmt.Sprintf("unknown option %s", arg))
		default:
			positional = append(positional, arg)
		}
	}
	return positional, values, nil
}

func dispatch(args []string) (int, error) {
	if len(args) == 0 {
		return 2, usageError("missing command")
	}
	command, rest := args[0], args[1:]
	if command == "up" || command == "start" || command == "stop" || command == "restart" {
		positional, values, err := splitArgs(rest, map[string]bool{"--vm": true}, nil)
		if err != nil {
			return 2, err
		}
		if target, ok := values["--vm"]; ok {
			if target == "" || len(positional) > 0 {
				return 2, usageError("Use " + command + " --vm TARGET without a preview ID.")
			}
			action := command
			if action == "up" {
				action = "start"
			}
			return 0, noted("vm-"+action, target, controlVM(action, target))
		}
		if command == "restart" {
			return 2, usageError("restart needs --vm TARGET")
		}
		if command == "up" {
			command = "live"
		}
	}
	switch command {
	case "git":
		dashboard := len(rest) == 2 && rest[1] == "--dashboard"
		if len(rest) != 1 && !dashboard {
			return 2, usageError("usage: devkit git fetch|sync|push")
		}
		root, err := os.Getwd()
		if err != nil {
			return 1, err
		}
		return 0, noted("git-"+rest[0], "", runGitAction(root, rest[0], dashboard))
	case "-h", "--help", "help":
		return 0, helpRequested{}
	case "version", "--version":
		version, commit := buildVersion()
		fmt.Fprintln(stdout, strings.TrimSpace("devkit "+firstOf(version, "(unknown version)")+" "+commit))
		return 0, nil
	case "setup":
		set := flags(command)
		var opts setupOptions
		set.BoolVar(&opts.yes, "yes", false, "")
		set.StringVar(&opts.domain, "domain", "", "")
		set.StringVar(&opts.email, "email", "", "")
		set.StringVar(&opts.harness, "harness", "", "")
		if err := parse(set, rest); err != nil {
			return 2, err
		}
		return 0, noted("setup", "", setup(opts))
	case "cleanup":
		positional, values, err := splitArgs(rest, map[string]bool{"--vm": true, "--project": true}, map[string]bool{"--purge": true, "--yes": true})
		if err != nil {
			return 2, err
		}
		name, hasName := values["--vm"]
		project, hasProject := values["--project"]
		if len(positional) != 0 || hasName == hasProject || (hasName && strings.TrimSpace(name) == "") || (hasProject && !projectSlug.MatchString(project)) {
			return 2, usageError("Use cleanup --vm NAME or cleanup --project SLUG, optionally --purge and --yes.")
		}
		_, purge := values["--purge"]
		_, yes := values["--yes"]
		return 0, noted("vm-cleanup", firstOf(name, project), cleanupVMs(name, project, purge, yes))
	case "vms":
		set := flags(command)
		asJSON := set.Bool("json", false, "")
		if err := parse(set, rest); err != nil {
			return 2, err
		}
		return 0, listVMs(*asJSON)
	case "doctor":
		if err := parse(flags(command), rest); err != nil {
			return 2, err
		}
		return doctor()
	case "proxy":
		if len(rest) != 1 || rest[0] != "up" {
			if len(rest) == 1 && (rest[0] == "-h" || rest[0] == "--help") {
				return 0, helpRequested{}
			}
			return 2, usageError("usage: devkit proxy up")
		}
		return 0, noted("proxy up", "", proxyUp())
	case "live":
		if len(rest) > 0 {
			if _, err := optionalArg(command, rest); err != nil {
				return 2, err
			}
			return 2, usageError("live takes no arguments")
		}
		return 0, noted("live", "", preparePreview("live", ""))
	case "review", "publish":
		ref, err := optionalArg(command, rest)
		if err != nil {
			return 2, err
		}
		if ref == "" {
			ref = "HEAD"
		}
		if command == "review" {
			return 0, noted("review", "", preparePreview("review", ref))
		}
		return 0, noted("publish", "", publish(ref))
	case "exec":
		// Only a leading -s/--service is devkit's; everything after it belongs to the command.
		service := ""
		if len(rest) > 0 {
			name, value, hasValue := strings.Cut(rest[0], "=")
			switch {
			case (name == "-s" || name == "--service") && hasValue:
				service, rest = value, rest[1:]
			case rest[0] == "-s" || rest[0] == "--service":
				if len(rest) < 2 {
					return 2, usageError(rest[0] + " needs a value")
				}
				service, rest = rest[1], rest[2:]
			}
		}
		if len(rest) > 0 && rest[0] == "--" {
			rest = rest[1:]
		}
		if len(rest) == 0 {
			return 1, fail("Usage: devkit exec [-s service] <command> [args...]")
		}
		return execLive(service, rest)
	case "logs":
		set := flags(command)
		follow := set.Bool("follow", false, "")
		set.BoolVar(follow, "f", false, "")
		service := set.String("service", "", "")
		set.StringVar(service, "s", "", "")
		if err := parse(set, rest); err != nil {
			return 2, err
		}
		return 0, logs(*service, *follow)
	case "ls":
		if err := parse(flags(command), rest); err != nil {
			return 2, err
		}
		return 0, listing()
	case "web":
		if len(rest) > 0 && rest[0] == "password" {
			if len(rest) > 1 {
				return 2, usageError("web password takes no arguments")
			}
			return 0, setWebPassword()
		}
		cfg, err := loadHostConfig()
		if err != nil {
			return 1, err
		}
		set := flags(command)
		host := set.String("host", "127.0.0.1", "")
		port := set.Int("port", cfg.Web.Port, "")
		if err := parse(set, rest); err != nil {
			return 2, err
		}
		return 0, serve(*host, *port)
	case "gc":
		set := flags(command)
		yes := set.Bool("yes", false, "")
		if err := parse(set, rest); err != nil {
			return 2, err
		}
		if !*yes {
			return 0, gc(false)
		}
		return 0, noted("gc", "", gc(true))
	case "prune":
		set := flags(command)
		yes := set.Bool("yes", false, "")
		if err := parse(set, rest); err != nil {
			return 2, err
		}
		if !*yes {
			return 0, prune(false)
		}
		return 0, noted("prune", "", prune(true))
	case "share":
		positional, values, err := splitArgs(rest, map[string]bool{"--host": true, "--hours": true}, nil)
		if err != nil {
			return 2, err
		}
		name, err := optionalArg(command, positional)
		if err != nil {
			return 2, err
		}
		hours := 0
		if v, ok := values["--hours"]; ok {
			if _, err := fmt.Sscan(v, &hours); err != nil || hours < 1 {
				return 2, usageError("--hours takes a whole number of hours")
			}
		}
		return 0, noted("share", name, sharePreview(name, values["--host"], hours))
	case "unshare":
		positional, values, err := splitArgs(rest, nil, map[string]bool{"--expired": true})
		if err != nil {
			return 2, err
		}
		name, err := optionalArg(command, positional)
		if err != nil {
			return 2, err
		}
		if _, expired := values["--expired"]; expired {
			if name != "" {
				return 2, usageError("unshare --expired takes no preview")
			}
			return 0, noted("unshare --expired", "", cleanShares())
		}
		return 0, noted("unshare", name, unsharePreview(name))
	case "shares":
		if err := parse(flags(command), rest); err != nil {
			return 2, err
		}
		return 0, listShares()
	case "pair":
		set := flags(command)
		asJSON := set.Bool("json", false, "")
		if err := parse(set, rest); err != nil {
			return 2, err
		}
		return 0, noted("pair", "", pair(*asJSON))
	case "history":
		positional, values, err := splitArgs(rest, map[string]bool{"-n": true}, nil)
		if err != nil {
			return 2, err
		}
		name, err := optionalArg(command, positional)
		if err != nil {
			return 2, err
		}
		limit := 30
		if v, ok := values["-n"]; ok {
			if _, err := fmt.Sscan(v, &limit); err != nil || limit < 1 {
				return 2, usageError("-n takes a whole number of events")
			}
		}
		return 0, showHistory(name, limit)
	case "start", "stop", "destroy":
		name, err := optionalArg(command, rest)
		if err != nil {
			return 2, err
		}
		return 0, noted(command, name, lifecycle(command, name))
	}
	return 2, usageError(fmt.Sprintf("unknown command %q", strings.TrimSpace(command)))
}
