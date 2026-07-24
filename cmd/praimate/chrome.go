package main

// Chrome wrapper: every screen renders body content, then calls
// renderChrome(title, body, help) to draw the rounded outer frame with
// a title bar at the top and a help bar at the bottom — the visual
// shape used by k9s / lazygit / claws.
//
// Terminal width is captured from tea.WindowSizeMsg in the root model
// and stashed in a package-level var so any screen can ask for the
// current usable body width without threading state through every
// model constructor.

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"git.jtsec.local/lab/PrAImate/internal/version"
)

// Tracked state captured from the most recent tea.WindowSizeMsg. Package
// scope so View() methods can ask for it without a passed-down ctx.
var (
	termWidth  = 100 // default until WindowSizeMsg arrives
	termHeight = 30
)

func setTermSize(w, h int) {
	if w > 0 {
		termWidth = w
	}
	if h > 0 {
		termHeight = h
	}
}

// bodyWidth returns the number of columns available to body content
// after subtracting the outer border (2) and inner padding (2).
func bodyWidth() int {
	w := termWidth - 4
	if w < 20 {
		return 20
	}
	if w > 160 {
		return 160 // don't sprawl across ultrawide terminals
	}
	return w
}

// chromeBorderStyle is the outer rounded frame.
var chromeBorderStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(t.Border).
	Padding(0, 1)

// renderChrome wraps body in the standard launcher layout:
//
//	╭─ <title> ────────────────────────────╮
//	│                                       │
//	│   <body>                              │
//	│                                       │
//	├───────────────────────────────────────┤
//	│ <help> · ctrl-c quit                  │
//	╰───────────────────────────────────────╯
//
// title and help may be empty; the corresponding bar is omitted in that
// case.
func renderChrome(title, body, help string) string {
	w := bodyWidth()

	var blocks []string
	if title != "" {
		blocks = append(blocks, renderTitleBar(title, w))
	}
	blocks = append(blocks, body)
	if help != "" {
		blocks = append(blocks, renderHelpBar(help, w))
	}

	inner := lipgloss.JoinVertical(lipgloss.Left, blocks...)
	return chromeBorderStyle.Render(inner)
}

func renderTitleBar(title string, w int) string {
	app := titleStyle.Render("praimate")
	sep := lipgloss.NewStyle().Foreground(t.Border).Render(" │ ")
	titleStyled := lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render(title)
	verBadge := lipgloss.NewStyle().Foreground(t.Muted).Render("v" + version.Current)

	left := app + sep + verBadge + sep + titleStyled
	// Right-align a hint that the user can always ctrl-c out.
	right := lipgloss.NewStyle().Foreground(t.Muted).Render("ctrl-c quit")

	// Pad in the middle so right hugs the right edge.
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right

	// Underline so the title bar visually separates from the body.
	rule := lipgloss.NewStyle().Foreground(t.Border).Render(strings.Repeat("─", w))
	return line + "\n" + rule
}

func renderHelpBar(help string, w int) string {
	rule := lipgloss.NewStyle().Foreground(t.Border).Render(strings.Repeat("─", w))
	body := lipgloss.NewStyle().Foreground(t.Muted).Render(help)
	return rule + "\n" + body
}
