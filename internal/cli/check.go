package cli

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/CloudPassenger/miaoprobe/internal/engine"
	"github.com/CloudPassenger/miaoprobe/internal/exporter"
	"github.com/CloudPassenger/miaoprobe/internal/network"
	"github.com/CloudPassenger/miaoprobe/internal/probe"
	"github.com/CloudPassenger/miaoprobe/internal/script"
)

func newCheckCommand() *cobra.Command {
	var scriptsPath, proxyRaw, filterRaw, format string
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Run scripts once and print the results",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger, err := loggerFromFlags(cmd)
			if err != nil {
				return err
			}
			filterSpec, err := resolveFilterSpec(cmd, true)
			if err != nil {
				return err
			}
			return runCheck(scriptsPath, proxyRaw, filterSpec, format, timeout, logger)
		},
	}

	cmd.Flags().StringVar(&scriptsPath, "scripts", "", "path to a .js file or a directory containing index.json (required)")
	cmd.Flags().StringVar(&proxyRaw, "proxy", "", "egress proxy: http://host:port or socks5://host:port (empty = direct)")
	cmd.Flags().StringVar(&filterRaw, "filter", "", `script selection, e.g. "category:media,ai;region:hk,us;id:netflix;mode:exclude" (see "miaoprobe list" and README.md#configuration)`)
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "per-script execution timeout")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table or json")
	_ = cmd.MarkFlagRequired("scripts")

	return cmd
}

func runCheck(scriptsPath, proxyRaw string, filterSpec script.FilterSpec, format string, timeout time.Duration, logger *slog.Logger) error {
	proxyCfg, err := network.ParseProxy(proxyRaw)
	if err != nil {
		return err
	}

	scripts, err := script.Load(scriptsPath)
	if err != nil {
		return err
	}
	scripts = script.Select(scripts, filterSpec)
	sort.SliceStable(scripts, func(i, j int) bool { return scripts[i].Priority < scripts[j].Priority })

	outcomes := make([]probe.Outcome, len(scripts))
	for i, sc := range scripts {
		outcomes[i] = probe.Run(sc, proxyCfg, timeout, logger)
	}

	switch format {
	case "json":
		return printJSON(outcomes)
	case "table", "":
		return printTable(outcomes)
	default:
		return fmt.Errorf("unknown format %q (want table or json)", format)
	}
}

type checkRow struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Regions    []string            `json:"regions,omitempty"`
	Tags       []string            `json:"tags,omitempty"`
	Text       string              `json:"text,omitempty"`
	Background string              `json:"background,omitempty"`
	Status     string              `json:"status"`
	Region     string              `json:"region,omitempty"`
	Message    string              `json:"message,omitempty"`
	Extra      []engine.ExtraField `json:"extra,omitempty"`
	DurationMs int64               `json:"durationMs"`
	Error      string              `json:"error,omitempty"`
}

func toRow(o probe.Outcome) checkRow {
	row := checkRow{
		ID:         o.Script.ID,
		Name:       o.Script.Name,
		Regions:    o.Script.Regions,
		Tags:       o.Script.Tags,
		Text:       o.Result.Text,
		Background: o.Result.Background,
		Region:     o.Result.Region,
		Message:    o.Result.Message,
		Extra:      o.Result.Extra,
		DurationMs: o.Duration.Milliseconds(),
	}
	if o.Err != nil {
		row.Status = "error"
		row.Error = o.Err.Error()
		return row
	}
	row.Status = exporter.Classify(o.Result.Status, o.Result.Background).Label
	row.Error = o.Result.Error
	return row
}

// detailsLine renders row's Region/Message/Extra as a single fixed-format
// "label: value" line for the CLI table, where JSON output keeps them as
// separate structured fields instead. Returns "" when there is nothing to
// show.
func detailsLine(row checkRow) string {
	var parts []string
	if row.Region != "" {
		parts = append(parts, "region: "+row.Region)
	}
	if row.Message != "" {
		parts = append(parts, row.Message)
	}
	if extra := formatExtra(row.Extra); extra != "" {
		parts = append(parts, extra)
	}
	return strings.Join(parts, " | ")
}

// formatExtra renders a script's extra fields as "label: value[unit]"
// pairs, falling back to Key when Label is unset.
func formatExtra(fields []engine.ExtraField) string {
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		label := f.Label
		if label == "" {
			label = f.Key
		}
		val := fmt.Sprintf("%v%s", f.Value, f.Unit)
		parts = append(parts, label+": "+val)
	}
	return strings.Join(parts, ", ")
}

func printJSON(outcomes []probe.Outcome) error {
	rows := make([]checkRow, len(outcomes))
	for i, o := range outcomes {
		rows[i] = toRow(o)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

func printTable(outcomes []probe.Outcome) error {
	colorsEnabled := term.IsTerminal(int(os.Stdout.Fd()))

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetStyle(table.StyleRounded)
	t.AppendHeader(table.Row{"ID", "NAME", "STATUS", "TEXT", "DURATION", "ERROR"})

	for _, o := range outcomes {
		row := toRow(o)
		status := row.Status
		if colorsEnabled {
			status = statusColor(row.Status).Sprint(row.Status)
		}
		cellText := row.Text
		if d := detailsLine(row); d != "" {
			d = "\u2514 " + d
			if colorsEnabled {
				d = text.FgHiBlack.Sprint(d)
			}
			cellText += "\n" + d
		}
		t.AppendRow(table.Row{row.ID, row.Name, status, cellText, o.Duration.Round(time.Millisecond), row.Error})
	}

	t.Render()
	return nil
}

// statusColor mirrors the background color semantics in
// internal/exporter.ClassifyColor for terminal display.
func statusColor(status string) text.Color {
	switch status {
	case "unlocked":
		return text.FgGreen
	case "failed":
		return text.FgRed
	case "warning":
		return text.FgYellow
	case "unknown":
		return text.FgCyan
	case "n/a":
		return text.FgHiBlack
	case "error":
		return text.FgHiRed
	default:
		return text.FgWhite
	}
}
