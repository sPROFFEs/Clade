package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sdksdk/code-launcher/internal/launcher"
)

// Each screen is its own model type. The root model delegates Update/View
// to the active screen and consumes "done" messages to transition.
//
// We hand-roll the list/selection logic instead of using bubbles/list to
// keep the visual output tight and stay in full control of styling.

// --- shared messages ------------------------------------------------------

type (
	screenDoneMsg struct {
		next       tea.Model
		launch     *launcher.LaunchPlan // when set, root quits and execs after Run
		updateCfg  *launcher.Config     // when set, root persists it before transitioning
		launchedWS *launcher.Workspace  // for post-launch hooks (memory sync-back)
	}
	errMsg struct{ err error }
)

func wrap(m tea.Model) tea.Cmd        { return func() tea.Msg { return screenDoneMsg{next: m} } }
func wrapErr(err error) tea.Cmd       { return func() tea.Msg { return errMsg{err: err} } }
func wrapLaunch(p launcher.LaunchPlan, c *launcher.Config) tea.Cmd {
	return func() tea.Msg { return screenDoneMsg{launch: &p, updateCfg: c} }
}

// --- first-run screen ----------------------------------------------------

type firstRunModel struct {
	input  textinput.Model
	err    string
	status string
}

func newFirstRun() firstRunModel {
	ti := textinput.New()
	ti.Placeholder = defaultWorkspacesRoot()
	ti.SetValue(defaultWorkspacesRoot())
	ti.Focus()
	ti.CharLimit = 4096
	ti.Width = 60
	return firstRunModel{input: ti}
}

func defaultWorkspacesRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "code-launcher-workspaces"
	}
	return filepath.Join(home, "code-launcher-workspaces")
}

func (m firstRunModel) Init() tea.Cmd { return textinput.Blink }

func (m firstRunModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			root, err := filepath.Abs(strings.TrimSpace(m.input.Value()))
			if err != nil {
				m.err = err.Error()
				return m, nil
			}
			m.status = "Seeding samples..."
			return m, func() tea.Msg {
				execDir, _ := os.Executable()
				if execDir != "" {
					execDir = filepath.Dir(execDir)
				}
				candidates := launcher.SampleCandidates(execDir)
				// Also try cwd-relative — useful in `go run .` from the repo root.
				if cwd, err := os.Getwd(); err == nil {
					candidates = append(candidates, filepath.Join(cwd, "samples", "workpaths"))
				}
				_, err := launcher.SeedSamples(root, candidates)
				if err != nil {
					return errMsg{err: fmt.Errorf("seed samples: %w", err)}
				}
				cfg := &launcher.Config{WorkspacesRoot: root}
				if err := launcher.SaveConfig(cfg); err != nil {
					return errMsg{err: fmt.Errorf("save config: %w", err)}
				}
				return screenDoneMsg{next: newChatListModel(cfg)}
			}
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m firstRunModel) View() string {
	var b strings.Builder
	b.WriteString(header("First run — pick a home for your workspaces"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render(
		"Each workspace bundles a workpath (knowledge base) and a sandbox (agent cwd).",
	))
	b.WriteString("\n")
	dir, file, _ := launcher.ConfigPaths()
	b.WriteString(versionStyle.Render(
		fmt.Sprintf("Config will live at %s (in %s)", filepath.Base(file), dir),
	))
	b.WriteString("\n\n")
	b.WriteString(inputLabelStyle.Render("Workspaces root: "))
	b.WriteString(m.input.View())
	b.WriteString("\n")
	if m.status != "" {
		b.WriteString("\n" + hintStyle.Render(m.status))
	}
	if m.err != "" {
		b.WriteString("\n" + errorStyle.Render("Error: "+m.err))
	}
	b.WriteString(helpStyle.Render("enter to continue · ctrl-c to abort"))
	return b.String()
}

// --- agents screen -------------------------------------------------------

type agentsModel struct {
	cfg     *launcher.Config
	ws      launcher.Workspace
	items   []launcher.Agent
	cursor  int
	loading bool
}

func newAgentsModel(cfg *launcher.Config, ws launcher.Workspace) agentsModel {
	return agentsModel{cfg: cfg, ws: ws, loading: true}
}

type agentsLoadedMsg struct{ items []launcher.Agent }

func (m agentsModel) Init() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		return agentsLoadedMsg{items: launcher.DetectAgents(ctx)}
	}
}

