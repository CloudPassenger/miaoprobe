package script

import "fmt"

// EmbeddedAvailable reports whether this binary was built with an embedded
// miaospeed-scripts build. Regular builds (plain `go build`/`make build`)
// do not embed anything; `make build-embedded` (or any `go build` with
// `-tags embedscripts`) fetches the latest miaospeed-scripts nightly
// release into internal/script/embedded/ and bakes it into the binary
// using Go's embed directive. See tools/fetchscripts.
func EmbeddedAvailable() bool {
	return embeddedFS != nil
}

// EmbeddedVersion returns the embedded miaospeed-scripts version (e.g.
// "nightly@2b98661"), or "" if this binary has no embedded scripts.
func EmbeddedVersion() string {
	return embeddedVersion
}

// LoadEmbedded loads the miaospeed-scripts build embedded into this binary
// at build time. It returns an error if EmbeddedAvailable is false.
func LoadEmbedded() ([]Script, error) {
	if embeddedFS == nil {
		return nil, fmt.Errorf("no scripts embedded in this build: pass --scripts, or use a build with -tags embedscripts")
	}
	return loadFS(embeddedFS)
}
