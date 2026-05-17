package main

// Integration tests that drive every Bubble Tea screen with synthetic
// messages, verifying transitions end-to-end without needing a TTY.
//
// The shape is always:
//   1. construct the model
//   2. exec its Init() Cmd (if any) and feed the resulting msg back
//   3. send keys / synthetic msgs
//   4. assert on (a) the next screen returned via screenDoneMsg.next,
//      (b) the View() output, or (c) side effects on disk

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sdksdk/code-launcher/internal/launcher"
)

// repoRoot is two dirs above the package dir (cmd/code-launcher).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("can't determine caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// runCmd executes a tea.Cmd and returns the message it produced. tea.Cmds
// are just `func() tea.Msg` under the hood so this is trivial — we don't
// need a Bubble Tea Program to drive a single Cmd.
func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	return cmd()
}

// redirectConfig points os.UserConfigDir at tmp on whatever OS this is.
func redirectConfig(t *testing.T, tmp string) {
	t.Helper()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("APPDATA", tmp)
	case "darwin":
		t.Setenv("HOME", tmp)
	default:
		t.Setenv("XDG_CONFIG_HOME", tmp)
	}
}

func TestFirstRun_EnterSeedsSamplesAndAdvances(t *testing.T) {
	tmp := t.TempDir()
	wsRoot := filepath.Join(tmp, "ws")
	redirectConfig(t, filepath.Join(tmp, "cfg"))
	t.Chdir(repoRoot(t)) // makes the cwd-relative samples candidate hit

	m := newFirstRun()
	m.input.SetValue(wsRoot)

	// Enter triggers seeding + config save.
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("firstRun Enter should return a Cmd")
	}
	// The model snapshot returned still shows the "working" state.
	if !strings.Contains(next.View(), "Seeding samples") {
		t.Errorf("expected status line; got: %s", next.View())
	}

	msg := runCmd(t, cmd)
	done, ok := msg.(screenDoneMsg)
	if !ok {
		t.Fatalf("expected screenDoneMsg, got %T: %+v", msg, msg)
	}
	if done.next == nil {
		t.Fatal("expected next screen")
	}
	if _, isWS := done.next.(workspacesModel); !isWS {
		t.Errorf("expected workspacesModel, got %T", done.next)
	}

	// Config persisted with the right root.
	cfg, err := launcher.LoadConfig()
	if err != nil || cfg == nil {
		t.Fatalf("LoadConfig: cfg=%v err=%v", cfg, err)
	}
	if cfg.WorkspacesRoot != wsRoot {
		t.Errorf("WorkspacesRoot = %q, want %q", cfg.WorkspacesRoot, wsRoot)
	}

	// Samples landed on disk.
	wss, err := launcher.ListWorkspaces(wsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(wss) < 2 {
		t.Errorf("expected at least 2 seeded workspaces, got %d", len(wss))
	}
}

func TestWorkspacesScreen_NewItemNavigation(t *testing.T) {
	tmp := t.TempDir()
	redirectConfig(t, filepath.Join(tmp, "cfg"))
	t.Chdir(repoRoot(t))

	// Seed two workspaces directly through the library.
	seeded, err := launcher.SeedSamples(tmp, []string{filepath.Join(repoRoot(t), "samples", "workpaths")})
	if err != nil {
		t.Fatal(err)
	}
	if len(seeded) < 2 {
		t.Fatalf("seed needs at least 2; got %v", seeded)
	}

	cfg := &launcher.Config{WorkspacesRoot: tmp}
	m := newWorkspacesModel(cfg)

	// Init returns the "load workspaces" Cmd.
	loaded := runCmd(t, m.Init())
	next, _ := m.Update(loaded)
	m = next.(workspacesModel)

	if len(m.items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(m.items))
	}

	// Pressing 'down' twice should land on the synthetic "+ new workspace" entry
	// (cursor == len(items)).
	for i := 0; i < 2; i++ {
		nx, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = nx.(workspacesModel)
	}
	if m.cursor != len(m.items) {
		t.Errorf("cursor = %d, want %d (the + new entry)", m.cursor, len(m.items))
	}

	// Enter on the + entry transitions to newWorkspaceModel.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	done := runCmd(t, cmd).(screenDoneMsg)
	if _, ok := done.next.(newWorkspaceModel); !ok {
		t.Errorf("expected newWorkspaceModel, got %T", done.next)
	}
}

