package main

// installModel offers candidate install methods for one agent, runs the
// chosen one with live output, then re-detects agents on the way back to
// the agent picker.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sdksdk/code-launcher/internal/installer"
	"github.com/sdksdk/code-launcher/internal/launcher"
)

// tickDuration paces the polling tick that refreshes streaming-output
// views (install / ollama). Short enough to feel live, long enough to
// keep CPU idle.
const tickDuration = 200 * time.Millisecond

type installModel struct {
	cfg     *launcher.Config
	ws      launcher.Workspace
	agentID launcher.AgentID
	methods []installer.Method
	cursor  int

	// running state
	running    bool
	output     *runningOutput
	exitErr    error
	exitDone   bool
	prereqWarn string
}

// runningOutput is a thread-safe sink Bubble Tea polls via tick messages.
// We accumulate lines from the install command's stdout/stderr there.
type runningOutput struct {
	mu    sync.Mutex
	lines []string
}

func (o *runningOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		if line != "" {
			o.lines = append(o.lines, line)
		}
	}
	return len(p), nil
}

func (o *runningOutput) snapshot() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]string, len(o.lines))
	copy(out, o.lines)
	return out
}

func newInstallModel(cfg *launcher.Config, ws launcher.Workspace, agent launcher.AgentID) installModel {
	id := installer.AgentID(agent)
	methods := installer.Methods(id, installer.ActionInstall, installer.DetectOS())
	return installModel{
		cfg:     cfg,
		ws:      ws,
		agentID: agent,
		methods: methods,
	}
}

type installDoneMsg struct{ err error }
type installTickMsg struct{}

func (m installModel) Init() tea.Cmd { return nil }

func (m installModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.running {
			// Block keys while a command is running — too much can go
			// wrong if we transition mid-stream. Esc after completion is
			// allowed below.
			if m.exitDone && (msg.String() == "esc" || msg.String() == "enter") {
				return m, wrap(newAgentsModel(m.cfg, m.ws))
			}
			return m, nil
		}
		switch msg.String() {
		case "esc":
			return m, wrap(newAgentsModel(m.cfg, m.ws))
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.methods)-1 {
				m.cursor++
			}
		case "enter":
			if m.cursor >= len(m.methods) {
				return m, nil
			}
			chosen := m.methods[m.cursor]
			missing := installer.PrereqsMissing(chosen)
			if len(missing) > 0 {
				m.prereqWarn = "missing prereq: " + strings.Join(missing, ", ") +
					" — install them first, or pick another method"
				return m, nil
			}
			m.prereqWarn = ""
			m.running = true
			m.output = &runningOutput{}
			out := m.output
			return m, tea.Batch(
				func() tea.Msg {
					err := installer.Run(context.Background(), chosen, out, out)
					return installDoneMsg{err: err}
				},
				tickInstall(),
			)
		}
	case installDoneMsg:
		m.exitErr = msg.err
		m.exitDone = true
		return m, nil
	case installTickMsg:
		if !m.running || m.exitDone {
			return m, nil
		}
		return m, tickInstall()
	}
	return m, nil
}

func tickInstall() tea.Cmd {
	return tea.Tick(tickDuration, func(time.Time) tea.Msg { return installTickMsg{} })
}

func (m installModel) View() string {
	var b strings.Builder
	b.WriteString(header(fmt.Sprintf("Install %s", m.agentID)))
	b.WriteString("\n")

	if len(m.methods) == 0 {
		b.WriteString(errorStyle.Render("No install method available on this OS for "+string(m.agentID)) + "\n")
		b.WriteString(hintStyle.Render("Open https://github.com/anthropics/claude-code (or vendor docs) and install manually.") + "\n")
		b.WriteString(helpStyle.Render("esc to go back"))
		return b.String()
	}

	if !m.running {
		b.WriteString(hintStyle.Render("Pick a method. Recommended is marked. " +
			"You'll see the exact command before it runs.") + "\n\n")
		for i, mth := range m.methods {
			marker := "  "
			render := listItemStyle.Render
			if i == m.cursor {
				marker = "› "
				render = listItemSelectedStyle.Render
			}
			label := mth.Label
			if mth.Recommended {
				label += " " + availableStyle.Render("[recommended]")
			}
			b.WriteString(render(marker + label))
			b.WriteString("\n")
			b.WriteString(descStyle.Render("$ " + mth.Command))
			b.WriteString("\n")
			if i == m.cursor && len(mth.Prereqs) > 0 {
				missing := installer.PrereqsMissing(mth)
				if len(missing) > 0 {
					b.WriteString(descStyle.Render(
						errorStyle.Render("missing prereqs: "+strings.Join(missing, ", "))) + "\n")
				} else {
					b.WriteString(descStyle.Render("prereqs ok: "+strings.Join(mth.Prereqs, ", ")) + "\n")
				}
			}
		}
		if m.prereqWarn != "" {
			b.WriteString("\n" + errorStyle.Render(m.prereqWarn) + "\n")
		}
		b.WriteString(helpStyle.Render("↑/↓ select · enter run · esc back"))
		return b.String()
	}

	// Running / done view.
	b.WriteString(hintStyle.Render("Running...") + "\n\n")
	lines := m.output.snapshot()
	for _, l := range lines {
		b.WriteString("  " + l + "\n")
	}
	if m.exitDone {
		b.WriteString("\n")
		if m.exitErr != nil {
			b.WriteString(errorStyle.Render("Failed: "+m.exitErr.Error()) + "\n")
		} else {
			b.WriteString(okStyle.Render("Done — re-detecting agents...") + "\n")
		}
		b.WriteString(helpStyle.Render("enter / esc to continue"))
	}
	return b.String()
}
