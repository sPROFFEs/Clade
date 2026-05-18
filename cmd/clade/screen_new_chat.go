package main

// New-chat flow: pick template → name + pick agent → create + open
// (jumps straight to agent launch in the new chat's sandbox).

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sPROFFEs/Clade/internal/launcher"
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
	return renderChrome(m.Title(), m.Body(), m.Help())
}

func (m pickTemplateModel) Title() string { return "New chat · pick a template" }
func (m pickTemplateModel) Help() string {
	return "↑/↓ select · enter pick · t manage templates · esc back"
}
func (m pickTemplateModel) NavSection() string    { return navSectionChats }
func (m pickTemplateModel) CapturingInput() bool  { return false }

func (m pickTemplateModel) Body() string {
	var b strings.Builder
	if !m.loaded {
		b.WriteString(hintStyle.Render("Loading templates..."))
		return b.String()
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
	return b.String()
}

// --- step 2: name + pick agent -------------------------------------------

type newChatFromTemplateModel struct {
	cfg      *launcher.Config
	template launcher.Template

	label       textinput.Model
	step        int // 0: label, 1: agent, 2: ollama y/n
	agents      []launcher.Agent
	cursor      int
	pickedAgent launcher.Agent // captured at step 1 → used at step 2's launch
	wantOllama  bool           // toggled with y/n/space at step 2
	err         string
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
				// Advance to step 2 (Ollama y/n) instead of launching.
				m.pickedAgent = a
				m.step = 2
				return m, nil
			}

		case 2:
			// Self-hosted model question.
			switch msg.String() {
			case "esc":
				m.step = 1
				return m, nil
			case "y", "Y":
				m.wantOllama = true
				return m, m.finalize()
			case "n", "N":
				m.wantOllama = false
				return m, m.finalize()
			case " ":
				m.wantOllama = !m.wantOllama
				return m, nil
			case "enter":
				return m, m.finalize()
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

// finalize creates the chat and routes either straight to launch (when
// the user declined Ollama) or to the Ollama config screen first (with
// a returnTo callback that launches the chat after Ollama is applied).
func (m newChatFromTemplateModel) finalize() tea.Cmd {
	cfg := *m.cfg
	cfg.LastAgent = string(m.pickedAgent.ID)
	tpl := m.template
	label := strings.TrimSpace(m.label.Value())
	agentID := m.pickedAgent.ID
	picked := m.pickedAgent
	wantOllama := m.wantOllama

	return func() tea.Msg {
		chat, err := launcher.CreateChat(cfg.WorkspacesRoot, tpl, label, agentID)
		if err != nil {
			return errMsg{err: err}
		}
		if wantOllama {
			// Route to the Ollama screen with a returnTo that, once the
			// user dismisses the apply step, builds the launch plan
			// against the chat's (now-updated) Ollama settings and
			// fires it.
			cfgCopy := cfg
			chatCopy := chat
			pickedCopy := picked
			returnTo := func() tea.Cmd {
				return func() tea.Msg {
					// Re-load the chat so we pick up the freshly-saved
					// Ollama settings (the Ollama screen wrote them).
					reloaded, lerr := launcher.LoadChat(cfgCopy.WorkspacesRoot, chatCopy.ID)
					if lerr != nil || reloaded == nil {
						return errMsg{err: fmt.Errorf("reload chat after Ollama apply: %v", lerr)}
					}
					plan, perr := launcher.Plan(reloaded.AsWorkspace(), pickedCopy)
					if perr != nil {
						return errMsg{err: perr}
					}
					_ = launcher.TouchChat(reloaded)
					ws := reloaded.AsWorkspace()
					return screenDoneMsg{launch: &plan, updateCfg: &cfgCopy, launchedWS: &ws}
				}
			}
			ollama := newOllamaModelWithReturn(&cfgCopy, chat.AsWorkspace(), returnTo)
			return screenDoneMsg{next: ollama}
		}

		// No Ollama — launch directly.
		plan, err := launcher.Plan(chat.AsWorkspace(), picked)
		if err != nil {
			return errMsg{err: err}
		}
		_ = launcher.TouchChat(&chat)
		wsCopy := chat.AsWorkspace()
		return screenDoneMsg{launch: &plan, updateCfg: &cfg, launchedWS: &wsCopy}
	}
}

func (m newChatFromTemplateModel) View() string {
	return renderChrome(m.Title(), m.Body(), m.Help())
}

func (m newChatFromTemplateModel) Title() string {
	return fmt.Sprintf("New chat from %q", m.template.Name)
}
func (m newChatFromTemplateModel) NavSection() string { return navSectionChats }
// step 0 is the name text input; later steps are list/toggle. Only the
// name step truly captures text, but `:` in a chat-name is unusual —
// being conservative and returning true for the whole wizard avoids
// the colon-eats-palette gotcha mid-flow.
func (m newChatFromTemplateModel) CapturingInput() bool { return m.step == 0 }
func (m newChatFromTemplateModel) Help() string {
	switch m.step {
	case 1:
		return "↑/↓ select · enter pick · esc back"
	case 2:
		return "y / n to choose · space to toggle · enter to accept · esc back"
	default:
		return "enter to continue · esc back"
	}
}

func (m newChatFromTemplateModel) Body() string {
	var b strings.Builder

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
	case 2:
		b.WriteString(subtitleStyle.Render("Name: ") + m.label.Value() + "\n")
		b.WriteString(subtitleStyle.Render("Agent: ") + m.pickedAgent.Label + "\n\n")
		mark := "[ ] No, use the agent's default backend"
		if m.wantOllama {
			mark = availableStyle.Render("[x] Yes, configure an Ollama / self-hosted model")
		}
		b.WriteString(inputLabelStyle.Render("Self-hosted model? ") + mark + "\n\n")
		b.WriteString(descStyle.Render(
			"Yes → drops you into the Ollama wizard (endpoint + model + per-agent " +
				"writes); after you apply, this chat launches. You can change or " +
				"clear the setting later via 'o' on the home screen.") + "\n")
		b.WriteString(descStyle.Render(
			"No → launches immediately with whatever cloud/OAuth backend the agent " +
				"uses by default.") + "\n")
	}
	if m.err != "" {
		b.WriteString("\n" + errorStyle.Render("✗ "+m.err))
	}
	return b.String()
}
