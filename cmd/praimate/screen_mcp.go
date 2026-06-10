package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sPROFFEs/PrAImate/internal/core"
	"github.com/sPROFFEs/PrAImate/internal/launcher"
)

type mcpTab int

const (
	mcpTabCatalogue mcpTab = iota
	mcpTabConnected
)

type mcpModel struct {
	cfg *launcher.Config

	tab       mcpTab
	cursor    int
	loaded    bool
	err       string
	status    string
	catalogue []core.MCPCatalogueEntry
	servers   []core.MCPServer

	editingKey bool
	apiKey     textinput.Model
	pending    *core.MCPCatalogueEntry
}

func newMCPModel(cfg *launcher.Config) mcpModel {
	ti := textinput.New()
	ti.Placeholder = "paste API key"
	ti.CharLimit = 4096
	ti.Width = 56
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '*'
	return mcpModel{cfg: cfg, apiKey: ti}
}

type mcpLoadedMsg struct {
	servers []core.MCPServer
	err     error
}

type mcpActionMsg struct {
	status string
	err    error
}

func (m mcpModel) Init() tea.Cmd {
	return func() tea.Msg {
		c := getAppCore()
		if c == nil {
			return mcpLoadedMsg{err: fmtCoreInitErr()}
		}
		servers, err := c.ListMCPServers(context.Background(), false)
		return mcpLoadedMsg{servers: servers, err: err}
	}
}

func (m mcpModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case mcpLoadedMsg:
		m.loaded = true
		m.catalogue = core.ListMCPCatalogue()
		m.servers = msg.servers
		m.err = ""
		if msg.err != nil {
			m.err = msg.err.Error()
		}
		m.clampCursor()
		return m, nil
	case mcpActionMsg:
		m.status = msg.status
		m.err = ""
		if msg.err != nil {
			m.err = msg.err.Error()
		}
		m.editingKey = false
		m.pending = nil
		m.apiKey.SetValue("")
		return m, m.Init()
	case tea.KeyMsg:
		if m.editingKey {
			switch msg.String() {
			case "esc":
				m.editingKey = false
				m.pending = nil
				m.apiKey.SetValue("")
				return m, nil
			case "enter":
				return m, m.connectPending()
			}
			var cmd tea.Cmd
			m.apiKey, cmd = m.apiKey.Update(msg)
			return m, cmd
		}
		switch msg.String() {
		case "left", "h":
			m.tab = mcpTabCatalogue
			m.cursor = 0
		case "right", "l":
			m.tab = mcpTabConnected
			m.cursor = 0
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < m.rowCount()-1 {
				m.cursor++
			}
		case "r":
			m.loaded = false
			return m, m.Init()
		case "enter", "c":
			if m.tab == mcpTabCatalogue {
				return m.startConnect()
			}
			return m.toggleConnected()
		case "d":
			if m.tab == mcpTabConnected {
				return m.deleteConnected()
			}
		}
	}
	return m, nil
}

