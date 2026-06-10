package main

// recipesModel is the new YAML-agent launch flow added in Phase 2c.
// It is the TUI equivalent of `praimate -run-agent` from agents_cli.go.
//
// State machine:
//
//	stepPickAgent     → user picks one of the seeded YAML agents
//	stepPickWorkflow  → if the agent has multiple workflows
//	stepFillInputs    → one textinput per WorkflowInput
//	stepRunning       → workflow is executing; spinner-ish status
//	stepShowResult    → done (or errored); reply text on screen
//
// Naming: the navigator section is called "Recipes" deliberately. The
// existing "Agents" tab in the navigator points at the legacy CLI
// installer (claude/codex/opencode binaries); they collide visually.
// Phase 6 (rebrand) will rename "Agents" → "CLIs" and this pane to
// "Agents". For now the placeholder keeps the rename out of feature
// PRs.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sPROFFEs/PrAImate/internal/core"
	"github.com/sPROFFEs/PrAImate/internal/launcher"
)

type recipeStep int

const (
	stepPickAgent recipeStep = iota
	stepPickWorkflow
	stepFillInputs
	stepRunning
	stepShowResult
)

type recipesModel struct {
	cfg *launcher.Config

	// Loaded once on Init. err is set when getAppCore() returns nil or
	// agent listing fails; in either case the pane renders a single
	// error message and refuses to advance.
	loaded bool
	err    string

	agents []core.Agent

	step           recipeStep
	cursor         int
	selectedAgent  *core.Agent
	selectedWf     *core.Workflow
	inputs         []textinput.Model
	inputCursor    int
	inputValues    map[string]string

	// CLI to drive — defaults to "claude" since it's the only adapter
	// registered today. Phase 2d adds Codex and OpenCode.
	cli string

	// Runtime state for stepRunning / stepShowResult.
	runStart   time.Time
	runResult  *core.RunResult
}

func newRecipesModel(cfg *launcher.Config) recipesModel {
	return recipesModel{cfg: cfg, cli: "claude", inputValues: map[string]string{}}
}

type recipesLoadedMsg struct {
	agents []core.Agent
	err    error
}

type recipeRunDoneMsg struct {
	res *core.RunResult
}

func (m recipesModel) Init() tea.Cmd {
	return func() tea.Msg {
		c := getAppCore()
		if c == nil {
			return recipesLoadedMsg{err: fmtCoreInitErr()}
		}
		agents, err := c.ListAgents(context.Background())
		return recipesLoadedMsg{agents: agents, err: err}
	}
}

func fmtCoreInitErr() error {
	if e := getAppCoreErr(); e != nil {
		return fmt.Errorf("core unavailable: %w", e)
	}
	return fmt.Errorf("core unavailable (init not called)")
}

func (m recipesModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case recipesLoadedMsg:
		m.loaded = true
		if msg.err != nil {
			m.err = msg.err.Error()
		}
		m.agents = msg.agents
		return m, nil

	case recipeRunDoneMsg:
		m.runResult = msg.res
		m.step = stepShowResult
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Forward to the active text input in stepFillInputs so typing works.
	if m.step == stepFillInputs && m.inputCursor < len(m.inputs) {
		var cmd tea.Cmd
		m.inputs[m.inputCursor], cmd = m.inputs[m.inputCursor].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m recipesModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !m.loaded || m.err != "" {
		return m, nil
	}

	switch m.step {
	case stepPickAgent:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.agents)-1 {
				m.cursor++
			}
		case "enter":
			if len(m.agents) == 0 {
				return m, nil
			}
			agent := m.agents[m.cursor]
			m.selectedAgent = &agent
			m.cursor = 0
			if len(agent.Workflows) <= 1 {
				wf := agent.ResolveDefaultWorkflow()
				if wf == nil && len(agent.Workflows) == 1 {
					wf = &agent.Workflows[0]
				}
				if wf == nil {
					m.err = fmt.Sprintf("agent %q has no runnable workflow", agent.ID)
					return m, nil
				}
				return m.beginInputs(wf), textinput.Blink
			}
			m.step = stepPickWorkflow
		}

	case stepPickWorkflow:
		switch msg.String() {
		case "esc":
			m.step = stepPickAgent
			m.selectedAgent = nil
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.selectedAgent.Workflows)-1 {
				m.cursor++
			}
		case "enter":
			wf := m.selectedAgent.Workflows[m.cursor]
			return m.beginInputs(&wf), textinput.Blink
		}

	case stepFillInputs:
		switch msg.String() {
		case "esc":
			m.step = stepPickAgent
			m.selectedAgent = nil
			m.inputs = nil
			m.inputCursor = 0
		case "tab", "down":
			if m.inputCursor < len(m.inputs)-1 {
				m.inputs[m.inputCursor].Blur()
				m.inputCursor++
				m.inputs[m.inputCursor].Focus()
			}
		case "shift+tab", "up":
			if m.inputCursor > 0 {
				m.inputs[m.inputCursor].Blur()
				m.inputCursor--
				m.inputs[m.inputCursor].Focus()
			}
		case "enter":
			// Last input + enter = submit.
			if m.inputCursor < len(m.inputs)-1 {
				m.inputs[m.inputCursor].Blur()
				m.inputCursor++
				m.inputs[m.inputCursor].Focus()
				return m, textinput.Blink
			}
			return m.submit()
		}

	case stepRunning:
		// No interactive input while running. Esc cancels (Phase 2d).

	case stepShowResult:
		switch msg.String() {
		case "esc", "enter":
			// Reset to start.
			return newRecipesModel(m.cfg), nil
		}
	}
	return m, nil
}

