package main

// helpPane is the Help section in the navigator. Surfaces the keybind
// reference plus a short cheat sheet on how chats / templates / agents
// fit together, so a new user has somewhere persistent to land.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sPROFFEs/PrAImate/internal/launcher"
)

type helpPaneModel struct {
	cfg *launcher.Config
}

func newHelpPane(cfg *launcher.Config) helpPaneModel {
	return helpPaneModel{cfg: cfg}
}

func (m helpPaneModel) Init() tea.Cmd                           { return nil }
func (m helpPaneModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

func (m helpPaneModel) View() string {
	return renderChrome(m.Title(), m.Body(), m.Help())
}

func (m helpPaneModel) Title() string         { return "Help · keybinds & concepts" }
func (m helpPaneModel) NavSection() string    { return navSectionHelp }
func (m helpPaneModel) CapturingInput() bool  { return false }
func (m helpPaneModel) Help() string {
	return "ctrl-p palette · tab focus · ctrl-1..7 sections · ctrl-c quit"
}

func (m helpPaneModel) Body() string {
	var b strings.Builder

	b.WriteString(subtitleStyle.Render("Concepts") + "\n")
	b.WriteString(descStyle.Render("Template — reusable workpath (mission/playbook/rules) that chats clone from.") + "\n")
	b.WriteString(descStyle.Render("Chat     — one fork of a template; has its own sandbox, agent binding, memory.") + "\n")
	b.WriteString(descStyle.Render("Agent    — the CLI that drives a chat (claude, codex, opencode, deepseek-tui).") + "\n")
	b.WriteString("\n")

	b.WriteString(subtitleStyle.Render("Global keys") + "\n")
	for _, r := range [][2]string{
		{"tab / shift-tab", "cycle focus between pane / nav / tabs"},
		{"ctrl-1 .. ctrl-7", "jump to a nav section directly (chats/templates/agents/tools/backup/local-llm/help)"},
		{"ctrl-p", "open the command palette"},
		{"F1", "toggle the help overlay"},
		{":", "palette shortcut (only when no text input is focused)"},
		{"?", "help shortcut (only when no text input is focused)"},
		{"ctrl-w", "close the active chat tab"},
		{"ctrl-c", "quit praimate"},
	} {
		b.WriteString("  " + okStyle.Render(r[0]) + "  " + descStyle.Render(r[1]) + "\n")
	}
	b.WriteString("\n")
	b.WriteString(descStyle.Render(
		"Note: `:` and `?` defer to whatever text input is focused (Ollama endpoint, chat name, etc) so URLs and free-text descriptions can include them. ctrl-p / F1 always work as the universal fallback.") + "\n\n")

	b.WriteString(subtitleStyle.Render("Command palette") + "\n")
	for _, c := range [][2]string{
		{"chats", "open the Chats list"},
		{"templates", "open the Templates list"},
		{"agents", "open the Agents browser"},
		{"new", "start a new chat (template picker)"},
		{"quit", "exit praimate"},
	} {
		b.WriteString("  " + okStyle.Render(c[0]) + "  " + descStyle.Render(c[1]) + "\n")
	}
	b.WriteString("\n")

	b.WriteString(subtitleStyle.Render("Workspaces root") + "\n")
	b.WriteString(descStyle.Render("  " + m.cfg.WorkspacesRoot) + "\n")
	return b.String()
}