func TestWorkspacesScreen_EnterOnExistingPicksAgents(t *testing.T) {
	tmp := t.TempDir()
	redirectConfig(t, filepath.Join(tmp, "cfg"))
	t.Chdir(repoRoot(t))

	if _, err := launcher.SeedSamples(tmp, []string{filepath.Join(repoRoot(t), "samples", "workpaths")}); err != nil {
		t.Fatal(err)
	}
	cfg := &launcher.Config{WorkspacesRoot: tmp}
	m := newWorkspacesModel(cfg)
	loaded := runCmd(t, m.Init())
	next, _ := m.Update(loaded)
	m = next.(workspacesModel)

	// Cursor starts at 0 — pick the first workspace.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	done := runCmd(t, cmd).(screenDoneMsg)
	agents, ok := done.next.(agentsModel)
	if !ok {
		t.Fatalf("expected agentsModel, got %T", done.next)
	}
	if agents.ws.Name != m.items[0].Name {
		t.Errorf("agentsModel got ws %q, expected %q", agents.ws.Name, m.items[0].Name)
	}
}

func TestNewWorkspaceModel_FullWizardCreates(t *testing.T) {
	tmp := t.TempDir()
	redirectConfig(t, filepath.Join(tmp, "cfg"))

	cfg := &launcher.Config{WorkspacesRoot: tmp}
	m := newNewWorkspaceModel(cfg)

	// step 0 — name
	m.name.SetValue("integration-test")
	nx, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(newWorkspaceModel)
	if m.step != 1 {
		t.Fatalf("after name step = %d, want 1", m.step)
	}

	// step 1 — description
	m.description.SetValue("scaffolded by the integration test")
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(newWorkspaceModel)
	if m.step != 2 {
		t.Fatalf("after desc step = %d, want 2", m.step)
	}

	// step 2 — language
	m.language.SetValue("Italian")
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(newWorkspaceModel)
	if m.step != 3 {
		t.Fatalf("after lang step = %d, want 3", m.step)
	}

	// step 3 — memory toggle: y
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = nx.(newWorkspaceModel)
	if m.step != 4 || !m.memory {
		t.Fatalf("after memory y: step=%d memory=%v", m.step, m.memory)
	}

	// step 4 — add one skill, then blank Enter to finalize
	m.skillInput.SetValue("https://example.com/skill.git")
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(newWorkspaceModel)
	if len(m.skills) != 1 {
		t.Fatalf("skills = %v", m.skills)
	}
	nx, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(newWorkspaceModel)
	if cmd == nil {
		t.Fatal("finalize Enter must produce a Cmd")
	}
	done := runCmd(t, cmd).(screenDoneMsg)
	agents, ok := done.next.(agentsModel)
	if !ok {
		t.Fatalf("expected agentsModel, got %T", done.next)
	}
	if agents.ws.Name != "integration-test" {
		t.Errorf("ws.Name = %q", agents.ws.Name)
	}

	// All settings persisted.
	loaded, err := launcher.LoadWorkspace(tmp, "integration-test")
	if err != nil || loaded == nil {
		t.Fatalf("LoadWorkspace: %v %v", err, loaded)
	}
	if loaded.Settings.Language != "Italian" {
		t.Errorf("Language = %q", loaded.Settings.Language)
	}
	if !loaded.Settings.MemoryEnabled {
		t.Error("MemoryEnabled not persisted")
	}
	if len(loaded.Settings.OnlineSkills) != 1 || loaded.Settings.OnlineSkills[0] != "https://example.com/skill.git" {
		t.Errorf("OnlineSkills = %v", loaded.Settings.OnlineSkills)
	}
}

