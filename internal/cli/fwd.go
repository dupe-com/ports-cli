package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/dupe-com/ports-cli/internal/kube"
	"github.com/dupe-com/ports-cli/internal/notify"
)

func newFwdCmd() *cobra.Command {
	var (
		namespace string
		kctx      string
		noNotify  bool
	)
	cmd := &cobra.Command{
		Use:   "fwd <target> <port[:remotePort]>...",
		Short: "Run an auto-reconnecting kubectl port-forward in the foreground",
		Long: `A kubectl port-forward that doesn't die: when the connection drops
(pod restart, network blip, laptop sleep), it reconnects with exponential
backoff instead of exiting. Ctrl-C to stop.

For multiple managed sessions with status and logs, use the TUI
(bare ` + "`ports`" + `, Forwards tab).`,
		Example: `  ports fwd svc/api 8080:80
  ports fwd pod/web-0 3000 -n staging
  ports fwd deploy/api 5432:5432 --context prod-cluster`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := kube.NewManager()
			spec := kube.Spec{
				Context:   kctx,
				Namespace: namespace,
				Target:    args[0],
				Ports:     args[1:],
			}
			s, err := mgr.Start(spec)
			if err != nil {
				return err
			}
			defer mgr.StopAll()

			fmt.Fprintf(cmd.OutOrStdout(), "forwarding %s (auto-reconnect on drop, ctrl-c to stop)\n", spec.Label())

			sig := make(chan os.Signal, 1)
			signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

			// Tail the session: print new log lines as they arrive and
			// surface connect/disconnect events.
			printed := 0
			tick := time.NewTicker(300 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-sig:
					fmt.Fprintln(cmd.OutOrStdout(), "\nstopping")
					return nil
				case e := <-mgr.Events():
					if !noNotify && (e.Kind == kube.EventConnected || e.Kind == kube.EventDisconnected) {
						_ = notify.Send("ports — kubectl forward", fmt.Sprintf("%s: %s", e.Kind, e.Detail))
					}
				case <-tick.C:
					logs := s.Logs()
					for ; printed < len(logs); printed++ {
						fmt.Fprintln(cmd.OutOrStdout(), logs[printed])
					}
				}
			}
		},
	}
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "kubernetes namespace")
	cmd.Flags().StringVar(&kctx, "context", "", "kubeconfig context")
	cmd.Flags().BoolVar(&noNotify, "no-notify", false, "disable desktop notifications")
	return cmd
}
