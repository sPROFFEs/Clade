package main

// layoutModel is the chiko-style persistent root that replaces the
// per-screen full-screen models once the user has finished first-run.
// It owns:
//
//   - a left-side Navigator (Chats / Templates / Agents / Help)
//   - a top tab strip listing opened chats (pinned by the user)
//   - a right-side detail Pane whose body comes from the active screen
//   - a bottom statusline with workspaces root + selection context
//   - two overlays: a `:` command palette and a `?` help sheet
//
// The pane content still comes from the existing screen types (now
// implementing Pane). The layout intercepts screenDoneMsg{next:...}
// internally and swaps panes; only screenDoneMsg{launch:...} bubbles
// up to rootModel.
//
// Focus model: keys go to the focused region (nav / pane / palette
// / help). Globals — ctrl-c, `:`, `?`, ctrl-1..4 — are handled at
// the layout level before forwarding.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sPROFFEs/Clade/internal/launcher"
)

// --- focus regions -------------------------------------------------------

type focusRegion int

const (
	focusPane focusRegion = iota
	focusNav
	focusTabs
	focusPalette
	focusHelp
)

// --- navigator sections --------------------------------------------------

const (
	navSectionChats     = "chats"
	navSectionTemplates = "templates"
	navSectionAgents    = "agents"
	navSectionHelp      = "help"
)

type navEntry struct {
	id       string
	label    string
	hotkey   string // ctrl-N digit, displayed as a hint
	makePane func(*launcher.Config) Pane
}

var navEntries = []navEntry{
	{
		id:    navSectionChats,
		label: "Chats",
		hotkey: "1",
		makePane: func(cfg *launcher.Config) Pane {
			return newChatListModel(cfg)
		},
	},
	{
		id:    navSectionTemplates,
		label: "Templates",
		hotkey: "2",
		makePane: func(cfg *launcher.Config) Pane {
			return newTemplateListModel(cfg)
		},
	},
	{
		id:    navSectionAgents,
		label: "Agents",
		hotkey: "3",
		makePane: func(cfg *launcher.Config) Pane {
			// Agents pane here is the "browse + install" view —
			// uses a sentinel workspace because no chat is bound.
			return newAgentsBrowser(cfg)
		},
	},
	{
		id:    navSectionHelp,
		label: "Help",
		hotkey: "4",
		makePane: func(cfg *launcher.Config) Pane {
			return newHelpPane(cfg)
		},
	},
}

// --- tabs ----------------------------------------------------------------

// chatTab is a pinned chat the user has stashed in the tab strip.
// Tabs are session-only (not persisted). The chat list pane emits
// pinChatMsg on `p`; ctrl-w closes the active tab.
type chatTab struct {
	chatID string
	label  string
}

// pinChatMsg is emitted by the chat list pane on `p` and intercepted
// by the layout to add a tab. We use a message (rather than touching
// layout state directly) so panes stay pure and testable.
type pinChatMsg struct {
	chatID string
	label  string
}

// --- layoutModel ---------------------------------------------------------

type layoutModel struct {
	cfg *launcher.Config

	// active pane
	pane Pane

	// navigator
	navCursor  int    // hover position
	navCurrent string // which section the pane is rendering for

	// tab strip
	tabs      []chatTab
	activeTab int // -1 means "no tab pinned, viewing nav section"

	// focus
	focus focusRegion

	// overlays
	palette paletteState
	help    helpOverlayState

	// status / errors
	statusErr string
}

func newLayoutModel(cfg *launcher.Config) *layoutModel {
	l := &layoutModel{
		cfg:        cfg,
		focus:      focusPane,
		navCursor:  0,
		navCurrent: navSectionChats,
		activeTab:  -1,
	}
	l.pane = navEntries[0].makePane(cfg)
	return l
}

func (m *layoutModel) Init() tea.Cmd {
	return m.pane.Init()
}

// --- update --------------------------------------------------------------

