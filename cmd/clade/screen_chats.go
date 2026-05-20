package main

// Home screen: a list of existing chats (cloned-and-running instances of
// templates), with shortcuts for new/delete and a path into template
// management. Replaces the old workspacesModel.

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sPROFFEs/Clade/internal/launcher"
)

type chatListModel struct {
	cfg         *launcher.Config
	items       []launcher.Chat
	cursor      int
	loaded      bool
	err         string
	migrateNote string
	deleteAsk   bool // showing "really delete?" prompt
}

// chatListExtra are the persistent rows at the bottom of the list that
// aren't real chats but still take a cursor position. Order matters —
// they line up with index offsets in Update/View.
const (
	chatListExtraNew = iota // "+ new chat…"
	chatListExtraTpl        // "Manage templates →"
	chatListExtraCount
)

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

		maxCursor := len(m.items) + chatListExtraCount - 1
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < maxCursor {
				m.cursor++
			}
		case "enter":
			// Real chats live at indices [0..len-1]. The extra rows
			// (+ new chat, manage templates) come after.
			switch {
			case m.cursor == len(m.items)+chatListExtraNew:
				return m, wrap(newPickTemplateModel(m.cfg))
			case m.cursor == len(m.items)+chatListExtraTpl:
				return m, wrap(newTemplateListModel(m.cfg))
			default:
				c := m.items[m.cursor]
				return m, wrap(newLaunchingModel(m.cfg, c))
			}
		case "F":
			// Fresh-launch escape hatch: same as Enter but tells OpenChat
			// to skip native session restore. Useful when you want to
			// start over on a chat that already has captured sessions
			// without manually `rm -rf <chat>/sessions/*`. The on-disk
			// sessions/ dir is left intact — a subsequent plain Enter
			// will resume normally.
			if m.cursor < len(m.items) {
				return m, wrap(newLaunchingModelFresh(m.cfg, m.items[m.cursor]))
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
		case "f":
			// Workpath file editor — mission.md / playbook.md / rules.md,
			// or "open in file manager" for tools/agents/etc.
			if m.cursor < len(m.items) {
				c := m.items[m.cursor]
				parent := newChatListModel(m.cfg)
				return m, wrap(newFilesModel(m.cfg, c.WorkpathDir, "chat "+c.Label, parent))
			}
		// Per-chat agent override moved into the settings menu (Step 4
		// of the settings list — "Agent"). The Agents tab in the left
		// nav now serves install management only. Keeping the case
		// empty here would shadow the layout-level `a` handler from
		// jumping to the Agents tab, so we just don't register it.
		// Ollama / local-endpoint config moved into the settings menu
		// (`e` key → "Local endpoint" row). The chat list `o` key no
		// longer opens it directly — keeping all chat config under one
		// roof matches the agent picker's move in 0.1.10.
		case "t":
			return m, wrap(newTemplateListModel(m.cfg))
		case "p":
			// Pin the highlighted chat to the tab strip. The layout
			// intercepts pinChatMsg; this pane just emits.
			if m.cursor < len(m.items) {
				c := m.items[m.cursor]
				return m, func() tea.Msg {
					return pinChatMsg{chatID: c.ID, label: c.Label}
				}
			}
		case "r":
			return m, m.Init()
		case "/":
			return m, wrap(newSearchModel(m.cfg))
		}
	}
	return m, nil
}

func (m chatListModel) View() string {
	return renderChrome(m.Title(), m.Body(), m.Help())
}

func (m chatListModel) Title() string         { return chatListTitle(m) }
func (m chatListModel) Help() string          { return chatListHelp(m) }
func (m chatListModel) NavSection() string    { return navSectionChats }
func (m chatListModel) CapturingInput() bool  { return false }

func (m chatListModel) Body() string {
	var b strings.Builder

	if m.migrateNote != "" {
		b.WriteString(hintStyle.Render(m.migrateNote) + "\n\n")
	}
	if m.err != "" {
		b.WriteString(errorStyle.Render("✗ "+m.err) + "\n\n")
	}

	if !m.loaded {
		b.WriteString(hintStyle.Render("Loading chats..."))
		return b.String()
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
		if isSel {
			if c.Description != "" {
				b.WriteString(descStyle.Render(c.Description) + "\n")
			}
			b.WriteString(renderResumeDiagnostics(c) + "\n")
		}
	}

	// Persistent extra rows below the chat list.
	extras := []string{
		"+ new chat…",
		"Manage templates →",
	}
	for i, label := range extras {
		isSel := m.cursor == len(m.items)+i
		marker := "  "
		if isSel {
			marker = "› "
		}
		b.WriteString(selectionRow(marker+label, isSel) + "\n")
	}

	if m.deleteAsk && m.cursor < len(m.items) {
		b.WriteString("\n" + errorStyle.Render(
			fmt.Sprintf("Delete chat %q? This removes its sandbox, memory, and session log. (y/n)",
				m.items[m.cursor].Label)) + "\n")
	}

	return b.String()
}

func chatListTitle(m chatListModel) string {
	return fmt.Sprintf("Chats (%d)", len(m.items))
}

// chatListHelp builds the help line from only the keys that apply to
// the current cursor position. When "+ new chat" or "Manage templates"
// is highlighted, chat-action keys (e/f/o/a/d) are hidden so the user
// isn't told about actions that would no-op.
func chatListHelp(m chatListModel) string {
	parts := []string{"↑/↓ select"}
	chatSelected := m.cursor < len(m.items)
	if chatSelected {
		parts = append(parts,
			"enter open",
			"F fresh (skip resume)",
			"e settings (agent, local endpoint, language, memory, mirror, skills)",
			"f files",
			"p pin tab",
			"d delete",
		)
	} else {
		// Make Enter's effect explicit for the highlighted extra row.
		switch m.cursor {
		case len(m.items) + chatListExtraNew:
			parts = append(parts, "enter new chat")
		case len(m.items) + chatListExtraTpl:
			parts = append(parts, "enter manage templates")
		}
	}
	parts = append(parts, "n new", "/ search", "t templates", "r refresh")
	return strings.Join(parts, " · ")
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
