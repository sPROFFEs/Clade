package main

// Local LLM tab — sets the GLOBAL default connection for a self-hosted /
// local OpenAI-compatible endpoint (Ollama, GPUStack, vLLM, LiteLLM, …).
//
// This stores reusable defaults: endpoint URL + API key + wire API +
// model token caps. It exists purely so the user doesn't retype the
// same local backend defaults on every new chat — when a default is
// saved, the new-chat Ollama wizard offers "use the saved endpoint" as
// a shortcut. Model + per-agent selection stay per-chat (queried/picked
// live in the wizard), so each chat can still diverge.
//
// Layout mirrors the Backup tab: a list of rows, arrow to highlight, the
// two text fields edit inline, space cycles the wire-API toggle, and the
// action rows (Test / Save / Clear) fire on Enter.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"git.jtsec.local/lab/PrAImate/internal/launcher"
	"git.jtsec.local/lab/PrAImate/internal/ollama"
)

// localLLMRow indexes the row list. Keep in sync with renderList.
type localLLMRow int

const (
	localRowEndpoint localLLMRow = iota
	localRowAPIKey
	localRowWireAPI
	localRowContextTokens
	localRowOutputTokens
	localRowTest
	localRowSave
	localRowClear
	localRowCount
)

// Token-cap inputs intentionally default to BLANK (= unset = "let the
// CLI use its own default; do not write a cap into the agent's config").
// Earlier versions baked in 4096/1024 which silently truncated long-
// context models (qwen 32k, llama 128k, …) when the user just wanted
// "use whatever the model supports". Audit notes per agent:
//
//   - codex: writes `model_context_window` only when >0 (current
//     0.40+ codex no longer exposes `model_max_output_tokens`).
//   - opencode: writes `limit.context` + `limit.output` per-model
//     only when >0.
//   - openclaude: emits the `CLAUDE_CODE_OPENAI_CONTEXT_WINDOWS` /
//     `CLAUDE_CODE_OPENAI_MAX_OUTPUT_TOKENS` env vars only when >0.
//   - claude (Anthropic Claude Code): no
//     documented config surface for caps today — caps entered here
//     would be silently ignored even when the user fills them in.
//     The Local-LLM tab footer notes that.
//
// Keeping the constants at 0 lets every "fallback" caller through
// localContextDefault / localOutputDefault produce 0, which downstream
// writers treat as "skip this field". Removing them entirely would
// ripple too much; setting them to zero keeps the code shape stable.
const (
	defaultLocalContextTokens = 0
	defaultLocalOutputTokens  = 0
)

// wireAPIChoices is the cycle order for the wire-API toggle. "" = auto
// (let each agent decide); "responses" is what codex 0.130+ needs;
// "chat" is the legacy chat-completions shape.
var wireAPIChoices = []string{"", "responses", "chat"}

func wireAPILabel(v string) string {
	switch v {
	case "":
		return "auto (unset)"
	case "responses":
		return "responses (codex 0.130+)"
	case "chat":
		return "chat (legacy chat-completions)"
	}
	return v
}

type localLLMModel struct {
	cfg *launcher.Config

	cursor int

	endpoint      textinput.Model
	apiKey        textinput.Model
	wireAPI       string
	contextTokens textinput.Model
	outputTokens  textinput.Model

	// editing is true while a text field has focus and is capturing
	// keystrokes; toggled with enter/esc on those rows.
	editing bool

	// transient status line under the rows (test result / save ack).
	status string
	// busy is set while the connection test is in flight.
	busy bool
}

func newLocalLLMModel(cfg *launcher.Config) *localLLMModel {
	ep := textinput.New()
	ep.Placeholder = "http://192.168.1.50:11434"
	ep.CharLimit = 200
	ep.Width = 56
	ep.SetValue(cfg.DefaultLocalEndpoint)

	ak := textinput.New()
	ak.Placeholder = "Bearer token (blank = no auth / Ollama)"
	ak.CharLimit = 400
	ak.Width = 56
	ak.EchoMode = textinput.EchoPassword
	ak.EchoCharacter = '•'
	ak.SetValue(cfg.DefaultLocalAPIKey)

	ct := textinput.New()
	ct.Placeholder = "blank = let CLI decide"
	ct.CharLimit = 8
	ct.Width = 24
	if v := localContextDefault(cfg); v > 0 {
		ct.SetValue(strconv.Itoa(v))
	}

	ot := textinput.New()
	ot.Placeholder = "blank = let CLI decide"
	ot.CharLimit = 8
	ot.Width = 24
	if v := localOutputDefault(cfg); v > 0 {
		ot.SetValue(strconv.Itoa(v))
	}

	return &localLLMModel{
		cfg:           cfg,
		endpoint:      ep,
		apiKey:        ak,
		wireAPI:       cfg.DefaultLocalWireAPI,
		contextTokens: ct,
		outputTokens:  ot,
	}
}

