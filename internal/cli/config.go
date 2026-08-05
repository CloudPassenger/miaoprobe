package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/cobra"

	"github.com/CloudPassenger/miaoprobe/internal/script"
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
//
// Script selection (--filter) is the one exception: it doesn't have a
// single scalar shape shared across all three sources (YAML uses a nested
// `filter.{id,category,region,tag,mode}` map of lists, the environment uses
// separate MP_FILTER_* variables, and the flag is a compact
// "key:v1,v2;key2:v3" string), so it can't be synced onto the flag by the
// generic mechanism above. Instead, the file/environment layers are parsed
// into a script.FilterSpec here and stashed on cmd's context for commands to
// combine with the flag via resolveFilterSpec.
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
			// MP_FILTER_CATEGORY etc. address the nested filter.category
			// key, not a flag; every other variable maps onto a flag name.
			if rest, ok := strings.CutPrefix(key, "filter_"); ok {
				return "filter." + rest, v
			}
			return strings.ReplaceAll(key, "_", "-"), v
		},
	})
	if err := k.Load(envProvider, nil); err != nil {
		return fmt.Errorf("loading environment variables: %w", err)
	}

	var filterSpec script.FilterSpec
	if err := k.UnmarshalWithConf("filter", &filterSpec, koanf.UnmarshalConf{
		DecoderConfig: &mapstructure.DecoderConfig{
			WeaklyTypedInput: true,
			// MP_FILTER_* values are plain comma-separated strings; split
			// them into the []string fields of FilterSpec. YAML list
			// values are unaffected (already typed slices).
			DecodeHook: mapstructure.StringToSliceHookFunc(","),
		},
	}); err != nil {
		return fmt.Errorf("parsing filter config: %w", err)
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	cmd.SetContext(context.WithValue(ctx, filterSpecContextKey{}, filterSpec))

	// Keys set by the file/env layers, captured before merging in flags.
	// Only these need writing back: any other key is either a flag's own
	// default (already in effect, nothing to do) or a value the user
	// passed on the command line (pflag already applied and marked it
	// Changed during normal parsing). Overwriting those too would wrongly
	// mark untouched required flags as Changed, defeating validation.
	// "filter" and its "filter.*" sub-keys are excluded: they feed
	// filterSpec above instead of a flag.
	overrideKeys := make([]string, 0, len(k.Keys()))
	for _, key := range k.Keys() {
		if key == "filter" || strings.HasPrefix(key, "filter.") {
			continue
		}
		overrideKeys = append(overrideKeys, key)
	}

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

// filterSpecContextKey is the cmd.Context() key loadConfig uses to stash
// the script.FilterSpec parsed from the config file and MP_FILTER_*
// environment variables, for resolveFilterSpec to pick up.
type filterSpecContextKey struct{}

// resolveFilterSpec returns the script.FilterSpec that should govern script
// selection for cmd. An explicit --filter flag is self-contained and
// entirely replaces whatever the config file/environment specified, rather
// than being merged field-by-field with it. Otherwise, when allowConfigEnv
// is true, the FilterSpec loadConfig parsed from the config file and
// MP_FILTER_* environment variables is used; when false (list defaults to
// "show every script" unless told otherwise on the command line), it is
// ignored.
func resolveFilterSpec(cmd *cobra.Command, allowConfigEnv bool) (script.FilterSpec, error) {
	if cmd.Flags().Changed("filter") {
		raw, err := cmd.Flags().GetString("filter")
		if err != nil {
			return script.FilterSpec{}, err
		}
		return script.ParseFilterFlag(raw)
	}
	if !allowConfigEnv {
		return script.FilterSpec{}, nil
	}
	spec, _ := cmd.Context().Value(filterSpecContextKey{}).(script.FilterSpec)
	return spec, nil
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
