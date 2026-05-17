package main

// Ollama config screen. Multi-step:
//   step 0: endpoint input
//   step 1: model picker (loaded via probe) — falls back to manual entry
//   step 2: agent multi-select (which agents to configure)
//   step 3: apply + per-agent result
//
// Claude is configured per-workspace (env injected at launch). Codex
// and OpenCode get their per-user config files written.

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sdksdk/code-launcher/internal/launcher"
	"github.com/sdksdk/code-launcher/internal/ollama"
)

type ollamaStep int

const (
	ollamaStepEndpoint ollamaStep = iota
	ollamaStepModel
	ollamaStepAgents
	ollamaStepApply
)

type ollamaModel struct {
	cfg *launcher.Config
	ws  launcher.Workspace

	step       ollamaStep
	endpoint   textinput.Model
	modelInput textinput.Model // used when probe returns no models

	probing      bool
	probeErr     string
	probedModels []string
	modelCursor  int

	// agent multi-select
	pickClaude   bool
	pickCodex    bool
	pickOpenCode bool
	agentCursor  int // 0..2

	// apply results
	applying bool
	applied  bool
	results  []string
}

func newOllamaModel(cfg *launcher.Config, ws launcher.Workspace) ollamaModel {
	ep := textinput.New()
	ep.Placeholder = "http://192.168.1.50:11434"
	ep.Width = 60
	ep.CharLimit = 200
	// Pre-fill from workspace settings if already configured.
	if ws.Settings.Ollama.Endpoint != "" {
		ep.SetValue(ws.Settings.Ollama.Endpoint)
	}
	ep.Focus()

	mi := textinput.New()
	mi.Placeholder = "model name (e.g. qwen3-coder)"
	mi.Width = 50
	mi.CharLimit = 120
	if ws.Settings.Ollama.Model != "" {
		mi.SetValue(ws.Settings.Ollama.Model)
	}

	return ollamaModel{
		cfg:        cfg,
		ws:         ws,
		endpoint:   ep,
		modelInput: mi,
		// Default: configure Claude per-workspace, leave the global agent
		// configs alone unless the user explicitly opts in.
		pickClaude: true,
	}
}

type ollamaProbeDoneMsg struct {
	models []string
	err    error
}

type ollamaApplyDoneMsg struct{ results []string }

func (m ollamaModel) Init() tea.Cmd { return textinput.Blink }

func (m ollamaModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case ollamaProbeDoneMsg:
		m.probing = false
		if msg.err != nil {
			m.probeErr = msg.err.Error()
			// Fall through to model step with manual entry available.
			m.step = ollamaStepModel
			m.modelInput.Focus()
			return m, textinput.Blink
		}
		m.probedModels = msg.models
		m.probeErr = ""
		m.step = ollamaStepModel
		if len(m.probedModels) == 0 {
			m.modelInput.Focus()
			return m, textinput.Blink
		}
		return m, nil

	case ollamaApplyDoneMsg:
		m.applying = false
		m.applied = true
		m.results = msg.results
		return m, nil

	case tea.KeyMsg:
		switch m.step {
		case ollamaStepEndpoint:
			return m.updateEndpoint(msg)
		case ollamaStepModel:
			return m.updateModel(msg)
		case ollamaStepAgents:
			return m.updateAgents(msg)
		case ollamaStepApply:
			if m.applied {
				switch msg.String() {
				case "esc", "enter":
					return m, wrap(newWorkspacesModel(m.cfg))
				}
			}
		}
	}

	// Default: forward to whichever input is focused on this step.
	var cmd tea.Cmd
	switch m.step {
	case ollamaStepEndpoint:
		m.endpoint, cmd = m.endpoint.Update(msg)
	case ollamaStepModel:
		if len(m.probedModels) == 0 {
			m.modelInput, cmd = m.modelInput.Update(msg)
		}
	}
	return m, cmd
}

func (m ollamaModel) updateEndpoint(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		return m, wrap(newWorkspacesModel(m.cfg))
	case tea.KeyEnter:
		ep := strings.TrimSpace(m.endpoint.Value())
		if ep == "" {
			return m, nil
		}
		m.probing = true
		m.probeErr = ""
		ep = ollama.NormalizeEndpoint(ep)
		m.endpoint.SetValue(ep)
		return m, func() tea.Msg {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			models, err := ollama.ListModels(ctx, ep)
			return ollamaProbeDoneMsg{models: models, err: err}
		}
	}
	var cmd tea.Cmd
	m.endpoint, cmd = m.endpoint.Update(msg)
	return m, cmd
}

func (m ollamaModel) updateModel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.step = ollamaStepEndpoint
		m.endpoint.Focus()
		return m, textinput.Blink
	}
	if len(m.probedModels) == 0 {
		// Manual entry path.
		switch msg.Type {
		case tea.KeyEnter:
			if strings.TrimSpace(m.modelInput.Value()) == "" {
				return m, nil
			}
			m.step = ollamaStepAgents
			return m, nil
		}
		var cmd tea.Cmd
		m.modelInput, cmd = m.modelInput.Update(msg)
		return m, cmd
	}
	switch msg.String() {
	case "up", "k":
		if m.modelCursor > 0 {
			m.modelCursor--
		}
	case "down", "j":
		if m.modelCursor < len(m.probedModels)-1 {
			m.modelCursor++
		}
	case "enter":
		m.modelInput.SetValue(m.probedModels[m.modelCursor])
		m.step = ollamaStepAgents
	}
	return m, nil
}

