package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sPROFFEs/PrAImate/internal/backup"
	"github.com/sPROFFEs/PrAImate/internal/launcher"
)

// Each screen is its own model type. The root model delegates Update/View
// to the active screen and consumes "done" messages to transition.
//
// We hand-roll the list/selection logic instead of using bubbles/list to
// keep the visual output tight and stay in full control of styling.

// --- shared messages ------------------------------------------------------

type (
	screenDoneMsg struct {
		next          tea.Model
		launch        *launcher.LaunchPlan // when set, root quits and execs after Run
		updateCfg     *launcher.Config     // when set, root persists it before transitioning
		launchedWS    *launcher.Workspace  // for post-launch hooks (memory sync-back)
		launchedAgent launcher.AgentID     // so post-exit can run transcript capture
	}
	errMsg struct{ err error }
)

func wrap(m tea.Model) tea.Cmd  { return func() tea.Msg { return screenDoneMsg{next: m} } }
func wrapErr(err error) tea.Cmd { return func() tea.Msg { return errMsg{err: err} } }
func wrapLaunch(p launcher.LaunchPlan, c *launcher.Config) tea.Cmd {
	return func() tea.Msg { return screenDoneMsg{launch: &p, updateCfg: c} }
}

// --- first-run screen ----------------------------------------------------

// First-run wizard is two short steps:
//
//  0. text input for the workspaces-root path
//  1. y/n: copy the bundled example templates? (default yes)
//
// The seeding step is only ever shown on first run — once config.json
// exists the launcher jumps straight to the chat list. If the user
// declines seeding here, they can still add templates later via
// `t` → `+ new template`, or by copying files into <root>/templates/
// manually.
type firstRunStep int

const (
	firstRunStepRoot     firstRunStep = iota
	firstRunStepMethod                // pick init method (empty / samples / clone)
	firstRunStepCloneURL              // shown only when method == Clone
	firstRunStepWorking
)

// firstRunMethod is the init choice on step 1. Maps to one of three
// branches in finalize().
type firstRunMethod int

const (
	firstRunMethodEmpty   firstRunMethod = iota // [1] just chats/ + templates/
	firstRunMethodSamples                       // [2] empty + copy bundled samples
	firstRunMethodClone                         // [3] git clone a remote
)

type firstRunModel struct {
	input    textinput.Model // workspaces root on step 0
	urlInput textinput.Model // clone URL on step 2
	step     firstRunStep
	method   firstRunMethod // chosen on step 1
	cursor   int            // 0..2 cursor on step 1
	root     string
	cloneURL string

	// cloneFallback explains why we fell back to method=Empty (e.g.
	// "remote unreachable; falling back to empty folder"). Surfaced
	// on the working step so the user knows what just happened.
	cloneFallback string

	err     string
	status  string
	bundled []string // names of templates that would be seeded
}

func newFirstRun() firstRunModel {
	ti := textinput.New()
	ti.Placeholder = defaultWorkspacesRoot()
	ti.SetValue(defaultWorkspacesRoot())
	ti.Focus()
	ti.CharLimit = 4096
	ti.Width = 60

	urlIn := textinput.New()
	urlIn.Placeholder = "https://github.com/user/clade-backup.git"
	urlIn.CharLimit = 2048
	urlIn.Width = 60

	return firstRunModel{
		input:    ti,
		urlInput: urlIn,
		step:     firstRunStepRoot,
		method:   firstRunMethodSamples, // best default for first-time users
	}
}

func defaultWorkspacesRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "praimate-workspaces"
	}
	return filepath.Join(home, "praimate-workspaces")
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
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && !strings.HasPrefix(e.Name(), "_") {
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
	case firstRunCloneResultMsg:
		// Came back from the clone attempt on the working step.
		if msg.err == nil {
			// Test passed AND clone succeeded — backup is
			// implicitly enabled (the user explicitly chose to
			// clone a remote into this workspaces root).
			cfg := &launcher.Config{
				WorkspacesRoot:  m.root,
				BackupEnabled:   true,
				BackupRemoteURL: m.cloneURL,
			}
			if err := launcher.SaveConfig(cfg); err != nil {
				m.err = err.Error()
				m.step = firstRunStepCloneURL
				return m, nil
			}
			return m, wrap(newLayoutModel(cfg))
		}
		// Connection test or clone failed — fall back to empty
		// folder, but **preserve the URL the user typed** so the
		// Backup tab opens with it pre-filled and they can fix +
		// retry without retyping. Backup itself stays disabled.
		m.cloneFallback = msg.err.Error()
		m.method = firstRunMethodEmpty
		return m, m.finalize()

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
				m.step = firstRunStepMethod
				m.cursor = int(firstRunMethodSamples) // default selection
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd

		case firstRunStepMethod:
			switch msg.String() {
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down", "j":
				if m.cursor < 2 {
					m.cursor++
				}
			case "1":
				m.method = firstRunMethodEmpty
				return m, m.finalize()
			case "2":
				m.method = firstRunMethodSamples
				return m, m.finalize()
			case "3":
				m.method = firstRunMethodClone
				m.step = firstRunStepCloneURL
				m.urlInput.Focus()
				return m, textinput.Blink
			case "enter":
				m.method = firstRunMethod(m.cursor)
				if m.method == firstRunMethodClone {
					m.step = firstRunStepCloneURL
					m.urlInput.Focus()
					return m, textinput.Blink
				}
				return m, m.finalize()
			case "esc":
				m.step = firstRunStepRoot
				m.input.Focus()
				return m, textinput.Blink
			}

		case firstRunStepCloneURL:
			switch msg.Type {
			case tea.KeyEnter:
				url := strings.TrimSpace(m.urlInput.Value())
				if url == "" {
					m.err = "URL can't be empty"
					return m, nil
				}
				m.cloneURL = url
				m.err = ""
				return m, m.startClone()
			case tea.KeyEsc:
				m.step = firstRunStepMethod
				return m, nil
			}
			var cmd tea.Cmd
			m.urlInput, cmd = m.urlInput.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

// firstRunCloneResultMsg arrives once the background clone attempt
// from startClone() finishes.
type firstRunCloneResultMsg struct {
	err error
}

// finalize handles methods Empty + Samples (clone is handled by
// startClone). Creates the directory skeleton, optionally seeds the
// bundled samples, writes the managed .gitignore so a later
// "link a remote" flow works without surprises, saves config, then
// hands off to the layout.
func (m *firstRunModel) finalize() tea.Cmd {
	root := m.root
	method := m.method
	fallbackNote := m.cloneFallback
	m.step = firstRunStepWorking
	switch method {
	case firstRunMethodEmpty:
		m.status = "Creating empty workspaces root..."
	case firstRunMethodSamples:
		m.status = "Creating workspaces root + seeding bundled templates..."
	}
	return func() tea.Msg {
		// Always create the directory skeleton.
		if err := os.MkdirAll(filepath.Join(root, launcher.TemplatesDir), 0o755); err != nil {
			return errMsg{err: fmt.Errorf("mkdir templates: %w", err)}
		}
		if err := os.MkdirAll(filepath.Join(root, launcher.ChatsDir), 0o755); err != nil {
			return errMsg{err: fmt.Errorf("mkdir chats: %w", err)}
		}
		if method == firstRunMethodSamples {
			if _, err := launcher.SeedSamples(root, sampleCandidatesFromCwd()); err != nil {
				return errMsg{err: fmt.Errorf("seed samples: %w", err)}
			}
		}
		// NOTE: we deliberately do NOT write .gitignore /
		// .gitattributes here. The backup feature is opt-in — those
		// managed files only appear when the user explicitly flips
		// the master switch in the Backup tab (or picks the "clone
		// from remote" first-run option, which sets the switch on as
		// part of the clone).
		cfg := &launcher.Config{WorkspacesRoot: root}
		// If the user typed a URL during the clone step but the
		// connection test or clone itself failed, preserve the URL
		// so the Backup tab opens pre-filled. Backup remains
		// DISABLED — the user has to flip the master switch to
		// engage it.
		if m.cloneURL != "" {
			cfg.BackupRemoteURL = m.cloneURL
		}
		if err := launcher.SaveConfig(cfg); err != nil {
			return errMsg{err: fmt.Errorf("save config: %w", err)}
		}
		next := newLayoutModel(cfg)
		// Surface the clone fallback breadcrumb (e.g. "remote
		// unreachable; falling back…") as a one-off note so the user
		// understands why their "clone" choice didn't land.
		if fallbackNote != "" {
			next.firstFlash = "Remote clone failed; created empty folder instead.\n  " + fallbackNote
		}
		return screenDoneMsg{next: next}
	}
}

// startClone runs the first-run clone path in two stages:
//
//  1. Connection test (`git ls-remote`) — lightweight, no disk
//     writes. Confirms the URL is reachable AND that the user's
//     credentials can read it. Failures here mean "the URL is
//     wrong, the network is down, or auth isn't set up"; we fall
//     back without ever touching the filesystem.
//
//  2. If the test passes, the actual `git clone` into the
//     workspaces root. Failures here mean "the remote is reachable
//     but something else went wrong" (disk error, partial transfer,
//     etc.). Caller still falls back, with the URL preserved.
//
// On either failure, the URL the user typed is preserved in the
// fallback config so the Backup tab opens pre-filled for retry.
func (m *firstRunModel) startClone() tea.Cmd {
	root := m.root
	url := m.cloneURL
	m.step = firstRunStepWorking
	m.status = "Testing connection to " + url + "..."
	return func() tea.Msg {
		// The clone target must not exist (or must be empty). If
		// the user's root already exists with files, fail with a
		// clear message rather than letting git complain cryptically.
		if entries, err := os.ReadDir(root); err == nil && len(entries) > 0 {
			return firstRunCloneResultMsg{err: fmt.Errorf(
				"%s already has files in it; clone target must be empty. "+
					"Remove the directory or pick a different workspaces root.", root)}
		}
		// Step 1: lightweight reachability test.
		testCtx, testCancel := context.WithTimeout(context.Background(), 15*time.Second)
		if _, lsErr := backup.LsRemote(testCtx, url); lsErr != nil {
			testCancel()
			return firstRunCloneResultMsg{err: classifyCloneError(lsErr)}
		}
		testCancel()
		// Step 2: the actual clone.
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := backup.Clone(ctx, url, root); err != nil {
			return firstRunCloneResultMsg{err: classifyCloneError(err)}
		}
		// Clone succeeded — ensure templates/ + chats/ exist (some
		// repos may not have them yet). The managed .gitignore /
		// .gitattributes come from the remote we just cloned; if
		// they're missing we write them on the first Sync from the
		// Backup tab.
		_ = os.MkdirAll(filepath.Join(root, launcher.TemplatesDir), 0o755)
		_ = os.MkdirAll(filepath.Join(root, launcher.ChatsDir), 0o755)
		// Belt-and-braces: drop the managed files if the remote
		// didn't ship them. WriteManaged* refuses to clobber user-
		// edited files, so this is safe.
		_ = backup.WriteManagedGitignore(root)
		_ = backup.WriteManagedGitattributes(root)
		return firstRunCloneResultMsg{}
	}
}

// classifyCloneError turns a backup.Clone failure into a friendlier
// message for first-run users who may not be deep on git internals.
func classifyCloneError(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "authentication"),
		strings.Contains(msg, "permission denied"),
		strings.Contains(msg, "could not read username"),
		strings.Contains(msg, "publickey"):
		return fmt.Errorf(
			"remote repository is not reachable or is private. Please review " +
				"that your git credentials are configured (HTTPS credential " +
				"helper or SSH key) and try again from the Backup tab once " +
				"you've created an empty workspace")
	case strings.Contains(msg, "could not resolve host"),
		strings.Contains(msg, "name or service not known"),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "network unreachable"):
		return fmt.Errorf(
			"remote repository is not reachable. Please check the URL and " +
				"your network connection")
	case strings.Contains(msg, "not found"),
		strings.Contains(msg, "does not appear to be a git repository"):
		return fmt.Errorf(
			"remote repository not found. Verify the URL is correct and " +
				"the repository is either public or your credentials grant " +
				"access")
	}
	return err
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

	case firstRunStepMethod:
		help = "↑/↓ select · enter accept · 1/2/3 jump · esc back"
		b.WriteString(subtitleStyle.Render("Workspaces root: ") + m.root + "\n\n")
		b.WriteString(inputLabelStyle.Render("How should this root be initialised?") + "\n\n")
		opts := []struct {
			label string
			hint  string
		}{
			{
				"[1] Empty folder",
				"Just chats/ and templates/. Add templates by hand later via 't'.",
			},
			{
				"[2] Copy the bundled sample templates",
				"Seeds the reversing, code-review, and workpath-author starter templates.",
			},
			{
				"[3] Clone a git repository (cross-machine sync)",
				"Pulls existing chats + templates from a remote you already pushed " +
					"to from another machine. The repo must be public OR your git " +
					"credentials (HTTPS helper / SSH key) must already be configured.",
			},
		}
		for i, opt := range opts {
			isSel := i == m.cursor
			marker := "  "
			if isSel {
				marker = "› "
			}
			b.WriteString(selectionRow(marker+opt.label, isSel) + "\n")
			if isSel {
				b.WriteString(descStyle.Render("   "+opt.hint) + "\n")
			}
		}
		b.WriteString("\n")
		if len(m.bundled) > 0 {
			b.WriteString(descStyle.Render("Bundled templates available for option [2]: "+
				strings.Join(m.bundled, ", ")) + "\n")
		}

	case firstRunStepCloneURL:
		help = "enter clone · esc back"
		b.WriteString(subtitleStyle.Render("Workspaces root: ") + m.root + "\n\n")
		b.WriteString(inputLabelStyle.Render("Git remote URL: "))
		b.WriteString(m.urlInput.View() + "\n\n")
		b.WriteString(descStyle.Render(
			"Examples:") + "\n")
		b.WriteString(descStyle.Render(
			"  https://github.com/<user>/<repo>.git") + "\n")
		b.WriteString(descStyle.Render(
			"  git@github.com:<user>/<repo>.git") + "\n")
		b.WriteString(descStyle.Render(
			"  https://gitea.example/<user>/<repo>.git") + "\n\n")
		b.WriteString(hintStyle.Render(
			"⚠ The repo must be public OR your git credentials must be configured.\n" +
				"  On clone failure we'll fall back to creating an empty folder; you can\n" +
				"  link the remote later from the Backup tab."))

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
		// Sort here, ONCE, so the cursor index, the visual rendering,
		// and the Enter handler all agree on which agent is at which
		// position. Body() used to sort a local copy at render time,
		// which silently desynced from m.cursor whenever the available-
		// first ordering differed from KnownAgents() order — leading
		// to "I picked the installed agent but it asked me to install
		// a different one" on the chat agent-swap flow.
		items := msg.items
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].Available != items[j].Available {
				return items[i].Available
			}
			return false
		})
		m.items = items
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

			// Install-only mode (the left-nav Agents tab — no chat,
			// no override). Enter on an INSTALLED agent now opens the
			// install screen too so the user can upgrade / reinstall
			// from here. Per-chat agent swap lives in chat settings.
			if m.override == nil && m.ws.Name == "" {
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
	return renderChrome(m.Title(), m.Body(), m.Help())
}

