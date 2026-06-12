package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dupe-com/ports-cli/internal/config"
	"github.com/dupe-com/ports-cli/internal/netscan"
	"github.com/dupe-com/ports-cli/internal/proc"
)

func newKillCmd() *cobra.Command {
	var (
		force bool
		yes   bool
	)
	cmd := &cobra.Command{
		Use:   "kill <port|name>...",
		Short: "Kill whatever is listening on the given ports (or matching name)",
		Long: `Kill the processes listening on the given ports. Arguments that aren't
numbers are treated as process-name substrings.

Termination is graceful: SIGTERM first, then a short grace window. With
--force, survivors get SIGKILL.`,
		Example: `  ports kill 3000
  ports kill 3000 8080 --yes
  ports kill node --force
  ports kill 5432 --yes --force`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ls, err := netscan.Scan()
			if err != nil {
				return err
			}

			var (
				ports []uint32
				names []string
			)
			for _, a := range args {
				if n, err := strconv.ParseUint(a, 10, 32); err == nil && n > 0 && n <= 65535 {
					ports = append(ports, uint32(n))
				} else {
					names = append(names, a)
				}
			}

			matched := netscan.FilterPorts(ls, ports)
			for _, n := range names {
				matched = append(matched, netscan.FilterName(ls, n)...)
			}
			matched = dedupeListeners(matched)
			if len(matched) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "nothing matching %s is listening\n", strings.Join(args, ", "))
				return nil
			}

			fmt.Fprintln(cmd.OutOrStdout(), "about to terminate:")
			for _, l := range matched {
				fmt.Fprintf(cmd.OutOrStdout(), "  :%d\tpid %d\t%s (%s)\n", l.Port, l.PID, l.Name, l.User)
			}
			if !yes && !confirm(cmd) {
				fmt.Fprintln(cmd.OutOrStdout(), "aborted")
				return nil
			}

			cfg, err := config.Load()
			if err != nil {
				return err
			}
			results := proc.GracefulKill(context.Background(), pids(matched), cfg.Grace(), force)

			var failed int
			for _, r := range results {
				switch {
				case r.Err != nil:
					failed++
					fmt.Fprintf(cmd.OutOrStdout(), "✕ pid %d: %v — try sudo\n", r.PID, r.Err)
				case r.Forced:
					fmt.Fprintf(cmd.OutOrStdout(), "→ pid %d force-killed (SIGKILL)\n", r.PID)
				case r.Exited:
					fmt.Fprintf(cmd.OutOrStdout(), "→ pid %d terminated\n", r.PID)
				default:
					failed++
					fmt.Fprintf(cmd.OutOrStdout(), "… pid %d survived SIGTERM — re-run with --force\n", r.PID)
				}
			}
			if failed > 0 {
				return fmt.Errorf("%d process(es) not terminated", failed)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "escalate to SIGKILL after the grace window")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation")
	return cmd
}

func confirm(cmd *cobra.Command) bool {
	fmt.Fprint(cmd.OutOrStdout(), "kill these? [y/N] ")
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return false
	}
	a := strings.ToLower(strings.TrimSpace(sc.Text()))
	return a == "y" || a == "yes"
}

func pids(ls []netscan.Listener) []int32 {
	seen := map[int32]bool{}
	var out []int32
	for _, l := range ls {
		if !seen[l.PID] {
			seen[l.PID] = true
			out = append(out, l.PID)
		}
	}
	return out
}

func dedupeListeners(ls []netscan.Listener) []netscan.Listener {
	type key struct {
		pid  int32
		port uint32
	}
	seen := map[key]bool{}
	var out []netscan.Listener
	for _, l := range ls {
		k := key{l.PID, l.Port}
		if !seen[k] {
			seen[k] = true
			out = append(out, l)
		}
	}
	return out
}
