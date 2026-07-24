package main

// recipesModel is the new YAML-agent launch flow added in Phase 2c.
// It is the TUI equivalent of `praimate -run-agent` from agents_cli.go.
//
// State machine:
//
//	stepPickAgent     → user picks one of the seeded YAML agents
//	stepPickWorkflow  → if the agent has multiple workflows
//	stepFillInputs    → one textinput per WorkflowInput
//	stepPrivacyReview → confirm regex privacy findings before sending
//	stepRunning       → workflow is executing; spinner-ish status
//	stepShowResult    → done (or errored); reply text on screen
//
// This pane is shown in the navigator as "Agents" — it became the
// primary "start new work" surface when templates retired in 1.1.
// (The CLI-installer browser that used to own the "Agents" label is
// now "CLIs".) Besides scripted workflow runs, `c` on the agent list
// opens an INTERACTIVE chat: the agent's instructions become the
// chat's mission via launcher.CreateChatFromInstructions and the
// normal launch/resume machinery takes over.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"git.jtsec.local/lab/PrAImate/internal/core"
	"git.jtsec.local/lab/PrAImate/internal/launcher"
)

type recipeStep int

const (
	stepPickAgent recipeStep = iota
	stepPickCLI              // choose which CLI drives an interactive chat
	stepPickWorkflow
	stepFillInputs
	stepPrivacyReview
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

	step            recipeStep
	cursor          int
	selectedAgent   *core.Agent
	selectedWf      *core.Workflow
	inputs          []textinput.Model
	inputCursor     int
	inputValues     map[string]string
	privacyMatches  []core.Match
	privacyReviewed bool

	// CLI to drive — defaults to "claude" since it's the only adapter
	// registered today. Phase 2d adds Codex and OpenCode.
	cli string

	// Interactive-chat CLI picker (the `c` flow). cliChoices is the
	// selected agent's Supports list; cliCursor indexes it.
	chatAgent  *core.Agent
	cliChoices []string
	cliCursor  int

	// Runtime state for stepRunning / stepShowResult.
	runStart  time.Time
	runResult *core.RunResult
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
		case "c":
			// Open an INTERACTIVE chat from this agent: synthesise a
			// workpath from the agent's instructions and hand off to
			// the normal launch flow (tea.ExecProcess, native resume,
			// everything). This is the template-flow replacement.
			if len(m.agents) == 0 {
				return m, nil
			}
			agent := m.agents[m.cursor]
			if len(agent.Supports) == 0 {
				m.err = fmt.Sprintf("agent %q supports no CLI", agent.ID)
				return m, nil
			}
			// One supported CLI → launch directly. Multiple → let the
			// user pick which CLI drives the chat.
			if len(agent.Supports) == 1 {
				return m.startChatWithCLI(&agent, agent.Supports[0])
			}
			a := agent
			m.chatAgent = &a
			m.cliChoices = agent.Supports
			m.cliCursor = 0
			m.step = stepPickCLI
			return m, nil
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

	case stepPickCLI:
		switch msg.String() {
		case "esc":
			m.step = stepPickAgent
			m.chatAgent = nil
		case "up", "k":
			if m.cliCursor > 0 {
				m.cliCursor--
			}
		case "down", "j":
			if m.cliCursor < len(m.cliChoices)-1 {
				m.cliCursor++
			}
		case "enter":
			return m.startChatWithCLI(m.chatAgent, m.cliChoices[m.cliCursor])
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

	case stepPrivacyReview:
		switch msg.String() {
		case "esc":
			m.step = stepFillInputs
			m.privacyMatches = nil
			m.privacyReviewed = false
		case "enter":
			m.privacyReviewed = true
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
// startChatWithCLI synthesises a workpath from the agent's instructions
// bound to the chosen CLI and hands off to the normal launch flow.
func (m recipesModel) startChatWithCLI(agent *core.Agent, cli string) (tea.Model, tea.Cmd) {
	label := agent.Name + " " + time.Now().Format("Jan-2 15:04")
	chat, err := launcher.CreateChatFromInstructions(
		m.cfg.WorkspacesRoot, label,
		launcher.AgentID(cli),
		agent.ID, firstLine(agent.Description), agent.Instructions,
	)
	if err != nil {
		m.err = err.Error()
		m.step = stepPickAgent
		return m, nil
	}
	return m, wrap(newLaunchingModel(m.cfg, chat))
}

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
	if !m.privacyReviewed {
		matches := m.previewPrivacyMatches(values)
		if len(matches) > 0 {
			m.privacyMatches = matches
			m.step = stepPrivacyReview
			return m, nil
		}
	}
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

func (m recipesModel) previewPrivacyMatches(values map[string]string) []core.Match {
	if m.selectedAgent == nil || m.selectedWf == nil {
		return nil
	}
	rendered, err := core.RenderWorkflow(m.selectedAgent, m.selectedWf, values)
	if err != nil {
		return nil
	}
	scanner := core.NewPrivacyScanner()
	if c := getAppCore(); c != nil {
		scanner = c.PrivacyScanner()
	}
	var out []core.Match
	for _, step := range rendered.Steps {
		if step.Kind != core.StepUserMessage {
			continue
		}
		out = append(out, scanner.Match(step.Body)...)
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
		return "Agents · pick an agent"
	case stepPickCLI:
		return "Agents · " + m.chatAgent.Name + " · pick a CLI"
	case stepPickWorkflow:
		return "Agents · " + m.selectedAgent.Name + " · pick a workflow"
	case stepFillInputs:
		return "Agents · " + m.selectedAgent.Name + " · " + m.selectedWf.Name
	case stepPrivacyReview:
		return "Agents · privacy review"
	case stepRunning:
		return "Agents · running…"
	case stepShowResult:
		return "Agents · result"
	}
	return "Agents"
}

func (m recipesModel) NavSection() string { return "recipes" }

func (m recipesModel) CapturingInput() bool { return m.step == stepFillInputs }

func (m recipesModel) Help() string {
	switch m.step {
	case stepPickAgent:
		return "↑↓ select · enter run workflow · c interactive chat · esc back · ctrl-c quit"
	case stepPickCLI:
		return "↑↓ select CLI · enter start chat · esc back"
	case stepPickWorkflow:
		return "↑↓ select · enter open · esc back · ctrl-c quit"
	case stepFillInputs:
		return "tab/↑↓ next field · enter submit (on last field) · esc back"
	case stepPrivacyReview:
		return "enter continue with redaction · esc edit inputs"
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
	case stepPickCLI:
		return m.bodyPickCLI()
	case stepPickWorkflow:
		return m.bodyPickWorkflow()
	case stepFillInputs:
		return m.bodyFillInputs()
	case stepPrivacyReview:
		return m.bodyPrivacyReview()
	case stepRunning:
		return m.bodyRunning()
	case stepShowResult:
		return m.bodyShowResult()
	}
	return ""
}

func (m recipesModel) bodyPickCLI() string {
	var b strings.Builder
	b.WriteString(subtitleStyle.Render("Which CLI should drive this chat?") + "\n")
	b.WriteString(descStyle.Render(m.chatAgent.Name+" supports several — pick one.") + "\n\n")
	for i, cli := range m.cliChoices {
		marker := "  "
		if i == m.cliCursor {
			marker = "▸ "
		}
		b.WriteString(marker + okStyle.Render(cli) + "\n")
	}
	return b.String()
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
	b.WriteString(subtitleStyle.Render("Workflows for "+m.selectedAgent.Name) + "\n")
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
	b.WriteString(subtitleStyle.Render("Inputs · "+m.selectedWf.Name) + "\n")
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

func (m recipesModel) bodyPrivacyReview() string {
	var b strings.Builder
	b.WriteString(subtitleStyle.Render("Privacy review") + "\n")
	b.WriteString(descStyle.Render("Sensitive-looking values will be replaced before the CLI sees the prompt.") + "\n\n")
	counts := map[core.PrivacyCategory]int{}
	for _, match := range m.privacyMatches {
		counts[match.Category]++
	}
	if len(counts) == 0 {
		b.WriteString(descStyle.Render("(no matches)") + "\n")
		return b.String()
	}
	seen := map[core.PrivacyCategory]bool{}
	for _, match := range m.privacyMatches {
		category := match.Category
		if seen[category] {
			continue
		}
		seen[category] = true
		count := counts[category]
		b.WriteString(okStyle.Render(string(category)) + descStyle.Render(fmt.Sprintf("  %d match(es)", count)) + "\n")
	}
	b.WriteString("\n" + descStyle.Render("The stored chat and result view keep your original values; only the outbound CLI prompt is scrubbed.") + "\n")
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