func (m ollamaModel) updateAgents(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.step = ollamaStepModel
		return m, nil
	case "up", "k":
		if m.agentCursor > 0 {
			m.agentCursor--
		}
	case "down", "j":
		if m.agentCursor < 2 {
			m.agentCursor++
		}
	case " ", "x":
		switch m.agentCursor {
		case 0:
			m.pickClaude = !m.pickClaude
		case 1:
			m.pickCodex = !m.pickCodex
		case 2:
			m.pickOpenCode = !m.pickOpenCode
		}
	case "enter":
		settings := ollama.Settings{
			Endpoint: m.endpoint.Value(),
			Model:    strings.TrimSpace(m.modelInput.Value()),
		}
		ws := m.ws
		pickClaude, pickCodex, pickOpenCode := m.pickClaude, m.pickCodex, m.pickOpenCode
		m.step = ollamaStepApply
		m.applying = true
		return m, func() tea.Msg {
			return ollamaApplyDoneMsg{results: applyOllama(ws, settings, pickClaude, pickCodex, pickOpenCode)}
		}
	}
	return m, nil
}

// applyOllama performs the actual writes and returns a per-line result
// log the screen renders.
func applyOllama(ws launcher.Workspace, s ollama.Settings, claude, codex, opencode bool) []string {
	var out []string
	if claude {
		ws.Settings.Ollama = launcher.OllamaSettings{
			Endpoint: s.Endpoint, Model: s.Model, WireAPI: s.WireAPI,
		}
		if err := launcher.SaveWorkspaceSettings(ws); err != nil {
			out = append(out, "✗ claude (workspace): "+err.Error())
		} else {
			out = append(out, "✓ claude: per-workspace env will be set on next launch")
		}
	}
	if codex {
		if path, err := ollama.ApplyCodex(s); err != nil {
			out = append(out, "✗ codex: "+err.Error())
		} else {
			out = append(out, "✓ codex: "+path+" (use: codex -p ollama_remote)")
		}
	}
	if opencode {
		if path, err := ollama.ApplyOpenCode(s, true); err != nil {
			out = append(out, "✗ opencode: "+err.Error())
		} else {
			out = append(out, "✓ opencode: "+path)
		}
	}
	if !claude && !codex && !opencode {
		out = append(out, "(nothing selected — pressed apply with no agents checked)")
	}
	return out
}

func (m ollamaModel) View() string {
	var b strings.Builder
	b.WriteString(header("Ollama — local model routing"))
	b.WriteString("\n")
	b.WriteString(hintStyle.Render(
		"Configure agents to route through an OpenAI-compatible Ollama endpoint.",
	))
	b.WriteString("\n\n")

	switch m.step {
	case ollamaStepEndpoint:
		b.WriteString(inputLabelStyle.Render("Endpoint: "))
		b.WriteString(m.endpoint.View())
		b.WriteString("\n")
		if m.probing {
			b.WriteString("\n" + hintStyle.Render("Probing /api/tags..."))
		}
		if m.probeErr != "" {
			b.WriteString("\n" + errorStyle.Render("Probe failed: "+m.probeErr))
			b.WriteString("\n" + hintStyle.Render("You can still continue — manual model entry on next screen."))
		}
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("enter probe · esc back"))

	case ollamaStepModel:
		b.WriteString(subtitleStyle.Render("Endpoint: ") + m.endpoint.Value() + "\n\n")
		if len(m.probedModels) == 0 {
			b.WriteString(inputLabelStyle.Render("Model: "))
			b.WriteString(m.modelInput.View())
			b.WriteString("\n")
			b.WriteString(helpStyle.Render("enter to continue · esc back"))
		} else {
			b.WriteString(hintStyle.Render(fmt.Sprintf("%d model(s) detected:", len(m.probedModels))) + "\n\n")
			for i, name := range m.probedModels {
				marker := "  "
				r := listItemStyle.Render
				if i == m.modelCursor {
					marker = "› "
					r = listItemSelectedStyle.Render
				}
				b.WriteString(r(marker + name))
				b.WriteString("\n")
			}
			b.WriteString("\n")
			b.WriteString(helpStyle.Render("↑/↓ select · enter pick · esc back"))
		}

	case ollamaStepAgents:
		b.WriteString(subtitleStyle.Render("Endpoint: ") + m.endpoint.Value() + "\n")
		b.WriteString(subtitleStyle.Render("Model: ") + m.modelInput.Value() + "\n\n")
		b.WriteString(hintStyle.Render("Which agents to configure?") + "\n\n")
		entries := []struct {
			label, hint string
			picked      bool
		}{
			{"claude   (per-workspace env injection)", "ANTHROPIC_BASE_URL on next code-launcher launch", m.pickClaude},
			{"codex    (writes ~/.codex/config.toml)", "creates [profiles.ollama_remote] — use: codex -p ollama_remote", m.pickCodex},
			{"opencode (writes ~/.config/opencode/opencode.json)", "registers ollama_remote provider, sets default model", m.pickOpenCode},
		}
		for i, e := range entries {
			marker := "  "
			r := listItemStyle.Render
			if i == m.agentCursor {
				marker = "› "
				r = listItemSelectedStyle.Render
			}
			check := "[ ]"
			if e.picked {
				check = availableStyle.Render("[x]")
			}
			b.WriteString(r(marker + check + " " + e.label))
			b.WriteString("\n")
			if i == m.agentCursor {
				b.WriteString(descStyle.Render(e.hint) + "\n")
			}
		}
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("↑/↓ select · space/x toggle · enter apply · esc back"))

	case ollamaStepApply:
		if m.applying {
			b.WriteString(hintStyle.Render("Applying...") + "\n")
		} else {
			for _, line := range m.results {
				b.WriteString("  " + line + "\n")
			}
			b.WriteString("\n" + helpStyle.Render("enter / esc to return to workspaces"))
		}
	}
	return b.String()
}
