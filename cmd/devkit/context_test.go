package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextFullIdentityAndPartialInventory(t *testing.T) {
	repo := newGitRepo(t)
	t.Chdir(repo.root)
	os.WriteFile(filepath.Join(repo.root, "dirty.txt"), []byte("private"), 0600)
	swap(t, &dockerContainers, func() ([]container, error) { return nil, fail("secret-SENTINEL") })
	code, r := validationCLI(t, "context", "--json")
	b := string(mustJSON(t, r))
	if code != 1 || r["complete"] != false || !strings.Contains(b, repo.commit) || !strings.Contains(b, `"dirty":true`) || strings.Contains(b, "SENTINEL") {
		t.Fatal(b)
	}
}
func TestListingJSONPreservesFullCommit(t *testing.T) {
	sha := strings.Repeat("a", 40)
	c := container{}
	c.Config.Labels = map[string]string{managedLabel: "true", "devkit.web": "true", "devkit.preview": "demo", "devkit.mode": "review", "devkit.commit": sha, "secret": "SENTINEL"}
	swap(t, &dockerContainers, func() ([]container, error) { return []container{c}, nil })
	code, r := validationCLI(t, "ls", "--json")
	b := string(mustJSON(t, r))
	if code != 0 || !strings.Contains(b, sha) || strings.Contains(b, "SENTINEL") {
		t.Fatal(b)
	}
}
func TestDoctorJSONNeverSerializesDetail(t *testing.T) {
	r := doctorReport([]check{bad("docker", "secret SENTINEL"), warn("share", "credential %s", "SENTINEL")}, false)
	b := string(mustJSON(t, r))
	if strings.Contains(b, "SENTINEL") || r.exitCode() != 1 || r.Complete {
		t.Fatal(b)
	}
	if doctorReport([]check{warn("agent-context", "private")}, true).exitCode() != 1 {
		t.Fatal("agents warnings must fail")
	}
	if doctorReport([]check{warn("share", "private")}, false).exitCode() != 0 {
		t.Fatal("host warnings must preserve success")
	}
}
