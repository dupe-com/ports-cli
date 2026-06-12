// Package tui implements the interactive terminal UI: a tabbed view over
// listening ports, kubectl port-forward sessions, and Cloudflare tunnels.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	rtrunc "github.com/muesli/reflow/truncate"

	"github.com/dupe-com/ports-cli/internal/config"
	"github.com/dupe-com/ports-cli/internal/kube"
)

const (
	tabPorts = iota
	tabForwards
	tabTunnels
	tabCount
)

// Tab names say exactly what each covers: everything listening locally vs
// kubectl port-forwards this app manages vs detected cloudflared processes
// (which dial out and never show up in a port scan).
var tabNames = [tabCount]string{"Ports", "kubectl", "cloudflared"}

// Model is the root bubbletea model.
type Model struct {
	cfg *config.Config
	mgr *kube.Manager

	tab    int
	w, h   int
	help   bool
	flash  string
	flashN int

	ports portsTab
	fwds  fwdTab
	tuns  tunsTab
}

// Run starts the TUI and blocks until exit. Port-forward sessions are
// children of this process and are stopped on the way out.
func Run(cfg *config.Config) error {
	mgr := kube.NewManager()
	defer mgr.StopAll()

	m := Model{
		cfg:   cfg,
		mgr:   mgr,
		ports: newPortsTab(cfg),
		fwds:  newFwdTab(mgr, cfg),
		tuns:  newTunsTab(),
	}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

// Init kicks off the first scan, the refresh ticker, and the kube event pump.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{scanCmd, detectTunnelsCmd, waitKubeEvent(m.mgr.Events())}
	if d := m.cfg.Refresh(); d > 0 {
		cmds = append(cmds, tickCmd(d))
	}
	return tea.Batch(cmds...)
}

// Update routes messages: global keys first, then the active tab.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.fwds.setSize(msg.Width, msg.Height)
		return m, nil

	case tickMsg:
		var cmds []tea.Cmd
		if !m.ports.paused {
			cmds = append(cmds, scanCmd)
		}
		if m.tab == tabTunnels {
			cmds = append(cmds, detectTunnelsCmd)
		}
		if d := m.cfg.Refresh(); d > 0 {
			cmds = append(cmds, tickCmd(d))
		}
		return m, tea.Batch(cmds...)

	case flashMsg:
		m.flash = msg.text
		m.flashN++
		return m, flashClearCmd(m.flashN)

	case flashClearMsg:
		if msg.id == m.flashN {
			m.flash = ""
		}
		return m, nil

	case kubeEventMsg:
		var cmds []tea.Cmd
		cmds = append(cmds, waitKubeEvent(m.mgr.Events()))
		text := fmt.Sprintf("port-forward %s: %s", msg.Kind, msg.Detail)
		cmds = append(cmds, flash(text))
		if m.cfg.Notify && (msg.Kind == kube.EventConnected || msg.Kind == kube.EventDisconnected) {
			cmds = append(cmds, notifyCmd("ports — kubectl forward", text))
		}
		return m, tea.Batch(cmds...)

	case scanMsg:
		var cmd tea.Cmd
		m.ports, cmd = m.ports.onScan(msg)
		return m, cmd

	case killDoneMsg:
		var cmd tea.Cmd
		m.ports, cmd = m.ports.onKillDone(msg)
		return m, cmd

	case tunnelsMsg:
		m.tuns.onTunnels(msg)
		return m, nil

	case tea.KeyMsg:
		// While a text input owns the keyboard, the tab gets everything.
		if m.captured() {
			return m.routeKey(msg)
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "esc":
			// esc backs out of whatever is open first; at the top level it quits
			if m.help {
				m.help = false
				return m, nil
			}
			if m.consumesEsc() {
				return m.routeKey(msg)
			}
			return m, tea.Quit
		case "?":
			m.help = !m.help
			return m, nil
		case "1":
			m.tab = tabPorts
			return m, nil
		case "2":
			m.tab = tabForwards
			return m, nil
		case "3":
			m.tab = tabTunnels
			return m, detectTunnelsCmd
		case "tab", "right":
			return m.switchTab(+1)
		case "shift+tab", "left":
			return m.switchTab(-1)
		}
		if m.help {
			m.help = false // any other key dismisses help
			return m, nil
		}
		return m.routeKey(msg)
	}
	return m, nil
}

// switchTab moves delta tabs (wrapping) and kicks off a tunnel re-detect
// when landing on the Tunnels tab.
func (m Model) switchTab(delta int) (tea.Model, tea.Cmd) {
	m.tab = (m.tab + delta + tabCount) % tabCount
	if m.tab == tabTunnels {
		return m, detectTunnelsCmd
	}
	return m, nil
}

// consumesEsc reports whether the active tab still has something for esc to
// back out of (an applied filter, a logs view); when false, esc quits.
func (m Model) consumesEsc() bool {
	switch m.tab {
	case tabPorts:
		return m.ports.consumesEsc()
	case tabForwards:
		return m.fwds.consumesEsc()
	}
	return false
}