func (m agentsModel) Title() string {
	if m.override != nil {
		return fmt.Sprintf("Switch agent for chat · %s", m.ws.Name)
	}
	if m.ws.Name == "" {
		return "Agents · browse + install"
	}
	return fmt.Sprintf("Pick an agent · %s", m.ws.Name)
}
func (m agentsModel) Help() string {
	if m.override != nil {
		return "enter swap+launch · same agent re-launches as-is · i install · esc back"
	}
	if m.ws.Name == "" {
		// Install-only mode (left-nav Agents tab).
		return "↑/↓ select · enter install/update · i install · esc back"
	}
	return "↑/↓ select · enter launch/install · i install · o ollama · esc back"
}
func (m agentsModel) NavSection() string   { return navSectionAgents }
func (m agentsModel) CapturingInput() bool { return false }

func (m agentsModel) Body() string {
	var b strings.Builder

	if m.loading {
		b.WriteString(hintStyle.Render("Scanning PATH for agent CLIs..."))
		return b.String()
	}

	if m.override != nil {
		b.WriteString(hintStyle.Render(
			fmt.Sprintf("Currently bound to: %s. Pick a different agent to swap, "+
				"or Enter on the same agent to re-launch.", m.override.current)) + "\n\n")
	}

	// m.items is already sorted available-first at load time (see the
	// agentsLoadedMsg branch). Rendering here MUST iterate m.items
	// directly so the visual cursor and the Enter handler resolve
	// to the same agent.
	for i, a := range m.items {
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
	if m.ws.Name == "" && m.override == nil {
		b.WriteString(hintStyle.Render(
			"Install management. Enter opens the installer for the selected agent (whether installed or not). " +
				"Per-chat agent swap lives in chat settings (e key on the chat list).",
		))
	} else {
		b.WriteString(hintStyle.Render(
			"Grey entries aren't on PATH — press enter (or i) to install them.",
		))
	}
	return b.String()
}

// newAgentsBrowser is the nav-level Agents pane. No workspace context
// — Enter on an available agent is a no-op (no chat to launch into);
// Enter on a missing one routes to the installer. Use the per-chat
// agent picker (from the chat list) when you want to launch.
func newAgentsBrowser(cfg *launcher.Config) agentsModel {
	return agentsModel{cfg: cfg, loading: true}
}

// --- shared helpers ------------------------------------------------------

func wpcVersionString() string {
	return fmt.Sprintf("praimate / %s %s/%s", "v1.0", runtime.GOOS, runtime.GOARCH)
}
