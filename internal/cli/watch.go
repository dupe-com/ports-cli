package cli

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/dupe-com/ports-cli/internal/netscan"
	"github.com/dupe-com/ports-cli/internal/notify"
)

func newWatchCmd() *cobra.Command {
	var (
		interval time.Duration
		noNotify bool
	)
	cmd := &cobra.Command{
		Use:   "watch <port>...",
		Short: "Watch ports and report when they start/stop listening",
		Long: `Poll the given ports and print (and desktop-notify) every transition —
when something starts listening and when it stops. Ctrl-C to exit.

Useful for "tell me when the dev server is actually up" and "tell me when
that build process finally lets go of the port".`,
		Example: `  ports watch 3000
  ports watch 3000 5432 --interval 1s
  ports watch 8080 --no-notify`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var ports []uint32
			for _, a := range args {
				n, err := strconv.ParseUint(a, 10, 32)
				if err != nil || n == 0 || n > 65535 {
					return fmt.Errorf("not a port: %s", a)
				}
				ports = append(ports, uint32(n))
			}

			state := map[uint32]*bool{}
			report := func(first bool) error {
				ls, err := netscan.Scan()
				if err != nil {
					return err
				}
				byPort := map[uint32]netscan.Listener{}
				for _, l := range ls {
					byPort[l.Port] = l
				}
				for _, p := range ports {
					l, listening := byPort[p]
					prev := state[p]
					state[p] = &listening

					ts := time.Now().Format("15:04:05")
					switch {
					case first && listening:
						fmt.Fprintf(cmd.OutOrStdout(), "%s  :%d listening — %s (pid %d)\n", ts, p, l.Name, l.PID)
					case first:
						fmt.Fprintf(cmd.OutOrStdout(), "%s  :%d not listening\n", ts, p)
					case prev != nil && *prev != listening:
						var text string
						if listening {
							text = fmt.Sprintf(":%d started listening — %s (pid %d)", p, l.Name, l.PID)
						} else {
							text = fmt.Sprintf(":%d stopped listening", p)
						}
						fmt.Fprintf(cmd.OutOrStdout(), "%s  %s\n", ts, text)
						if !noNotify {
							_ = notify.Send("ports — watched port", text)
						}
					}
				}
				return nil
			}

			if err := report(true); err != nil {
				return err
			}

			sig := make(chan os.Signal, 1)
			signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
			tick := time.NewTicker(interval)
			defer tick.Stop()
			for {
				select {
				case <-sig:
					return nil
				case <-tick.C:
					if err := report(false); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "scan error: %v\n", err)
					}
				}
			}
		},
	}
	cmd.Flags().DurationVar(&interval, "interval", 2*time.Second, "poll interval")
	cmd.Flags().BoolVar(&noNotify, "no-notify", false, "disable desktop notifications")
	return cmd
}
