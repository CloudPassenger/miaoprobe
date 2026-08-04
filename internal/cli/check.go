package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

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
			return runCheck(scriptsPath, proxyRaw, filterRaw, format, timeout)
		},
	}

	cmd.Flags().StringVar(&scriptsPath, "scripts", "", "path to a .js file or a directory containing index.json (required)")
	cmd.Flags().StringVar(&proxyRaw, "proxy", "", "egress proxy: http://host:port or socks5://host:port (empty = direct)")
	cmd.Flags().StringVar(&filterRaw, "filter", "", "comma-separated region/tag filter")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "per-script execution timeout")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table or json")
	_ = cmd.MarkFlagRequired("scripts")

	return cmd
}

func runCheck(scriptsPath, proxyRaw, filterRaw, format string, timeout time.Duration) error {
	proxyCfg, err := network.ParseProxy(proxyRaw)
	if err != nil {
		return err
	}

	scripts, err := script.Load(scriptsPath)
	if err != nil {
		return err
	}
	scripts = script.Filter(scripts, script.ParseFilter(filterRaw))
	sort.SliceStable(scripts, func(i, j int) bool { return scripts[i].Priority < scripts[j].Priority })

	outcomes := make([]probe.Outcome, len(scripts))
	for i, sc := range scripts {
		outcomes[i] = probe.Run(sc, proxyCfg, timeout)
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
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Regions    []string `json:"regions,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Text       string   `json:"text,omitempty"`
	Background string   `json:"background,omitempty"`
	Status     string   `json:"status"`
	DurationMs int64    `json:"durationMs"`
	Error      string   `json:"error,omitempty"`
}

func toRow(o probe.Outcome) checkRow {
	row := checkRow{
		ID:         o.Script.ID,
		Name:       o.Script.Name,
		Regions:    o.Script.Regions,
		Tags:       o.Script.Tags,
		Text:       o.Result.Text,
		Background: o.Result.Background,
		DurationMs: o.Duration.Milliseconds(),
	}
	if o.Err != nil {
		row.Status = "error"
		row.Error = o.Err.Error()
		return row
	}
	row.Status = exporter.ClassifyColor(o.Result.Background).Label
	return row
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
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tNAME\tSTATUS\tTEXT\tDURATION\tERROR")
	for _, o := range outcomes {
		row := toRow(o)
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			row.ID, row.Name, row.Status, row.Text, o.Duration.Round(time.Millisecond), row.Error)
	}
	return w.Flush()
}
