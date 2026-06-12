package tui

import "github.com/charmbracelet/lipgloss"

var (
	cAccent  = lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#A78BFA"}
	cDim     = lipgloss.AdaptiveColor{Light: "#9CA3AF", Dark: "#6B7280"}
	cText    = lipgloss.AdaptiveColor{Light: "#111827", Dark: "#E5E7EB"}
	cWarn    = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FBBF24"}
	cDanger  = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"}
	cOK      = lipgloss.AdaptiveColor{Light: "#047857", Dark: "#34D399"}
	cBadgeBG = lipgloss.AdaptiveColor{Light: "#EDE9FE", Dark: "#312E81"}

	// Tabs share one shape — same underline, same padding — and differ only
	// in color: accent when active, gray/dormant otherwise.
	sTabActive = lipgloss.NewStyle().Bold(true).Foreground(cAccent).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(cAccent).Padding(0, 2)
	sTab = lipgloss.NewStyle().Foreground(cDim).
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(cDim).Padding(0, 2)

	sHeader   = lipgloss.NewStyle().Bold(true).Foreground(cDim)
	sCursor   = lipgloss.NewStyle().Bold(true).Background(cBadgeBG).Foreground(cText)
	sSelected = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	sRow      = lipgloss.NewStyle().Foreground(cText)
	sDim      = lipgloss.NewStyle().Foreground(cDim)
	sOK       = lipgloss.NewStyle().Foreground(cOK)
	sWarn     = lipgloss.NewStyle().Foreground(cWarn)
	sDanger   = lipgloss.NewStyle().Foreground(cDanger).Bold(true)
	sAccent   = lipgloss.NewStyle().Foreground(cAccent)
	sStar     = lipgloss.NewStyle().Foreground(cWarn)

	sBadge = map[string]lipgloss.Style{
		"DEV": lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#1D4ED8", Dark: "#93C5FD"}),
		"WEB": lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#047857", Dark: "#6EE7B7"}),
		"DB":  lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FCD34D"}),
		"MSG": lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#9D174D", Dark: "#F9A8D4"}),
		"TUN": lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#0E7490", Dark: "#67E8F9"}),
		"SYS": lipgloss.NewStyle().Foreground(cDim),
		"·":   lipgloss.NewStyle().Foreground(cDim),
	}

	sModal = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).
		Padding(1, 3)

	sPane = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true, false, false, false).
		BorderForeground(cDim)

	sStatusBar = lipgloss.NewStyle().Foreground(cDim)
	sFlash     = lipgloss.NewStyle().Foreground(cOK).Bold(true)
)

// keyHint renders one status-bar hint: key in accent colour, label dimmed.
func keyHint(key, label string) string {
	return sAccent.Render(key) + sDim.Render(" "+label)
}

// keySep is the dimmed dot separator between hints.
var keySep = sDim.Render(" · ")

func badge(b string) string {
	if st, ok := sBadge[b]; ok {
		return st.Render(b)
	}
	return b
}
