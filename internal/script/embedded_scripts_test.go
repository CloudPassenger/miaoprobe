//go:build embedscripts

package script

import "testing"

// TestEmbeddedWithBuildTag only runs for `go test -tags embedscripts ./...`
// (after `make fetch-scripts`), verifying the embedded miaospeed-scripts
// build actually loads.
func TestEmbeddedWithBuildTag(t *testing.T) {
	if !EmbeddedAvailable() {
		t.Fatal("EmbeddedAvailable() = false with -tags embedscripts")
	}
	if EmbeddedVersion() == "" {
		t.Fatal("EmbeddedVersion() is empty")
	}
	scripts, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	if len(scripts) == 0 {
		t.Fatal("LoadEmbedded returned no scripts")
	}
}
