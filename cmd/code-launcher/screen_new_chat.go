package main

// New-chat flow: pick template → name + pick agent → create + open
// (jumps straight to agent launch in the new chat's sandbox).

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sdksdk/code-launcher/internal/launcher"
)

// --- step 1: pick template -----------------------------------------------

type pickTemplateModel struct {
	cfg    *launcher.Config
	items  []launcher.Template
	cursor int
	loaded bool
	err    string
}

func newPickTemplateModel(cfg *launcher.Config) pickTemplateModel {
	return pickTemplateModel{cfg: cfg}
}

type templatesLoadedMsg struct {
	items   []launcher.Template
	skipped []launcher.SkippedTemplate
	err     error
}

func (m pickTemplateModel) Init() tea.Cmd {
	cfg := m.cfg
	return func() tea.Msg {
		items, err := launcher.ListTemplates(cfg.WorkspacesRoot)
		return templatesLoadedMsg{items: items, err: err}
	}
}

func (m pickTemplateModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case templatesLoadedMsg:
		m.loaded = true
		m.items = msg.items
		if msg.err != nil {
			m.err = msg.err.Error()
		}
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, wrap(newChatListModel(m.cfg))
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
				return m, wrap(newNewTemplateModel(m.cfg))
			}
			if len(m.items) == 0 {
				return m, nil
			}
			return m, wrap(newNewChatFromTemplateModel(m.cfg, m.items[m.cursor]))
		case "t":
			return m, wrap(newTemplateListModel(m.cfg))
		}
	}
	return m, nil
}

func (m pickTemplateModel) View() string {
	var b strings.Builder
	title := "New chat · pick a template"
	help := "↑/↓ select · enter pick · t manage templates · esc back"
	if !m.loaded {
		b.WriteString(hintStyle.Render("Loading templates..."))
		return renderChrome(title, b.String(), help)
	}
	if m.err != "" {
		b.WriteString(errorStyle.Render("✗ "+m.err) + "\n\n")
	}
	if len(m.items) == 0 {
		b.WriteString(hintStyle.Render("No templates yet — pick the '+ new template…' row below.") + "\n\n")
	}
	for i, tpl := range m.items {
		isSel := i == m.cursor
		marker := "  "
		if isSel {
			marker = "› "
		}
		b.WriteString(selectionRow(marker+tpl.Name, isSel) + "\n")
		if isSel && tpl.Description != "" {
			b.WriteString(descStyle.Render(tpl.Description) + "\n")
		}
	}
	isSelNew := m.cursor == len(m.items)
	marker := "  "
	if isSelNew {
		marker = "› "
	}
	b.WriteString(selectionRow(marker+"+ new template…", isSelNew) + "\n")
	return renderChrome(title, b.String(), help)
}

// --- step 2: name + pick agent -------------------------------------------

type newChatFromTemplateModel struct {
	cfg      *launcher.Config
	template launcher.Template

	label  textinput.Model
	step   int // 0: label, 1: agent
	agents []launcher.Agent
	cursor int
	err    string
}

func newNewChatFromTemplateModel(cfg *launcher.Config, tpl launcher.Template) newChatFromTemplateModel {
	ti := textinput.New()
	ti.Placeholder = "e.g. cve-fix · pr-123-review · pong-port"
	ti.Width = 60
	ti.CharLimit = 80
	ti.SetValue(tpl.Name + "-chat")
	ti.Focus()
	return newChatFromTemplateModel{cfg: cfg, template: tpl, label: ti}
}

type detectedAgentsMsg struct{ items []launcher.Agent }

func (m newChatFromTemplateModel) Init() tea.Cmd { return textinput.Blink }

