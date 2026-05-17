package main

import "github.com/charmbracelet/lipgloss"

// All on-screen styling lives here so the rest of the TUI stays focused on
// behavior. Colors are picked to land well on both dark and light terminals;
// we lean on the 256-color palette so they degrade gracefully.
var (
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("212")). // soft magenta
			Bold(true)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")) // grey

	headerSepStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("239"))

	listItemStyle = lipgloss.NewStyle().
			PaddingLeft(2)

	listItemSelectedStyle = lipgloss.NewStyle().
				PaddingLeft(2).
				Foreground(lipgloss.Color("39")).
				Bold(true)

	descStyle = lipgloss.NewStyle().
			PaddingLeft(5).
			Foreground(lipgloss.Color("244")).
			Italic(true)

	availableStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42"))

	missingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	versionStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("203")).
			Bold(true)

	hintStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")).
			Italic(true)

	okStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("42")).
		Bold(true)

	inputLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("250"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("239")).
			MarginTop(1)
)

func header(subtitle string) string {
	line := titleStyle.Render("code-launcher") +
		headerSepStyle.Render(" — ") +
		subtitleStyle.Render("agent launcher over wpc workpaths")
	if subtitle == "" {
		return line + "\n"
	}
	return line + "\n" + subtitleStyle.Render(subtitle) + "\n"
}
