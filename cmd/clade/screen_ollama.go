package main

// Local-endpoint config screen (Ollama + any OpenAI-compatible backend
// that requires a Bearer key: GPUStack, vLLM-with-key, LiteLLM, …).
// Multi-step:
//   step 0: endpoint input
//   step 1: API key (optional — leave blank for vanilla Ollama)
//   step 2: model picker (loaded via probe) — falls back to manual entry
//   step 3: agent multi-select (which agents to configure)
//   step 4: apply + per-agent result
//
// Claude is configured per-workspace (env injected at launch). Codex
// and OpenCode get their per-user config files written.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sPROFFEs/Clade/internal/launcher"
	"github.com/sPROFFEs/Clade/internal/ollama"
)

type ollamaStep int

const (
	ollamaStepEndpoint ollamaStep = iota
	ollamaStepAPIKey
	ollamaStepModel
	ollamaStepAgents
	ollamaStepApply
)

type ollamaModel struct {
	cfg *launcher.Config
	ws  launcher.Workspace

	step       ollamaStep
	endpoint   textinput.Model
	apiKey     textinput.Model // optional; empty = no Bearer auth (Ollama)
	modelInput textinput.Model // used when probe returns no models

	probing      bool
	probeErr     string
	probedModels []string
	modelCursor  int

	// agent multi-select. agentCursor indexes into the entries list
	// rendered below, in this order: claude / codex / opencode /
	// deepseek. Bump the comment + bounds when you add another agent.
	pickClaude   bool
	pickCodex    bool
	pickOpenCode bool
	pickDeepSeek bool
	agentCursor  int // 0..3

	// returnTo, when set, names the Cmd to fire after the user
	// dismisses the apply screen. New-chat uses this to immediately
	// launch the chat we just configured Ollama for, instead of
	// dumping the user back at the home list.
	returnTo func() tea.Cmd

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

	ak := textinput.New()
	ak.Placeholder = "Bearer token (blank = no auth / Ollama)"
	ak.Width = 60
	ak.CharLimit = 400
	ak.EchoMode = textinput.EchoPassword
	ak.EchoCharacter = '•'
	if ws.Settings.Ollama.APIKey != "" {
		ak.SetValue(ws.Settings.Ollama.APIKey)
	}

	mi := textinput.New()
	mi.Placeholder = "model name (e.g. qwen3-coder)"
	mi.Width = 50
	mi.CharLimit = 120
	if ws.Settings.Ollama.Model != "" {
		mi.SetValue(ws.Settings.Ollama.Model)
	}

	// Pre-check each agent based on what's already on disk so a re-open
	// shows the current state instead of resetting to "only Claude".
	return ollamaModel{
		cfg:          cfg,
		ws:           ws,
		endpoint:     ep,
		apiKey:       ak,
		modelInput:   mi,
		pickClaude:   ws.Settings.Ollama.Endpoint != "",
		pickCodex:    ollama.CodexConfigured(),
		pickOpenCode: ollama.OpenCodeConfigured(),
		pickDeepSeek: ollama.DeepSeekConfigured(),
	}
}

// preTickAgentForChat ensures the chat's locked agent is pre-ticked
// when the user opens the Local-endpoint wizard from a chat. Without
// this, configuring Ollama for a codex-locked chat needed the user to
// remember to tick the codex checkbox — easy to miss, with the
// payoff being that Plan() injects `-p ollama_remote` even though the
// codex config file never got the profile, silently falling back to
// the default (OpenAI) provider. The chat-level setting gets saved
// either way (the claude checkbox writes it), creating a mismatch
// between "chat wants Ollama" and "agent's config knows nothing
// about it."
func preTickAgentForChat(m *ollamaModel, agent launcher.AgentID) {
	switch agent {
	case launcher.AgentClaude:
		m.pickClaude = true
	case launcher.AgentCodex:
		m.pickCodex = true
	case launcher.AgentOpenCode:
		m.pickOpenCode = true
	case launcher.AgentDeepSeek:
		m.pickDeepSeek = true
	}
}

