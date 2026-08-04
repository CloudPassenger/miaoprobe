package cli

import "github.com/spf13/cobra"

// Execute runs the miaoprobe root command.
func Execute() error {
	root := &cobra.Command{
		Use:          "miaoprobe",
		Short:        "Media unlock / network probe tool driven by embedded JS scripts",
		SilenceUsage: true,
	}
	root.AddCommand(newCheckCommand())
	root.AddCommand(newServeCommand())
	return root.Execute()
}
