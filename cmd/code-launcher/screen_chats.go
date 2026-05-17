package main

// Home screen: a list of existing chats (cloned-and-running instances of
// templates), with shortcuts for new/delete and a path into template
// management. Replaces the old workspacesModel.

import (
	"errors"
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
			cfg := *m.cfg
			cfg.LastAgent = string(c.AgentID)
			return m, func() tea.Msg {
				plan, _, err := launcher.OpenChat(c)
				if errors.Is(err, launcher.ErrAgentUnavailable) {
					// Locked agent isn't installed — route to the install
					// screen instead of crashing. After install, user can
					// resume the chat normally.
					return screenDoneMsg{next: newInstallModel(&cfg, c.AsWorkspace(), c.AgentID)}
				}
				if err != nil {
					return errMsg{err: err}
				}
				_ = launcher.TouchChat(&c)
				wsCopy := c.AsWorkspace()
				return screenDoneMsg{launch: &plan, updateCfg: &cfg, launchedWS: &wsCopy}
			}
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
		case "a":
			// Power-user escape hatch: open the agents picker for this
			// chat (install / update / pick a different agent).
			if m.cursor < len(m.items) {
				return m, wrap(newAgentsModel(m.cfg, m.items[m.cursor].AsWorkspace()))
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
	b.WriteString(header("Chats · " + m.cfg.WorkspacesRoot))
	b.WriteString("\n")

	if m.migrateNote != "" {
		b.WriteString(hintStyle.Render(m.migrateNote) + "\n\n")
	}
	if m.err != "" {
		b.WriteString(errorStyle.Render("Error: "+m.err) + "\n\n")
	}

	if !m.loaded {
		b.WriteString(hintStyle.Render("Loading chats...") + "\n")
		return b.String()
	}

	if len(m.items) == 0 {
		b.WriteString(hintStyle.Render("No chats yet — start one from a template.") + "\n\n")
	}

	for i, c := range m.items {
		marker := "  "
		render := listItemStyle.Render
		if i == m.cursor {
			marker = "› "
			render = listItemSelectedStyle.Render
		}
		line := fmt.Sprintf("%s%s", marker, c.Label)
		meta := []string{}
		if c.Template != "" {
			meta = append(meta, c.Template)
		}
		if c.AgentID != "" {
			meta = append(meta, string(c.AgentID))
		}
		if !c.LastUsed.IsZero() {
			meta = append(meta, "last used "+humanAgo(c.LastUsed))
		}
		if len(meta) > 0 {
			line += " " + versionStyle.Render("("+strings.Join(meta, " · ")+")")
		}
		b.WriteString(render(line) + "\n")
		if i == m.cursor && c.Description != "" {
			b.WriteString(descStyle.Render(c.Description) + "\n")
		}
	}

	// "+ new chat" pseudo-row, always at the bottom.
	marker := "  "
	render := listItemStyle.Render
	if m.cursor == len(m.items) {
		marker = "› "
		render = listItemSelectedStyle.Render
	}
	b.WriteString(render(marker+"+ new chat…") + "\n")

	if m.deleteAsk && m.cursor < len(m.items) {
		b.WriteString("\n" + errorStyle.Render(
			fmt.Sprintf("Delete chat %q? This removes its sandbox, memory, and session log. (y/n)",
				m.items[m.cursor].Label)) + "\n")
	}

	b.WriteString(helpStyle.Render(
		"↑/↓ select · enter open · n new · e settings · a agents · d delete · t templates · r refresh · ctrl-c quit"))
	return b.String()
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
