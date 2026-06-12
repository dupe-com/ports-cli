package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dupe-com/ports-cli/internal/config"
	"github.com/dupe-com/ports-cli/internal/kube"
)

type fwdMode int

const (
	fwdList fwdMode = iota
	fwdForm
	fwdLogs
)

// fwdListItem represents either a live session or a saved (not-running) spec.
type fwdListItem struct {
	session  *kube.Session
	savedIdx int                // index into cfg.Forwards; valid only when session == nil
	saved    config.ForwardSpec // copy of spec; valid only when session == nil
}

func (item fwdListItem) isSaved() bool { return item.session == nil }

type fwdTab struct {
	mgr    *kube.Manager
	cfg    *config.Config
	mode   fwdMode
	cursor int

	// new-session form
	inputs  [4]textinput.Model // context, namespace, target, ports
	focus   int
	formErr string

	// logs view
	logsFor string
	vp      viewport.Model

	w, h int
}

var formLabels = [4]string{"context (blank = current)", "namespace (blank = default)", "target — svc/api, pod/web-0, deploy/web", "ports — 8080:80 [, 5432 …]"}

func newFwdTab(mgr *kube.Manager, cfg *config.Config) fwdTab {
	t := fwdTab{mgr: mgr, cfg: cfg, vp: viewport.New(80, 20)}
	for i := range t.inputs {
		ti := textinput.New()
		ti.Placeholder = formLabels[i]
		ti.CharLimit = 128
		t.inputs[i] = ti
	}
	return t
}

func (t *fwdTab) setSize(w, h int) {
	t.w, t.h = w, h
	t.vp.Width = w - 2
	t.vp.Height = h - 8
	if t.vp.Height < 4 {
		t.vp.Height = 4
	}
}

func (t fwdTab) captured() bool { return t.mode == fwdForm }

// consumesEsc: the logs view uses esc to go back to the list.
func (t fwdTab) consumesEsc() bool { return t.mode == fwdLogs }

// buildItems returns live sessions first, then saved specs that aren't running.
func (t fwdTab) buildItems() []fwdListItem {
	sessions := t.mgr.List()
	items := make([]fwdListItem, 0, len(sessions)+len(t.cfg.Forwards))
	for _, s := range sessions {
		items = append(items, fwdListItem{session: s, savedIdx: -1})
	}
	for i, spec := range t.cfg.Forwards {
		running := false
		for _, s := range sessions {
			if s.Spec.Target == spec.Target &&
				strings.Join(s.Spec.Ports, ",") == strings.Join(spec.Ports, ",") &&
				s.Spec.Namespace == spec.Namespace &&
				s.Spec.Context == spec.Context {
				running = true
				break
			}
		}
		if !running {
			items = append(items, fwdListItem{savedIdx: i, saved: spec})
		}
	}
	return items
}

func (t fwdTab) curItem(items []fwdListItem) (fwdListItem, bool) {
	if t.cursor < 0 || t.cursor >= len(items) {
		return fwdListItem{}, false
	}
	return items[t.cursor], true
}

func (t fwdTab) update(msg tea.KeyMsg) (fwdTab, tea.Cmd) {
	switch t.mode {
	case fwdForm:
		return t.updateForm(msg)
	case fwdLogs:
		return t.updateLogs(msg)
	}
	return t.updateList(msg)
}