// newOllamaModelWithReturn is the variant the new-chat flow uses so
// "apply then dismiss" launches the just-created chat rather than
// dropping the user back at the home list. Defaults all checkboxes to
// pre-checked on the basis of disk state but, for a brand-new chat
// with no prior config anywhere, tick Claude as the most useful
// default (the user picked Ollama with intent, after all).
func newOllamaModelWithReturn(cfg *launcher.Config, ws launcher.Workspace, returnTo func() tea.Cmd) ollamaModel {
	m := newOllamaModel(cfg, ws)
	m.returnTo = returnTo
	if !m.pickClaude && !m.pickCodex && !m.pickOpenCode && !m.pickDeepSeek {
		// Brand-new chat: assume the chat's locked agent benefits, and
		// pre-check Claude. The user can flip the others too if they
		// want global config writes.
		m.pickClaude = true
	}
	return m
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
		case ollamaStepAPIKey:
			return m.updateAPIKey(msg)
		case ollamaStepModel:
			return m.updateModel(msg)
		case ollamaStepAgents:
			return m.updateAgents(msg)
		case ollamaStepApply:
			if m.applied {
				switch msg.String() {
				case "esc", "enter":
					if m.returnTo != nil {
						return m, m.returnTo()
					}
					return m, wrap(newChatListModel(m.cfg))
				}
			}
		}
	}

	// Default: forward to whichever input is focused on this step.
	var cmd tea.Cmd
	switch m.step {
	case ollamaStepEndpoint:
		m.endpoint, cmd = m.endpoint.Update(msg)
	case ollamaStepAPIKey:
		m.apiKey, cmd = m.apiKey.Update(msg)
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
		return m, wrap(newChatListModel(m.cfg))
	case tea.KeyEnter:
		ep := strings.TrimSpace(m.endpoint.Value())
		if ep == "" {
			return m, nil
		}
		ep = ollama.NormalizeEndpoint(ep)
		m.endpoint.SetValue(ep)
		// Advance to the API-key step instead of probing immediately —
		// providers like GPUStack require the Bearer on /v1/models, so
		// we have to collect it first.
		m.step = ollamaStepAPIKey
		m.endpoint.Blur()
		m.apiKey.Focus()
		return m, textinput.Blink
	}
	var cmd tea.Cmd
	m.endpoint, cmd = m.endpoint.Update(msg)
	return m, cmd
}

// updateAPIKey handles the optional Bearer-key step. Enter (with or
// without a value) kicks off the probe. Esc goes back to endpoint.
// An empty key means "no auth" — vanilla Ollama path.
func (m ollamaModel) updateAPIKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.step = ollamaStepEndpoint
		m.apiKey.Blur()
		m.endpoint.Focus()
		return m, textinput.Blink
	case tea.KeyEnter:
		ep := ollama.NormalizeEndpoint(strings.TrimSpace(m.endpoint.Value()))
		key := strings.TrimSpace(m.apiKey.Value())
		m.probing = true
		m.probeErr = ""
		return m, func() tea.Msg {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			models, err := ollama.ListModels(ctx, ep, key)
			return ollamaProbeDoneMsg{models: models, err: err}
		}
	}
	var cmd tea.Cmd
	m.apiKey, cmd = m.apiKey.Update(msg)
	return m, cmd
}

func (m ollamaModel) updateModel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.step = ollamaStepAPIKey
		m.apiKey.Focus()
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
		if m.agentCursor < 3 {
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
		case 3:
			m.pickDeepSeek = !m.pickDeepSeek
		}
	case "enter":
		settings := ollama.Settings{
			Endpoint: m.endpoint.Value(),
			Model:    strings.TrimSpace(m.modelInput.Value()),
			APIKey:   strings.TrimSpace(m.apiKey.Value()),
		}
		ws := m.ws
		picks := applyPicks{
			claude:   m.pickClaude,
			codex:    m.pickCodex,
			opencode: m.pickOpenCode,
			deepseek: m.pickDeepSeek,
		}
		m.step = ollamaStepApply
		m.applying = true
		return m, func() tea.Msg {
			return ollamaApplyDoneMsg{
				results: applyOllama(ws, settings, picks),
			}
		}
	}
	return m, nil
}

// applyPicks groups the per-agent checkboxes the apply flow needs.
// Bundled into a struct so adding another agent doesn't ripple
// through every signature.
type applyPicks struct {
	claude, codex, opencode, deepseek bool
}

