//go:build embedscripts

package script

import (
	"embed"
	"io/fs"
	"strings"
)

// embedded/dist and embedded/VERSION are populated by `go run
// ./tools/fetchscripts` (wired up as the Makefile's `fetch-scripts`
// target, a prerequisite of `build`) before this file is
// compiled; it downloads the latest miaospeed-scripts nightly release
// (index.json + scripts.zip) and records its version. Building with
// -tags embedscripts without running that first fails here with "no
// matching files found".
//
//go:embed embedded/dist
var embeddedDistFS embed.FS

//go:embed embedded/VERSION
var embeddedVersionFile string

var embeddedVersion = strings.TrimSpace(embeddedVersionFile)

var embeddedFS fs.FS = func() fs.FS {
	sub, err := fs.Sub(embeddedDistFS, "embedded/dist")
	if err != nil {
		panic(err)
	}
	return sub
}()
