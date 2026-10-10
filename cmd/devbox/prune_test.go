package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestPruneDryRunOnlyReportsAndApplyNeverTouchesVolumes(t *testing.T) {
	var calls []string
	swap(t, &runCommand, func(args []string, o runOpts) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		switch args[1] {
		case "builder":
			return "ID\tRECLAIMABLE\tSIZE\nabc\ttrue\t1GB\nTotal:\t12.5GB", nil
		case "image":
			return "Deleted Images:\nsha256:abc\n\nTotal reclaimed space: 3.1GB", nil
		}
		return "TYPE SIZE", nil
	})
	out := &bytes.Buffer{}
	swap[io.Writer](t, &stdout, out)

	if err := prune(false); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != "docker system df" {
		t.Fatalf("dry run ran %v", calls)
	}

	calls = nil
	if err := prune(true); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"docker builder prune --force --max-used-space 30GB",
		"docker image prune --all --force --filter until=168h",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("ran %v, want %v", calls, want)
	}
	if !strings.Contains(out.String(), "build cache 12.5GB, images 3.1GB") {
		t.Errorf("output %q", out.String())
	}
}

func TestReclaimedWithoutTotalIsZero(t *testing.T) {
	if got := reclaimed(""); got != "0B" {
		t.Errorf("reclaimed(\"\") = %q", got)
	}
}
