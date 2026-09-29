// Command devbox is a small, single-host preview runner for Git worktrees.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

const usage = `usage: devbox <command> [arguments]

Small, single-host preview runner for Git worktrees.

commands:
  live                 start or resume a preview of working files
  review [commit]      fresh build from a committed version (default HEAD)
  publish [commit]     publish an existing reviewed image through the project publish hook
  exec <command...>    run a command in this worktree's live web container
  logs [-f]            show this worktree's live web log (-f, --follow to follow it)
  ls                   list managed previews, including stopped ones
  web [--host H] [--port P]
                       serve the read-only web overview (default 127.0.0.1:3780)
  gc [--yes]           destroy previews whose worktree no longer exists (dry run by default)
  start [preview]      resume a stopped preview; data is retained
  stop [preview]       stop a preview's containers; data is retained
  destroy [preview]    remove a preview's containers, networks and volumes

Without a preview ID, start, stop and destroy act on the current worktree's preview.
`

func main() {
	code, err := dispatch(os.Args[1:])
	if err != nil {
		var help helpRequested
		if errors.As(err, &help) {
			fmt.Print(usage)
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "devbox: %v\n", err)
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

func dispatch(args []string) (int, error) {
	if len(args) == 0 {
		return 2, usageError("missing command")
	}
	command, rest := args[0], args[1:]
	switch command {
	case "-h", "--help", "help":
		return 0, helpRequested{}
	case "live":
		if len(rest) > 0 {
			if _, err := optionalArg(command, rest); err != nil {
				return 2, err
			}
			return 2, usageError("live takes no arguments")
		}
		return 0, preparePreview("live", "")
	case "review", "publish":
		ref, err := optionalArg(command, rest)
		if err != nil {
			return 2, err
		}
		if ref == "" {
			ref = "HEAD"
		}
		if command == "review" {
			return 0, preparePreview("review", ref)
		}
		return 0, publish(ref)
	case "exec":
		if len(rest) > 0 && rest[0] == "--" {
			rest = rest[1:]
		}
		if len(rest) == 0 {
			return 1, fail("Usage: devbox exec <command> [args...]")
		}
		return execLive(rest)
	case "logs":
		set := flags(command)
		follow := set.Bool("follow", false, "")
		set.BoolVar(follow, "f", false, "")
		if err := parse(set, rest); err != nil {
			return 2, err
		}
		return 0, logs(*follow)
	case "ls":
		if err := parse(flags(command), rest); err != nil {
			return 2, err
		}
		return 0, listing()
	case "web":
		set := flags(command)
		host := set.String("host", "127.0.0.1", "")
		port := set.Int("port", 3780, "")
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
		return 0, gc(*yes)
	case "start", "stop", "destroy":
		name, err := optionalArg(command, rest)
		if err != nil {
			return 2, err
		}
		return 0, lifecycle(command, name)
	}
	return 2, usageError(fmt.Sprintf("unknown command %q", strings.TrimSpace(command)))
}
