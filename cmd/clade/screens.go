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

	"github.com/sPROFFEs/Clade/internal/launcher"
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

// First-run wizard is two short steps:
//
//   0. text input for the workspaces-root path
//   1. y/n: copy the bundled example templates? (default yes)
//
// The seeding step is only ever shown on first run — once config.json
// exists the launcher jumps straight to the chat list. If the user
// declines seeding here, they can still add templates later via
// `t` → `+ new template`, or by copying files into <root>/templates/
// manually.
type firstRunStep int

const (
	firstRunStepRoot firstRunStep = iota
	firstRunStepSeed
	firstRunStepWorking
)

type firstRunModel struct {
	input    textinput.Model
	step     firstRunStep
	seed     bool // user's choice on step 1 (default true)
	root     string
	err      string
	status   string
	bundled  []string // names of templates that would be seeded
}

func newFirstRun() firstRunModel {
	ti := textinput.New()
	ti.Placeholder = defaultWorkspacesRoot()
	ti.SetValue(defaultWorkspacesRoot())
	ti.Focus()
	ti.CharLimit = 4096
	ti.Width = 60
	return firstRunModel{
		input: ti,
		step:  firstRunStepRoot,
		seed:  true, // recommended default
	}
}

func defaultWorkspacesRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "clade-workspaces"
	}
	return filepath.Join(home, "clade-workspaces")
}

func (m firstRunModel) Init() tea.Cmd { return textinput.Blink }

// sampleCandidatesFromCwd returns the same candidate locations the
// seed-and-save Cmd uses, exposed here so step 1 can preview the list
// of bundled templates the user would get.
func sampleCandidatesFromCwd() []string {
	execDir, _ := os.Executable()
	if execDir != "" {
		execDir = filepath.Dir(execDir)
	}
	candidates := launcher.SampleCandidates(execDir)
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "samples", "workpaths"))
	}
	return candidates
}

// bundledTemplateNames peeks at the first sample dir that exists and
// returns the subdir names. Pure read-only — does not seed anything.
func bundledTemplateNames() []string {
	for _, c := range sampleCandidatesFromCwd() {
		entries, err := os.ReadDir(c)
		if err != nil {
			continue
		}
		var names []string
		for _, e := range entries {
			if e.IsDir() {
				names = append(names, e.Name())
			}
		}
		if len(names) > 0 {
			return names
		}
	}
	return nil
}

func (m firstRunModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.step {
		case firstRunStepRoot:
			if msg.Type == tea.KeyEnter {
				root, err := filepath.Abs(strings.TrimSpace(m.input.Value()))
				if err != nil {
					m.err = err.Error()
					return m, nil
				}
				m.root = root
				m.bundled = bundledTemplateNames()
				m.step = firstRunStepSeed
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd

		case firstRunStepSeed:
			switch msg.String() {
			case "y", "Y":
				m.seed = true
				return m, m.finalize()
			case "n", "N":
				m.seed = false
				return m, m.finalize()
			case " ":
				m.seed = !m.seed
				return m, nil
			case "enter":
				// Accept whatever the toggle is currently showing.
				return m, m.finalize()
			case "esc":
				m.step = firstRunStepRoot
				m.input.Focus()
				return m, textinput.Blink
			}
		}
	}
	return m, nil
}