// --- Pane interface ---------------------------------------------------------

func (m *localLLMModel) Init() tea.Cmd { return nil }

func (m *localLLMModel) Title() string        { return "Local LLM · default endpoint" }
func (m *localLLMModel) NavSection() string   { return navSectionLocalLLM }
func (m *localLLMModel) CapturingInput() bool { return m.editing }
func (m *localLLMModel) Help() string {
	if m.editing {
		return "enter save field · esc cancel edit"
	}
	return "↑/↓ select · enter edit/run · space cycle wire-api · esc back"
}

// --- messages ---------------------------------------------------------------

type localLLMTestMsg struct {
	models []string
	err    error
}

// --- update -----------------------------------------------------------------

func (m *localLLMModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case localLLMTestMsg:
		m.busy = false
		if msg.err != nil {
			m.status = errorStyle.Render("✗ connection failed: " + trimErrLine(msg.err.Error()))
			return m, nil
		}
		n := len(msg.models)
		preview := ""
		if n > 0 {
			show := msg.models
			if len(show) > 4 {
				show = show[:4]
			}
			preview = " — " + strings.Join(show, ", ")
			if n > 4 {
				preview += fmt.Sprintf(", +%d more", n-4)
			}
		}
		m.status = availableStyle.Render(fmt.Sprintf("✓ reachable · %d model(s)%s", n, preview))
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Forward to whichever field is being edited so it animates the cursor.
	if m.editing {
		var cmd tea.Cmd
		switch m.cursor {
		case int(localRowEndpoint):
			m.endpoint, cmd = m.endpoint.Update(msg)
		case int(localRowAPIKey):
			m.apiKey, cmd = m.apiKey.Update(msg)
		case int(localRowContextTokens):
			m.contextTokens, cmd = m.contextTokens.Update(msg)
		case int(localRowOutputTokens):
			m.outputTokens, cmd = m.outputTokens.Update(msg)
		}
		return m, cmd
	}
	return m, nil
}

func (m *localLLMModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Editing mode: route keystrokes into the focused text field.
	if m.editing {
		switch msg.String() {
		case "enter", "esc":
			m.editing = false
			m.endpoint.Blur()
			m.apiKey.Blur()
			m.contextTokens.Blur()
			m.outputTokens.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		switch m.cursor {
		case int(localRowEndpoint):
			m.endpoint, cmd = m.endpoint.Update(msg)
		case int(localRowAPIKey):
			m.apiKey, cmd = m.apiKey.Update(msg)
		case int(localRowContextTokens):
			m.contextTokens, cmd = m.contextTokens.Update(msg)
		case int(localRowOutputTokens):
			m.outputTokens, cmd = m.outputTokens.Update(msg)
		}
		return m, cmd
	}

	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		m.status = ""
	case "down", "j":
		if m.cursor < int(localRowCount)-1 {
			m.cursor++
		}
		m.status = ""
	case " ":
		if localLLMRow(m.cursor) == localRowWireAPI {
			m.wireAPI = nextWireAPI(m.wireAPI)
		}
	case "enter":
		return m.activateRow()
	}
	return m, nil
}

func (m *localLLMModel) activateRow() (tea.Model, tea.Cmd) {
	switch localLLMRow(m.cursor) {
	case localRowEndpoint:
		m.editing = true
		m.endpoint.Focus()
		return m, textinput.Blink
	case localRowAPIKey:
		m.editing = true
		m.apiKey.Focus()
		return m, textinput.Blink
	case localRowContextTokens:
		m.editing = true
		m.contextTokens.Focus()
		return m, textinput.Blink
	case localRowOutputTokens:
		m.editing = true
		m.outputTokens.Focus()
		return m, textinput.Blink
	case localRowWireAPI:
		m.wireAPI = nextWireAPI(m.wireAPI)
	case localRowTest:
		return m, m.startTest()
	case localRowSave:
		m.save()
	case localRowClear:
		m.clear()
	}
	return m, nil
}

