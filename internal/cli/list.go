package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/dupe-com/ports-cli/internal/categorize"
	"github.com/dupe-com/ports-cli/internal/netscan"
)

// listEntry is the JSON shape: a Listener plus its category.
type listEntry struct {
	netscan.Listener
	Category categorize.Category `json:"category"`
}

func newListCmd() *cobra.Command {
	var (
		jsonOut  bool
		category string
		filter   string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List listening TCP ports (scriptable)",
		Example: `  ports list
  ports list --json | jq '.[] | select(.category == "dev")'
  ports list --category db
  ports list --filter node`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ls, err := netscan.Scan()
			if err != nil {
				return err
			}
			if filter != "" {
				ls = netscan.FilterName(ls, filter)
			}

			entries := make([]listEntry, 0, len(ls))
			var wantCat categorize.Category
			if category != "" {
				c, ok := categorize.Parse(category)
				if !ok {
					return fmt.Errorf("unknown category %q (db, web, dev, messaging, system, other)", category)
				}
				wantCat = c
			}
			for _, l := range ls {
				c := categorize.Categorize(l.Port, l.Name, l.Cmdline)
				if wantCat != "" && c != wantCat {
					continue
				}
				entries = append(entries, listEntry{Listener: l, Category: c})
			}

			if jsonOut {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(entries)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "PORT\tCAT\tPID\tUSER\tUPTIME\tADDR\tCOMMAND")
			for _, e := range entries {
				cmdline := e.Cmdline
				if len(cmdline) > 60 {
					cmdline = cmdline[:59] + "…"
				}
				fmt.Fprintf(w, "%d\t%s\t%d\t%s\t%s\t%s\t%s\n",
					e.Port, e.Category.Badge(), e.PID, e.User, e.Uptime(), e.AddrSummary(), cmdline)
			}
			return w.Flush()
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output JSON")
	cmd.Flags().StringVar(&category, "category", "", "filter by category: db, web, dev, messaging, system, other")
	cmd.Flags().StringVar(&filter, "filter", "", "filter by process name/cmdline substring")
	_ = os.Stdout // keep imports honest if flags change
	return cmd
}
