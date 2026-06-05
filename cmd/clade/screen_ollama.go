package main

// Local-endpoint config screen (Ollama + any OpenAI-compatible backend
// that requires a Bearer key: GPUStack, vLLM-with-key, LiteLLM, …).
// Multi-step:
//   step 0: endpoint input
//   step 1: API key (optional — leave blank for vanilla Ollama)
//   step 2: model picker (loaded via probe) — falls back to manual entry
//   step 3: token limits (context + output caps for supported CLIs)
//   step 4: agent multi-select (which agents to configure)
//   step 5: apply + per-agent result
//
// Claude is configured per-workspace (env injected at launch). Codex
// and OpenCode get their per-user config files written.

import (
	"context"
	"fmt"
	"strconv"
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
	ollamaStepTokenLimits
	ollamaStepAgents
	ollamaStepApply
	// ollamaStepUseDefault is an optional FIRST step shown only when a
	// global default endpoint is configured (cfg.HasLocalDefault) and
	// the chat has no endpoint of its own yet. It offers "use the saved
	// endpoint" vs "enter a new one". Declared last so the zero value of
	// the step field stays ollamaStepEndpoint (unchanged default).
	ollamaStepUseDefault
)

type ollamaModel struct {
	cfg *launcher.Config
	ws  launcher.Workspace

	step          ollamaStep
	endpoint      textinput.Model
	apiKey        textinput.Model // optional; empty = no Bearer auth (Ollama)
	modelInput    textinput.Model // used when probe returns no models
	contextTokens textinput.Model
	outputTokens  textinput.Model
	limitCursor   int
	limitErr      string
	// wireAPI is the saved wire-API choice ("", "responses", "chat")
	// carried from the global default into the applied chat settings so
	// the codex compat path reuses it. Empty = unset/auto.
	wireAPI string
	// useDefaultCursor indexes the two choices on ollamaStepUseDefault:
	// 0 = use saved endpoint, 1 = enter a new one.
	useDefaultCursor int

	probing      bool
	probeErr     string
	probedModels []string
	modelCursor  int

	// agent multi-select. agentCursor indexes into the entries list
	// rendered below, in this order: claude / openclaude / codex /
	// opencode / deepseek. Bump the comment + bounds when you add
	// another agent.
	pickClaude     bool
	pickOpenClaude bool
	pickCodex      bool
	pickOpenCode   bool
	pickDeepSeek   bool
	agentCursor    int // 0..4

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

	ct := textinput.New()
	ct.Placeholder = strconv.Itoa(defaultLocalContextTokens)
	ct.Width = 12
	ct.CharLimit = 8
	contextDefault := localContextDefault(cfg)
	if ws.Settings.Ollama.ContextTokens > 0 {
		contextDefault = ws.Settings.Ollama.ContextTokens
	}
	ct.SetValue(strconv.Itoa(contextDefault))
	ct.Blur()

	ot := textinput.New()
	ot.Placeholder = strconv.Itoa(defaultLocalOutputTokens)
	ot.Width = 12
	ot.CharLimit = 8
	outputDefault := localOutputDefault(cfg)
	if ws.Settings.Ollama.OutputTokens > 0 {
		outputDefault = ws.Settings.Ollama.OutputTokens
	}
	ot.SetValue(strconv.Itoa(outputDefault))

	// Wire-API + start step. When the chat has no endpoint of its own
	// but a global default IS configured, pre-fill the connection fields
	// from the default and open on the "use saved endpoint?" step so the
	// user doesn't retype it. Otherwise fall through to the normal blank
	// endpoint entry (step zero value).
	wireAPI := ws.Settings.Ollama.WireAPI
	startStep := ollamaStepEndpoint
	if ws.Settings.Ollama.Endpoint == "" && cfg.HasLocalDefault() {
		ep.SetValue(cfg.DefaultLocalEndpoint)
		if cfg.DefaultLocalAPIKey != "" {
			ak.SetValue(cfg.DefaultLocalAPIKey)
		}
		if wireAPI == "" {
			wireAPI = cfg.DefaultLocalWireAPI
		}
		if cfg.DefaultLocalContextTokens > 0 && ws.Settings.Ollama.ContextTokens == 0 {
			ct.SetValue(strconv.Itoa(cfg.DefaultLocalContextTokens))
		}
		if cfg.DefaultLocalOutputTokens > 0 && ws.Settings.Ollama.OutputTokens == 0 {
			ot.SetValue(strconv.Itoa(cfg.DefaultLocalOutputTokens))
		}
		startStep = ollamaStepUseDefault
		ep.Blur()
	}

	// Pre-check each agent based on what's already on disk so a re-open
	// shows the current state instead of resetting to "only Claude".
	return ollamaModel{
		cfg:           cfg,
		ws:            ws,
		step:          startStep,
		wireAPI:       wireAPI,
		endpoint:      ep,
		apiKey:        ak,
		modelInput:    mi,
		contextTokens: ct,
		outputTokens:  ot,
		// Per-chat ticks come from the chat-level Agents list — the
		// authoritative record of "did the user tick this in this
		// chat's wizard?" Codex/opencode/deepseek also fall through
		// to disk-state if the chat-level list doesn't mention them
		// (e.g. global config exists from another chat / manual
		// edit) so the wizard still reflects that something IS
		// configured globally.
		pickClaude:     ws.Settings.Ollama.HasAgent(launcher.AgentClaude),
		pickOpenClaude: ws.Settings.Ollama.HasAgent(launcher.AgentOpenClaude),
		pickCodex:      ws.Settings.Ollama.HasAgent(launcher.AgentCodex) || ollama.CodexConfigured(),
		pickOpenCode:   ws.Settings.Ollama.HasAgent(launcher.AgentOpenCode) || ollama.OpenCodeConfigured(),
		pickDeepSeek:   ws.Settings.Ollama.HasAgent(launcher.AgentDeepSeek) || ollama.DeepSeekConfigured(),
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
	case launcher.AgentOpenClaude:
		m.pickOpenClaude = true
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
// dropping the user back at the home list. Checkbox defaults are the
// disk-state ones from newOllamaModel — callers that have the chat's
// locked agent in hand should follow up with preTickAgentForChat so
// only the agent that THIS chat will actually run gets pre-ticked.
//
// We used to always pre-tick claude as a "no agent configured yet?
// claude is the most useful default" fallback, which produced two
// boxes ticked when preTickAgentForChat then ticked the chat's real
// agent on top. Removed — preTickAgentForChat is now the only place
// that augments the disk-state defaults.
func newOllamaModelWithReturn(cfg *launcher.Config, ws launcher.Workspace, returnTo func() tea.Cmd) ollamaModel {
	m := newOllamaModel(cfg, ws)
	m.returnTo = returnTo
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
		case ollamaStepUseDefault:
			return m.updateUseDefault(msg)
		case ollamaStepEndpoint:
			return m.updateEndpoint(msg)
		case ollamaStepAPIKey:
			return m.updateAPIKey(msg)
		case ollamaStepModel:
			return m.updateModel(msg)
		case ollamaStepTokenLimits:
			return m.updateTokenLimits(msg)
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
	case ollamaStepTokenLimits:
		if m.limitCursor == 0 {
			m.contextTokens, cmd = m.contextTokens.Update(msg)
		} else {
			m.outputTokens, cmd = m.outputTokens.Update(msg)
		}
	}
	return m, cmd
}

// updateUseDefault handles the optional first step offered when a global
// default endpoint is configured. Two choices: [0] use the saved
// endpoint (connection fields are already pre-filled, so we probe its
// models straight away and jump to the model step), or [1] enter a new
// endpoint (clears the pre-fill and drops into the normal endpoint step).
func (m ollamaModel) updateUseDefault(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m, wrap(newChatListModel(m.cfg))
	case "up", "k":
		if m.useDefaultCursor > 0 {
			m.useDefaultCursor--
		}
		return m, nil
	case "down", "j":
		if m.useDefaultCursor < 1 {
			m.useDefaultCursor++
		}
		return m, nil
	case "enter":
		if m.useDefaultCursor == 1 {
			// "Enter a new one" — clear the pre-fill and go to the
			// normal blank endpoint step.
			m.endpoint.SetValue("")
			m.apiKey.SetValue("")
			m.wireAPI = ""
			m.step = ollamaStepEndpoint
			m.endpoint.Focus()
			return m, textinput.Blink
		}
		// "Use saved endpoint" — connection fields are already filled;
		// probe its models and land on the model step, exactly as the
		// API-key step's Enter does.
		ep := ollama.NormalizeEndpoint(strings.TrimSpace(m.endpoint.Value()))
		m.endpoint.SetValue(ep)
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
	return m, nil
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
			m.step = ollamaStepTokenLimits
			m.contextTokens.Focus()
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
		m.step = ollamaStepTokenLimits
		m.contextTokens.Focus()
	}
	return m, nil
}

