package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/CloudPassenger/miaoprobe/internal/script"
)

// newTestCommand builds a minimal command with the same flag shape as
// check/serve for exercising loadConfig in isolation.
func newTestCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "test", RunE: func(*cobra.Command, []string) error { return nil }}
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("scripts", "", "")
	cmd.Flags().String("log-level", "info", "")
	cmd.Flags().Duration("timeout", 30*time.Second, "")
	cmd.Flags().Bool("otel-insecure", false, "")
	cmd.Flags().String("filter", "", "")
	return cmd
}

func TestLoadConfigDefaultsUntouched(t *testing.T) {
	cmd := newTestCommand()
	if err := loadConfig(cmd); err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cmd.Flags().Changed("scripts") {
		t.Error("scripts should not be marked Changed when nothing set it")
	}
	if v, _ := cmd.Flags().GetString("log-level"); v != "info" {
		t.Errorf("log-level = %q, want default \"info\"", v)
	}
}

func TestLoadConfigEnvOverridesDefault(t *testing.T) {
	cmd := newTestCommand()
	t.Setenv("MP_SCRIPTS", "/from/env")
	t.Setenv("MP_LOG_LEVEL", "debug")

	if err := loadConfig(cmd); err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if v, _ := cmd.Flags().GetString("scripts"); v != "/from/env" {
		t.Errorf("scripts = %q, want /from/env", v)
	}
	if !cmd.Flags().Changed("scripts") {
		t.Error("scripts should be marked Changed when set via env")
	}
	if v, _ := cmd.Flags().GetString("log-level"); v != "debug" {
		t.Errorf("log-level = %q, want debug", v)
	}
}

func TestLoadConfigFileThenEnvThenFlagPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("scripts: /from/file\nlog-level: warn\ntimeout: 10s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MP_LOG_LEVEL", "debug") // should win over file, lose to flag
	t.Setenv("MP_TIMEOUT", "20s")     // should win over file, nothing overrides it

	cmd := newTestCommand()
	if err := cmd.Flags().Set("config", path); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("log-level", "error"); err != nil { // explicit flag wins over all
		t.Fatal(err)
	}

	if err := loadConfig(cmd); err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if v, _ := cmd.Flags().GetString("scripts"); v != "/from/file" {
		t.Errorf("scripts = %q, want /from/file (from config file)", v)
	}
	if v, _ := cmd.Flags().GetString("log-level"); v != "error" {
		t.Errorf("log-level = %q, want error (explicit flag beats env and file)", v)
	}
	if v, _ := cmd.Flags().GetDuration("timeout"); v != 20*time.Second {
		t.Errorf("timeout = %v, want 20s (env beats file)", v)
	}
}

func TestLoadConfigMissingExplicitFileErrors(t *testing.T) {
	cmd := newTestCommand()
	if err := cmd.Flags().Set("config", "/does/not/exist.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := loadConfig(cmd); err == nil {
		t.Error("expected error for missing explicit --config path")
	}
}

func TestLoadConfigAutoDiscoveredMissingFileIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir) // miaoprobe/config.yaml under here doesn't exist
	cmd := newTestCommand()
	if err := loadConfig(cmd); err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
}

func TestResolveFilterSpecFromEnv(t *testing.T) {
	cmd := newTestCommand()
	t.Setenv("MP_FILTER_CATEGORY", "media,ai")
	t.Setenv("MP_FILTER_MODE", "exclude")

	if err := loadConfig(cmd); err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	spec, err := resolveFilterSpec(cmd, true)
	if err != nil {
		t.Fatalf("resolveFilterSpec: %v", err)
	}
	if len(spec.Category) != 2 || spec.Category[0] != "media" || spec.Category[1] != "ai" {
		t.Errorf("category = %+v, want [media ai]", spec.Category)
	}
	if spec.Mode != script.ModeExclude {
		t.Errorf("mode = %q, want exclude", spec.Mode)
	}
}

func TestResolveFilterSpecFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := "filter:\n  region: [hk, us]\n  id: [netflix]\n"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newTestCommand()
	if err := cmd.Flags().Set("config", path); err != nil {
		t.Fatal(err)
	}
	if err := loadConfig(cmd); err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	spec, err := resolveFilterSpec(cmd, true)
	if err != nil {
		t.Fatalf("resolveFilterSpec: %v", err)
	}
	if len(spec.Region) != 2 || spec.Region[0] != "hk" || spec.Region[1] != "us" {
		t.Errorf("region = %+v, want [hk us]", spec.Region)
	}
	if len(spec.ID) != 1 || spec.ID[0] != "netflix" {
		t.Errorf("id = %+v, want [netflix]", spec.ID)
	}
}

func TestResolveFilterSpecCLIFullyOverridesEnv(t *testing.T) {
	cmd := newTestCommand()
	t.Setenv("MP_FILTER_CATEGORY", "media")
	t.Setenv("MP_FILTER_REGION", "hk")
	if err := cmd.Flags().Set("filter", "id:netflix"); err != nil {
		t.Fatal(err)
	}

	if err := loadConfig(cmd); err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	spec, err := resolveFilterSpec(cmd, true)
	if err != nil {
		t.Fatalf("resolveFilterSpec: %v", err)
	}
	if len(spec.Category) != 0 || len(spec.Region) != 0 {
		t.Errorf("expected env fields to be fully discarded, got %+v", spec)
	}
	if len(spec.ID) != 1 || spec.ID[0] != "netflix" {
		t.Errorf("id = %+v, want [netflix]", spec.ID)
	}
}

func TestResolveFilterSpecDisallowConfigEnv(t *testing.T) {
	cmd := newTestCommand()
	t.Setenv("MP_FILTER_CATEGORY", "media")

	if err := loadConfig(cmd); err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	spec, err := resolveFilterSpec(cmd, false)
	if err != nil {
		t.Fatalf("resolveFilterSpec: %v", err)
	}
	if !spec.IsZero() {
		t.Errorf("expected zero-value spec when config/env is disallowed (list semantics), got %+v", spec)
	}

	// But an explicit --filter still applies even with allowConfigEnv=false.
	if err := cmd.Flags().Set("filter", "id:netflix"); err != nil {
		t.Fatal(err)
	}
	spec, err = resolveFilterSpec(cmd, false)
	if err != nil {
		t.Fatalf("resolveFilterSpec: %v", err)
	}
	if len(spec.ID) != 1 || spec.ID[0] != "netflix" {
		t.Errorf("id = %+v, want [netflix] even with allowConfigEnv=false", spec.ID)
	}
}

func TestLoadConfigXDGAutoDiscovery(t *testing.T) {
	dir := t.TempDir()
	confDir := filepath.Join(dir, "miaoprobe")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(confDir, "config.yaml"), []byte("scripts: /from/xdg\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)

	cmd := newTestCommand()
	if err := loadConfig(cmd); err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if v, _ := cmd.Flags().GetString("scripts"); v != "/from/xdg" {
		t.Errorf("scripts = %q, want /from/xdg", v)
	}
}
