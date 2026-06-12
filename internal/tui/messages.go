package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dupe-com/ports-cli/internal/cftunnel"
	"github.com/dupe-com/ports-cli/internal/kube"
	"github.com/dupe-com/ports-cli/internal/netscan"
	"github.com/dupe-com/ports-cli/internal/notify"
	"github.com/dupe-com/ports-cli/internal/proc"
)

type (
	tickMsg time.Time
	scanMsg struct {
		listeners []netscan.Listener
		err       error
	}
	killDoneMsg struct {
		results []proc.Result
	}
	tunnelsMsg struct {
		tunnels []cftunnel.Tunnel
		err     error
	}
	kubeEventMsg  kube.Event
	flashMsg      struct{ text string }
	flashClearMsg struct{ id int }
)

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func scanCmd() tea.Msg {
	ls, err := netscan.Scan()
	return scanMsg{listeners: ls, err: err}
}

func killCmd(pids []int32, grace time.Duration, force bool) tea.Cmd {
	return func() tea.Msg {
		return killDoneMsg{results: proc.GracefulKill(context.Background(), pids, grace, force)}
	}
}

func detectTunnelsCmd() tea.Msg {
	ts, err := cftunnel.Detect()
	return tunnelsMsg{tunnels: ts, err: err}
}

// waitKubeEvent blocks on the manager's event stream and re-arms itself
// after each delivery (standard bubbletea channel-pump pattern).
func waitKubeEvent(ch <-chan kube.Event) tea.Cmd {
	return func() tea.Msg {
		e, ok := <-ch
		if !ok {
			return nil
		}
		return kubeEventMsg(e)
	}
}

// notifyCmd fires a desktop notification without blocking the UI.
func notifyCmd(title, body string) tea.Cmd {
	return func() tea.Msg {
		_ = notify.Send(title, body)
		return nil
	}
}

func flash(text string) tea.Cmd {
	return func() tea.Msg { return flashMsg{text: text} }
}

func flashClearCmd(id int) tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return flashClearMsg{id: id} })
}
