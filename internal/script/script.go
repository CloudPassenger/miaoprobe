package script

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Script is one loadable check: its source and the metadata used for
// filtering/labeling (id/name/regions/tags come from index.json when
// available, or are derived from the file name for a standalone script).
type Script struct {
	ID          string
	Name        string
	Description string
	Category    string
	Regions     []string
	Tags        []string
	Priority    int
	Path        string
	Source      string
}

// manifestEntry mirrors one element of miaospeed-scripts/dist/index.json.
type manifestEntry struct {
	ID          string   `json:"id"`
	Path        string   `json:"path"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Regions     []string `json:"regions"`
	Tags        []string `json:"tags"`
	Priority    int      `json:"priority"`
}

// Load resolves target into a list of scripts. target may be a single .js
// file, or a directory containing an index.json manifest (as produced by
// miaospeed-scripts' build) alongside the referenced .js files.
func Load(target string) ([]Script, error) {
	info, err := os.Stat(target)
	if err != nil {
		return nil, fmt.Errorf("stat scripts path: %w", err)
	}

	if !info.IsDir() {
		return loadSingleFile(target)
	}
	return loadDirectory(target)
}

func loadSingleFile(path string) ([]Script, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read script %s: %w", path, err)
	}
	base := filepath.Base(path)
	id := strings.TrimSuffix(base, filepath.Ext(base))
	return []Script{{
		ID:     id,
		Name:   id,
		Path:   path,
		Source: string(src),
	}}, nil
}

func loadDirectory(dir string) ([]Script, error) {
	scripts, err := loadFS(os.DirFS(dir))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	for i := range scripts {
		scripts[i].Path = filepath.Join(dir, filepath.FromSlash(scripts[i].Path))
	}
	return scripts, nil
}

// loadFS is the shared implementation behind loadDirectory and the
// embedded-scripts loader (see embedded.go): it reads an index.json
// manifest and the .js files it references from fsys, which uses "/"
// paths regardless of OS (as required by fs.FS/embed.FS).
func loadFS(fsys fs.FS) ([]Script, error) {
	data, err := fs.ReadFile(fsys, "index.json")
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var entries []manifestEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	scripts := make([]Script, 0, len(entries))
	for _, e := range entries {
		scriptPath := path.Clean(e.Path)
		src, err := fs.ReadFile(fsys, scriptPath)
		if err != nil {
			return nil, fmt.Errorf("read script %s (id=%s): %w", scriptPath, e.ID, err)
		}
		scripts = append(scripts, Script{
			ID:          e.ID,
			Name:        e.Name,
			Description: e.Description,
			Category:    e.Category,
			Regions:     e.Regions,
			Tags:        e.Tags,
			Priority:    e.Priority,
			Path:        scriptPath,
			Source:      string(src),
		})
	}
	return scripts, nil
}
