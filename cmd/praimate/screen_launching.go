package main

// Transient "Launching..." screen shown between the user pressing Enter
// on a chat and the agent taking over the terminal. Without it the
// chat-list view stays on screen for the 0.5–2s it takes to compile
// the workpath into the sandbox, stage MEMORY.md, and clone any online
// skills — looks frozen and unresponsive.
//
// Drives a small ASCII spinner via tea.Tick so the user gets continuous
// feedback while the work is in progress.

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sPROFFEs/PrAImate/internal/launcher"
)

type launchingModel struct {
	cfg  *launcher.Config
	chat launcher.Chat

	// fresh, when true, tells OpenChat to bypass RestoreNativeSession
	// for this launch. The chat's captured sessions/ dir is left as-is
	// so a subsequent normal launch can still resume. Set by the chat
	// list's `F` key (vs. plain Enter, which auto-resumes).
	fresh bool

	step    string // human-readable phase label
	frame   int    // spinner frame
	planErr error
	plan    *launcher.LaunchPlan
}

type launchPlannedMsg struct {
	plan *launcher.LaunchPlan
	err  error
}

type launchTickMsg struct{}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func newLaunchingModel(cfg *launcher.Config, chat launcher.Chat) launchingModel {
	return launchingModel{cfg: cfg, chat: chat, step: "Resolving agent..."}
}

// newLaunchingModelFresh is the "skip native resume" variant — bound to
// the chat list's `F` key. Used when the user wants to start over on a
// chat that already has a captured session, without manually deleting
// the sessions/ dir.
func newLaunchingModelFresh(cfg *launcher.Config, chat launcher.Chat) launchingModel {
	m := newLaunchingModel(cfg, chat)
	m.fresh = true
	m.step = "Resolving agent (fresh launch — skipping resume)..."
	return m
}

func (m launchingModel) Init() tea.Cmd {
	chat := m.chat
	opts := launcher.OpenChatOptions{SkipResume: m.fresh}
	return tea.Batch(
		func() tea.Msg {
			plan, _, err := launcher.OpenChatWithOptions(chat, opts)
			if err != nil {
				return launchPlannedMsg{err: err}
			}
			return launchPlannedMsg{plan: &plan}
		},
		tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return launchTickMsg{} }),
	)
}

func (m launchingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case launchTickMsg:
		m.frame = (m.frame + 1) % len(spinnerFrames)
		if m.plan != nil || m.planErr != nil {
			return m, nil
		}
		return m, tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return launchTickMsg{} })

	case launchPlannedMsg:
		if msg.err != nil {
			m.planErr = msg.err
			if errors.Is(msg.err, launcher.ErrAgentUnavailable) {
				// Agent missing — route to installer so the user can
				// resolve without dropping back to the home screen.
				cfg := m.cfg
				agentID := m.chat.AgentID
				return m, wrap(newInstallModelWithReturn(cfg, agentID, func() tea.Model {
					return newChatListModel(cfg)
				}))
			}
			return m, nil
		}
		m.plan = msg.plan
		cfg := *m.cfg
		cfg.LastAgent = string(m.chat.AgentID)
		_ = launcher.TouchChat(&m.chat)
		wsCopy := m.chat.AsWorkspace()
		agentID := m.chat.AgentID
		return m, func() tea.Msg {
			return screenDoneMsg{
				launch:        m.plan,
				updateCfg:     &cfg,
				launchedWS:    &wsCopy,
				launchedAgent: agentID,
			}
		}

	case tea.KeyMsg:
		if msg.String() == "esc" && m.planErr != nil {
			return m, wrap(newChatListModel(m.cfg))
		}
	}
	return m, nil
}

func (m launchingModel) View() string {
	return renderChrome(m.Title(), m.Body(), m.Help())
}

func (m launchingModel) Title() string {
	return fmt.Sprintf("Launching · %s", m.chat.Label)
}
func (m launchingModel) Help() string {
	if m.planErr != nil {
		return "esc to go back"
	}
	return "ctrl-c quit"
}
func (m launchingModel) NavSection() string    { return navSectionChats }
func (m launchingModel) CapturingInput() bool  { return false }

func (m launchingModel) Body() string {
	var b strings.Builder

	if m.planErr != nil {
		b.WriteString(errorStyle.Render("✗ Failed: " + m.planErr.Error()))
		return b.String()
	}

	spin := titleStyle.Render(spinnerFrames[m.frame])
	b.WriteString(spin + " " + hintStyle.Render(m.step) + "\n\n")
	b.WriteString(subtitleStyle.Render("Template: ") + m.chat.Template + "\n")
	b.WriteString(subtitleStyle.Render("Agent:    ") + string(m.chat.AgentID) + "\n")
	b.WriteString(subtitleStyle.Render("Sandbox:  ") + m.chat.SandboxDir + "\n")
	mode := "auto-resume (use F on the chat list for a fresh launch)"
	if m.fresh {
		mode = errorStyle.Render("fresh launch — captured session NOT restored")
	}
	b.WriteString(subtitleStyle.Render("Mode:     ") + mode + "\n")

	// Token estimate for the workpath injection. Rough — bytes/4 —
	// but gives the user a "this chat warms up at ~N tokens" number
	// before they pay the API cost. Skipped pre-launch if the
	// sandbox hasn't been compiled yet (estimates would all read 0).
	if agent, ok := launcher.ResolveAgentForChat(m.chat.AgentID, 2*time.Second); ok {
		est := launcher.EstimateInjection(m.chat, agent)
		if est.Total > 0 || est.KnowledgeBytes > 0 {
			b.WriteString(subtitleStyle.Render("Tokens:   ") +
				renderTokenEstimate(est) + "\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(hintStyle.Render(
		"Compiling workpath into sandbox, staging MEMORY.md, " +
			"cloning online skills, recording session metadata..."))
	return b.String()
}

// renderTokenEstimate formats a token estimate as a one-liner with a
// dim breakdown.
func renderTokenEstimate(est launcher.InjectionEstimate) string {
	parts := []string{}
	if est.RootMarkdownBytes > 0 {
		parts = append(parts, fmt.Sprintf("root %d", est.RootMarkdownBytes/4))
	}
	if est.MemoryBytes > 0 {
		parts = append(parts, fmt.Sprintf("memory %d", int(est.MemoryBytes)/4))
	}
	if est.PrimerBytes > 0 {
		parts = append(parts, fmt.Sprintf("primer %d", est.PrimerBytes/4))
	}
	breakdown := ""
	if len(parts) > 0 {
		breakdown = "  " + descStyle.Render("("+strings.Join(parts, " · ")+" tokens)")
	}
	main := fmt.Sprintf("~%d tokens", est.Total)
	if est.KnowledgeBytes > 0 {
		main += fmt.Sprintf("  +%d KB knowledge (on demand)",
			est.KnowledgeBytes/1024)
	}
	return main + breakdown
}