func (m *layoutModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pinChatMsg:
		m.pinChatTab(msg.chatID, msg.label)
		return m, nil

	case screenDoneMsg:
		// Internal pane swap. (screenDoneMsg.launch is handled in
		// rootModel.Update before we ever see it — Bubble Tea routes
		// the message through rootModel's switch first.)
		if msg.next != nil {
			if p, ok := msg.next.(Pane); ok {
				m.pane = p
				m.navCurrent = p.NavSection()
				for i, e := range navEntries {
					if e.id == m.navCurrent {
						m.navCursor = i
						break
					}
				}
				return m, m.pane.Init()
			}
		}
		return m, nil

	case errMsg:
		m.statusErr = msg.err.Error()
		return m, nil

	case tea.KeyMsg:
		// Global keys handled regardless of focus.
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case ":":
			if m.focus != focusPalette {
				m.openPalette()
				return m, textinput.Blink
			}
		case "?":
			if m.focus != focusPalette {
				m.toggleHelp()
				return m, nil
			}
		case "ctrl+1", "ctrl+2", "ctrl+3", "ctrl+4":
			// Direct nav-section jumps.
			digit := int(msg.String()[len(msg.String())-1] - '0')
			if digit >= 1 && digit <= len(navEntries) {
				return m, m.selectNav(digit - 1)
			}
		}

		// Focused-region routing.
		switch m.focus {
		case focusPalette:
			return m.updatePalette(msg)
		case focusHelp:
			// Any key closes the overlay; explicit ? toggle handled above.
			m.help.visible = false
			m.focus = focusPane
			return m, nil
		case focusNav:
			switch msg.String() {
			case "esc", "tab":
				m.focus = focusPane
				return m, nil
			case "up", "k":
				if m.navCursor > 0 {
					m.navCursor--
				}
				return m, nil
			case "down", "j":
				if m.navCursor < len(navEntries)-1 {
					m.navCursor++
				}
				return m, nil
			case "enter":
				return m, m.selectNav(m.navCursor)
			}
			return m, nil
		case focusPane:
			switch msg.String() {
			case "tab":
				m.focus = focusNav
				return m, nil
			case "shift+tab":
				if len(m.tabs) > 0 {
					m.focus = focusTabs
					return m, nil
				}
			case "ctrl+w":
				// Close active tab.
				m.closeTab(m.activeTab)
				return m, nil
			}
			// Fall through to pane.
		case focusTabs:
			switch msg.String() {
			case "tab", "esc":
				m.focus = focusPane
				return m, nil
			case "left", "h":
				if m.activeTab > 0 {
					m.activeTab--
					return m, m.openActiveTab()
				}
				return m, nil
			case "right", "l":
				if m.activeTab < len(m.tabs)-1 {
					m.activeTab++
					return m, m.openActiveTab()
				}
				return m, nil
			case "enter":
				return m, m.openActiveTab()
			case "ctrl+w":
				m.closeTab(m.activeTab)
				return m, nil
			}
			return m, nil
		}
	}

	// Default: forward to active pane.
	next, cmd := m.pane.Update(msg)
	if p, ok := next.(Pane); ok {
		m.pane = p
	}
	return m, cmd
}

// --- nav helpers ---------------------------------------------------------

// selectNav swaps the active pane to the given nav entry. Drops any
// pinned tab focus so the user sees the section's root pane.
func (m *layoutModel) selectNav(i int) tea.Cmd {
	if i < 0 || i >= len(navEntries) {
		return nil
	}
	m.navCursor = i
	m.navCurrent = navEntries[i].id
	m.activeTab = -1
	m.pane = navEntries[i].makePane(m.cfg)
	m.focus = focusPane
	return m.pane.Init()
}

// pinChatTab adds a tab for the given chat if one doesn't exist, and
// makes it active. Returns true if the tab was newly added. Called by
// the chat list pane via a custom message.
func (m *layoutModel) pinChatTab(chatID, label string) bool {
	for i, t := range m.tabs {
		if t.chatID == chatID {
			m.activeTab = i
			return false
		}
	}
	m.tabs = append(m.tabs, chatTab{chatID: chatID, label: label})
	m.activeTab = len(m.tabs) - 1
	return true
}

// openActiveTab rebuilds the right-pane to render whichever chat the
// active tab points at. We use the launching pane (the same one
// triggered by Enter on the chat list) so the user sees the same
// "compiling sandbox" feedback.
func (m *layoutModel) openActiveTab() tea.Cmd {
	if m.activeTab < 0 || m.activeTab >= len(m.tabs) {
		return nil
	}
	tab := m.tabs[m.activeTab]
	chat, err := launcher.LoadChat(m.cfg.WorkspacesRoot, tab.chatID)
	if err != nil || chat == nil {
		m.statusErr = "tab " + tab.label + ": " + fmt.Sprint(err)
		return nil
	}
	m.pane = newLaunchingModel(m.cfg, *chat)
	m.focus = focusPane
	return m.pane.Init()
}

