package script

import "testing"

// TestEmbeddedWithoutBuildTag pins down the fallback behavior of a
// non-embedscripts build: no embedded scripts, and LoadEmbedded fails
// instead of panicking.
func TestEmbeddedWithoutBuildTag(t *testing.T) {
	if EmbeddedAvailable() {
		t.Skip("built with -tags embedscripts; see embedded_scripts_test.go-equivalent coverage via `make build-embedded`")
	}
	if v := EmbeddedVersion(); v != "" {
		t.Fatalf("EmbeddedVersion() = %q, want \"\"", v)
	}
	if _, err := LoadEmbedded(); err == nil {
		t.Fatal("LoadEmbedded() succeeded without an embedded build")
	}
}