func (m newChatFromTemplateModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case detectedAgentsMsg:
		m.agents = msg.items
		// Default cursor to last-used agent if available.
		if m.cfg.LastAgent != "" {
			for i, a := range m.agents {
				if string(a.ID) == m.cfg.LastAgent && a.Available {
					m.cursor = i
					break
				}
			}
		}
		return m, nil
	case tea.KeyMsg:
		switch m.step {
		case 0:
			switch msg.Type {
			case tea.KeyEsc:
				return m, wrap(newPickTemplateModel(m.cfg))
			case tea.KeyEnter:
				if strings.TrimSpace(m.label.Value()) == "" {
					m.err = "name cannot be empty"
					return m, nil
				}
				m.step = 1
				m.label.Blur()
				return m, func() tea.Msg {
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					return detectedAgentsMsg{items: launcher.DetectAgents(ctx)}
				}
			}
			var cmd tea.Cmd
			m.label, cmd = m.label.Update(msg)
			return m, cmd
		case 1:
			switch msg.String() {
			case "esc":
				m.step = 0
				m.label.Focus()
				return m, textinput.Blink
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down", "j":
				if m.cursor < len(m.agents)-1 {
					m.cursor++
				}
			case "enter":
				if m.cursor >= len(m.agents) {
					return m, nil
				}
				a := m.agents[m.cursor]
				if !a.Available {
					// Jump to the installer. Pass returnTo so when the
					// install completes the user lands back at the
					// template picker (re-detect agents, re-pick) — NOT
					// at an agents picker carrying a stub workspace,
					// which would later trigger the empty-sandbox bug
					// when the user picks a launchable agent.
					cfg := m.cfg
					return m, wrap(newInstallModelWithReturn(cfg, a.ID, func() tea.Model {
						return newPickTemplateModel(cfg)
					}))
				}
				cfg := *m.cfg
				cfg.LastAgent = string(a.ID)
				tpl := m.template
				label := strings.TrimSpace(m.label.Value())
				agentID := a.ID
				picked := a
				return m, func() tea.Msg {
					chat, err := launcher.CreateChat(cfg.WorkspacesRoot, tpl, label, agentID)
					if err != nil {
						return errMsg{err: err}
					}
					// Skip the redundant agents picker — we already know
					// the agent. Build the plan and launch directly.
					plan, err := launcher.Plan(chat.AsWorkspace(), picked)
					if err != nil {
						return errMsg{err: err}
					}
					_ = launcher.TouchChat(&chat)
					wsCopy := chat.AsWorkspace()
					return screenDoneMsg{launch: &plan, updateCfg: &cfg, launchedWS: &wsCopy}
				}
			}
		}
	case errMsg:
		m.step = 0
		m.err = msg.err.Error()
		m.label.Focus()
		return m, nil
	}
	return m, nil
}

func (m newChatFromTemplateModel) View() string {
	var b strings.Builder
	title := fmt.Sprintf("New chat from %q", m.template.Name)
	help := "enter to continue · esc back"

	b.WriteString(subtitleStyle.Render("Template: ") + m.template.Name + "\n")
	if m.template.Description != "" {
		b.WriteString(descStyle.Render(m.template.Description) + "\n")
	}
	b.WriteString("\n")

	switch m.step {
	case 0:
		b.WriteString(inputLabelStyle.Render("Chat name: "))
		b.WriteString(m.label.View() + "\n")
		b.WriteString(descStyle.Render(
			"A timestamp is added to make the directory unique; this name is what " +
				"shows in the home list.") + "\n")
	case 1:
		b.WriteString(subtitleStyle.Render("Name: ") + m.label.Value() + "\n\n")
		b.WriteString(inputLabelStyle.Render("Agent (locked at chat creation):") + "\n\n")
		if m.agents == nil {
			b.WriteString(hintStyle.Render("Scanning PATH for agent CLIs..."))
			break
		}
		for i, a := range m.agents {
			isSel := i == m.cursor && a.Available
			marker := "  "
			if i == m.cursor {
				marker = "› "
			}
			status := availableStyle.Render("● available")
			if !a.Available {
				status = missingStyle.Render("○ not installed")
				if a.ProbeError != "" {
					status = errorStyle.Render("✗ broken install")
				}
			}
			line := marker + a.Label + "   " + status
			if a.Version != "" {
				line += "  " + lipglossDimRender("("+a.Version+")", isSel)
			}
			b.WriteString(selectionRow(line, isSel) + "\n")
		}
	}
	if m.err != "" {
		b.WriteString("\n" + errorStyle.Render("✗ "+m.err))
	}
	return renderChrome(title, b.String(), help)
}