func (m *layoutModel) closeTab(i int) {
	if i < 0 || i >= len(m.tabs) {
		return
	}
	m.tabs = append(m.tabs[:i], m.tabs[i+1:]...)
	if m.activeTab >= len(m.tabs) {
		m.activeTab = len(m.tabs) - 1
	}
	if len(m.tabs) == 0 {
		m.activeTab = -1
		m.focus = focusPane
	}
}

// --- view ----------------------------------------------------------------

func (m *layoutModel) View() string {
	w := termWidth
	if w < 60 {
		w = 60
	}
	h := termHeight
	if h < 14 {
		h = 14
	}

	// Outer frame eats 2 cols (border) + 2 cols (padding) = 4.
	innerW := w - 4
	// Reserve rows for: top bar (2), tab strip (1 if any), help bar (2),
	// statusline (1), bottom border (1). Roughly 6–7 chrome rows.
	tabRows := 0
	if len(m.tabs) > 0 {
		tabRows = 1
	}
	chromeRows := 2 + tabRows + 2 + 1
	innerH := h - chromeRows - 2 // 2 for outer borders
	if innerH < 6 {
		innerH = 6
	}

	navW := 22
	if innerW < 70 {
		navW = 16
	}
	paneW := innerW - navW - 1 // 1 column gutter

	// Build the regions.
	top := m.renderTopBar(innerW)
	navCol := m.renderNav(navW, innerH)
	paneCol := m.renderPane(paneW, innerH)
	body := lipgloss.JoinHorizontal(lipgloss.Top, navCol, " ", paneCol)
	var tabRow string
	if tabRows > 0 {
		tabRow = m.renderTabs(innerW)
	}
	help := m.renderHelpBar(innerW)
	status := m.renderStatusline(innerW)

	parts := []string{top}
	if tabRow != "" {
		parts = append(parts, tabRow)
	}
	parts = append(parts, body, help, status)
	stack := lipgloss.JoinVertical(lipgloss.Left, parts...)
	frame := chromeBorderStyle.Render(stack)

	if m.help.visible {
		return overlayCenter(frame, m.renderHelpOverlay(), w, h)
	}
	if m.palette.visible {
		return overlayBottom(frame, m.renderPalette(innerW), w, h)
	}
	return frame
}

// renderTopBar is the persistent header: app name + workspaces root.
func (m *layoutModel) renderTopBar(w int) string {
	app := titleStyle.Render("λ clade")
	sepStr := lipgloss.NewStyle().Foreground(t.Border).Render(" │ ")
	root := lipglossDim(m.cfg.WorkspacesRoot)
	hint := lipgloss.NewStyle().Foreground(t.Muted).Render(": cmd  ? help  ctrl-c quit")

	left := app + sepStr + root
	gap := w - lipgloss.Width(left) - lipgloss.Width(hint)
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + hint
	rule := lipgloss.NewStyle().Foreground(t.Border).Render(strings.Repeat("─", w))
	return line + "\n" + rule
}