func TestNewWorkspaceModel_BadNameSurfacesErrorAtFinalize(t *testing.T) {
	tmp := t.TempDir()
	redirectConfig(t, filepath.Join(tmp, "cfg"))
	cfg := &launcher.Config{WorkspacesRoot: tmp}
	m := newNewWorkspaceModel(cfg)

	// Drive through the wizard with a forbidden name (caps + space).
	m.name.SetValue("Bad Name")
	m.description.SetValue("anything")
	// step 0 → 1 → 2 → 3 (memory, just press enter to accept default)
	// → 4 (skills, blank Enter to finalize)
	for i := 0; i < 3; i++ {
		nx, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = nx.(newWorkspaceModel)
	}
	// Now on step 3 (memory). Send Enter to advance to step 4.
	nx, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(newWorkspaceModel)
	if m.step != 4 {
		t.Fatalf("after walking through wizard, step = %d, want 4", m.step)
	}
	// Blank Enter → finalize → CreateWorkspace fails on bad name.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := runCmd(t, cmd)
	if _, isErr := msg.(errMsg); !isErr {
		t.Fatalf("expected errMsg from invalid name at finalize, got %T", msg)
	}
	// Feed the errMsg back; should bounce to step 0.
	nx2, _ := m.Update(msg)
	m2 := nx2.(newWorkspaceModel)
	if m2.step != 0 {
		t.Errorf("after errMsg step = %d, want 0", m2.step)
	}
	if m2.err == "" {
		t.Error("expected non-empty err after invalid name")
	}
}

func TestAgentsScreen_EnterOnAvailableProducesLaunch(t *testing.T) {
	tmp := t.TempDir()
	redirectConfig(t, filepath.Join(tmp, "cfg"))
	t.Chdir(repoRoot(t))

	if _, err := launcher.SeedSamples(tmp, []string{filepath.Join(repoRoot(t), "samples", "workpaths")}); err != nil {
		t.Fatal(err)
	}
	ws, err := launcher.LoadWorkspace(tmp, "reversing")
	if err != nil || ws == nil {
		t.Fatalf("LoadWorkspace: %v %v", err, ws)
	}
	cfg := &launcher.Config{WorkspacesRoot: tmp}

	m := newAgentsModel(cfg, *ws)

	// Inject a known, available agent so this test doesn't depend on what's
	// installed on the dev box. Skip Init's real detection.
	m.items = []launcher.Agent{
		{
			ID:        launcher.AgentCodex,
			Label:     "Codex CLI (fake)",
			Binary:    fakeCmd(),
			WpcTarget: "codex",
			Available: true,
			Version:   "fake",
		},
	}
	m.loading = false

	// Enter on cursor 0 → Cmd that compiles + returns launch plan.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := runCmd(t, cmd)
	done, ok := msg.(screenDoneMsg)
	if !ok {
		t.Fatalf("expected screenDoneMsg, got %T %+v", msg, msg)
	}
	if done.launch == nil {
		t.Fatal("expected launch plan")
	}
	if done.launch.Dir != ws.SandboxDir {
		t.Errorf("launch.Dir = %q, want %q", done.launch.Dir, ws.SandboxDir)
	}
	if done.updateCfg == nil || done.updateCfg.LastAgent != string(launcher.AgentCodex) {
		t.Errorf("updateCfg.LastAgent = %+v", done.updateCfg)
	}
}

func TestAgentsScreen_EnterOnUnavailableRoutesToInstall(t *testing.T) {
	tmp := t.TempDir()
	redirectConfig(t, filepath.Join(tmp, "cfg"))
	t.Chdir(repoRoot(t))
	if _, err := launcher.SeedSamples(tmp, []string{filepath.Join(repoRoot(t), "samples", "workpaths")}); err != nil {
		t.Fatal(err)
	}
	ws, _ := launcher.LoadWorkspace(tmp, "reversing")
	cfg := &launcher.Config{WorkspacesRoot: tmp}

	m := newAgentsModel(cfg, *ws)
	m.items = []launcher.Agent{
		{ID: launcher.AgentCodex, Label: "Codex", Binary: "codex", WpcTarget: "codex", Available: false},
	}
	m.loading = false

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Phase 2: enter on greyed agent should route to install screen, not no-op")
	}
	msg := runCmd(t, cmd)
	done, ok := msg.(screenDoneMsg)
	if !ok {
		t.Fatalf("expected screenDoneMsg, got %T", msg)
	}
	if _, isInstall := done.next.(installModel); !isInstall {
		t.Errorf("expected installModel, got %T", done.next)
	}
}

