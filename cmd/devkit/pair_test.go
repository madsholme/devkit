package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func TestPairingAsksTheServiceVersionOfT3AndKeepsTheSecretOutOfHistory(t *testing.T) {
	hostWith(t, `{"domain": "dev.example.com"}`)
	swap(t, &harnessUp, func(harnessConfig) bool { return true })
	swap(t, &t3Binary, func() string { return "/t3/versions/0.1.5/t3" })
	if _, err := pairDevice(); err == nil || !strings.Contains(err.Error(), "T3 Code harness") {
		t.Errorf("without T3: %v", err)
	}

	cfg, _ := readConfigFile()
	cfg["harness"] = t3Harness
	writeConfigFile(cfg)
	var ran []string
	swap(t, &runCommand, func(args []string, _ runOpts) (string, error) {
		ran = args
		// t3 may log before its JSON.
		return "[12:00] WARN something\n{\"credential\":\"SECRETCODE12\",\"expiresAt\":\"2026-11-03T12:10:00Z\"," +
			"\"pairUrl\":\"https://t3.dev.example.com/pair#token=SECRETCODE12\"}", nil
	})
	var out bytes.Buffer
	swap[io.Writer](t, &stdout, &out)
	if err := pair(true); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ran, " "); !strings.HasPrefix(got, "/t3/versions/0.1.5/t3 auth pairing create --json --ttl 10m") ||
		!strings.HasSuffix(got, "--base-url https://t3.dev.example.com") {
		t.Errorf("ran %q", got)
	}
	var p pairing
	if err := json.Unmarshal(out.Bytes(), &p); err != nil || p.Code != "SECRETCODE12" || len(p.QR) != 33 {
		t.Errorf("pairing %+v, %v", p, err)
	}
	history, _ := os.ReadFile(historyPath())
	if !strings.Contains(string(history), `"pair"`) || strings.Contains(string(history), "SECRET") {
		t.Errorf("history %s", history)
	}
}
