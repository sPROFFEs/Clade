package main

// Home screen: a list of existing chats (cloned-and-running instances of
// templates), with shortcuts for new/delete and a path into template
// management. Replaces the old workspacesModel.

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sdksdk/code-launcher/internal/launcher"
)

type chatListModel struct {
	cfg          *launcher.Config
	items        []launcher.Chat
	cursor       int
	loaded       bool
	err          string
	migrateNote  string
	deleteAsk    bool // showing "really delete?" prompt
}

func newChatListModel(cfg *launcher.Config) chatListModel {
	return chatListModel{cfg: cfg}
}

type chatsLoadedMsg struct {
	items   []launcher.Chat
	migrate launcher.MigrationResult
	err     error
}

func (m chatListModel) Init() tea.Cmd {
	cfg := m.cfg
	return func() tea.Msg {
		// Self-heal: promote any leftover legacy workspace dirs to templates.
		migrate, _ := launcher.MigrateLegacyLayout(cfg.WorkspacesRoot)
		items, err := launcher.ListChats(cfg.WorkspacesRoot)
		return chatsLoadedMsg{items: items, migrate: migrate, err: err}
	}
}

func (m chatListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case chatsLoadedMsg:
		m.loaded = true
		m.items = msg.items
		if msg.err != nil {
			m.err = msg.err.Error()
		}
		if len(msg.migrate.Promoted) > 0 {
			m.migrateNote = fmt.Sprintf("Promoted %d legacy workspace(s) to templates: %s",
				len(msg.migrate.Promoted), strings.Join(msg.migrate.Promoted, ", "))
		}
		return m, nil

	case tea.KeyMsg:
		if m.deleteAsk {
			switch msg.String() {
			case "y", "Y":
				if m.cursor < len(m.items) {
					id := m.items[m.cursor].ID
					_ = launcher.DeleteChat(m.cfg.WorkspacesRoot, id)
				}
				m.deleteAsk = false
				return m, m.Init() // reload
			case "n", "N", "esc":
				m.deleteAsk = false
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items) {
				m.cursor++
			}
		case "enter":
			if m.cursor == len(m.items) {
				return m, wrap(newPickTemplateModel(m.cfg))
			}
			c := m.items[m.cursor]
			// Transition to the launching screen so the user gets
			// continuous feedback (spinner + phase line) during the
			// compile+decorate work, rather than a frozen chat list.
			return m, wrap(newLaunchingModel(m.cfg, c))
		case "n":
			return m, wrap(newPickTemplateModel(m.cfg))
		case "d":
			if m.cursor < len(m.items) {
				m.deleteAsk = true
			}
		case "e":
			if m.cursor < len(m.items) {
				// Per-chat settings editor: same screen as templates,
				// smart saver routes the write to chat.json.
				c := m.items[m.cursor]
				ws := c.AsWorkspace()
				return m, wrap(newSettingsModel(m.cfg, ws))
			}
		case "f":
			// Workpath file editor — mission.md / playbook.md / rules.md,
			// or "open in file manager" for tools/agents/etc.
			if m.cursor < len(m.items) {
				c := m.items[m.cursor]
				parent := newChatListModel(m.cfg)
				return m, wrap(newFilesModel(m.cfg, c.WorkpathDir, "chat "+c.Label, parent))
			}
		case "a":
			// Power-user escape hatch: open the agents picker for this
			// chat (install / update / pick a different agent).
			if m.cursor < len(m.items) {
				return m, wrap(newAgentsModel(m.cfg, m.items[m.cursor].AsWorkspace()))
			}
		case "o":
			// Ollama config — per-chat (writes into chat.json via the
			// smart saver). Surfaced here too so the user doesn't have
			// to dive through the agents picker to reach it.
			if m.cursor < len(m.items) {
				return m, wrap(newOllamaModel(m.cfg, m.items[m.cursor].AsWorkspace()))
			}
		case "t":
			return m, wrap(newTemplateListModel(m.cfg))
		case "r":
			return m, m.Init()
		}
	}
	return m, nil
}

func (m chatListModel) View() string {
	var b strings.Builder

	if m.migrateNote != "" {
		b.WriteString(hintStyle.Render(m.migrateNote) + "\n\n")
	}
	if m.err != "" {
		b.WriteString(errorStyle.Render("✗ "+m.err) + "\n\n")
	}

	if !m.loaded {
		b.WriteString(hintStyle.Render("Loading chats..."))
		return renderChrome(chatListTitle(m), b.String(), chatListHelp())
	}

	if len(m.items) == 0 {
		b.WriteString(hintStyle.Render("No chats yet — press n (or Enter on '+ new chat') to start.") + "\n\n")
	}

	for i, c := range m.items {
		marker := "  "
		isSel := i == m.cursor
		if isSel {
			marker = "› "
		}
		line := marker + c.Label
		meta := []string{}
		if c.Template != "" {
			meta = append(meta, c.Template)
		}
		if c.AgentID != "" {
			meta = append(meta, string(c.AgentID))
		}
		if !c.LastUsed.IsZero() {
			meta = append(meta, humanAgo(c.LastUsed))
		}
		if len(meta) > 0 {
			line += "  " + lipglossDimRender("("+strings.Join(meta, " · ")+")", isSel)
		}
		b.WriteString(selectionRow(line, isSel) + "\n")
		if isSel && c.Description != "" {
			b.WriteString(descStyle.Render(c.Description) + "\n")
		}
	}

	// "+ new chat" pseudo-row.
	isSelNew := m.cursor == len(m.items)
	marker := "  "
	if isSelNew {
		marker = "› "
	}
	b.WriteString(selectionRow(marker+"+ new chat…", isSelNew) + "\n")

	if m.deleteAsk && m.cursor < len(m.items) {
		b.WriteString("\n" + errorStyle.Render(
			fmt.Sprintf("Delete chat %q? This removes its sandbox, memory, and session log. (y/n)",
				m.items[m.cursor].Label)) + "\n")
	}

	return renderChrome(chatListTitle(m), b.String(), chatListHelp())
}

func chatListTitle(m chatListModel) string {
	tag := fmt.Sprintf("Chats (%d)", len(m.items))
	return tag + "  " + chromeContextSegment(m.cfg.WorkspacesRoot)
}

func chatListHelp() string {
	return "↑/↓ select · enter open · n new · e settings · f files · o ollama · a agents · d delete · t templates · r refresh"
}

// chromeContextSegment renders a path/label dimly so the title bar can
// carry secondary context (e.g. workspaces root) without dominating.
func chromeContextSegment(s string) string {
	return lipglossDim(s)
}

func lipglossDim(s string) string {
	return subtitleStyle.Render(s)
}

// lipglossDimRender renders s in the "dim/muted" colour, but uses the
// selected-bg-friendly palette when the surrounding row is selected so
// the text stays readable on the violet highlight.
func lipglossDimRender(s string, selected bool) string {
	if selected {
		return subtitleStyle.Render(s)
	}
	return versionStyle.Render(s)
}

// humanAgo formats a time as "5m ago" / "2h ago" / "3d ago" / etc.
func humanAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
