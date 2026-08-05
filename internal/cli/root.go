package cli

import "github.com/spf13/cobra"

// Execute runs the miaoprobe root command.
func Execute() error {
	root := &cobra.Command{
		Use:          "miaoprobe",
		Short:        "Media unlock / network probe tool driven by embedded JS scripts",
		SilenceUsage: true,
	}
	root.PersistentFlags().String("log-level", "info", "log level: trace, debug, info, warn, or error")
	root.PersistentFlags().String("log-format", "rich", "log format: rich (colored console), text, or json")
	root.AddCommand(newCheckCommand())
	root.AddCommand(newServeCommand())
	return root.Execute()
}
