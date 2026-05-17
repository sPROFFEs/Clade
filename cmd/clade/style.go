package main

// Styling layer for the launcher TUI. Inspired by k9s / claws / lazygit:
// a single rounded chrome around every screen with a title bar at the
// top, a body region, and a help bar at the bottom. Strong selection
// highlights so the user can always tell where focus is.

import (
	"github.com/charmbracelet/lipgloss"
)

// theme holds every colour the launcher uses. Centralising lets us
// switch palettes by swapping one struct. The defaults aim for both
// dark and light terminals (256-colour palette).
type theme struct {
	Border      lipgloss.Color // borders + light separators
	Title       lipgloss.Color // app name, big section labels
	Subtitle    lipgloss.Color // secondary header text
	Body        lipgloss.Color // default text
	Muted       lipgloss.Color // hints, descriptions, help bar
	Accent      lipgloss.Color // headers, focus highlights
	SelectedBG  lipgloss.Color // background of the highlighted row
	SelectedFG  lipgloss.Color // foreground of the highlighted row
	Success     lipgloss.Color // ✓ marks, availability badges
	Warning     lipgloss.Color // soft warnings
	Error       lipgloss.Color // failures, hard errors
	Disabled    lipgloss.Color // greyed-out items
}

var defaultTheme = theme{
	Border:     lipgloss.Color("63"),  // soft indigo
	Title:      lipgloss.Color("212"), // soft magenta
	Subtitle:   lipgloss.Color("245"), // light grey
	Body:       lipgloss.Color("254"), // near-white
	Muted:      lipgloss.Color("243"), // dim grey
	Accent:     lipgloss.Color("39"),  // bright cyan
	SelectedBG: lipgloss.Color("57"),  // deep violet
	SelectedFG: lipgloss.Color("231"), // pure white
	Success:    lipgloss.Color("42"),  // green
	Warning:    lipgloss.Color("214"), // amber
	Error:      lipgloss.Color("203"), // soft red
	Disabled:   lipgloss.Color("240"), // very dim grey
}

var t = defaultTheme

// Legacy single-line styles. Kept around so screens that haven't been
// re-skinned yet still compile; new code should compose from theme
// directly via the helpers below.
var (
	titleStyle = lipgloss.NewStyle().Foreground(t.Title).Bold(true)

	subtitleStyle = lipgloss.NewStyle().Foreground(t.Subtitle)

	headerSepStyle = lipgloss.NewStyle().Foreground(t.Border)

	listItemStyle = lipgloss.NewStyle().
			PaddingLeft(2).
			Foreground(t.Body)

	listItemSelectedStyle = lipgloss.NewStyle().
				PaddingLeft(2).
				Foreground(t.SelectedFG).
				Background(t.SelectedBG).
				Bold(true)

	descStyle = lipgloss.NewStyle().
			PaddingLeft(5).
			Foreground(t.Muted).
			Italic(true)

	availableStyle = lipgloss.NewStyle().Foreground(t.Success)

	missingStyle = lipgloss.NewStyle().Foreground(t.Disabled)

	versionStyle = lipgloss.NewStyle().Foreground(t.Disabled)

	errorStyle = lipgloss.NewStyle().Foreground(t.Error).Bold(true)

	hintStyle = lipgloss.NewStyle().Foreground(t.Muted).Italic(true)

	okStyle = lipgloss.NewStyle().Foreground(t.Success).Bold(true)

	inputLabelStyle = lipgloss.NewStyle().Foreground(t.Body)

	helpStyle = lipgloss.NewStyle().Foreground(t.Muted)
)

// header is the legacy in-body header (deprecated by renderChrome — kept
// so older screens still compile). New code passes a title string to
// renderChrome and skips this.
func header(subtitle string) string {
	line := titleStyle.Render("clade") +
		headerSepStyle.Render(" — ") +
		subtitleStyle.Render(subtitle)
	return line + "\n"
}

// selectionRow renders one list row with full-width selection highlight
// when isSelected. Pad with spaces so the background colour extends to
// the right edge of the chrome.
func selectionRow(text string, isSelected bool) string {
	width := bodyWidth()
	if isSelected {
		// Pad text to width-2 so the selected background fills the row.
		// The first 2 chars are the marker ("› " or "  ") already.
		return listItemSelectedStyle.Copy().
			Width(width - 2).
			Render(text)
	}
	return listItemStyle.Copy().
		Width(width - 2).
		Render(text)
}