func (m agentsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case agentsLoadedMsg:
		m.items = msg.items
		m.loading = false
		// Seed cursor on the user's last-used agent if it's available.
		if m.cfg.LastAgent != "" {
			for i, a := range m.items {
				if string(a.ID) == m.cfg.LastAgent && a.Available {
					m.cursor = i
					break
				}
			}
		}
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, wrap(newChatListModel(m.cfg))
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "enter":
			if m.cursor >= len(m.items) {
				return m, nil
			}
			a := m.items[m.cursor]
			if !a.Available {
				// Greyed-out: route to install screen on Enter too,
				// since that's the natural action the user wants.
				return m, wrap(newInstallModel(m.cfg, m.ws, a.ID))
			}
			ws := m.ws
			cfg := *m.cfg
			cfg.LastAgent = string(a.ID)
			return m, func() tea.Msg {
				plan, err := launcher.Plan(ws, a)
				if err != nil {
					return errMsg{err: err}
				}
				wsCopy := ws
				return screenDoneMsg{launch: &plan, updateCfg: &cfg, launchedWS: &wsCopy}
			}
		case "i":
			// 'i' explicitly opens the install screen for the highlighted
			// agent (whether or not it's already installed — useful for
			// the 'install' = upgrade path).
			if m.cursor >= len(m.items) {
				return m, nil
			}
			return m, wrap(newInstallModel(m.cfg, m.ws, m.items[m.cursor].ID))
		case "o":
			// 'o' opens the Ollama config screen.
			return m, wrap(newOllamaModel(m.cfg, m.ws))
		}
	}
	return m, nil
}

func (m agentsModel) View() string {
	var b strings.Builder
	b.WriteString(header(fmt.Sprintf("Pick an agent for %q", m.ws.Name)))
	b.WriteString("\n")
	if m.loading {
		b.WriteString(hintStyle.Render("Scanning PATH for agent CLIs...") + "\n")
		return b.String()
	}

	// Sort: available first, missing last (deterministic UX), but preserve
	// the canonical agent order within each bucket.
	items := append([]launcher.Agent(nil), m.items...)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Available != items[j].Available {
			return items[i].Available
		}
		return false
	})
	for i, a := range items {
		marker := "  "
		render := listItemStyle.Render
		if i == m.cursor {
			marker = "› "
			render = listItemSelectedStyle.Render
		}
		label := a.Label
		statusStyle := availableStyle
		statusText := "available"
		if !a.Available {
			statusStyle = missingStyle
			statusText = "not installed"
			if a.ProbeError != "" {
				statusText = "broken install"
			}
			render = listItemStyle.Render // never highlight a disabled row
		}
		line := fmt.Sprintf("%s%s %s", marker, label, statusStyle.Render("— "+statusText))
		if a.Version != "" {
			line += " " + versionStyle.Render("("+a.Version+")")
		}
		b.WriteString(render(line) + "\n")
		if i == m.cursor && !a.Available {
			if a.ProbeError != "" {
				b.WriteString(descStyle.Render(errorStyle.Render("--version failed: "+a.ProbeError)) + "\n")
			}
			if a.InstallHint != "" {
				b.WriteString(descStyle.Render("install/repair: "+a.InstallHint) + "\n")
			}
		}
	}

	b.WriteString("\n")
	b.WriteString(hintStyle.Render(
		"Grey entries aren't on PATH — press enter (or i) to install them.",
	))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑/↓ select · enter launch/install · i install · o ollama · esc back · ctrl-c quit"))
	return b.String()
}

// --- shared helpers ------------------------------------------------------

func wpcVersionString() string {
	return fmt.Sprintf("code-launcher / %s %s/%s", "v0.1", runtime.GOOS, runtime.GOARCH)
}
