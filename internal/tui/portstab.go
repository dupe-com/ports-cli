package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/atotto/clipboard"
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
	treeView   bool
	showAll    bool // reveal noise (system/unclassified) rows
	hiddenN    int  // noise rows folded away by the last applyFilter
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

// consumesEsc: an applied (but unfocused) filter or an expanded noise fold is
// something esc can step back from.
func (t portsTab) consumesEsc() bool {
	return t.filter.Value() != "" || t.catFilter != 0 || t.showAll
}

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

// sortListeners groups by category (dev first, noise last) with favorites at
// the top of their category (flat mode), or groups by PID in tree mode.
func (t *portsTab) sortListeners() {
	if t.treeView {
		t.sortListenersTree()
		return
	}
	sort.SliceStable(t.listeners, func(i, j int) bool {
		li, lj := t.listeners[i], t.listeners[j]
		ri := categorize.Categorize(li.Port, li.Name, li.Cmdline).Rank()
		rj := categorize.Categorize(lj.Port, lj.Name, lj.Cmdline).Rank()
		if ri != rj {
			return ri < rj
		}
		fi, fj := t.cfg.IsFavorite(li.Port), t.cfg.IsFavorite(lj.Port)
		if fi != fj {
			return fi
		}
		if li.Port != lj.Port {
			return li.Port < lj.Port
		}
		return li.PID < lj.PID
	})
}

// sortListenersTree groups by PID (PID groups with any favorite port sort first),
// then PID ascending, then port ascending within each group.
func (t *portsTab) sortListenersTree() {
	favPIDs := map[int32]bool{}
	for _, l := range t.listeners {
		if t.cfg.IsFavorite(l.Port) {
			favPIDs[l.PID] = true
		}
	}
	sort.SliceStable(t.listeners, func(i, j int) bool {
		li, lj := t.listeners[i], t.listeners[j]
		fi, fj := favPIDs[li.PID], favPIDs[lj.PID]
		if fi != fj {
			return fi
		}
		if li.PID != lj.PID {
			return li.PID < lj.PID
		}
		return li.Port < lj.Port
	})
}

