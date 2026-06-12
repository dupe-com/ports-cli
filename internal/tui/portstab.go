package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"github.com/dupe-com/ports-cli/internal/categorize"
	"github.com/dupe-com/ports-cli/internal/config"
	"github.com/dupe-com/ports-cli/internal/netscan"
)

// rowKey identifies a listener stably across rescans.
type rowKey struct {
	pid  int32
	port uint32
}

type portsTab struct {
	cfg *config.Config

	listeners []netscan.Listener // full scan result, favorites-first sort
	visible   []int              // indices into listeners after filters
	cursor    int
	selected  map[rowKey]bool

	filter    textinput.Model
	filtering bool
	catFilter int // index into catCycle; 0 = all

	showDetail bool
	paused     bool
	scanErr    string

	confirming bool
	pending    []netscan.Listener

	killing bool

	// last known listening-state per watched port, for change notifications
	watchState map[uint32]bool
}

var catCycle = append([]categorize.Category{""}, categorize.All...)

func newPortsTab(cfg *config.Config) portsTab {
	ti := textinput.New()
	ti.Placeholder = "fuzzy filter — port, name, user…"
	ti.Prompt = "/ "
	ti.CharLimit = 64
	return portsTab{
		cfg:        cfg,
		filter:     ti,
		selected:   map[rowKey]bool{},
		watchState: map[uint32]bool{},
		showDetail: true, // visible by default; d hides it
	}
}

func (t portsTab) captured() bool { return t.filtering || t.confirming }

// consumesEsc: an applied (but unfocused) filter is something esc can clear.
func (t portsTab) consumesEsc() bool { return t.filter.Value() != "" || t.catFilter != 0 }

// ── data plumbing ──────────────────────────────────────────────────────────

func (t portsTab) onScan(msg scanMsg) (portsTab, tea.Cmd) {
	if msg.err != nil {
		t.scanErr = msg.err.Error()
		return t, nil
	}
	t.scanErr = ""
	t.listeners = msg.listeners
	t.sortListeners()
	t.applyFilter()
	t.clampCursor()
	t.pruneSelection()
	return t, t.checkWatched()
}

func (t portsTab) onKillDone(msg killDoneMsg) (portsTab, tea.Cmd) {
	t.killing = false
	var ok, forced, failed int
	for _, r := range msg.results {
		switch {
		case r.Err != nil:
			failed++
		case r.Forced:
			forced++
		case r.Exited:
			ok++
		default:
			failed++
		}
	}
	t.selected = map[rowKey]bool{}
	text := fmt.Sprintf("killed %d", ok+forced)
	if forced > 0 {
		text += fmt.Sprintf(" (%d forced)", forced)
	}
	if failed > 0 {
		text += fmt.Sprintf(" · %d failed — permission? try sudo", failed)
	}
	return t, tea.Batch(flash(text), scanCmd)
}

// sortListeners puts favorites first, then ascending port.
func (t *portsTab) sortListeners() {
	sort.SliceStable(t.listeners, func(i, j int) bool {
		fi, fj := t.cfg.IsFavorite(t.listeners[i].Port), t.cfg.IsFavorite(t.listeners[j].Port)
		if fi != fj {
			return fi
		}
		if t.listeners[i].Port != t.listeners[j].Port {
			return t.listeners[i].Port < t.listeners[j].Port
		}
		return t.listeners[i].PID < t.listeners[j].PID
	})
}

// applyFilter recomputes visible from the fuzzy query + category filter.
func (t *portsTab) applyFilter() {
	t.visible = t.visible[:0]
	cat := catCycle[t.catFilter]

	candidates := make([]int, 0, len(t.listeners))
	for i, l := range t.listeners {
		if cat != "" && categorize.Categorize(l.Port, l.Name, l.Cmdline) != cat {
			continue
		}
		candidates = append(candidates, i)
	}

	q := strings.TrimSpace(t.filter.Value())
	if q == "" {
		t.visible = candidates
		return
	}
	hay := make([]string, len(candidates))
	for i, idx := range candidates {
		l := t.listeners[idx]
		hay[i] = fmt.Sprintf("%d %s %s %s", l.Port, l.Name, l.User, l.Cmdline)
	}
	for _, match := range fuzzy.Find(q, hay) {
		t.visible = append(t.visible, candidates[match.Index])
	}
}

