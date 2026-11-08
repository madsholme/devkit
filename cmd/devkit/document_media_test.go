package main

import (
	"encoding/base64"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocumentImages(t *testing.T) {
	root := documentHost(t)
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aN1cAAAAASUVORK5CYII=")
	os.MkdirAll(filepath.Join(root, "docs", "images"), 0755)
	os.WriteFile(filepath.Join(root, "docs/images/one.png"), png, 0644)
	data, kind, err := readDocumentImage(root, "docs/images/one.png")
	if err != nil || kind != "image/png" || len(data) != len(png) {
		t.Fatalf("image: %s %v", kind, err)
	}
	os.WriteFile(filepath.Join(root, "logo.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0"/></svg>`), 0644)
	if _, kind, err := readDocumentImage(root, "logo.svg"); err != nil || kind != "image/svg+xml" {
		t.Fatalf("svg: %s %v", kind, err)
	}
	outside := filepath.Join(t.TempDir(), "private.png")
	os.WriteFile(outside, png, 0600)
	os.Symlink(outside, filepath.Join(root, "linked.png"))
	os.Link(outside, filepath.Join(root, "hard.png"))
	os.WriteFile(filepath.Join(root, "fake.png"), []byte("<html>not an image</html>"), 0644)
	os.WriteFile(filepath.Join(root, "dtd.svg"), []byte(`<!DOCTYPE svg><svg/>`), 0644)
	os.WriteFile(filepath.Join(root, "huge.png"), make([]byte, 8*1024*1024+1), 0644)
	for _, path := range []string{"../private.png", "/etc/passwd", ".git/logo.png", "docs/../logo.svg", "linked.png", "hard.png", "fake.png", "dtd.svg", "huge.png", "AGENTS.md"} {
		if _, _, err := readDocumentImage(root, path); err == nil {
			t.Errorf("accepted unsafe %s", path)
		}
	}
	if _, _, err := readDocumentImage(t.TempDir(), "one.png"); err == nil {
		t.Fatal("unregistered checkout")
	}
}

func TestDocumentImageHeaders(t *testing.T) {
	root := documentHost(t)
	os.WriteFile(filepath.Join(root, "logo.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), 0644)
	req := httptest.NewRequest("GET", "/api/document-image?"+url.Values{"worktree": {root}, "path": {"logo.svg"}}.Encode(), nil)
	res := httptest.NewRecorder()
	if err := serveDocumentImage(res, req); err != nil {
		t.Fatal(err)
	}
	if res.Code != 200 || res.Header().Get("Content-Type") != "image/svg+xml" || res.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(res.Header().Get("Content-Security-Policy"), "sandbox") || res.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unsafe headers: %+v", res.Header())
	}
}
