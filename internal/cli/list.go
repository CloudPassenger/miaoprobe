package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/CloudPassenger/miaoprobe/internal/script"
)

func newListCommand() *cobra.Command {
	var scriptsPath, filterRaw, format string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List detection scripts available under --scripts",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Unlike check/serve, list always shows every script by
			// default: it ignores any filter set via config file or
			// MP_FILTER_* environment variables, only honoring an explicit
			// --filter passed on this invocation's command line.
			filterSpec, err := resolveFilterSpec(cmd, false)
			if err != nil {
				return err
			}
			return runList(scriptsPath, filterSpec, format)
		},
	}

	cmd.Flags().StringVar(&scriptsPath, "scripts", "", "path to a .js file or a directory containing index.json (required)")
	cmd.Flags().StringVar(&filterRaw, "filter", "", `script selection, e.g. "category:media,ai;region:hk,us;id:netflix;mode:exclude"; unlike check/serve, list ignores any filter from config file/environment unless passed here`)
	cmd.Flags().StringVar(&format, "format", "table", "output format: table or json")
	_ = cmd.MarkFlagRequired("scripts")

	return cmd
}

func runList(scriptsPath string, filterSpec script.FilterSpec, format string) error {
	scripts, err := script.Load(scriptsPath)
	if err != nil {
		return err
	}
	scripts = script.Select(scripts, filterSpec)
	sort.SliceStable(scripts, func(i, j int) bool { return scripts[i].Priority < scripts[j].Priority })

	switch format {
	case "json":
		return printListJSON(scripts)
	case "table", "":
		return printListTable(scripts)
	default:
		return fmt.Errorf("unknown format %q (want table or json)", format)
	}
}

type listRow struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Category    string   `json:"category,omitempty"`
	Regions     []string `json:"regions,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Priority    int      `json:"priority"`
}

func toListRow(s script.Script) listRow {
	return listRow{
		ID:          s.ID,
		Name:        s.Name,
		Description: s.Description,
		Category:    s.Category,
		Regions:     s.Regions,
		Tags:        s.Tags,
		Priority:    s.Priority,
	}
}

func printListJSON(scripts []script.Script) error {
	rows := make([]listRow, len(scripts))
	for i, s := range scripts {
		rows[i] = toListRow(s)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

func printListTable(scripts []script.Script) error {
	colorsEnabled := term.IsTerminal(int(os.Stdout.Fd()))

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetStyle(table.StyleRounded)
	t.AppendHeader(table.Row{"ID", "NAME", "CATEGORY", "REGIONS", "TAGS", "PRIORITY"})

	for _, s := range scripts {
		row := toListRow(s)
		nameCell := row.Name
		if row.Description != "" {
			desc := "\u2514 " + row.Description
			if colorsEnabled {
				desc = text.FgHiBlack.Sprint(desc)
			}
			nameCell += "\n" + desc
		}
		t.AppendRow(table.Row{row.ID, nameCell, row.Category, strings.Join(row.Regions, ","), strings.Join(row.Tags, ","), row.Priority})
	}

	t.Render()
	fmt.Printf("%d script(s)\n", len(scripts))
	return nil
}
