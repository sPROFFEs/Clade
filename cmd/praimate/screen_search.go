package main

// Cross-chat search pane. Bound to "/" from the chat list (and
// reachable via the palette as `search`). Searches MEMORY.md and
// every captured session summary/transcript across all chats for the
// typed query; live debounce on input so big workspaces stay snappy.

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"git.jtsec.local/lab/PrAImate/internal/launcher"
)

type searchModel struct {
	cfg *launcher.Config

	input   textinput.Model
	results []launcher.SearchHit
	cursor  int

	lastQuery   string
	lastRun     time.Time
	running     bool
	err         string
	debouncedAt time.Time
}

type searchResultsMsg struct {
	query   string
	results []launcher.SearchHit
	err     error
}

type searchTickMsg struct{}

func newSearchModel(cfg *launcher.Config) searchModel {
	ti := textinput.New()
	ti.Placeholder = "search MEMORY.md, summaries, transcripts across all chats…"
	ti.Width = 70
	ti.CharLimit = 200
	ti.Focus()
	return searchModel{cfg: cfg, input: ti}
}

// Init kicks off a debounce tick so we re-evaluate the query after
// the user pauses typing.
func (m searchModel) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg { return searchTickMsg{} }),
	)
}

func (m searchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case searchTickMsg:
		query := strings.TrimSpace(m.input.Value())
		if query != m.lastQuery && !m.running {
			// Debounce: only fire once the input has settled for
			// 200ms — avoids re-scanning on every keystroke.
			m.lastQuery = query
			m.lastRun = time.Now()
			m.running = true
			cfg := m.cfg
			runQuery := query
			return m, tea.Batch(
				func() tea.Msg {
					hits, err := launcher.Search(cfg.WorkspacesRoot, runQuery, launcher.SearchOptions{})
					return searchResultsMsg{query: runQuery, results: hits, err: err}
				},
				tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg { return searchTickMsg{} }),
			)
		}
		return m, tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg { return searchTickMsg{} })

	case searchResultsMsg:
		m.running = false
		if msg.query != m.lastQuery {
			// Stale result — a newer query has already started.
			return m, nil
		}
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.err = ""
		m.results = msg.results
		if m.cursor >= len(m.results) {
			m.cursor = 0
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, wrap(newChatListModel(m.cfg))
		case "up", "ctrl+k":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case "down", "ctrl+j":
			if m.cursor+1 < len(m.results) {
				m.cursor++
			}
			return m, nil
		case "enter":
			if m.cursor < len(m.results) {
				h := m.results[m.cursor]
				chat, err := launcher.LoadChat(m.cfg.WorkspacesRoot, h.ChatID)
				if err == nil && chat != nil {
					return m, wrap(newLaunchingModel(m.cfg, *chat))
				}
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m searchModel) View() string { return renderChrome(m.Title(), m.Body(), m.Help()) }

func (m searchModel) Title() string {
	if q := strings.TrimSpace(m.input.Value()); q != "" {
		return fmt.Sprintf("Search · %q (%d hits)", q, len(m.results))
	}
	return "Search"
}

func (m searchModel) Help() string {
	return "↑/↓ select · enter open chat · esc back"
}

func (m searchModel) NavSection() string   { return navSectionChats }
func (m searchModel) CapturingInput() bool { return true }

func (m searchModel) Body() string {
	var b strings.Builder
	b.WriteString(hintStyle.Render("Substring search across MEMORY.md + captured session summaries + transcripts. ↑/↓ move · enter resumes the chat the hit is in.") + "\n\n")
	b.WriteString(m.input.View() + "\n\n")
	if m.err != "" {
		b.WriteString(errorStyle.Render("✗ "+m.err) + "\n\n")
	}
	if strings.TrimSpace(m.input.Value()) == "" {
		b.WriteString(hintStyle.Render("(type at least one character to search)"))
		return b.String()
	}
	if m.running && len(m.results) == 0 {
		b.WriteString(hintStyle.Render("scanning…"))
		return b.String()
	}
	if len(m.results) == 0 {
		b.WriteString(hintStyle.Render("no hits — try a shorter substring"))
		return b.String()
	}

	var lastChat string
	for i, h := range m.results {
		isSel := i == m.cursor
		marker := "  "
		if isSel {
			marker = "› "
		}
		if h.ChatID != lastChat {
			lastChat = h.ChatID
			b.WriteString("\n")
			b.WriteString(subtitleStyle.Render(fmt.Sprintf("◆ %s", h.ChatLabel)) + "\n")
		}
		loc := h.File
		if h.LineNum > 0 {
			loc = fmt.Sprintf("%s:%d", h.File, h.LineNum)
		}
		line := fmt.Sprintf("%s%s  %s", marker, loc, lipglossDimRender(h.Snippet, isSel))
		b.WriteString(selectionRow(line, isSel) + "\n")
	}
	return b.String()
}
