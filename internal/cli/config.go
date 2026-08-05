package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/cobra"
)

// envPrefix is stripped from environment variable names before they are
// matched against flags, e.g. MP_LOG_LEVEL -> log-level.
const envPrefix = "MP_"

// loadConfig merges, from lowest to highest priority: an optional YAML
// config file, MP_-prefixed environment variables, and flags actually
// passed on the command line. The merged values are written back onto
// cmd's own flags (which also marks them as Changed), so both the
// existing *Var-bound option structs and cobra's required-flag validation
// transparently see values that came from a file or the environment.
//
// YAML keys and environment variable names (after stripping the prefix and
// lowercasing) must match flag names verbatim, e.g. the --otel-endpoint
// flag is configured as `otel-endpoint: ...` in YAML or MP_OTEL_ENDPOINT
// in the environment.
func loadConfig(cmd *cobra.Command) error {
	path, explicit, err := resolveConfigPath(cmd)
	if err != nil {
		return err
	}

	k := koanf.New(".")

	if path != "" {
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			if explicit || !os.IsNotExist(err) {
				return fmt.Errorf("loading config file %s: %w", path, err)
			}
		}
	}

	envProvider := env.Provider(".", env.Opt{
		Prefix: envPrefix,
		TransformFunc: func(k, v string) (string, any) {
			key := strings.ToLower(strings.TrimPrefix(k, envPrefix))
			return strings.ReplaceAll(key, "_", "-"), v
		},
	})
	if err := k.Load(envProvider, nil); err != nil {
		return fmt.Errorf("loading environment variables: %w", err)
	}

	// Keys set by the file/env layers, captured before merging in flags.
	// Only these need writing back: any other key is either a flag's own
	// default (already in effect, nothing to do) or a value the user
	// passed on the command line (pflag already applied and marked it
	// Changed during normal parsing). Overwriting those too would wrongly
	// mark untouched required flags as Changed, defeating validation.
	overrideKeys := k.Keys()

	if err := k.Load(posflag.Provider(cmd.Flags(), ".", k), nil); err != nil {
		return fmt.Errorf("loading flags: %w", err)
	}

	for _, key := range overrideKeys {
		if cmd.Flags().Lookup(key) == nil {
			continue
		}
		if err := cmd.Flags().Set(key, fmt.Sprint(k.Get(key))); err != nil {
			return fmt.Errorf("applying %q: %w", key, err)
		}
	}
	return nil
}

// resolveConfigPath returns the config file to load, if any. An explicit
// --config flag always wins; otherwise the first of the standard discovery
// paths that exists on disk is used. explicit reports whether the path
// came from --config, so callers can decide whether a missing file is an
// error.
func resolveConfigPath(cmd *cobra.Command) (path string, explicit bool, err error) {
	explicitPath, err := cmd.Flags().GetString("config")
	if err != nil {
		return "", false, err
	}
	if explicitPath != "" {
		return explicitPath, true, nil
	}

	for _, candidate := range defaultConfigPaths() {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, false, nil
		}
	}
	return "", false, nil
}

// defaultConfigPaths lists auto-discovered config locations in priority
// order: a per-user XDG config directory, then the system-wide directory
// conventionally used by systemd-managed services.
func defaultConfigPaths() []string {
	var paths []string
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "miaoprobe", "config.yaml"))
	} else if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "miaoprobe", "config.yaml"))
	}
	paths = append(paths, filepath.Join("/etc", "miaoprobe", "config.yaml"))
	return paths
}