func (p applyPicks) any() bool {
	return p.claude || p.codex || p.opencode || p.deepseek
}

// applyOllama performs the actual writes and returns a per-line result
// log the screen renders.
func applyOllama(ws launcher.Workspace, s ollama.Settings, picks applyPicks) []string {
	var out []string
	if picks.claude {
		ws.Settings.Ollama = launcher.OllamaSettings{
			Endpoint: s.Endpoint, Model: s.Model, WireAPI: s.WireAPI, APIKey: s.APIKey,
		}
		if err := launcher.SaveWorkspaceLikeSettings(ws); err != nil {
			out = append(out, "✗ claude (chat settings): "+err.Error())
		} else {
			out = append(out, "✓ claude: per-chat ANTHROPIC_BASE_URL + --model on next launch")
		}
	}
	if picks.codex {
		// Probe BEFORE writing the profile. codex 0.130+ requires
		// /v1/responses; most OpenAI-compatible servers (vanilla
		// Ollama, vLLM until recently, llama.cpp's server, LocalAI)
		// implement only /v1/chat/completions. Writing the profile
		// against an endpoint that can't serve responses produces a
		// chat that loads but every turn fails with codex's generic
		// "experiencing high demand" retry message.
		//
		// The probe returns (warning, err): err blocks the apply, a
		// non-empty warning passes the apply but surfaces a note.
		// 5xx upstream errors (e.g. GPUStack worker temporarily down)
		// fall into the "warn but apply" bucket — the route exists,
		// the user just has a backend health issue separate from the
		// wire_api question.
		probeCtx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		probeWarn, probeErr := ollama.ProbeCodexCompat(probeCtx, s.Endpoint, s.APIKey, s.Model)
		cancel()
		out = append(out, hintStyle.Render("  · codex probe → /v1/responses ran"))
		if probeErr != nil {
			out = append(out, "✗ codex: "+probeErr.Error())
			// Strip any stale ollama_remote block left by a previous
			// (working or partial) apply so the user isn't left with
			// a config that codex picks up at launch and then chokes
			// on. Best-effort — DisableCodex is a no-op when nothing's
			// there.
			if _, derr := ollama.DisableCodex(); derr == nil {
				out = append(out, "  (cleaned up any stale ollama_remote block in ~/.codex/config.toml)")
			}
		} else {
			if path, err := ollama.ApplyCodex(s); err != nil {
				out = append(out, "✗ codex: "+err.Error())
			} else {
				out = append(out, "✓ codex: "+path+" (launched with -p ollama_remote)")
			}
			if probeWarn != "" {
				out = append(out, "  ⚠ codex: "+probeWarn)
			}
		}
	}
	if picks.opencode {
		if path, err := ollama.ApplyOpenCode(s, true); err != nil {
			out = append(out, "✗ opencode: "+err.Error())
		} else {
			out = append(out, "✓ opencode: "+path)
		}
	}
	if picks.deepseek {
		if path, err := ollama.ApplyDeepSeek(s); err != nil {
			out = append(out, "✗ deepseek: "+err.Error())
		} else {
			out = append(out, "✓ deepseek: "+path+" (provider=ollama; default model set)")
		}
	}
	if !picks.any() {
		out = append(out, "(nothing selected — pressed apply with no agents checked)")
	}
	return out
}

func (m ollamaModel) View() string {
	return renderChrome(m.Title(), m.Body(), m.Help())
}

func (m ollamaModel) Title() string { return "Local endpoint · Ollama / OpenAI-compat" }
func (m ollamaModel) Help() string {
	switch m.step {
	case ollamaStepEndpoint:
		return "enter next · esc back"
	case ollamaStepAPIKey:
		return "enter probe (blank = no auth) · esc back"
	case ollamaStepModel:
		if len(m.probedModels) == 0 {
			return "enter to continue · esc back"
		}
		return "↑/↓ select · enter pick · esc back"
	case ollamaStepAgents:
		return "↑/↓ select · space/x toggle · enter apply · esc back"
	case ollamaStepApply:
		return "enter / esc to return"
	}
	return "enter / esc"
}
func (m ollamaModel) NavSection() string { return navSectionChats }
// Endpoint + model input steps need ':' to flow through (URLs!).
// The agent multi-select and apply screens are list-driven but we
// keep the claim true throughout the wizard for consistency.
func (m ollamaModel) CapturingInput() bool { return true }