func (m *mcpModel) clampCursor() {
	if m.cursor >= m.rowCount() {
		m.cursor = m.rowCount() - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m mcpModel) rowCount() int {
	if m.tab == mcpTabConnected {
		return len(m.servers)
	}
	return len(m.catalogue)
}

func (m mcpModel) startConnect() (tea.Model, tea.Cmd) {
	if m.cursor >= len(m.catalogue) {
		return m, nil
	}
	entry := m.catalogue[m.cursor]
	switch entry.Auth.Type {
	case core.MCPAuthAPIKey:
		m.pending = &entry
		m.editingKey = true
		m.apiKey.Focus()
		return m, textinput.Blink
	default:
		return m, m.connectEntry(entry, "")
	}
}

func (m mcpModel) connectPending() tea.Cmd {
	if m.pending == nil {
		return nil
	}
	entry := *m.pending
	key := strings.TrimSpace(m.apiKey.Value())
	return m.connectEntry(entry, key)
}

func (m mcpModel) connectEntry(entry core.MCPCatalogueEntry, apiKey string) tea.Cmd {
	return func() tea.Msg {
		c := getAppCore()
		if c == nil {
			return mcpActionMsg{err: fmtCoreInitErr()}
		}
		_, err := c.ConnectMCP(context.Background(), core.ConnectMCPRequest{
			CatalogueKey: entry.Key,
			APIKey:       apiKey,
		})
		if err != nil {
			return mcpActionMsg{err: err}
		}
		status := "connected " + entry.Name
		if entry.Auth.Type == core.MCPAuthOAuth {
			status += " (OAuth will complete in the agent CLI when first used)"
		}
		return mcpActionMsg{status: status}
	}
}

func (m mcpModel) toggleConnected() (tea.Model, tea.Cmd) {
	if m.cursor >= len(m.servers) {
		return m, nil
	}
	s := m.servers[m.cursor]
	return m, func() tea.Msg {
		c := getAppCore()
		if c == nil {
			return mcpActionMsg{err: fmtCoreInitErr()}
		}
		next := !s.Enabled
		if err := c.SetMCPEnabled(context.Background(), s.ID, next); err != nil {
			return mcpActionMsg{err: err}
		}
		if next {
			return mcpActionMsg{status: "enabled " + s.Name}
		}
		return mcpActionMsg{status: "disabled " + s.Name}
	}
}

func (m mcpModel) deleteConnected() (tea.Model, tea.Cmd) {
	if m.cursor >= len(m.servers) {
		return m, nil
	}
	s := m.servers[m.cursor]
	return m, func() tea.Msg {
		c := getAppCore()
		if c == nil {
			return mcpActionMsg{err: fmtCoreInitErr()}
		}
		if err := c.DeleteMCPServer(context.Background(), s.ID); err != nil {
			return mcpActionMsg{err: err}
		}
		return mcpActionMsg{status: "deleted " + s.Name}
	}
}

func (m mcpModel) View() string { return renderChrome(m.Title(), m.Body(), m.Help()) }

func (m mcpModel) Title() string {
	if m.tab == mcpTabConnected {
		return "MCP · connected"
	}
	return "MCP · catalogue"
}

func (m mcpModel) NavSection() string   { return navSectionMCP }
func (m mcpModel) CapturingInput() bool { return m.editingKey }

func (m mcpModel) Help() string {
	if m.editingKey {
		return "enter connect · esc cancel"
	}
	if m.tab == mcpTabConnected {
		return "h/l switch tab · enter toggle · d delete · r reload · ctrl-c quit"
	}
	return "h/l switch tab · enter connect · r reload · ctrl-c quit"
}

func (m mcpModel) Body() string {
	if !m.loaded {
		return descStyle.Render("loading MCP catalogue...")
	}
	var b strings.Builder
	b.WriteString(m.renderTabs() + "\n\n")
	if m.err != "" {
		b.WriteString(errorStyle.Render("error: "+m.err) + "\n\n")
	}
	if m.status != "" {
		b.WriteString(okStyle.Render(m.status) + "\n\n")
	}
	if m.editingKey && m.pending != nil {
		b.WriteString(subtitleStyle.Render("Connect "+m.pending.Name) + "\n")
		b.WriteString(descStyle.Render("API key is stored in ~/.praimate/db.sqlite and injected as an environment variable at launch.") + "\n")
		b.WriteString(m.apiKey.View() + "\n")
		return b.String()
	}
	if m.tab == mcpTabConnected {
		return b.String() + m.bodyConnected()
	}
	return b.String() + m.bodyCatalogue()
}

func (m mcpModel) renderTabs() string {
	left := "Catalogue"
	right := "Connected"
	if m.tab == mcpTabCatalogue {
		left = okStyle.Render(left)
	} else {
		left = descStyle.Render(left)
	}
	if m.tab == mcpTabConnected {
		right = okStyle.Render(right)
	} else {
		right = descStyle.Render(right)
	}
	return left + descStyle.Render("  |  ") + right
}

func (m mcpModel) bodyCatalogue() string {
	var b strings.Builder
	b.WriteString(descStyle.Render("Connect providers to make their tools available to YAML agents that list them in mcp_servers.") + "\n\n")
	for i, e := range m.catalogue {
		marker := "  "
		if i == m.cursor {
			marker = "> "
		}
		auth := string(e.Auth.Type)
		row := fmt.Sprintf("%s%-18s %-22s %-8s %s", marker, e.Key, e.Name, e.Transport, auth)
		b.WriteString(selectionRow(row, i == m.cursor) + "\n")
		if i == m.cursor {
			b.WriteString(descStyle.Render("  "+e.Description) + "\n")
			if e.Auth.EnvVar != "" {
				b.WriteString(descStyle.Render("  env: "+e.Auth.EnvVar) + "\n")
			}
		}
	}
	return b.String()
}

func (m mcpModel) bodyConnected() string {
	if len(m.servers) == 0 {
		return descStyle.Render("(no connected MCP servers)")
	}
	var b strings.Builder
	for i, s := range m.servers {
		marker := "  "
		if i == m.cursor {
			marker = "> "
		}
		state := "disabled"
		if s.Enabled {
			state = "enabled"
		}
		target := s.Command
		if target == "" {
			target = s.URL
		}
		row := fmt.Sprintf("%s%-18s %-22s %-8s %s", marker, s.ID, s.Name, s.Transport, state)
		b.WriteString(selectionRow(row, i == m.cursor) + "\n")
		if i == m.cursor {
			b.WriteString(descStyle.Render("  "+target) + "\n")
			if s.CatalogueKey != "" {
				b.WriteString(descStyle.Render("  catalogue: "+s.CatalogueKey) + "\n")
			}
		}
	}
	return b.String()
}
