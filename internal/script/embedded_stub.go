//go:build !embedscripts

package script

import "io/fs"

// embeddedFS and embeddedVersion are nil/"" in ordinary builds. Building
// with -tags embedscripts swaps in embedded_scripts.go instead, which
// embeds the miaospeed-scripts build fetched by tools/fetchscripts.
var (
	embeddedFS      fs.FS
	embeddedVersion string
)
