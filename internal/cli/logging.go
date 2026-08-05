package cli

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/CloudPassenger/miaoprobe/internal/logging"
)

// loggerFromFlags builds a logger from the --log.level/--log.format
// persistent flags, writing to stderr so stdout stays reserved for check's
// table/json result output.
func loggerFromFlags(cmd *cobra.Command) (*slog.Logger, error) {
	levelRaw, err := cmd.Flags().GetString("log.level")
	if err != nil {
		return nil, err
	}
	formatRaw, err := cmd.Flags().GetString("log.format")
	if err != nil {
		return nil, err
	}

	level, err := logging.ParseLevel(levelRaw)
	if err != nil {
		return nil, err
	}
	return logging.New(formatRaw, level, os.Stderr)
}
