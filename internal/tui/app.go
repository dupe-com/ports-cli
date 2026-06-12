// Package tui implements the interactive terminal UI: a tabbed view over
// listening ports, kubectl port-forward sessions, and Cloudflare tunnels.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dupe-com/ports-cli/internal/config"
	"github.com/dupe-com/ports-cli/internal/kube"
)

const (
	tabPorts = iota
	tabForwards
	tabTunnels
	tabCount
)

var tabNames = [tabCount]string{"Ports", "Forwards", "Tunnels"}

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
		fwds:  newFwdTab(mgr),
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
		case "tab":
			m.tab = (m.tab + 1) % tabCount
			if m.tab == tabTunnels {
				return m, detectTunnelsCmd
			}
			return m, nil
		}
		if m.help {
			m.help = false // any other key dismisses help
			return m, nil
		}
		return m.routeKey(msg)
	}
	return m, nil
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
	out := lipgloss.JoinVertical(lipgloss.Left, header, body, status)

	if m.help {
		return overlay(out, m.helpView(), m.w, m.h)
	}
	return out
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
			left = "r refresh · 1/2/3 tabs · ? help · q quit"
		}
		return sStatusBar.Render(truncate(left, m.w))
	}
	return sFlash.Render(truncate(left, m.w))
}

func (m Model) helpView() string {
	rows := [][2]string{
		{"1 / 2 / 3, tab", "switch tabs"},
		{"?", "toggle help"},
		{"q, ctrl+c", "quit"},
		{"", ""},
		{"Ports", ""},
		{"↑/↓, j/k", "move · g/G top/bottom"},
		{"/", "fuzzy filter (esc clears)"},
		{"c", "cycle category filter"},
		{"space", "multi-select"},
		{"enter, x", "kill (confirm: y graceful · F force)"},
		{"d", "toggle detail pane"},
		{"f", "toggle favorite ★"},
		{"w", "toggle watch 👁 (notify on change)"},
		{"r / p", "refresh now / pause auto-refresh"},
		{"", ""},
		{"Forwards", ""},
		{"n", "new kubectl port-forward"},
		{"enter / l", "view session logs"},
		{"x / X", "stop session / remove from list"},
		{"", ""},
		{"Tunnels", ""},
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
		b.WriteString(fmt.Sprintf("  %s  %s\n", sAccent.Render(pad(r[0], 16)), r[1]))
	}
	b.WriteString("\n" + sDim.Render("press any key to close"))
	return sModal.Render(b.String())
}

// overlay centers box on top of base (simple full-replace overlay — bubbletea
// has no compositing, and dimming the background costs more than it's worth).
func overlay(base, box string, w, h int) string {
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box)
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
	// rune-safe trim; ANSI-styled strings are never passed here pre-styling
	r := []rune(s)
	if len(r) <= w-1 {
		return s
	}
	return string(r[:w-1]) + "…"
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
