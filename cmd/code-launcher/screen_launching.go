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

	"github.com/sdksdk/code-launcher/internal/launcher"
)

type launchingModel struct {
	cfg  *launcher.Config
	chat launcher.Chat

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

func (m launchingModel) Init() tea.Cmd {
	chat := m.chat
	return tea.Batch(
		func() tea.Msg {
			plan, _, err := launcher.OpenChat(chat)
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
		return m, func() tea.Msg {
			return screenDoneMsg{launch: m.plan, updateCfg: &cfg, launchedWS: &wsCopy}
		}

	case tea.KeyMsg:
		if msg.String() == "esc" && m.planErr != nil {
			return m, wrap(newChatListModel(m.cfg))
		}
	}
	return m, nil
}

func (m launchingModel) View() string {
	var b strings.Builder
	title := fmt.Sprintf("Launching · %s", m.chat.Label)
	help := "ctrl-c quit"

	if m.planErr != nil {
		b.WriteString(errorStyle.Render("✗ Failed: " + m.planErr.Error()))
		return renderChrome(title, b.String(), "esc to go back")
	}

	spin := titleStyle.Render(spinnerFrames[m.frame])
	b.WriteString(spin + " " + hintStyle.Render(m.step) + "\n\n")
	b.WriteString(subtitleStyle.Render("Template: ") + m.chat.Template + "\n")
	b.WriteString(subtitleStyle.Render("Agent:    ") + string(m.chat.AgentID) + "\n")
	b.WriteString(subtitleStyle.Render("Sandbox:  ") + m.chat.SandboxDir + "\n\n")
	b.WriteString(hintStyle.Render(
		"Compiling workpath into sandbox, staging MEMORY.md, " +
			"cloning online skills, recording session metadata..."))
	return renderChrome(title, b.String(), help)
}