func (t fwdTab) updateList(msg tea.KeyMsg) (fwdTab, tea.Cmd) {
	items := t.buildItems()
	if t.cursor >= len(items) && len(items) > 0 {
		t.cursor = len(items) - 1
	}
	switch msg.String() {
	case "up", "k":
		if t.cursor > 0 {
			t.cursor--
		}
	case "down", "j":
		if t.cursor < len(items)-1 {
			t.cursor++
		}
	case "n":
		t.mode = fwdForm
		t.focus = 0
		t.formErr = ""
		for i := range t.inputs {
			t.inputs[i].Blur()
		}
		t.inputs[0].Focus()
		return t, textinput.Blink
	case "s":
		if item, ok := t.curItem(items); ok && !item.isSaved() {
			spec := config.ForwardSpec{
				Target:    item.session.Spec.Target,
				Ports:     item.session.Spec.Ports,
				Namespace: item.session.Spec.Namespace,
				Context:   item.session.Spec.Context,
			}
			if err := t.cfg.SaveForward(spec); err != nil {
				return t, flash("save failed: " + err.Error())
			}
			return t, flash("saved " + spec.Target)
		}
	case "D":
		if item, ok := t.curItem(items); ok && item.isSaved() {
			if err := t.cfg.RemoveForward(item.savedIdx); err != nil {
				return t, flash("remove failed: " + err.Error())
			}
			newItems := t.buildItems()
			if t.cursor >= len(newItems) && t.cursor > 0 {
				t.cursor--
			}
			return t, flash("removed saved spec")
		}
	case "x":
		if item, ok := t.curItem(items); ok && !item.isSaved() {
			_ = t.mgr.Stop(item.session.ID)
			return t, flash("stopped " + item.session.Spec.Label())
		}
	case "X":
		if item, ok := t.curItem(items); ok && !item.isSaved() {
			t.mgr.Remove(item.session.ID)
			newItems := t.buildItems()
			if t.cursor >= len(newItems) && t.cursor > 0 {
				t.cursor--
			}
			return t, flash("removed " + item.session.Spec.Label())
		}
	case "enter", "l":
		if item, ok := t.curItem(items); ok {
			if item.isSaved() {
				ks := kube.Spec{
					Target:    item.saved.Target,
					Ports:     item.saved.Ports,
					Namespace: item.saved.Namespace,
					Context:   item.saved.Context,
				}
				s, err := t.mgr.Start(ks)
				if err != nil {
					return t, flash("start failed: " + err.Error())
				}
				return t, flash("starting " + s.Spec.Label())
			}
			t.mode = fwdLogs
			t.logsFor = item.session.ID
			t.refreshLogs()
			t.vp.GotoBottom()
		}
	}
	return t, nil
}

func (t fwdTab) updateForm(msg tea.KeyMsg) (fwdTab, tea.Cmd) {
	switch msg.String() {
	case "esc":
		t.mode = fwdList
		return t, nil
	case "tab", "down":
		t.inputs[t.focus].Blur()
		t.focus = (t.focus + 1) % len(t.inputs)
		t.inputs[t.focus].Focus()
		return t, textinput.Blink
	case "shift+tab", "up":
		t.inputs[t.focus].Blur()
		t.focus = (t.focus + len(t.inputs) - 1) % len(t.inputs)
		t.inputs[t.focus].Focus()
		return t, textinput.Blink
	case "enter":
		spec := kube.Spec{
			Context:   strings.TrimSpace(t.inputs[0].Value()),
			Namespace: strings.TrimSpace(t.inputs[1].Value()),
			Target:    strings.TrimSpace(t.inputs[2].Value()),
			Ports:     splitPorts(t.inputs[3].Value()),
		}
		s, err := t.mgr.Start(spec)
		if err != nil {
			t.formErr = err.Error()
			return t, nil
		}
		t.mode = fwdList
		for i := range t.inputs {
			t.inputs[i].SetValue("")
		}
		return t, flash("starting " + s.Spec.Label())
	}
	var cmd tea.Cmd
	t.inputs[t.focus], cmd = t.inputs[t.focus].Update(msg)
	return t, cmd
}

func (t fwdTab) updateLogs(msg tea.KeyMsg) (fwdTab, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "l":
		t.mode = fwdList
		return t, nil
	case "r":
		t.refreshLogs()
		t.vp.GotoBottom()
		return t, nil
	}
	var cmd tea.Cmd
	t.vp, cmd = t.vp.Update(msg)
	return t, cmd
}

func (t *fwdTab) refreshLogs() {
	if s, ok := t.mgr.Get(t.logsFor); ok {
		t.vp.SetContent(strings.Join(s.Logs(), "\n"))
	}
}