func nextWireAPI(cur string) string {
	for i, v := range wireAPIChoices {
		if v == cur {
			return wireAPIChoices[(i+1)%len(wireAPIChoices)]
		}
	}
	return wireAPIChoices[0]
}

func (m *localLLMModel) startTest() tea.Cmd {
	ep := strings.TrimSpace(m.endpoint.Value())
	if ep == "" {
		m.status = errorStyle.Render("✗ enter an endpoint first")
		return nil
	}
	key := strings.TrimSpace(m.apiKey.Value())
	m.busy = true
	m.status = "testing " + ep + " ..."
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		models, err := ollama.ListModels(ctx, ollama.NormalizeEndpoint(ep), key)
		return localLLMTestMsg{models: models, err: err}
	}
}

func (m *localLLMModel) save() {
	contextTokens, outputTokens, err := parseTokenLimits(m.contextTokens.Value(), m.outputTokens.Value())
	if err != nil {
		m.status = errorStyle.Render("✗ " + err.Error())
		return
	}
	m.cfg.DefaultLocalEndpoint = strings.TrimSpace(m.endpoint.Value())
	m.cfg.DefaultLocalAPIKey = strings.TrimSpace(m.apiKey.Value())
	m.cfg.DefaultLocalWireAPI = m.wireAPI
	m.cfg.DefaultLocalContextTokens = contextTokens
	m.cfg.DefaultLocalOutputTokens = outputTokens
	if err := launcher.SaveConfig(m.cfg); err != nil {
		m.status = errorStyle.Render("✗ save failed: " + trimErrLine(err.Error()))
		return
	}
	if m.cfg.DefaultLocalEndpoint == "" {
		m.status = availableStyle.Render("✓ saved (no endpoint set — new chats start blank)")
	} else {
		m.status = availableStyle.Render("✓ saved · new chats will offer " + m.cfg.DefaultLocalEndpoint)
	}
}

func (m *localLLMModel) clear() {
	m.endpoint.SetValue("")
	m.apiKey.SetValue("")
	m.wireAPI = ""
	// Blank token fields → 0 in config → writers skip the field → CLI
	// uses its own default. Don't bake 4096/1024 back in.
	m.contextTokens.SetValue("")
	m.outputTokens.SetValue("")
	m.cfg.DefaultLocalEndpoint = ""
	m.cfg.DefaultLocalAPIKey = ""
	m.cfg.DefaultLocalWireAPI = ""
	m.cfg.DefaultLocalContextTokens = 0
	m.cfg.DefaultLocalOutputTokens = 0
	if err := launcher.SaveConfig(m.cfg); err != nil {
		m.status = errorStyle.Render("✗ clear failed: " + trimErrLine(err.Error()))
		return
	}
	m.status = availableStyle.Render("✓ cleared — new chats start with a blank endpoint and no token caps")
}

// --- view -------------------------------------------------------------------

func (m *localLLMModel) View() string { return renderChrome(m.Title(), m.Body(), m.Help()) }

func (m *localLLMModel) Body() string {
	var b strings.Builder
	b.WriteString(hintStyle.Render(
		"Default connection for a self-hosted / local OpenAI-compatible endpoint.") + "\n")
	b.WriteString(hintStyle.Render(
		"Saved here once, offered on every new chat so you don't retype it.") + "\n\n")

	rows := []struct {
		label, value string
	}{
		{"Endpoint", endpointDisplay(m.endpoint.View(), m.endpoint.Value(), m.editing && m.cursor == int(localRowEndpoint))},
		{"API key", apiKeyDisplay(m.apiKey, m.editing && m.cursor == int(localRowAPIKey))},
		{"Wire API", wireAPILabel(m.wireAPI)},
		{"Context tokens", tokenDisplay(m.contextTokens.View(), m.contextTokens.Value(), m.editing && m.cursor == int(localRowContextTokens))},
		{"Output tokens", tokenDisplay(m.outputTokens.View(), m.outputTokens.Value(), m.editing && m.cursor == int(localRowOutputTokens))},
		{"Test connection", hintStyle.Render("query the endpoint for its model list")},
		{"Save as default", hintStyle.Render("persist to Clade config")},
		{"Clear default", hintStyle.Render("forget the saved endpoint")},
	}
	for i, r := range rows {
		sel := i == m.cursor
		marker := "  "
		if sel {
			marker = "› "
		}
		line := fmt.Sprintf("%s%-18s %s", marker, r.label+":", r.value)
		b.WriteString(selectionRow(line, sel) + "\n")
	}

	// Token-cap support per agent, surfaced inline so users don't fill
	// in numbers expecting them to take effect on agents that ignore
	// them. Kept terse — full audit lives in screen_locallm.go's
	// defaultLocalContextTokens comment.
	b.WriteString("\n" + descStyle.Render(
		"Token caps are honoured by: codex (model_context_window only), "+
			"opencode (limit.context + limit.output), openclaude "+
			"(CLAUDE_CODE_OPENAI_CONTEXT_WINDOWS + ..._MAX_OUTPUT_TOKENS env). "+
			"When routed locally, openclaude gets a native "+
			"~/.openclaude/.openclaude-profile.json openai profile. "+
			"Normal OpenClaude launches rename that profile to .bak so cloud routing is restored. "+
			"Claude, deepseek-tui, and gemini have no documented cap "+
			"surface — values entered here are silently ignored for those. "+
			"Blank fields = let the CLI use its own default (no cap written)."))

	if m.busy {
		b.WriteString("\n" + hintStyle.Render(m.status))
	} else if m.status != "" {
		b.WriteString("\n" + m.status)
	}
	return b.String()
}