// captured reports whether the active tab has a focused input/modal that
// should receive all keys (so "q" can be typed into a filter, etc.).
func (m Model) captured() bool {
	switch m.tab {
	case tabPorts:
		return m.ports.captured()
	case tabForwards:
		return m.fwds.captured()
	}
	return false
}

func (m Model) routeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.tab {
	case tabPorts:
		m.ports, cmd = m.ports.update(msg)
	case tabForwards:
		m.fwds, cmd = m.fwds.update(msg)
	case tabTunnels:
		m.tuns, cmd = m.tuns.update(msg)
	}
	return m, cmd
}

// View renders tab bar + active tab + status bar (+ help overlay).
func (m Model) View() string {
	if m.w == 0 {
		return "loading…"
	}

	var tabs []string
	for i, name := range tabNames {
		label := fmt.Sprintf("%d %s", i+1, name)
		if i == tabForwards {
			if n := len(m.mgr.List()); n > 0 {
				label = fmt.Sprintf("%d %s(%d)", i+1, name, n)
			}
		}
		if i == m.tab {
			tabs = append(tabs, sTabActive.Render(label))
		} else {
			tabs = append(tabs, sTab.Render(label))
		}
	}
	header := lipgloss.JoinHorizontal(lipgloss.Bottom, tabs...)

	bodyH := m.h - lipgloss.Height(header) - 1
	var body string
	switch m.tab {
	case tabPorts:
		body = m.ports.view(m.w, bodyH)
	case tabForwards:
		body = m.fwds.view(m.w, bodyH)
	case tabTunnels:
		body = m.tuns.view(m.w, bodyH)
	}
	body = lipgloss.NewStyle().Height(bodyH).MaxHeight(bodyH).Render(body)

	status := m.statusBar()
	if m.help {
		// full-replace overlay: bubbletea has no compositing, and dimming
		// the background costs more than it's worth
		return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, m.helpView())
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, body, status)
}

func (m Model) statusBar() string {
	left := m.flash
	if left == "" {
		switch m.tab {
		case tabPorts:
			left = m.ports.keybar()
		case tabForwards:
			left = m.fwds.keybar()
		case tabTunnels:
			left = strings.Join([]string{keyHint("r", "refresh"), keyHint("←/→", "tabs"), keyHint("?", "help"), keyHint("q", "quit")}, keySep)
		}
		return sStatusBar.Render(truncate(left, m.w))
	}
	return sFlash.Render(truncate(left, m.w))
}

func (m Model) helpView() string {
	rows := [][2]string{
		{"←/→, tab, 1/2/3", "switch tabs"},
		{"?", "toggle help"},
		{"q, esc, ctrl+c", "quit (esc backs out of filters/views first)"},
		{"", ""},
		{"Ports", ""},
		{"↑/↓, j/k", "move · g/G top/bottom"},
		{"/", "fuzzy filter (esc clears)"},
		{"c", "cycle category filter"},
		{"space", "multi-select"},
		{"enter, x", "kill (confirm: y graceful · F force)"},
		{"K", "kill all visible (with confirmation)"},
		{"y", "copy localhost:PORT to clipboard"},
		{"t", "toggle tree view (group ports by process)"},
		{"a", "show/hide system & misc ports (hidden by default; ↓ past the end also reveals)"},
		{"d", "hide/show detail pane (shown by default)"},
		{"f", "toggle favorite ★"},
		{"w", "toggle watch 👁 (notify on change)"},
		{"r / p", "refresh now / pause auto-refresh"},
		{"", ""},
		{"kubectl — port-forwards managed by this app", ""},
		{"n", "new kubectl port-forward"},
		{"enter / l", "start saved spec / view session logs"},
		{"s", "save session spec for later"},
		{"D", "delete saved spec"},
		{"x / X", "stop session / remove from list"},
		{"", ""},
		{"cloudflared — detected Cloudflare Tunnels (view-only)", ""},
		{"r", "re-detect cloudflared processes"},
	}
	var b strings.Builder
	b.WriteString(sAccent.Render("ports — keyboard reference") + "\n\n")
	for _, r := range rows {
		if r[0] == "" && r[1] == "" {
			b.WriteString("\n")
			continue
		}
		if r[1] == "" {
			b.WriteString(sHeader.Render(r[0]) + "\n")
			continue
		}
		fmt.Fprintf(&b, "  %s  %s\n", sAccent.Render(pad(r[0], 16)), r[1])
	}
	b.WriteString("\n" + sDim.Render("press any key to close"))
	return sModal.Render(b.String())
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func truncate(s string, w int) string {
	if w <= 1 || lipgloss.Width(s) <= w {
		return s
	}
	// ANSI-aware: styled strings (keybar hints, badges) must not have their
	// escape codes counted as width or sliced mid-sequence
	return rtrunc.StringWithTail(s, uint(w), "…")
}

func fmtAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}