// ── rendering ──────────────────────────────────────────────────────────────

func (t fwdTab) view(w, h int) string {
	switch t.mode {
	case fwdForm:
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, t.formView())
	case fwdLogs:
		return t.logsView(w, h)
	}
	return t.listView(w, h)
}

func (t fwdTab) listView(w, _ int) string {
	items := t.buildItems()
	if len(items) == 0 {
		return sDim.Render("\n  no kubectl port-forward sessions — press " +
			sAccent.Render("n") + sDim.Render(" to create one\n\n  sessions live as children of this TUI and auto-reconnect on drop"))
	}
	head := fmt.Sprintf("  %-12s %-26s %-14s %-10s %-8s %-8s %s",
		"STATUS", "TARGET", "PORTS", "NS", "CTX", "UP", "RESTARTS")
	rows := []string{sHeader.Render(truncate(head, w))}
	for i, item := range items {
		var line string
		if item.isSaved() {
			spec := item.saved
			line = fmt.Sprintf("  %-12s %-26s %-14s %-10s %-8s",
				sDim.Render("○ saved"),
				truncate(spec.Target, 26),
				truncate(strings.Join(spec.Ports, ","), 14),
				truncate(orDash(spec.Namespace), 10),
				truncate(orDash(spec.Context), 8))
		} else {
			s := item.session
			st := s.Status()
			stStr := string(st)
			switch st {
			case kube.StatusConnected:
				stStr = sOK.Render("● " + stStr)
			case kube.StatusConnecting, kube.StatusReconnecting:
				stStr = sWarn.Render("◌ " + stStr)
			case kube.StatusFailed:
				stStr = sDanger.Render("✕ " + stStr)
			default:
				stStr = sDim.Render("○ " + stStr)
			}
			line = fmt.Sprintf("  %-12s %-26s %-14s %-10s %-8s %-8s %d",
				stStr,
				truncate(s.Spec.Target, 26),
				truncate(strings.Join(s.Spec.Ports, ","), 14),
				truncate(orDash(s.Spec.Namespace), 10),
				truncate(orDash(s.Spec.Context), 8),
				fmtAgo(s.StartedAt()),
				s.Restarts())
		}
		if i == t.cursor {
			line = sCursor.Render("▸" + line[1:])
		}
		rows = append(rows, line)
		if i == t.cursor && !item.isSaved() && item.session.LastError() != "" {
			rows = append(rows, sDim.Render("     ↳ "+truncate(item.session.LastError(), w-8)))
		}
	}
	return strings.Join(rows, "\n")
}

func (t fwdTab) formView() string {
	var b strings.Builder
	b.WriteString(sAccent.Render("New kubectl port-forward") + "\n\n")
	for i := range t.inputs {
		cursor := "  "
		if i == t.focus {
			cursor = sAccent.Render("> ")
		}
		b.WriteString(cursor + t.inputs[i].View() + "\n")
	}
	if t.formErr != "" {
		b.WriteString("\n" + sDanger.Render(t.formErr) + "\n")
	}
	b.WriteString("\n" + sDim.Render("tab next field · enter start · esc cancel"))
	return sModal.Render(b.String())
}

func (t fwdTab) logsView(_, _ int) string {
	s, ok := t.mgr.Get(t.logsFor)
	if !ok {
		return sDim.Render("session gone")
	}
	title := sAccent.Render("logs — "+s.Spec.Label()) +
		sDim.Render("  (r refresh · esc back)")
	return lipgloss.JoinVertical(lipgloss.Left, title, t.vp.View())
}

func (t fwdTab) keybar() string {
	switch t.mode {
	case fwdLogs:
		return "↑/↓ scroll · r refresh · esc back"
	case fwdForm:
		return "tab next · enter start · esc cancel"
	}
	return "n new · enter start/logs · s save · D del saved · x stop · X remove · ? help"
}

func splitPorts(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
