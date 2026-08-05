package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/CloudPassenger/miaoprobe/internal/script"
)

// version, commit, and date are set via -ldflags at build time (see
// .goreleaser.yaml); they default to placeholders for `go build`/`go run`.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// versionString reports miaoprobe's own version/commit/date, plus, for
// builds made with -tags embedscripts (see tools/fetchscripts and the
// Makefile's build-embedded target), the embedded miaospeed-scripts
// version and a note that --scripts defaults to it.
func versionString() string {
	v := fmt.Sprintf("%s (commit %s, built %s)", version, commit, date)
	if script.EmbeddedAvailable() {
		v += fmt.Sprintf("; embeds miaospeed-scripts %s (used by default when --scripts is not set)", script.EmbeddedVersion())
	}
	return v
}

// Execute runs the miaoprobe root command.
func Execute() error {
	root := &cobra.Command{
		Use:          "miaoprobe",
		Short:        "Media unlock / network probe tool driven by embedded JS scripts",
		Version:      versionString(),
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return loadConfig(cmd)
		},
	}
	root.PersistentFlags().String("config", "", "path to a YAML config file (default: auto-discover $XDG_CONFIG_HOME/miaoprobe/config.yaml, ~/.config/miaoprobe/config.yaml, then /etc/miaoprobe/config.yaml)")
	root.PersistentFlags().String("log.level", "info", "log level: trace, debug, info, warn, or error")
	root.PersistentFlags().String("log.format", "rich", "log format: rich (colored console), text, or json")
	root.AddCommand(newListCommand())
	root.AddCommand(newCheckCommand())
	root.AddCommand(newServeCommand())
	return root.Execute()
}