func (t *portsTab) clampCursor() {
	if t.cursor >= len(t.visible) {
		t.cursor = len(t.visible) - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
}

// pruneSelection drops selections that no longer exist in the scan.
func (t *portsTab) pruneSelection() {
	alive := map[rowKey]bool{}
	for _, l := range t.listeners {
		alive[rowKey{l.PID, l.Port}] = true
	}
	for k := range t.selected {
		if !alive[k] {
			delete(t.selected, k)
		}
	}
}

// checkWatched diffs watched ports' state against the previous scan and
// notifies on transitions.
func (t *portsTab) checkWatched() tea.Cmd {
	if len(t.cfg.Watched) == 0 {
		return nil
	}
	now := map[uint32]bool{}
	for _, p := range t.cfg.Watched {
		now[p] = false
	}
	for _, l := range t.listeners {
		if _, ok := now[l.Port]; ok {
			now[l.Port] = true
		}
	}
	var cmds []tea.Cmd
	for port, listening := range now {
		prev, seen := t.watchState[port]
		if seen && prev != listening {
			state := "stopped listening"
			if listening {
				state = "started listening"
			}
			text := fmt.Sprintf("port %d %s", port, state)
			cmds = append(cmds, flash("👁 "+text))
			if t.cfg.Notify {
				cmds = append(cmds, notifyCmd("ports — watched port", text))
			}
		}
		t.watchState[port] = listening
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// ── input ──────────────────────────────────────────────────────────────────

func (t portsTab) update(msg tea.KeyMsg) (portsTab, tea.Cmd) {
	if t.confirming {
		return t.updateConfirm(msg)
	}
	if t.filtering {
		return t.updateFilter(msg)
	}

	switch msg.String() {
	case "esc":
		// clear applied filters (focused-filter esc is handled in updateFilter)
		t.filter.SetValue("")
		t.catFilter = 0
		t.applyFilter()
		t.clampCursor()
	case "up", "k":
		if t.cursor > 0 {
			t.cursor--
		}
	case "down", "j":
		if t.cursor < len(t.visible)-1 {
			t.cursor++
		}
	case "g":
		t.cursor = 0
	case "G":
		t.cursor = len(t.visible) - 1
		t.clampCursor()
	case "/":
		t.filtering = true
		t.filter.Focus()
		return t, textinput.Blink
	case "c":
		t.catFilter = (t.catFilter + 1) % len(catCycle)
		t.applyFilter()
		t.clampCursor()
	case "space":
		if l, ok := t.cur(); ok {
			k := rowKey{l.PID, l.Port}
			if t.selected[k] {
				delete(t.selected, k)
			} else {
				t.selected[k] = true
			}
			if t.cursor < len(t.visible)-1 {
				t.cursor++
			}
		}
	case "enter", "x":
		targets := t.targets()
		if len(targets) > 0 {
			t.confirming = true
			t.pending = targets
		}
	case "d":
		t.showDetail = !t.showDetail
	case "f":
		if l, ok := t.cur(); ok {
			on, err := t.cfg.ToggleFavorite(l.Port)
			if err != nil {
				return t, flash("config save failed: " + err.Error())
			}
			t.sortListeners()
			t.applyFilter()
			if on {
				return t, flash(fmt.Sprintf("★ favorited %d", l.Port))
			}
			return t, flash(fmt.Sprintf("unfavorited %d", l.Port))
		}
	case "w":
		if l, ok := t.cur(); ok {
			on, err := t.cfg.ToggleWatched(l.Port)
			if err != nil {
				return t, flash("config save failed: " + err.Error())
			}
			if on {
				t.watchState[l.Port] = true
				return t, flash(fmt.Sprintf("👁 watching %d — you'll be notified on change", l.Port))
			}
			delete(t.watchState, l.Port)
			return t, flash(fmt.Sprintf("stopped watching %d", l.Port))
		}
	case "r":
		return t, tea.Batch(flash("refreshing…"), scanCmd)
	case "p":
		t.paused = !t.paused
		if t.paused {
			return t, flash("auto-refresh paused")
		}
		return t, tea.Batch(flash("auto-refresh resumed"), scanCmd)
	}
	return t, nil
}

func (t portsTab) updateFilter(msg tea.KeyMsg) (portsTab, tea.Cmd) {
	switch msg.String() {
	case "esc":
		t.filtering = false
		t.filter.SetValue("")
		t.filter.Blur()
		t.applyFilter()
		t.clampCursor()
		return t, nil
	case "enter":
		t.filtering = false
		t.filter.Blur()
		return t, nil
	case "up", "down":
		// let arrows move the cursor without leaving filter mode
		t.filter.Blur()
		t.filtering = false
		return t.update(msg)
	}
	var cmd tea.Cmd
	t.filter, cmd = t.filter.Update(msg)
	t.applyFilter()
	t.clampCursor()
	return t, cmd
}

func (t portsTab) updateConfirm(msg tea.KeyMsg) (portsTab, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		t.confirming = false
		t.killing = true
		return t, killCmd(pendingPIDs(t.pending), t.cfg.Grace(), false)
	case "F":
		t.confirming = false
		t.killing = true
		return t, killCmd(pendingPIDs(t.pending), t.cfg.Grace(), true)
	case "esc", "n", "q":
		t.confirming = false
		t.pending = nil
	}
	return t, nil
}

// cur returns the listener under the cursor.
func (t portsTab) cur() (netscan.Listener, bool) {
	if t.cursor < 0 || t.cursor >= len(t.visible) {
		return netscan.Listener{}, false
	}
	return t.listeners[t.visible[t.cursor]], true
}

// targets returns the kill set: the multi-selection if any, else the cursor row.
func (t portsTab) targets() []netscan.Listener {
	if len(t.selected) > 0 {
		var out []netscan.Listener
		for _, l := range t.listeners {
			if t.selected[rowKey{l.PID, l.Port}] {
				out = append(out, l)
			}
		}
		return out
	}
	if l, ok := t.cur(); ok {
		return []netscan.Listener{l}
	}
	return nil
}

// pendingPIDs dedupes pids (one process may hold several selected ports).
func pendingPIDs(ls []netscan.Listener) []int32 {
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

// ── rendering ──────────────────────────────────────────────────────────────

func (t portsTab) view(w, h int) string {
	if t.confirming {
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, t.confirmView())
	}

	var sections []string
	if t.filtering || t.filter.Value() != "" {
		sections = append(sections, t.filter.View())
	}
	if t.scanErr != "" {
		sections = append(sections, sDanger.Render("scan error: "+t.scanErr))
	}

	detailH := 0
	if t.showDetail {
		detailH = 9
	}
	tableH := h - len(sections) - 1 - detailH // -1 header row
	if tableH < 3 {
		tableH = 3
	}

	sections = append(sections, t.tableView(w, tableH))
	if t.showDetail {
		sections = append(sections, t.detailView(w))
	}
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (t portsTab) tableView(w, maxRows int) string {
	cmdW := w - 52
	if cmdW < 10 {
		cmdW = 10
	}
	head := fmt.Sprintf("  %-7s %-4s %-8s %-10s %-7s %-9s %s",
		"PORT", "CAT", "PID", "USER", "UPTIME", "ADDR", "COMMAND")
	rows := []string{sHeader.Render(truncate(head, w))}

	if len(t.visible) == 0 {
		empty := "nothing is listening 🎉"
		if t.filter.Value() != "" || t.catFilter != 0 {
			empty = "no matches — esc clears the filter, c cycles categories"
		}
		rows = append(rows, sDim.Render("  "+empty))
		return strings.Join(rows, "\n")
	}

	// scroll window around the cursor
	start := 0
	if t.cursor >= maxRows {
		start = t.cursor - maxRows + 1
	}
	end := start + maxRows
	if end > len(t.visible) {
		end = len(t.visible)
	}

	for vi := start; vi < end; vi++ {
		l := t.listeners[t.visible[vi]]
		k := rowKey{l.PID, l.Port}
		cat := categorize.Categorize(l.Port, l.Name, l.Cmdline)

		marks := " "
		if t.cfg.IsFavorite(l.Port) {
			marks = sStar.Render("★")
		}
		sel := " "
		if t.selected[k] {
			sel = sSelected.Render("✓")
		}
		watch := ""
		if t.cfg.IsWatched(l.Port) {
			watch = " 👁"
		}

		line := fmt.Sprintf("%s%s%-7d %-4s %-8d %-10s %-7s %-9s %s%s",
			sel, marks, l.Port, cat.Badge(), l.PID,
			truncate(l.User, 10), l.Uptime(),
			truncate(l.AddrSummary(), 9),
			truncate(l.Name+" — "+l.Cmdline, cmdW), watch)

		if vi == t.cursor {
			rows = append(rows, sCursor.Render(truncate("▸"+line[1:], w)))
		} else {
			// recolor the badge after truncation-safe plain formatting
			rows = append(rows, sRow.Render(truncate(line, w)))
		}
	}

	if len(t.visible) > maxRows {
		rows = append(rows, sDim.Render(fmt.Sprintf("  … %d/%d (scroll with j/k)", end, len(t.visible))))
	}
	return strings.Join(rows, "\n")
}

func (t portsTab) detailView(w int) string {
	l, ok := t.cur()
	if !ok {
		return sPane.Render(sDim.Render("no selection"))
	}
	ports := netscan.PortsForPID(t.listeners, l.PID)
	portStrs := make([]string, len(ports))
	for i, p := range ports {
		portStrs[i] = fmt.Sprintf("%d", p)
	}
	cat := categorize.Categorize(l.Port, l.Name, l.Cmdline)
	body := fmt.Sprintf(
		"%s  pid %d · %s · %s\n%s\n\n%s %s\n%s cpu %.1f%% · mem %.1f%% · up %s\n%s %s",
		sAccent.Render(l.Name), l.PID, l.User, badge(cat.Badge()),
		sDim.Render(wrap(l.Cmdline, w-4, 3)),
		sHeader.Render("ports"), strings.Join(portStrs, ", "),
		sHeader.Render("usage"), l.CPUPercent, l.MemPercent, l.Uptime(),
		sHeader.Render("bind "), l.AddrSummary(),
	)
	return sPane.Width(w).Render(body)
}

func (t portsTab) confirmView() string {
	var b strings.Builder
	b.WriteString(sDanger.Render("Kill these processes?") + "\n\n")
	shown := t.pending
	if len(shown) > 8 {
		shown = shown[:8]
	}
	for _, l := range shown {
		fmt.Fprintf(&b, "  :%d  %s (pid %d, %s)\n", l.Port, l.Name, l.PID, l.User)
	}
	if extra := len(t.pending) - len(shown); extra > 0 {
		b.WriteString(sDim.Render(fmt.Sprintf("  … and %d more\n", extra)))
	}
	b.WriteString("\n" + sOK.Render("y") + " graceful (SIGTERM)  ")
	b.WriteString(sDanger.Render("F") + " force (SIGTERM → SIGKILL)  ")
	b.WriteString(sDim.Render("esc") + " cancel")
	return sModal.Render(b.String())
}

func (t portsTab) keybar() string {
	parts := []string{"/ filter", "space sel", "enter kill", "d detail", "f fav", "w watch", "c cat", "r refresh"}
	if t.paused {
		parts = append(parts, sWarn.Render("p resume ⏸"))
	} else {
		parts = append(parts, "p pause")
	}
	if cat := catCycle[t.catFilter]; cat != "" {
		parts = append(parts, sAccent.Render("cat:"+string(cat)))
	}
	if t.killing {
		parts = append(parts, sWarn.Render("killing…"))
	}
	parts = append(parts, "? help")
	return strings.Join(parts, " · ")
}

// wrap soft-wraps s to width, keeping at most maxLines lines.
func wrap(s string, width, maxLines int) string {
	if width < 10 {
		width = 10
	}
	var lines []string
	for len(s) > width && len(lines) < maxLines-1 {
		cut := strings.LastIndex(s[:width], " ")
		if cut < width/2 {
			cut = width
		}
		lines = append(lines, s[:cut])
		s = strings.TrimLeft(s[cut:], " ")
	}
	if len(s) > width {
		s = s[:width-1] + "…"
	}
	lines = append(lines, s)
	return strings.Join(lines, "\n")
}
