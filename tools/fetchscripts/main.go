// Command fetchscripts downloads the latest miaospeed-scripts nightly
// release (https://github.com/CloudPassenger/miaospeed-scripts) and lays
// it out under internal/script/embedded/ so that building miaoprobe with
// -tags embedscripts can go:embed it (see
// internal/script/embedded_scripts.go). It is normally invoked via `make
// fetch-scripts`, a prerequisite of `make build`.
package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	upstreamRepo = "CloudPassenger/miaospeed-scripts"
	nightlyTag   = "nightly"
)

// release mirrors the subset of GitHub's release API response this tool
// needs.
type release struct {
	TagName   string `json:"tag_name"`
	Name      string `json:"name"`
	Published string `json:"published_at"`
	Assets    []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fetchscripts:", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := embeddedDir()
	if err != nil {
		return err
	}
	distDir := filepath.Join(root, "dist")

	rel, err := fetchRelease()
	if err != nil {
		return fmt.Errorf("fetch release metadata: %w", err)
	}

	indexURL, err := assetURL(rel, "index.json")
	if err != nil {
		return err
	}
	zipURL, err := assetURL(rel, "scripts.zip")
	if err != nil {
		return err
	}

	if err := os.RemoveAll(distDir); err != nil {
		return fmt.Errorf("clean %s: %w", distDir, err)
	}
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", distDir, err)
	}

	indexPath := filepath.Join(distDir, "index.json")
	if err := downloadFile(indexURL, indexPath); err != nil {
		return fmt.Errorf("download index.json: %w", err)
	}

	zipPath := filepath.Join(root, "scripts.zip")
	if err := downloadFile(zipURL, zipPath); err != nil {
		return fmt.Errorf("download scripts.zip: %w", err)
	}
	defer func() { _ = os.Remove(zipPath) }()

	if err := extractZip(zipPath, distDir); err != nil {
		return fmt.Errorf("extract scripts.zip: %w", err)
	}

	version := releaseVersion(rel)
	versionPath := filepath.Join(root, "VERSION")
	if err := os.WriteFile(versionPath, []byte(version+"\n"), 0o644); err != nil {
		return fmt.Errorf("write VERSION: %w", err)
	}

	fmt.Printf("fetchscripts: embedded miaospeed-scripts %s into %s\n", version, distDir)
	return nil
}

// embeddedDir returns internal/script/embedded relative to this tool's own
// source location, so `go run ./tools/fetchscripts` works regardless of
// the caller's working directory.
func embeddedDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(wd, "internal", "script", "embedded"), nil
}

func fetchRelease() (*release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", upstreamRepo, nightlyTag)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET %s: %s: %s", url, resp.Status, strings.TrimSpace(string(body)))
	}

	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func assetURL(rel *release, name string) (string, error) {
	for _, a := range rel.Assets {
		if a.Name == name {
			return a.URL, nil
		}
	}
	return "", fmt.Errorf("release %s has no %q asset", rel.TagName, name)
}

func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(f, resp.Body)
	return err
}

func extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()

	for _, f := range r.File {
		path := filepath.Join(destDir, filepath.FromSlash(f.Name))
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := extractZipFile(f, path); err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
	}
	return nil
}

func extractZipFile(f *zip.File, dest string) error {
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, src)
	return err
}

// shortCommitRE picks the trailing short SHA out of nightly release names
// such as "🌙 Nightly 构建 · 2b98661".
var shortCommitRE = regexp.MustCompile(`([0-9a-f]{7,40})\s*$`)

// releaseVersion builds a compact version string like "nightly@2b98661
// (2026-08-05)" from a release's tag, name, and publish date.
func releaseVersion(rel *release) string {
	version := rel.TagName
	if m := shortCommitRE.FindStringSubmatch(rel.Name); m != nil {
		version += "@" + m[1]
	}
	if rel.Published != "" {
		if t, err := time.Parse(time.RFC3339, rel.Published); err == nil {
			version += " (" + t.Format("2006-01-02") + ")"
		}
	}
	return version
}