// applyFilter recomputes visible from the fuzzy query + category filter.
// With no filters active (and not in tree view), noise rows — system daemons
// and unclassified ports — are folded away unless showAll is set; favorites,
// watched ports, and current selections always stay visible.
func (t *portsTab) applyFilter() {
	t.visible = t.visible[:0]
	t.hiddenN = 0
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
		if cat == "" && !t.treeView && !t.showAll {
			for _, idx := range candidates {
				l := t.listeners[idx]
				c := categorize.Categorize(l.Port, l.Name, l.Cmdline)
				if c.Noise() && !t.cfg.IsFavorite(l.Port) && !t.cfg.IsWatched(l.Port) &&
					!t.selected[rowKey{l.PID, l.Port}] {
					t.hiddenN++
					continue
				}
				t.visible = append(t.visible, idx)
			}
			return
		}
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
		// step back one layer: clear applied filters first, then re-fold noise
		// (focused-filter esc is handled in updateFilter)
		if t.filter.Value() != "" || t.catFilter != 0 {
			t.filter.SetValue("")
			t.catFilter = 0
		} else if t.showAll {
			t.showAll = false
		}
		t.applyFilter()
		t.clampCursor()
	case "up", "k":
		if t.cursor > 0 {
			t.cursor--
		}
	case "down", "j":
		if t.cursor < len(t.visible)-1 {
			t.cursor++
		} else if t.hiddenN > 0 {
			// scrolling past the end reveals the folded noise rows
			n := t.hiddenN
			t.showAll = true
			t.applyFilter()
			if t.cursor < len(t.visible)-1 {
				t.cursor++
			}
			return t, flash(fmt.Sprintf("revealed %d system & misc ports — esc re-hides", n))
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
	case "t":
		t.treeView = !t.treeView
		t.sortListeners()
		t.applyFilter()
		t.clampCursor()
	case "a":
		t.showAll = !t.showAll
		t.applyFilter()
		t.clampCursor()
		if t.showAll {
			return t, flash("showing all ports")
		}
		return t, flash("focus mode — system & misc ports hidden")
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
	case "K":
		if len(t.visible) > 0 {
			targets := make([]netscan.Listener, 0, len(t.visible))
			for _, idx := range t.visible {
				targets = append(targets, t.listeners[idx])
			}
			t.confirming = true
			t.pending = targets
		}
	case "y":
		if l, ok := t.cur(); ok {
			text := fmt.Sprintf("localhost:%d", l.Port)
			if err := clipboard.WriteAll(text); err != nil {
				return t, flash("clipboard error: " + err.Error())
			}
			return t, flash("copied " + text)
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

	if t.treeView {
		sections = append(sections, t.treeTableView(w, tableH))
	} else {
		sections = append(sections, t.tableView(w, tableH))
	}
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
		switch {
		case t.filter.Value() != "" || t.catFilter != 0:
			empty = "no matches — esc clears the filter, c cycles categories"
		case t.hiddenN > 0:
			empty = fmt.Sprintf("%d background ports hidden — ↓ or a shows them", t.hiddenN)
		}
		rows = append(rows, sDim.Render("  "+empty))
		return strings.Join(rows, "\n")
	}

	// Interleave category section headers (visual only — the cursor walks
	// t.visible, exactly like tree view). Fuzzy results are ranked by match
	// score, so headers only make sense without a query.
	type renderItem struct {
		isHeader bool
		cat      categorize.Category
		count    int
		visIdx   int
	}
	grouped := strings.TrimSpace(t.filter.Value()) == ""
	var items []renderItem
	if grouped {
		counts := map[categorize.Category]int{}
		cats := make([]categorize.Category, len(t.visible))
		for i, idx := range t.visible {
			l := t.listeners[idx]
			cats[i] = categorize.Categorize(l.Port, l.Name, l.Cmdline)
			counts[cats[i]]++
		}
		var last categorize.Category = "\x00"
		for vi := range t.visible {
			if cats[vi] != last {
				items = append(items, renderItem{isHeader: true, cat: cats[vi], count: counts[cats[vi]]})
				last = cats[vi]
			}
			items = append(items, renderItem{visIdx: vi})
		}
	} else {
		for vi := range t.visible {
			items = append(items, renderItem{visIdx: vi})
		}
	}

	// scroll window around the cursor's render position
	cursorItemIdx := 0
	for i, item := range items {
		if !item.isHeader && item.visIdx == t.cursor {
			cursorItemIdx = i
			break
		}
	}
	start := 0
	if cursorItemIdx >= maxRows {
		start = cursorItemIdx - maxRows + 1
	}
	end := start + maxRows
	if end > len(items) {
		end = len(items)
	}

	for i := start; i < end; i++ {
		item := items[i]
		if item.isHeader {
			tail := fmt.Sprintf(" · %s (%d)", item.cat.Title(), item.count)
			rows = append(rows, " "+badge(item.cat.Badge())+sDim.Render(truncate(tail, w-6)))
			continue
		}
		l := t.listeners[t.visible[item.visIdx]]
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

		if item.visIdx == t.cursor {
			rows = append(rows, sCursor.Render(truncate("▸"+line[1:], w)))
		} else {
			// recolor the badge after truncation-safe plain formatting
			rows = append(rows, sRow.Render(truncate(line, w)))
		}
	}

	if end < len(items) {
		rows = append(rows, sDim.Render(fmt.Sprintf("  … %d/%d (scroll with j/k)", end, len(items))))
	}
	if t.hiddenN > 0 {
		rows = append(rows, sDim.Render(fmt.Sprintf("  ▸ %d system & misc ports hidden — ↓ past the end or a shows all", t.hiddenN)))
	}
	return strings.Join(rows, "\n")
}

// treeTableView renders listeners grouped by PID, with a process header per group.
// The cursor/selection/kill logic is unchanged — visible indices are still the
// unit of navigation; group headers are purely visual.
func (t portsTab) treeTableView(w, maxRows int) string {
	type renderItem struct {
		isHeader bool
		pid      int32 // header: PID being introduced
		visIdx   int   // row: index into t.visible (matches t.cursor)
	}

	// Build pid→first-listener map and render item list in one pass.
	pidInfo := map[int32]netscan.Listener{}
	var items []renderItem
	var lastPID int32 = -1
	for vi, idx := range t.visible {
		l := t.listeners[idx]
		if l.PID != lastPID {
			items = append(items, renderItem{isHeader: true, pid: l.PID})
			if _, seen := pidInfo[l.PID]; !seen {
				pidInfo[l.PID] = l
			}
			lastPID = l.PID
		}
		items = append(items, renderItem{visIdx: vi})
	}

	cmdW := w - 30
	if cmdW < 10 {
		cmdW = 10
	}
	head := fmt.Sprintf("  %-7s %-4s %-9s %s", "PORT", "CAT", "ADDR", "COMMAND")
	rows := []string{sHeader.Render(truncate(head, w))}

	if len(t.visible) == 0 {
		empty := "nothing is listening 🎉"
		if t.filter.Value() != "" || t.catFilter != 0 {
			empty = "no matches — esc clears the filter, c cycles categories"
		}
		rows = append(rows, sDim.Render("  "+empty))
		return strings.Join(rows, "\n")
	}

	// Find the cursor's position in the render list for scroll calculation.
	cursorItemIdx := 0
	for i, item := range items {
		if !item.isHeader && item.visIdx == t.cursor {
			cursorItemIdx = i
			break
		}
	}

	start := 0
	if cursorItemIdx >= maxRows {
		start = cursorItemIdx - maxRows + 1
	}
	end := start + maxRows
	if end > len(items) {
		end = len(items)
	}

	for i := start; i < end; i++ {
		item := items[i]
		if item.isHeader {
			l := pidInfo[item.pid]
			header := fmt.Sprintf("  %s  pid %d · %s · up %s · cpu %.1f%% · mem %.1f%%",
				sAccent.Render(l.Name), l.PID, l.User, l.Uptime(), l.CPUPercent, l.MemPercent)
			rows = append(rows, sHeader.Render(truncate(header, w)))
			continue
		}

		idx := t.visible[item.visIdx]
		l := t.listeners[idx]
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

		if item.visIdx == t.cursor {
			cursorLine := fmt.Sprintf("  ▸%s %-7d %-4s %-9s %s%s",
				marks, l.Port, cat.Badge(),
				truncate(l.AddrSummary(), 9),
				truncate(l.Cmdline, cmdW), watch)
			rows = append(rows, sCursor.Render(truncate(cursorLine, w)))
		} else {
			line := fmt.Sprintf("  %s%s %-7d %-4s %-9s %s%s",
				sel, marks, l.Port, cat.Badge(),
				truncate(l.AddrSummary(), 9),
				truncate(l.Cmdline, cmdW), watch)
			rows = append(rows, sRow.Render(truncate(line, w)))
		}
	}

	if len(items) > end {
		rows = append(rows, sDim.Render("  … (scroll with j/k)"))
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
	parts := []string{"/ filter", "space sel", "enter kill", "K kill-all", "y copy", "t tree", "a all", "d detail", "f fav", "w watch", "c cat", "r refresh"}
	if t.paused {
		parts = append(parts, sWarn.Render("p resume ⏸"))
	} else {
		parts = append(parts, "p pause")
	}
	if t.treeView {
		parts = append(parts, sAccent.Render("tree ✦"))
	}
	if t.showAll {
		parts = append(parts, sAccent.Render("all ✦"))
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