func (m ollamaModel) updateTokenLimits(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.step = ollamaStepModel
		m.limitErr = ""
		m.contextTokens.Blur()
		m.outputTokens.Blur()
		if len(m.probedModels) == 0 {
			m.modelInput.Focus()
			return m, textinput.Blink
		}
		return m, nil
	case "up", "k":
		if m.limitCursor > 0 {
			m.limitCursor--
			m.outputTokens.Blur()
			m.contextTokens.Focus()
		}
		return m, textinput.Blink
	case "down", "j":
		if m.limitCursor < 1 {
			m.limitCursor++
			m.contextTokens.Blur()
			m.outputTokens.Focus()
		}
		return m, textinput.Blink
	case "enter":
		if _, _, err := parseTokenLimits(m.contextTokens.Value(), m.outputTokens.Value()); err != nil {
			m.limitErr = err.Error()
			return m, nil
		}
		m.limitErr = ""
		m.contextTokens.Blur()
		m.outputTokens.Blur()
		m.step = ollamaStepAgents
		return m, nil
	}
	var cmd tea.Cmd
	if m.limitCursor == 0 {
		m.contextTokens, cmd = m.contextTokens.Update(msg)
	} else {
		m.outputTokens, cmd = m.outputTokens.Update(msg)
	}
	return m, cmd
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
		if m.agentCursor < 4 {
			m.agentCursor++
		}
	case " ", "x":
		switch m.agentCursor {
		case 0:
			m.pickClaude = !m.pickClaude
		case 1:
			m.pickOpenClaude = !m.pickOpenClaude
		case 2:
			m.pickCodex = !m.pickCodex
		case 3:
			m.pickOpenCode = !m.pickOpenCode
		case 4:
			m.pickDeepSeek = !m.pickDeepSeek
		}
	case "enter":
		contextTokens, outputTokens, err := parseTokenLimits(m.contextTokens.Value(), m.outputTokens.Value())
		if err != nil {
			m.step = ollamaStepTokenLimits
			m.limitErr = err.Error()
			m.contextTokens.Focus()
			return m, textinput.Blink
		}
		settings := ollama.Settings{
			Endpoint:      m.endpoint.Value(),
			Model:         strings.TrimSpace(m.modelInput.Value()),
			APIKey:        strings.TrimSpace(m.apiKey.Value()),
			WireAPI:       m.wireAPI,
			ContextTokens: contextTokens,
			OutputTokens:  outputTokens,
		}
		ws := m.ws
		picks := applyPicks{
			claude:     m.pickClaude,
			openclaude: m.pickOpenClaude,
			codex:      m.pickCodex,
			opencode:   m.pickOpenCode,
			deepseek:   m.pickDeepSeek,
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
	claude, openclaude, codex, opencode, deepseek bool
}

