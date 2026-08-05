package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// version, commit, and date are set via -ldflags at build time (see
// .goreleaser.yaml); they default to placeholders for `go build`/`go run`.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// Execute runs the miaoprobe root command.
func Execute() error {
	root := &cobra.Command{
		Use:          "miaoprobe",
		Short:        "Media unlock / network probe tool driven by embedded JS scripts",
		Version:      fmt.Sprintf("%s (commit %s, built %s)", version, commit, date),
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return loadConfig(cmd)
		},
	}
	root.PersistentFlags().String("config", "", "path to a YAML config file (default: auto-discover $XDG_CONFIG_HOME/miaoprobe/config.yaml, ~/.config/miaoprobe/config.yaml, then /etc/miaoprobe/config.yaml)")
	root.PersistentFlags().String("log-level", "info", "log level: trace, debug, info, warn, or error")
	root.PersistentFlags().String("log-format", "rich", "log format: rich (colored console), text, or json")
	root.AddCommand(newCheckCommand())
	root.AddCommand(newServeCommand())
	return root.Execute()
}
