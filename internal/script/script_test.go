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
	manifest := `[{"id":"netflix","path":"global/netflix.js","name":"Netflix","description":"d","category":"media","regions":["global"],"tags":["stream"],"priority":1}]`
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
	if s.ID != "netflix" || s.Name != "Netflix" || s.Category != "media" || len(s.Regions) != 1 || s.Regions[0] != "global" {
		t.Fatalf("unexpected script metadata: %+v", s)
	}
}

func TestSelect(t *testing.T) {
	scripts := []Script{
		{ID: "netflix", Category: "media", Regions: []string{"global"}, Tags: []string{"stream"}},
		{ID: "chatgpt", Category: "ai", Regions: []string{"global"}, Tags: []string{"ai"}},
		{ID: "ipquality", Category: "network", Regions: []string{"hk"}, Tags: []string{"tool"}},
	}

	// Zero-value spec: everything passes.
	if got := Select(scripts, FilterSpec{}); len(got) != 3 {
		t.Fatalf("expected all scripts with a zero-value spec, got %+v", got)
	}

	// ID alone.
	got := Select(scripts, FilterSpec{ID: []string{"chatgpt"}})
	if len(got) != 1 || got[0].ID != "chatgpt" {
		t.Fatalf("unexpected id-only selection: %+v", got)
	}

	// Category alone.
	got = Select(scripts, FilterSpec{Category: []string{"media"}})
	if len(got) != 1 || got[0].ID != "netflix" {
		t.Fatalf("unexpected category-only selection: %+v", got)
	}

	// Region alone.
	got = Select(scripts, FilterSpec{Region: []string{"hk"}})
	if len(got) != 1 || got[0].ID != "ipquality" {
		t.Fatalf("unexpected region-only selection: %+v", got)
	}

	// ID and Category are OR'd together.
	got = Select(scripts, FilterSpec{ID: []string{"chatgpt"}, Category: []string{"media"}})
	if len(got) != 2 {
		t.Fatalf("expected id+category to union, got %+v", got)
	}

	// Exclude mode keeps the complement of the match set.
	got = Select(scripts, FilterSpec{Mode: ModeExclude, Category: []string{"media"}})
	if len(got) != 2 {
		t.Fatalf("expected exclude mode to drop only the matching script, got %+v", got)
	}
	for _, s := range got {
		if s.ID == "netflix" {
			t.Fatalf("expected netflix to be excluded, got %+v", got)
		}
	}
}

func TestParseFilterFlag(t *testing.T) {
	spec, err := ParseFilterFlag("")
	if err != nil || !spec.IsZero() {
		t.Fatalf("expected zero-value spec for empty input, got %+v, err=%v", spec, err)
	}

	spec, err = ParseFilterFlag("category:media,ai ; region:hk,us; id:netflix ;mode:exclude")
	if err != nil {
		t.Fatalf("ParseFilterFlag: %v", err)
	}
	wantCategory := []string{"media", "ai"}
	wantRegion := []string{"hk", "us"}
	wantID := []string{"netflix"}
	if len(spec.Category) != len(wantCategory) || spec.Category[0] != wantCategory[0] || spec.Category[1] != wantCategory[1] {
		t.Errorf("category = %+v, want %+v", spec.Category, wantCategory)
	}
	if len(spec.Region) != len(wantRegion) || spec.Region[0] != wantRegion[0] || spec.Region[1] != wantRegion[1] {
		t.Errorf("region = %+v, want %+v", spec.Region, wantRegion)
	}
	if len(spec.ID) != len(wantID) || spec.ID[0] != wantID[0] {
		t.Errorf("id = %+v, want %+v", spec.ID, wantID)
	}
	if spec.Mode != ModeExclude {
		t.Errorf("mode = %q, want exclude", spec.Mode)
	}

	if _, err := ParseFilterFlag("bogus"); err == nil {
		t.Error("expected error for segment without a ':'")
	}
	if _, err := ParseFilterFlag("nope:x"); err == nil {
		t.Error("expected error for unknown key")
	}
	if _, err := ParseFilterFlag("mode:sideways"); err == nil {
		t.Error("expected error for invalid mode value")
	}
}