// renderNav draws the left-side navigator column.
func (m *layoutModel) renderNav(w, h int) string {
	var b strings.Builder
	header := "Navigator"
	if m.focus == focusNav {
		header = lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("Navigator")
	} else {
		header = lipgloss.NewStyle().Foreground(t.Subtitle).Render(header)
	}
	b.WriteString(header + "\n")
	b.WriteString(lipgloss.NewStyle().Foreground(t.Border).Render(strings.Repeat("─", w)) + "\n")

	for i, e := range navEntries {
		isCurrent := e.id == m.navCurrent
		isCursor := i == m.navCursor && m.focus == focusNav
		var marker string
		switch {
		case isCurrent && isCursor:
			marker = "▸ "
		case isCursor:
			marker = "› "
		case isCurrent:
			marker = "● "
		default:
			marker = "  "
		}
		label := e.label
		if e.hotkey != "" {
			label = e.label + " " + lipglossDim("(^"+e.hotkey+")")
		}
		row := marker + label
		style := lipgloss.NewStyle().Width(w).Foreground(t.Body)
		switch {
		case isCursor:
			style = style.Foreground(t.SelectedFG).Background(t.SelectedBG).Bold(true)
		case isCurrent:
			style = style.Foreground(t.Accent).Bold(true)
		}
		b.WriteString(style.Render(row) + "\n")
	}

	if len(m.tabs) > 0 {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(t.Subtitle).Render("Open tabs") + "\n")
		for i, tab := range m.tabs {
			marker := "  "
			if i == m.activeTab {
				marker = "▸ "
			}
			row := marker + tab.label
			style := lipgloss.NewStyle().Width(w).Foreground(t.Muted)
			if i == m.activeTab {
				style = style.Foreground(t.Body).Bold(true)
			}
			b.WriteString(style.Render(row) + "\n")
		}
	}

	// Pad to height.
	content := b.String()
	lines := strings.Split(content, "\n")
	for len(lines) <= h {
		lines = append(lines, "")
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

// renderPane draws the right-side detail area with the active Pane's
// title bar + body, padded to the requested size.
func (m *layoutModel) renderPane(w, h int) string {
	var b strings.Builder
	title := m.pane.Title()
	titleStyled := lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render(title)
	if m.focus == focusPane {
		titleStyled = lipgloss.NewStyle().Foreground(t.Accent).Bold(true).
			Render("▌ " + title)
	} else {
		titleStyled = lipgloss.NewStyle().Foreground(t.Subtitle).
			Render("  " + title)
	}
	b.WriteString(titleStyled + "\n")
	b.WriteString(lipgloss.NewStyle().Foreground(t.Border).Render(strings.Repeat("─", w)) + "\n")

	// The pane's body is its current full rendered content. We trim to
	// width by truncating overly long lines, and to height by clipping.
	body := m.pane.Body()
	lines := strings.Split(body, "\n")
	avail := h - 2
	if avail < 1 {
		avail = 1
	}
	if len(lines) > avail {
		lines = lines[:avail]
	}
	for _, line := range lines {
		// Truncate ANSI-aware: cheap and good enough; lipgloss.Width
		// would be ideal but we don't want to truncate mid-escape.
		if lipgloss.Width(line) > w {
			line = ansiTruncate(line, w)
		}
		b.WriteString(line + "\n")
	}
	// Pad remaining height.
	for i := len(lines); i < avail; i++ {
		b.WriteString("\n")
	}
	return b.String()
}

// renderTabs draws the chat tab strip above the body area.
func (m *layoutModel) renderTabs(w int) string {
	var parts []string
	for i, tab := range m.tabs {
		label := " " + tab.label + " "
		style := lipgloss.NewStyle().Foreground(t.Muted).
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(t.Border)
		if i == m.activeTab {
			style = style.Foreground(t.Accent).Bold(true)
		}
		parts = append(parts, style.Render(label))
	}
	row := strings.Join(parts, "")
	if lipgloss.Width(row) > w {
		row = ansiTruncate(row, w)
	}
	return row
}

// renderHelpBar shows the active pane's contextual key hints.
func (m *layoutModel) renderHelpBar(w int) string {
	rule := lipgloss.NewStyle().Foreground(t.Border).Render(strings.Repeat("─", w))
	body := lipgloss.NewStyle().Foreground(t.Muted).Render(m.pane.Help())
	return rule + "\n" + body
}

// renderStatusline is the very bottom line: focus indicator + selection +
// last error (if any).
func (m *layoutModel) renderStatusline(w int) string {
	focusLabel := map[focusRegion]string{
		focusPane:    "pane",
		focusNav:     "nav",
		focusTabs:    "tabs",
		focusPalette: "palette",
		focusHelp:    "help",
	}[m.focus]
	left := lipgloss.NewStyle().Foreground(t.Accent).Render("["+focusLabel+"]") +
		"  " + lipglossDim("section: "+m.navCurrent)
	right := ""
	if m.cfg.LastAgent != "" {
		right = lipglossDim("agent: " + m.cfg.LastAgent)
	}
	if m.statusErr != "" {
		right = errorStyle.Render("✗ " + m.statusErr)
	}
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// --- command palette -----------------------------------------------------

type paletteState struct {
	visible bool
	input   textinput.Model
	err     string
}

func (m *layoutModel) openPalette() {
	if !m.palette.visible {
		ti := textinput.New()
		ti.Placeholder = "command — type `help` to list"
		ti.Width = 60
		ti.CharLimit = 200
		ti.Focus()
		m.palette = paletteState{visible: true, input: ti}
	}
	m.focus = focusPalette
}

func (m *layoutModel) closePalette() {
	m.palette.visible = false
	m.palette.err = ""
	m.focus = focusPane
}

func (m *layoutModel) updatePalette(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closePalette()
		return m, nil
	case "enter":
		cmd := strings.TrimSpace(m.palette.input.Value())
		m.closePalette()
		return m, m.runCommand(cmd)
	}
	var cmd tea.Cmd
	m.palette.input, cmd = m.palette.input.Update(msg)
	return m, cmd
}

// runCommand dispatches the palette command. Supported:
//
//	chats / templates / agents / help    — jump to nav section
//	new                                  — start a new chat (template picker)
//	quit                                 — exit cleanly
func (m *layoutModel) runCommand(c string) tea.Cmd {
	c = strings.ToLower(strings.TrimSpace(c))
	if c == "" {
		return nil
	}
	switch c {
	case "quit", "q", "exit":
		return tea.Quit
	case "new", "new-chat", "n":
		m.pane = newPickTemplateModel(m.cfg)
		m.navCurrent = navSectionChats
		return m.pane.Init()
	}
	for i, e := range navEntries {
		if c == e.id || c == strings.ToLower(e.label) {
			return m.selectNav(i)
		}
	}
	m.statusErr = "unknown command: " + c
	return nil
}

func (m *layoutModel) renderPalette(w int) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Accent).
		Padding(0, 1).
		Width(w - 4)
	title := lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render(":")
	hint := lipglossDim("enter run · esc cancel · try `chats`, `templates`, `agents`, `new`, `quit`")
	content := title + " " + m.palette.input.View() + "\n" + hint
	return box.Render(content)
}

