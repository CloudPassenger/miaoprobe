package script

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSingleFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "netflix.js")
	if err := os.WriteFile(path, []byte("module.exports = function(){ return {text:'ok',background:'186,230,126'}; };"), 0o644); err != nil {
		t.Fatal(err)
	}

	scripts, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(scripts) != 1 || scripts[0].ID != "netflix" {
		t.Fatalf("unexpected scripts: %+v", scripts)
	}
}

func TestLoadDirectoryWithManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "global", "netflix.js"), []byte("module.exports = function(){ return {text:'ok',background:'186,230,126'}; };"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `[{"id":"netflix","path":"global/netflix.js","name":"Netflix","description":"d","regions":["global"],"tags":["stream"],"priority":1}]`
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	scripts, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(scripts) != 1 {
		t.Fatalf("expected 1 script, got %d", len(scripts))
	}
	s := scripts[0]
	if s.ID != "netflix" || s.Name != "Netflix" || len(s.Regions) != 1 || s.Regions[0] != "global" {
		t.Fatalf("unexpected script metadata: %+v", s)
	}
}

func TestFilterByRegionAndTag(t *testing.T) {
	scripts := []Script{
		{ID: "a", Regions: []string{"global"}, Tags: []string{"stream"}},
		{ID: "b", Regions: []string{"hk"}, Tags: []string{"tool"}},
	}

	got := Filter(scripts, ParseFilter("hk"))
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("unexpected filter result: %+v", got)
	}

	got = Filter(scripts, ParseFilter(""))
	if len(got) != 2 {
		t.Fatalf("empty filter should match all, got %+v", got)
	}

	got = Filter(scripts, ParseFilter("stream,tool"))
	if len(got) != 2 {
		t.Fatalf("expected both scripts to match, got %+v", got)
	}
}