// finalize seeds (if requested) and saves config, then advances to the
// chat list. Always called from step 1.
func (m *firstRunModel) finalize() tea.Cmd {
	root := m.root
	seed := m.seed
	m.step = firstRunStepWorking
	if seed {
		m.status = "Seeding bundled templates..."
	} else {
		m.status = "Skipping seed — you can add templates later via 't'."
	}
	return func() tea.Msg {
		if seed {
			if _, err := launcher.SeedSamples(root, sampleCandidatesFromCwd()); err != nil {
				return errMsg{err: fmt.Errorf("seed samples: %w", err)}
			}
		} else {
			// Make sure the templates/ + chats/ dirs exist so later
			// list operations don't choke.
			if err := os.MkdirAll(filepath.Join(root, launcher.TemplatesDir), 0o755); err != nil {
				return errMsg{err: fmt.Errorf("mkdir templates: %w", err)}
			}
			if err := os.MkdirAll(filepath.Join(root, launcher.ChatsDir), 0o755); err != nil {
				return errMsg{err: fmt.Errorf("mkdir chats: %w", err)}
			}
		}
		cfg := &launcher.Config{WorkspacesRoot: root}
		if err := launcher.SaveConfig(cfg); err != nil {
			return errMsg{err: fmt.Errorf("save config: %w", err)}
		}
		return screenDoneMsg{next: newChatListModel(cfg)}
	}
}

func (m firstRunModel) View() string {
	var b strings.Builder
	dir, file, _ := launcher.ConfigPaths()
	b.WriteString(versionStyle.Render(
		fmt.Sprintf("Config will live at %s (in %s)", filepath.Base(file), dir),
	))
	b.WriteString("\n\n")

	help := "enter to continue · ctrl-c to abort"
	title := "First run · set up your workspace"

	switch m.step {
	case firstRunStepRoot:
		b.WriteString(subtitleStyle.Render(
			"Each workspace bundles a workpath (knowledge base) and a sandbox (agent cwd).",
		))
		b.WriteString("\n\n")
		b.WriteString(inputLabelStyle.Render("Workspaces root: "))
		b.WriteString(m.input.View())

	case firstRunStepSeed:
		help = "y / n to choose · space to toggle · enter to accept · esc back"
		b.WriteString(subtitleStyle.Render("Workspaces root: ") + m.root + "\n\n")
		mark := "[ ] No, leave it empty"
		if m.seed {
			mark = availableStyle.Render("[x] Yes, copy the bundled templates (recommended)")
		}
		b.WriteString(inputLabelStyle.Render("Seed example templates? ") + mark + "\n\n")
		if len(m.bundled) > 0 {
			b.WriteString(descStyle.Render("Bundled templates:") + "\n")
			for _, name := range m.bundled {
				b.WriteString(descStyle.Render("  • "+name) + "\n")
			}
		} else {
			b.WriteString(hintStyle.Render(
				"(No bundled templates found near the launcher binary — nothing to seed.)") + "\n")
		}
		b.WriteString("\n")
		b.WriteString(hintStyle.Render(
			"Either way you can add or remove templates later via 't' on the home screen.",
		))

	case firstRunStepWorking:
		help = ""
		b.WriteString(hintStyle.Render(m.status))
	}

	if m.err != "" {
		b.WriteString("\n" + errorStyle.Render("✗ "+m.err))
	}
	return renderChrome(title, b.String(), help)
}

// --- agents screen -------------------------------------------------------

type agentsModel struct {
	cfg     *launcher.Config
	ws      launcher.Workspace
	items   []launcher.Agent
	cursor  int
	loading bool

	// override (optional) is set when the picker was opened on an
	// existing chat to switch its agent. When non-nil, the picker
	// behaves slightly differently:
	//   - the cursor seeds on the chat's CURRENT agent (rather than
	//     the user's last-used global default);
	//   - pressing Enter on a different agent persists the swap into
	//     chat.json BEFORE launching, so the chat list shows the new
	//     agent on return and the change survives a restart.
	override *chatOverride
}

// chatOverride is the per-chat agent-swap context. We hold the
// workspaces root + chat ID so we can reload the chat fresh from
// disk and write back the new agent without trusting a stale copy
// from the home screen.
type chatOverride struct {
	root    string
	chatID  string
	current launcher.AgentID // agent the chat currently has; cursor seed
}

func newAgentsModel(cfg *launcher.Config, ws launcher.Workspace) agentsModel {
	return agentsModel{cfg: cfg, ws: ws, loading: true}
}

