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

	"github.com/sPROFFEs/PrAImate/internal/installer"
	"github.com/sPROFFEs/PrAImate/internal/launcher"
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

	// returnTo, when set, names the screen to transition back to once
	// install completes (or the user backs out). Lets the install screen
	// integrate cleanly into either the chat-list flow (back to agents)
	// or the new-chat wizard (back to the template picker — passing a
	// stub workspace into the agents picker would otherwise trigger an
	// empty-sandbox error).
	returnTo func() tea.Model

	// installNodeOptIn is the user's explicit OK to also install
	// Node.js (Windows winget). Only meaningful when the selected
	// method declares node as a prereq and node isn't already on PATH.
	// Toggled with the 'n' key.
	installNodeOptIn bool

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

// Write splits incoming bytes on BOTH \n and \r (treating either as a
// line terminator) so pnpm/uv progress bars — which use \r to overwrite
// the same row — don't accumulate into one giant 80-char-per-update
// mega-line. Empty segments are dropped. Lines are capped at 240 chars
// to keep the TUI legible when an install command emits an extremely
// long line (e.g. a deep dep tree on one line).
func (o *runningOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	// Normalise \r\n and bare \r to \n, then split.
	s := strings.ReplaceAll(string(p), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if line == "" {
			continue
		}
		if len(line) > 240 {
			line = line[:237] + "…"
		}
		o.lines = append(o.lines, line)
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

// installOutputTailLines is how many output lines the install screen
// keeps visible in the "Running..." view. We tail (not head) because
// the interesting bits — final status, errors, the actual ✓/✗ marker —
// come at the END of an install. Older lines scroll out of view.
const installOutputTailLines = 25

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

// newInstallModelWithReturn lets the caller name where to go after
// install completes — used by new-chat to avoid carrying a stub
// workspace through to the post-install screen.
func newInstallModelWithReturn(cfg *launcher.Config, agent launcher.AgentID, returnTo func() tea.Model) installModel {
	id := installer.AgentID(agent)
	methods := installer.Methods(id, installer.ActionInstall, installer.DetectOS())
	return installModel{
		cfg:      cfg,
		agentID:  agent,
		methods:  methods,
		returnTo: returnTo,
	}
}

type installDoneMsg struct{ err error }
type installTickMsg struct{}

func (m installModel) Init() tea.Cmd { return nil }

// exitTo returns the Cmd the install screen should fire when the user
// backs out or finishes. Defaults to the agents picker on the wrapping
// workspace, but the new-chat flow overrides this with a returnTo that
// lands the user back at the template picker (so we don't drag a stub
// workspace into the launch path).
func (m installModel) exitTo() tea.Cmd {
	if m.returnTo != nil {
		return wrap(m.returnTo())
	}
	return wrap(newAgentsModel(m.cfg, m.ws))
}

func (m installModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.running {
			// Block keys while a command is running — too much can go
			// wrong if we transition mid-stream. Esc after completion is
			// allowed below.
			if m.exitDone && (msg.String() == "esc" || msg.String() == "enter") {
				return m, m.exitTo()
			}
			return m, nil
		}
		switch msg.String() {
		case "esc":
			return m, m.exitTo()
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.methods)-1 {
				m.cursor++
			}
		case "n":
			// Toggle the "also install Node.js" opt-in. Only meaningful
			// when (a) the selected method needs node, (b) node isn't
			// on PATH, and (c) the OS supports auto-install (Windows +
			// winget). The keybinding stays cheap to leave on; the flag
			// won't affect anything if those conditions aren't all met
			// by the time Enter is pressed.
			m.installNodeOptIn = !m.installNodeOptIn
		case "enter":
			if m.cursor >= len(m.methods) {
				return m, nil
			}
			chosen := m.methods[m.cursor]
			missing := installer.PrereqsMissing(chosen)
			// Only block on prereqs that need real user action (e.g. Node).
			// pnpm-style ones are auto-installed by Run().
			if unfix := installer.UnfixableMissing(missing); len(unfix) > 0 {
				// Treat node as fixable when the user has explicitly
				// opted in AND the runtime supports auto-install. The
				// opt-in is gated behind a key press (`n`), so reaching
				// this branch means deliberate consent.
				blockers := make([]string, 0, len(unfix))
				for _, p := range unfix {
					if p == "node" && m.installNodeOptIn && installer.InstallNodePossible() {
						continue
					}
					blockers = append(blockers, p)
				}
				if len(blockers) > 0 {
					hint := " — install it first, or pick another method"
					for _, p := range blockers {
						if p == "node" && installer.InstallNodePossible() {
							hint = " — press `n` to install Node.js via winget, or install it manually"
							break
						}
					}
					m.prereqWarn = "missing prereq: " + strings.Join(blockers, ", ") + hint
					return m, nil
				}
			}
			m.prereqWarn = ""
			m.running = true
			m.output = &runningOutput{}
			out := m.output
			opts := installer.RunOptions{InstallNode: m.installNodeOptIn}
			return m, tea.Batch(
				func() tea.Msg {
					err := installer.RunWithOptions(context.Background(), chosen, opts, out, out)
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
	return renderChrome(m.Title(), m.Body(), m.Help())
}

func (m installModel) Title() string {
	return fmt.Sprintf("Install · %s", m.agentID)
}
func (m installModel) Help() string {
	if len(m.methods) == 0 {
		return "esc to go back"
	}
	if m.running {
		return "enter / esc to continue"
	}
	return "↑/↓ select · n toggle Node opt-in · enter run · esc back"
}
func (m installModel) NavSection() string   { return navSectionAgents }
func (m installModel) CapturingInput() bool { return false }

func (m installModel) Body() string {
	var b strings.Builder

	if len(m.methods) == 0 {
		b.WriteString(errorStyle.Render("✗ No install method available on this OS for "+string(m.agentID)) + "\n")
		b.WriteString(hintStyle.Render("Check the agent's vendor docs and install manually."))
		return b.String()
	}

	if !m.running {
		b.WriteString(hintStyle.Render("Pick a method. Recommended is marked. "+
			"You'll see the exact command before it runs.") + "\n\n")
		for i, mth := range m.methods {
			isSel := i == m.cursor
			marker := "  "
			if isSel {
				marker = "› "
			}
			label := mth.Label
			if mth.Recommended {
				label += "   " + availableStyle.Render("[recommended]")
			}
			b.WriteString(selectionRow(marker+label, isSel) + "\n")
			b.WriteString(descStyle.Render("$ "+mth.Command) + "\n")
			if isSel && len(mth.Prereqs) > 0 {
				missing := installer.PrereqsMissing(mth)
				if len(missing) == 0 {
					b.WriteString(descStyle.Render("prereqs ok: "+strings.Join(mth.Prereqs, ", ")) + "\n")
				} else {
					unfix := installer.UnfixableMissing(missing)
					fixable := installer.AutoFixable(missing)
					if len(unfix) > 0 {
						b.WriteString(descStyle.Render(errorStyle.Render("you must install: "+strings.Join(unfix, ", "))) + "\n")
					}
					if len(fixable) > 0 {
						b.WriteString(descStyle.Render(availableStyle.Render("will auto-fix: "+strings.Join(fixable, ", ")+" (corepack + pnpm setup)")) + "\n")
					}
					// Render the Node opt-in checkbox when this method
					// needs node, node is the blocker, and the runtime
					// can actually install it. Stays a no-op otherwise.
					if containsString(unfix, "node") && installer.InstallNodePossible() {
						check := "[ ]"
						if m.installNodeOptIn {
							check = availableStyle.Render("[x]")
						}
						b.WriteString(descStyle.Render(check+" Also install Node.js LTS via winget  (press `n` to toggle)") + "\n")
					}
				}
			}
		}
		if m.prereqWarn != "" {
			b.WriteString("\n" + errorStyle.Render("✗ "+m.prereqWarn))
		}
		return b.String()
	}

	// Running / done view. Show only the tail so screens don't overflow
	// when an install produces hundreds of lines (pnpm fetching ~400
	// deps, uv resolving a full graphify dep graph, etc.). The
	// interesting bits — final status + errors + the ✓/✗ marker — live
	// at the end anyway.
	b.WriteString(hintStyle.Render("Running...") + "\n\n")
	lines := m.output.snapshot()
	start := 0
	if len(lines) > installOutputTailLines {
		start = len(lines) - installOutputTailLines
		b.WriteString(descStyle.Render(
			fmt.Sprintf("  … (%d earlier lines hidden) …\n", start)))
	}
	for _, l := range lines[start:] {
		b.WriteString("  " + l + "\n")
	}
	if m.exitDone {
		b.WriteString("\n")
		if m.exitErr != nil {
			b.WriteString(errorStyle.Render("✗ Failed: " + m.exitErr.Error()))
		} else {
			b.WriteString(okStyle.Render("✓ Done — re-detecting agents..."))
		}
	}
	return b.String()
}

func containsString(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}