func (m ollamaModel) Body() string {
	var b strings.Builder

	b.WriteString(hintStyle.Render(
		"Configure agents to route through any OpenAI-compatible local endpoint (Ollama, GPUStack, vLLM, LiteLLM, …).",
	))
	b.WriteString("\n\n")

	switch m.step {
	case ollamaStepEndpoint:
		b.WriteString(inputLabelStyle.Render("Endpoint: "))
		b.WriteString(m.endpoint.View())

	case ollamaStepAPIKey:
		b.WriteString(subtitleStyle.Render("Endpoint: ") + m.endpoint.Value() + "\n\n")
		b.WriteString(inputLabelStyle.Render("API key: "))
		b.WriteString(m.apiKey.View())
		b.WriteString("\n\n" + hintStyle.Render(
			"Sent as `Authorization: Bearer <key>`. Leave blank for vanilla Ollama (no auth)."))
		if m.probing {
			b.WriteString("\n\n" + hintStyle.Render("Probing endpoint..."))
		}
		if m.probeErr != "" {
			b.WriteString("\n\n" + errorStyle.Render("✗ Probe failed: "+m.probeErr))
			b.WriteString("\n" + hintStyle.Render("You can still continue — manual model entry on next screen."))
		}

	case ollamaStepModel:
		b.WriteString(subtitleStyle.Render("Endpoint: ") + m.endpoint.Value() + "\n")
		if m.probeErr != "" {
			b.WriteString(errorStyle.Render("✗ Probe failed: "+m.probeErr) + "\n")
			b.WriteString(hintStyle.Render("Endpoint unreachable — type a model name manually to continue, or esc to fix the URL.") + "\n")
		}
		b.WriteString("\n")
		if len(m.probedModels) == 0 {
			b.WriteString(inputLabelStyle.Render("Model: "))
			b.WriteString(m.modelInput.View())
		} else {
			b.WriteString(hintStyle.Render(fmt.Sprintf("%d model(s) detected:", len(m.probedModels))) + "\n\n")
			for i, name := range m.probedModels {
				isSel := i == m.modelCursor
				marker := "  "
				if isSel {
					marker = "› "
				}
				b.WriteString(selectionRow(marker+name, isSel) + "\n")
			}
		}

	case ollamaStepAgents:
		b.WriteString(subtitleStyle.Render("Endpoint: ") + m.endpoint.Value() + "\n")
		b.WriteString(subtitleStyle.Render("Model: ") + m.modelInput.Value() + "\n\n")
		b.WriteString(hintStyle.Render("Which agents to configure?") + "\n\n")
		entries := []struct {
			label, hint string
			picked      bool
		}{
			{"claude   (per-chat env injection)", "ANTHROPIC_BASE_URL + --model on next launch", m.pickClaude},
			{"codex    (writes ~/.codex/config.toml)", "creates [profiles.ollama_remote] — launch via -p flag", m.pickCodex},
			{"opencode (writes ~/.config/opencode/opencode.json)", "registers ollama_remote provider, sets default model", m.pickOpenCode},
			{"deepseek (writes ~/.deepseek/config.toml)", "provider=ollama + [providers.ollama] block + default model", m.pickDeepSeek},
		}
		for i, e := range entries {
			isSel := i == m.agentCursor
			marker := "  "
			if isSel {
				marker = "› "
			}
			check := "[ ]"
			if e.picked {
				check = availableStyle.Render("[x]")
			}
			b.WriteString(selectionRow(marker+check+" "+e.label, isSel) + "\n")
			if isSel {
				b.WriteString(descStyle.Render(e.hint) + "\n")
			}
		}
		b.WriteString("\n" + descStyle.Render(
			"gemini-cli — not supported (see README → \"Gemini + Ollama\")"))

	case ollamaStepApply:
		if m.applying {
			b.WriteString(hintStyle.Render("Applying..."))
		} else {
			for _, line := range m.results {
				b.WriteString("  " + line + "\n")
			}
		}
	}
	return b.String()
}