// newAgentsModelForChatOverride opens the picker in "swap the agent
// for this chat" mode. Caller passes the workspaces root + chat ID
// so the picker can reload + save the chat manifest on enter.
func newAgentsModelForChatOverride(cfg *launcher.Config, c launcher.Chat) agentsModel {
	m := newAgentsModel(cfg, c.AsWorkspace())
	m.override = &chatOverride{
		root:    cfg.WorkspacesRoot,
		chatID:  c.ID,
		current: c.AgentID,
	}
	return m
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
		// Cursor seed priority:
		//   1. Override mode → seed on the chat's CURRENT agent so the
		//      user sees "where I am now" before picking a different one.
		//   2. Otherwise → user's last-used global default.
		seedID := ""
		if m.override != nil {
			seedID = string(m.override.current)
		} else if m.cfg.LastAgent != "" {
			seedID = m.cfg.LastAgent
		}
		if seedID != "" {
			for i, a := range m.items {
				if string(a.ID) == seedID {
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

			// Override mode: persist the new agent into the chat's
			// manifest before launching. We always re-read the chat
			// from disk so a stale in-memory copy can't clobber a
			// concurrent edit (e.g. settings touched in another
			// screen). If the user picked the SAME agent the chat
			// already had, we skip the write — keeps mtimes clean.
			if m.override != nil {
				if a.ID != m.override.current {
					c, err := launcher.LoadChat(m.override.root, m.override.chatID)
					if err == nil && c != nil {
						c.AgentID = a.ID
						_ = launcher.SaveChatSettings(*c)
						// Route through the normal launching screen so
						// decoration / sandbox compile runs against the
						// new target. OpenChat re-detects agents and
						// builds the plan from the freshly-saved manifest.
						return m, wrap(newLaunchingModel(m.cfg, *c))
					}
					// Fall through on load/save error — better to
					// launch once with the chosen agent than to wedge
					// the user out of their chat.
				}
				// Same agent as before → just launch via the normal
				// path with no manifest write.
				if c, err := launcher.LoadChat(m.override.root, m.override.chatID); err == nil && c != nil {
					return m, wrap(newLaunchingModel(m.cfg, *c))
				}
			}

			// Default path (new-chat wizard, install screen return, etc.):
			// direct Plan + launch, no manifest writing.
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
	title := fmt.Sprintf("Pick an agent · %s", m.ws.Name)
	help := "↑/↓ select · enter launch/install · i install · o ollama · esc back"
	if m.override != nil {
		title = fmt.Sprintf("Switch agent for chat · %s", m.ws.Name)
		help = "enter swap+launch · same agent re-launches as-is · i install · esc back"
	}

	if m.loading {
		b.WriteString(hintStyle.Render("Scanning PATH for agent CLIs..."))
		return renderChrome(title, b.String(), help)
	}

	if m.override != nil {
		b.WriteString(hintStyle.Render(
			fmt.Sprintf("Currently bound to: %s. Pick a different agent to swap, "+
				"or Enter on the same agent to re-launch.", m.override.current)) + "\n\n")
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
		isSel := i == m.cursor && a.Available
		marker := "  "
		if i == m.cursor {
			marker = "› "
		}
		label := a.Label
		statusText := availableStyle.Render("● available")
		if !a.Available {
			statusText = missingStyle.Render("○ not installed")
			if a.ProbeError != "" {
				statusText = errorStyle.Render("✗ broken install")
			}
		}
		line := marker + label + "   " + statusText
		if a.Version != "" {
			line += "  " + lipglossDimRender("("+a.Version+")", isSel)
		}
		b.WriteString(selectionRow(line, isSel) + "\n")
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
	return renderChrome(title, b.String(), help)
}

// --- shared helpers ------------------------------------------------------

func wpcVersionString() string {
	return fmt.Sprintf("clade / %s %s/%s", "v0.1", runtime.GOOS, runtime.GOARCH)
}