func (p applyPicks) any() bool {
	return p.claude || p.openclaude || p.codex || p.opencode || p.deepseek
}

// applyOllama performs the actual writes and returns a per-line result
// log the screen renders. Three things happen:
//
//  1. The chat-level Ollama block is updated: written with the user's
//     endpoint+model+key + Agents list (which agents the user ticked)
//     when at least one agent is ticked; cleared when all are unticked.
//  2. For claude: nothing else — Plan() reads chat-level settings
//     directly and gates on `o.HasAgent(claude)`.
//  3. For codex / opencode / deepseek: ticked → write that agent's
//     global config; unticked → strip any global config left by a
//     previous apply (so the wizard's disk-state init doesn't re-tick
//     it on re-open).
//
// The split between "chat-level block" and "global per-agent config"
// matters because settings menu's "Local endpoint" row reads
// chat-level only — so the chat-level block has to be saved whenever
// the user configured ANY routing, not just claude.
func applyOllama(ws launcher.Workspace, s ollama.Settings, picks applyPicks) []string {
	var out []string

	// 1. Build the per-chat Agents opt-in list.
	var agents []string
	if picks.claude {
		agents = append(agents, string(launcher.AgentClaude))
	}
	if picks.openclaude {
		agents = append(agents, string(launcher.AgentOpenClaude))
	}
	if picks.codex {
		agents = append(agents, string(launcher.AgentCodex))
	}
	if picks.opencode {
		agents = append(agents, string(launcher.AgentOpenCode))
	}
	if picks.deepseek {
		agents = append(agents, string(launcher.AgentDeepSeek))
	}

	// 2. Write or clear the chat-level Ollama block.
	if len(agents) > 0 {
		ws.Settings.Ollama = launcher.OllamaSettings{
			Endpoint: s.Endpoint, Model: s.Model,
			WireAPI: s.WireAPI, APIKey: s.APIKey,
			ContextTokens: s.ContextTokens, OutputTokens: s.OutputTokens,
			Agents: agents,
		}
		if err := launcher.SaveWorkspaceLikeSettings(ws); err != nil {
			out = append(out, "✗ chat-level Ollama settings: "+err.Error())
		} else {
			out = append(out, "✓ chat: "+s.Model+" @ "+s.Endpoint+
				" (agents: "+strings.Join(agents, ", ")+")")
		}
	} else if ws.Settings.Ollama.Endpoint != "" || ws.Settings.Ollama.Model != "" {
		ws.Settings.Ollama = launcher.OllamaSettings{}
		if err := launcher.SaveWorkspaceLikeSettings(ws); err != nil {
			out = append(out, "✗ chat (clear Ollama settings): "+err.Error())
		} else {
			out = append(out, "↺ chat: cleared per-chat Ollama settings (no agents ticked)")
		}
	}

	// 3. Per-agent: claude is purely chat-level; the others write
	//    global config when ticked, strip it when unticked.
	if picks.claude {
		out = append(out, "✓ claude: per-chat ANTHROPIC_BASE_URL + --model on next launch (token caps not exposed by Claude env config)")
	}
	if picks.openclaude {
		// OpenClaude is purely chat-level too: Plan() injects
		// CLAUDE_CODE_USE_OPENAI=1 + OPENAI_BASE_URL/KEY/MODEL +
		// --model on next launch. No config file to write — the env
		// switch is openclaude's only routing knob.
		out = append(out, "✓ openclaude: per-chat CLAUDE_CODE_USE_OPENAI=1 + OPENAI_* env on next launch (token caps not exposed by OpenClaude env config)")
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
				out = append(out, fmt.Sprintf("✓ codex: %s (launched with -p ollama_remote; context cap %d; output cap not exposed by current Codex config)", path, s.ContextTokens))
			}
			if probeWarn != "" {
				out = append(out, "  ⚠ codex: "+probeWarn)
			}
		}
	}
	if !picks.codex && ollama.CodexConfigured() {
		// User unticked codex on a setup where it was previously
		// applied. Strip the global block so the wizard's disk-state
		// init won't re-tick it next time.
		if path, err := ollama.DisableCodex(); err == nil {
			out = append(out, "↺ codex: removed ollama_remote block from "+path+" (codex unticked)")
		}
	}
	if picks.opencode {
		if path, err := ollama.ApplyOpenCode(s, true); err != nil {
			out = append(out, "✗ opencode: "+err.Error())
		} else {
			out = append(out, fmt.Sprintf("✓ opencode: %s (caps %d/%d)", path, s.ContextTokens, s.OutputTokens))
		}
	} else if ollama.OpenCodeConfigured() {
		if path, err := ollama.DisableOpenCode(); err == nil {
			out = append(out, "↺ opencode: removed ollama_remote provider from "+path+" (opencode unticked)")
		}
	}
	if picks.deepseek {
		if path, err := ollama.ApplyDeepSeek(s); err != nil {
			out = append(out, "✗ deepseek: "+err.Error())
		} else {
			out = append(out, "✓ deepseek: "+path+" (provider=ollama; token caps not exposed by current config docs)")
		}
	} else if ollama.DeepSeekConfigured() {
		if path, err := ollama.DisableDeepSeek(); err == nil {
			out = append(out, "↺ deepseek: removed managed block from "+path+" (deepseek unticked)")
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
	case ollamaStepUseDefault:
		return "↑/↓ select · enter choose · esc cancel"
	case ollamaStepEndpoint:
		return "enter next · esc back"
	case ollamaStepAPIKey:
		return "enter probe (blank = no auth) · esc back"
	case ollamaStepModel:
		if len(m.probedModels) == 0 {
			return "enter to continue · esc back"
		}
		return "↑/↓ select · enter pick · esc back"
	case ollamaStepTokenLimits:
		return "↑/↓ select · type digits · enter continue · esc back"
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
	case ollamaStepUseDefault:
		b.WriteString(subtitleStyle.Render("A default local endpoint is saved:") + "\n")
		b.WriteString("  " + m.cfg.DefaultLocalEndpoint + "\n\n")
		choices := []string{
			"Use saved endpoint",
			"Enter a new endpoint",
		}
		for i, c := range choices {
			sel := i == m.useDefaultCursor
			marker := "  "
			if sel {
				marker = "› "
			}
			b.WriteString(selectionRow(marker+c, sel) + "\n")
		}
		if m.probing {
			b.WriteString("\n" + hintStyle.Render("Querying models from saved endpoint..."))
		}
		b.WriteString("\n" + hintStyle.Render(
			"Saved endpoint comes from the Local LLM tab (^5). Model + agents are still chosen per chat."))

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

	case ollamaStepTokenLimits:
		b.WriteString(subtitleStyle.Render("Endpoint: ") + m.endpoint.Value() + "\n")
		b.WriteString(subtitleStyle.Render("Model: ") + m.modelInput.Value() + "\n\n")
		b.WriteString(hintStyle.Render("Token limits sent to supported agent CLI configs.") + "\n\n")
		rows := []struct {
			label string
			view  string
		}{
			{"Context tokens", m.contextTokens.View()},
			{"Output tokens", m.outputTokens.View()},
		}
		for i, r := range rows {
			isSel := i == m.limitCursor
			marker := "  "
			if isSel {
				marker = "› "
			}
			b.WriteString(selectionRow(fmt.Sprintf("%s%-16s %s", marker, r.label+":", r.view), isSel) + "\n")
		}
		b.WriteString("\n" + descStyle.Render(
			"Defaults: context 4096, output 1024. For vLLM, output must stay below the server's max model length."))
		if m.limitErr != "" {
			b.WriteString("\n\n" + errorStyle.Render("✗ "+m.limitErr))
		}

	case ollamaStepAgents:
		b.WriteString(subtitleStyle.Render("Endpoint: ") + m.endpoint.Value() + "\n")
		b.WriteString(subtitleStyle.Render("Model: ") + m.modelInput.Value() + "\n\n")
		b.WriteString(hintStyle.Render("Which agents to configure?") + "\n\n")
		entries := []struct {
			label, hint string
			picked      bool
		}{
			{"claude     (per-chat env injection)", "ANTHROPIC_BASE_URL + --model on next launch", m.pickClaude},
			{"openclaude (per-chat env injection)", "CLAUDE_CODE_USE_OPENAI=1 + OPENAI_BASE_URL/MODEL/KEY on next launch", m.pickOpenClaude},
			{"codex      (writes ~/.codex/config.toml)", "creates [profiles.ollama_remote] — launch via -p flag", m.pickCodex},
			{"opencode   (writes ~/.config/opencode/opencode.json)", "registers ollama_remote provider, sets default model", m.pickOpenCode},
			{"deepseek   (writes ~/.deepseek/config.toml)", "provider=ollama + [providers.ollama] block + default model", m.pickDeepSeek},
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