// beginInputs moves the model to stepFillInputs with one textinput per
// WorkflowInput. If the workflow has zero inputs, jumps straight to
// submit (kicks the run off immediately).
func (m recipesModel) beginInputs(wf *core.Workflow) recipesModel {
	m.selectedWf = wf
	m.step = stepFillInputs
	m.inputs = make([]textinput.Model, 0, len(wf.Inputs))
	for i, in := range wf.Inputs {
		ti := textinput.New()
		ti.Prompt = "› "
		ti.Placeholder = in.Placeholder
		ti.SetValue(in.Default)
		if i == 0 {
			ti.Focus()
		}
		m.inputs = append(m.inputs, ti)
	}
	if len(m.inputs) == 0 {
		// nothing to ask — submit synchronously
		newModel, _ := m.submit()
		return newModel.(recipesModel)
	}
	return m
}

// submit collects input values, kicks off the workflow run as a tea.Cmd,
// and transitions to stepRunning. The run completion is delivered via
// recipeRunDoneMsg.
func (m recipesModel) submit() (tea.Model, tea.Cmd) {
	values := map[string]string{}
	for i, in := range m.selectedWf.Inputs {
		values[in.Name] = m.inputs[i].Value()
	}
	m.inputValues = values
	m.step = stepRunning
	m.runStart = time.Now()

	agent := *m.selectedAgent
	workflow := m.selectedWf.Name
	cli := m.cli
	cwd := m.cfg.WorkspacesRoot

	return m, func() tea.Msg {
		c := getAppCore()
		if c == nil {
			return recipeRunDoneMsg{res: &core.RunResult{
				AgentID: agent.ID, WorkflowName: workflow,
				Outcome: core.OutcomeAdapterErr, Err: fmtCoreInitErr(),
			}}
		}

		// Memory injection rides on the rendered first turn — only if
		// the user has enabled memory (default off). The first input is
		// the query the planner scores episodes against; we use the
		// concatenation of all inputs as a best-effort signal when the
		// workflow has multiple.
		ctx := context.Background()
		query := joinInputValues(values)
		injection, _ := c.BuildMemoryInjection(ctx, core.InjectionOptions{Query: query})

		res := c.RunWorkflow(ctx, core.RunOptions{
			Agent:           &agent,
			WorkflowName:    workflow,
			Inputs:          values,
			CLI:             cli,
			Cwd:             cwd,
			Persist:         true,
			ChatTitle:       agent.Name + " · " + workflow,
			MemoryInjection: injection,
		})

		// Best-effort distillation after the chat ends. Fire-and-forget
		// so the user sees their reply immediately; the episode lands
		// in the DB whenever the call returns (typically 2-10s).
		if res.ChatID != "" && res.Outcome == core.OutcomeCompleted {
			go func(chatID string) {
				_, _ = c.DistillChat(context.Background(), chatID, nil)
			}(res.ChatID)
		}
		return recipeRunDoneMsg{res: res}
	}
}

// joinInputValues concatenates input values into one space-separated
// string suitable as a retrieval-planner query. Order is map iteration
// order — fine because the planner is order-insensitive (set intersection).
func joinInputValues(inputs map[string]string) string {
	out := ""
	for _, v := range inputs {
		if out != "" {
			out += " "
		}
		out += v
	}
	return out
}

// --- Pane interface ----------------------------------------------------

func (m recipesModel) View() string {
	return renderChrome(m.Title(), m.Body(), m.Help())
}

func (m recipesModel) Title() string {
	switch m.step {
	case stepPickAgent:
		return "Recipes · pick an agent"
	case stepPickWorkflow:
		return "Recipes · " + m.selectedAgent.Name + " · pick a workflow"
	case stepFillInputs:
		return "Recipes · " + m.selectedAgent.Name + " · " + m.selectedWf.Name
	case stepRunning:
		return "Recipes · running…"
	case stepShowResult:
		return "Recipes · result"
	}
	return "Recipes"
}

