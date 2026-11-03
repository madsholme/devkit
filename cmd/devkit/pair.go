package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// devkit pair issues a one-time T3 Code pairing link for this host, for the CLI and the dashboard.
// The link is a secret: it is printed or returned once and never written to history.

const pairTTL = "10m" // in t3's duration format

type pairing struct {
	URL     string    `json:"url"`
	Code    string    `json:"code"`
	Expires time.Time `json:"expires"`
	QR      []string  `json:"qr"` // rows of the QR code, "1" for dark, without the quiet zone
}

// t3Binary is the t3 the T3 Code service runs, so pairing never opens T3's database with another
// version; without a service record it is the t3 on PATH or in ~/.local/bin.
var t3Binary = func() string {
	home := os.Getenv("T3CODE_HOME")
	if home == "" {
		h, _ := os.UserHomeDir()
		home = filepath.Join(h, ".t3")
	}
	var state struct {
		ActiveVersion string `json:"activeVersion"`
	}
	if data, err := os.ReadFile(filepath.Join(home, "runtime", "service-state.json")); err == nil &&
		json.Unmarshal(data, &state) == nil && state.ActiveVersion != "" && !strings.ContainsAny(state.ActiveVersion, `/\`) {
		if path := filepath.Join(home, "runtime", "versions", state.ActiveVersion, "t3"); isFile(path) {
			return path
		}
	}
	return findT3()
}

func pairDevice() (pairing, error) {
	var p pairing
	cfg, err := loadHostConfig()
	if err != nil {
		return p, err
	}
	h := cfg.Harness
	if h == nil || h.Name != t3Harness.Name || h.Host == "" {
		return p, fail("Pairing needs the T3 Code harness; run devkit setup and choose t3.")
	}
	dom, err := domain()
	if err != nil {
		return p, err
	}
	if !harnessUp(*h) {
		return p, fail("T3 Code does not answer at %s; see devkit doctor.", h.URL)
	}
	t3 := t3Binary()
	if t3 == "" {
		return p, fail("T3 Code is not installed here; run devkit setup.")
	}
	out, err := runCommand([]string{t3, "auth", "pairing", "create", "--json", "--ttl", pairTTL,
		"--label", "devkit " + actor, "--base-url", "https://" + h.Host + "." + dom}, runOpts{capture: true})
	if err != nil {
		return p, err
	}
	var created struct {
		Credential string    `json:"credential"`
		ExpiresAt  time.Time `json:"expiresAt"`
		PairURL    string    `json:"pairUrl"`
	}
	// t3 may log before its JSON; the object is the last thing it prints.
	if i := strings.LastIndex(out, "\n{"); i >= 0 {
		out = out[i+1:]
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil || !strings.HasPrefix(created.PairURL, "https://") {
		return p, fail("T3 Code did not return a pairing link.")
	}
	modules, err := qrCode(created.PairURL)
	if err != nil {
		return p, err
	}
	p = pairing{URL: created.PairURL, Code: created.Credential, Expires: created.ExpiresAt}
	for _, row := range modules {
		var b strings.Builder
		for _, dark := range row {
			b.WriteByte("01"[boolInt(dark)])
		}
		p.QR = append(p.QR, b.String())
	}
	happened("pair", "", "T3 Code device pairing, valid until "+created.ExpiresAt.Local().Format("15:04"))
	return p, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func pair(asJSON bool) error {
	p, err := pairDevice()
	if err != nil {
		return err
	}
	if asJSON {
		return json.NewEncoder(stdout).Encode(p)
	}
	modules := make([][]bool, len(p.QR))
	for y, row := range p.QR {
		for _, c := range row {
			modules[y] = append(modules[y], c == '1')
		}
	}
	fmt.Fprintf(stdout, "Pair one device with T3 Code until %s. Scan the code, or open\n  %s\n", p.Expires.Local().Format("15:04"), p.URL)
	if p.Code != "" {
		fmt.Fprintf(stdout, "or enter the code %s in the T3 app.\n", p.Code)
	}
	fmt.Fprint(stdout, "\n"+qrText(modules))
	return nil
}
