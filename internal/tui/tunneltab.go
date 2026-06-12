package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dupe-com/ports-cli/internal/cftunnel"
)

type tunsTab struct {
	tunnels []cftunnel.Tunnel
	err     string
	cursor  int
}

func newTunsTab() tunsTab { return tunsTab{} }

func (t *tunsTab) onTunnels(msg tunnelsMsg) {
	if msg.err != nil {
		t.err = msg.err.Error()
		return
	}
	t.err = ""
	t.tunnels = msg.tunnels
	if t.cursor >= len(t.tunnels) {
		t.cursor = len(t.tunnels) - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
}

func (t tunsTab) update(msg tea.KeyMsg) (tunsTab, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if t.cursor > 0 {
			t.cursor--
		}
	case "down", "j":
		if t.cursor < len(t.tunnels)-1 {
			t.cursor++
		}
	case "r":
		return t, tea.Batch(flash("re-detecting cloudflared…"), detectTunnelsCmd)
	}
	return t, nil
}

func (t tunsTab) view(w, _ int) string {
	if t.err != "" {
		return sDanger.Render("detect error: " + t.err)
	}
	if len(t.tunnels) == 0 {
		return sDim.Render("\n  no cloudflared processes detected\n\n" +
			"  named tunnel:  cloudflared tunnel run <name>\n" +
			"  quick tunnel:  cloudflared tunnel --url http://localhost:3000\n\n" +
			"  cloudflared dials out to Cloudflare's edge, so tunnels never\n" +
			"  appear as listening ports — this tab is how you see them")
	}
	head := fmt.Sprintf("  %-8s %-7s %-20s %-28s %-8s %s",
		"PID", "MODE", "NAME", "ORIGIN/HOSTNAME", "UP", "CONFIG")
	rows := []string{sHeader.Render(truncate(head, w))}
	for i, tn := range t.tunnels {
		target := tn.Origin
		if target == "" {
			target = tn.Hostname
		}
		line := fmt.Sprintf("  %-8d %-7s %-20s %-28s %-8s %s",
			tn.PID, tn.Mode,
			truncate(orDash(tn.Name), 20),
			truncate(orDash(target), 28),
			tn.Uptime(),
			truncate(orDash(tn.ConfigPath), 24))
		if i == t.cursor {
			line = sCursor.Render("▸" + line[1:])
		}
		rows = append(rows, line)
	}
	rows = append(rows, "", sDim.Render("  view-only: tunnels are managed by cloudflared / the Zero Trust dashboard"))
	return strings.Join(rows, "\n")
}
