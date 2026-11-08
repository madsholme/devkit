package main

import (
	"regexp"
	"strings"
	"testing"
)

func TestDashboardAssetsIncludeCompiledEntryPoints(t *testing.T) {
	assets, err := loadAssets()
	if err != nil {
		t.Fatal(err)
	}
	index := assets["/"]
	if index.cache != "no-store" || !strings.Contains(string(index.body), `id="app"`) {
		t.Fatal("dashboard entry must be the non-cached Vue page")
	}
	refs := regexp.MustCompile(`(?:src|href)="([^"]+)"`).FindAllStringSubmatch(string(index.body), -1)
	scripts, styles := 0, 0
	for _, ref := range refs {
		resource, ok := assets[ref[1]]
		if !ok {
			t.Errorf("entry references an unserved asset: %s", ref[1])
			continue
		}
		if strings.HasSuffix(ref[1], ".js") {
			scripts++
			if resource.kind != "text/javascript; charset=utf-8" || !strings.Contains(resource.cache, "immutable") {
				t.Errorf("bad script headers: %s", ref[1])
			}
		}
		if strings.HasSuffix(ref[1], ".css") {
			styles++
			if resource.kind != "text/css; charset=utf-8" {
				t.Errorf("bad CSS headers: %s", ref[1])
			}
		}
	}
	if scripts < 2 || styles == 0 {
		t.Fatalf("missing app/theme boot or CSS: %d scripts, %d styles", scripts, styles)
	}
	for _, source := range []string{"/src/main.ts", "/package.json", "/index.html", "/../frontend/package.json", "/assets/"} {
		if _, exposed := assets[source]; exposed {
			t.Errorf("exposed source or directory: %s", source)
		}
	}
}