// endpointDisplay returns the endpoint cell: the live textinput view when
// editing, otherwise the stored value (or a placeholder hint).
func endpointDisplay(view, value string, editing bool) string {
	if editing {
		return view
	}
	if strings.TrimSpace(value) == "" {
		return hintStyle.Render("(not set)")
	}
	return value
}

// apiKeyDisplay masks the key when not editing so it doesn't leak on
// screen; shows the live (still-masked) input while editing.
func apiKeyDisplay(ti textinput.Model, editing bool) string {
	if editing {
		return ti.View()
	}
	if strings.TrimSpace(ti.Value()) == "" {
		return hintStyle.Render("(none)")
	}
	return "••••••••"
}

func tokenDisplay(view, value string, editing bool) string {
	if editing {
		return view
	}
	if strings.TrimSpace(value) == "" {
		return hintStyle.Render("(default)")
	}
	return value
}

// localContextDefault returns the user's saved per-account default
// context-tokens cap, or 0 when none is set. The 0 case is treated as
// "no cap; let the CLI decide" by every consumer; we deliberately
// don't fall back to a hard-coded magic number anymore — that was the
// 4096/1024 footgun.
func localContextDefault(cfg *launcher.Config) int {
	if cfg != nil && cfg.DefaultLocalContextTokens > 0 {
		return cfg.DefaultLocalContextTokens
	}
	return 0
}

func localOutputDefault(cfg *launcher.Config) int {
	if cfg != nil && cfg.DefaultLocalOutputTokens > 0 {
		return cfg.DefaultLocalOutputTokens
	}
	return 0
}

// parseTokenLimits returns (context, output) parsed from the two user
// inputs. Blank = 0 = "no cap; let the CLI use its default". The
// previous "blank falls back to 4096/1024" behaviour silently truncated
// long-context models for users who didn't realise the inputs were
// pre-filled; the new shape makes "no cap" the explicit default.
//
// The output ≤ context guard is only checked when BOTH are explicitly
// set — if either is 0 (unset), the constraint doesn't apply because
// "unset" means "the CLI's own default is in force" for that side.
func parseTokenLimits(contextRaw, outputRaw string) (int, int, error) {
	contextTokens, err := parseTokenLimit(contextRaw, "context tokens")
	if err != nil {
		return 0, 0, err
	}
	outputTokens, err := parseTokenLimit(outputRaw, "output tokens")
	if err != nil {
		return 0, 0, err
	}
	if contextTokens > 0 && outputTokens > 0 && outputTokens > contextTokens {
		return 0, 0, fmt.Errorf("output tokens (%d) cannot exceed context tokens (%d)", outputTokens, contextTokens)
	}
	return contextTokens, outputTokens, nil
}

// parseTokenLimit returns 0 for blank input (meaning "no cap; CLI
// default") and the parsed positive integer otherwise. A non-blank
// non-positive-integer is rejected so users see a clear error instead
// of silently getting "unset" semantics from typos like "abc".
func parseTokenLimit(raw string, label string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer (or blank for the CLI's default)", label)
	}
	return n, nil
}

// trimErrLine keeps a single-line, bounded error for the status row.
func trimErrLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 100 {
		s = s[:97] + "..."
	}
	return s
}