func TestAgentsScreen_IKeyOpensInstaller(t *testing.T) {
	tmp := t.TempDir()
	redirectConfig(t, filepath.Join(tmp, "cfg"))
	t.Chdir(repoRoot(t))
	_, _ = launcher.SeedSamples(tmp, []string{filepath.Join(repoRoot(t), "samples", "workpaths")})
	ws, _ := launcher.LoadWorkspace(tmp, "reversing")
	cfg := &launcher.Config{WorkspacesRoot: tmp}

	m := newAgentsModel(cfg, *ws)
	m.items = []launcher.Agent{
		{ID: launcher.AgentClaude, Label: "Claude", Binary: "claude", WpcTarget: "claude", Available: true},
	}
	m.loading = false

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if cmd == nil {
		t.Fatal("i should open install screen even for available agents (upgrade path)")
	}
	done := runCmd(t, cmd).(screenDoneMsg)
	if _, ok := done.next.(installModel); !ok {
		t.Errorf("expected installModel, got %T", done.next)
	}
}

func TestAgentsScreen_OKeyOpensOllama(t *testing.T) {
	tmp := t.TempDir()
	redirectConfig(t, filepath.Join(tmp, "cfg"))
	t.Chdir(repoRoot(t))
	_, _ = launcher.SeedSamples(tmp, []string{filepath.Join(repoRoot(t), "samples", "workpaths")})
	ws, _ := launcher.LoadWorkspace(tmp, "reversing")
	cfg := &launcher.Config{WorkspacesRoot: tmp}

	m := newAgentsModel(cfg, *ws)
	m.items = []launcher.Agent{{ID: launcher.AgentClaude, Available: true}}
	m.loading = false

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if cmd == nil {
		t.Fatal("o should open Ollama config screen")
	}
	done := runCmd(t, cmd).(screenDoneMsg)
	if _, ok := done.next.(ollamaModel); !ok {
		t.Errorf("expected ollamaModel, got %T", done.next)
	}
}

func TestRootModel_RoutesScreensAndCapturesLaunch(t *testing.T) {
	// Construct a root with a stub screen; send a screenDoneMsg with a
	// launch payload and confirm the root captures it + quits.
	root := &rootModel{screen: newFirstRun()}

	plan := launcher.LaunchPlan{Command: "echo", Dir: t.TempDir()}
	cfg := &launcher.Config{WorkspacesRoot: "/tmp/x", LastAgent: "codex"}

	_, cmd := root.Update(screenDoneMsg{launch: &plan, updateCfg: cfg})
	if cmd == nil {
		t.Fatal("expected a Cmd (tea.Quit)")
	}
	if root.launch == nil || root.launch.Command != "echo" {
		t.Errorf("root.launch not captured: %+v", root.launch)
	}
	if root.updateCfg == nil || root.updateCfg.LastAgent != "codex" {
		t.Errorf("root.updateCfg not captured: %+v", root.updateCfg)
	}
}

func TestRootModel_ErrMsgIsFatal(t *testing.T) {
	root := &rootModel{screen: newFirstRun()}
	_, cmd := root.Update(errMsg{err: errExample{}})
	if cmd == nil {
		t.Fatal("expected tea.Quit from errMsg")
	}
	if root.fatal == nil {
		t.Error("root.fatal should be set")
	}
}

func TestExecAgent_RunsCommandAndPropagatesExit(t *testing.T) {
	// Pick a command that always exists on the host and exits cleanly.
	plan := launcher.LaunchPlan{Command: fakeCmd(), Args: fakeArgs(), Dir: t.TempDir()}
	if code := execAgent(plan); code != 0 {
		t.Errorf("execAgent returned %d for a successful command", code)
	}
}

// fakeCmd returns a portable command that exits 0 on whichever OS the
// tests run on, suitable for verifying the spawn pipeline without
// launching an actual agent.
func fakeCmd() string {
	if runtime.GOOS == "windows" {
		return "cmd"
	}
	return "true"
}

func fakeArgs() []string {
	if runtime.GOOS == "windows" {
		return []string{"/c", "exit", "0"}
	}
	return nil
}

type errExample struct{}

func (errExample) Error() string { return "example error" }