func (m recipesModel) NavSection() string { return "recipes" }

func (m recipesModel) CapturingInput() bool { return m.step == stepFillInputs }

func (m recipesModel) Help() string {
	switch m.step {
	case stepPickAgent, stepPickWorkflow:
		return "↑↓ select · enter open · esc back · ctrl-c quit"
	case stepFillInputs:
		return "tab/↑↓ next field · enter submit (on last field) · esc back"
	case stepRunning:
		return "running workflow against " + m.cli + "…"
	case stepShowResult:
		return "enter/esc start over · ctrl-c quit"
	}
	return ""
}

func (m recipesModel) Body() string {
	if !m.loaded {
		return descStyle.Render("loading…")
	}
	if m.err != "" {
		return errorStyle.Render("error: " + m.err)
	}

	switch m.step {
	case stepPickAgent:
		return m.bodyPickAgent()
	case stepPickWorkflow:
		return m.bodyPickWorkflow()
	case stepFillInputs:
		return m.bodyFillInputs()
	case stepRunning:
		return m.bodyRunning()
	case stepShowResult:
		return m.bodyShowResult()
	}
	return ""
}

func (m recipesModel) bodyPickAgent() string {
	if len(m.agents) == 0 {
		return descStyle.Render("(no agents — built-ins should auto-seed; this is a bug)")
	}
	var b strings.Builder
	b.WriteString(subtitleStyle.Render("Agents") + "\n")
	for i, a := range m.agents {
		marker := "  "
		if i == m.cursor {
			marker = "▸ "
		}
		desc := firstLine(a.Description)
		b.WriteString(marker + okStyle.Render(a.Name) + "  " +
			descStyle.Render(desc) + "\n")
		b.WriteString("  " + descStyle.Render("supports: "+strings.Join(a.Supports, ", ")) + "\n\n")
	}
	return b.String()
}

func (m recipesModel) bodyPickWorkflow() string {
	var b strings.Builder
	b.WriteString(subtitleStyle.Render("Workflows for " + m.selectedAgent.Name) + "\n")
	for i, wf := range m.selectedAgent.Workflows {
		marker := "  "
		if i == m.cursor {
			marker = "▸ "
		}
		b.WriteString(marker + okStyle.Render(wf.Name) + "  " +
			descStyle.Render(wf.Description) + "\n")
	}
	return b.String()
}

func (m recipesModel) bodyFillInputs() string {
	var b strings.Builder
	b.WriteString(subtitleStyle.Render("Inputs · " + m.selectedWf.Name) + "\n")
	if m.selectedWf.Description != "" {
		b.WriteString(descStyle.Render(m.selectedWf.Description) + "\n\n")
	}
	for i, in := range m.selectedWf.Inputs {
		marker := "  "
		if i == m.inputCursor {
			marker = "▸ "
		}
		req := ""
		if in.Required {
			req = errorStyle.Render(" *")
		}
		b.WriteString(marker + okStyle.Render(in.Name) + req + "  " +
			descStyle.Render(in.Prompt) + "\n")
		b.WriteString("    " + m.inputs[i].View() + "\n\n")
	}
	if len(m.selectedWf.Inputs) == 0 {
		b.WriteString(descStyle.Render("(no inputs — press enter to run)") + "\n")
	}
	return b.String()
}

func (m recipesModel) bodyRunning() string {
	elapsed := time.Since(m.runStart).Truncate(time.Millisecond)
	return subtitleStyle.Render("Running") + "\n\n" +
		descStyle.Render(fmt.Sprintf("agent:    %s", m.selectedAgent.Name)) + "\n" +
		descStyle.Render(fmt.Sprintf("workflow: %s", m.selectedWf.Name)) + "\n" +
		descStyle.Render(fmt.Sprintf("cli:      %s", m.cli)) + "\n" +
		descStyle.Render(fmt.Sprintf("elapsed:  %s", elapsed)) + "\n"
}

func (m recipesModel) bodyShowResult() string {
	var b strings.Builder
	r := m.runResult
	b.WriteString(subtitleStyle.Render("Result · "+string(r.Outcome)) + "\n\n")
	if r.Err != nil {
		b.WriteString(errorStyle.Render("error: "+r.Err.Error()) + "\n\n")
	}
	for _, t := range r.Turns {
		b.WriteString(descStyle.Render(fmt.Sprintf("--- turn %d (%dms) ---", t.Index+1, t.DurationMs)) + "\n")
		b.WriteString(t.Reply.Text + "\n\n")
	}
	if len(r.Turns) == 0 && r.Err == nil {
		b.WriteString(descStyle.Render("(no turns recorded)") + "\n")
	}
	return b.String()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