// --- help overlay --------------------------------------------------------

type helpOverlayState struct {
	visible bool
}

func (m *layoutModel) toggleHelp() {
	m.help.visible = !m.help.visible
	if m.help.visible {
		m.focus = focusHelp
	} else {
		m.focus = focusPane
	}
}

func (m *layoutModel) renderHelpOverlay() string {
	rows := [][2]string{
		{"tab / shift-tab", "cycle focus between pane / nav / tabs"},
		{"ctrl-1 .. ctrl-4", "jump to nav section directly"},
		{":", "open command palette"},
		{"?", "toggle this help overlay"},
		{"ctrl-w", "close the active chat tab"},
		{"ctrl-c", "quit clade"},
		{"", ""},
		{"in nav:", ""},
		{"↑ ↓ / j k", "move cursor"},
		{"enter", "switch to that section"},
		{"", ""},
		{"in pane:", ""},
		{"↑ ↓ enter", "pane-specific (see help bar at bottom)"},
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("clade · keybindings") + "\n\n")
	for _, r := range rows {
		if r[0] == "" && r[1] == "" {
			b.WriteString("\n")
			continue
		}
		if r[1] == "" {
			b.WriteString(subtitleStyle.Render(r[0]) + "\n")
			continue
		}
		key := lipgloss.NewStyle().Foreground(t.Accent).Width(20).Render(r[0])
		b.WriteString(key + " " + lipglossDim(r[1]) + "\n")
	}
	b.WriteString("\n" + lipglossDim("press any key to dismiss"))
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Accent).
		Padding(1, 2)
	return box.Render(b.String())
}

// --- overlay layout helpers ----------------------------------------------

func overlayCenter(base, overlay string, w, h int) string {
	// Crude overlay: replace the centre region by composing a new
	// string. Bubble Tea's lipgloss.Place could centre-align, but we
	// want the chrome behind to still hint at presence — so just
	// stack overlay below base. Simpler and reads well in terminals.
	return base + "\n\n" + lipgloss.Place(w, 6, lipgloss.Center, lipgloss.Center, overlay)
}

func overlayBottom(base, overlay string, w, h int) string {
	return base + "\n" + lipgloss.Place(w, 3, lipgloss.Center, lipgloss.Bottom, overlay)
}

// --- ansi-aware truncation ----------------------------------------------

// ansiTruncate cuts a rendered line to width w columns, ignoring ANSI
// escape sequences. We don't try to repair mid-escape splits — lipgloss
// renders self-closing sequences and the worst case is a dropped reset.
func ansiTruncate(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	// Walk runes counting visible cols; copy until we hit w.
	var b strings.Builder
	cols := 0
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			b.WriteRune(r)
			continue
		}
		if inEsc {
			b.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		if cols >= w {
			break
		}
		b.WriteRune(r)
		cols++
	}
	return b.String() + "\x1b[0m"
}
