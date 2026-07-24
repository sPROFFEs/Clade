package main

// Integration tests that drive every Bubble Tea screen with synthetic
// messages, verifying transitions end-to-end without needing a TTY.
// Rewritten for the chats/templates split (v0.2): the home screen is
// chatListModel, new chats clone from templates via newPickTemplateModel.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"git.jtsec.local/lab/PrAImate/internal/launcher"
)

// repoRoot is two dirs above the package dir (cmd/praimate).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("can't determine caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	return cmd()
}

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

// seededRoot returns a workspaces root with the bundled templates seeded.
func seededRoot(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	if _, err := launcher.SeedSamples(tmp, []string{filepath.Join(repoRoot(t), "samples", "workpaths")}); err != nil {
		t.Fatal(err)
	}
	return tmp
}

func TestFirstRun_YesSeedsTemplatesAndAdvances(t *testing.T) {
	tmp := t.TempDir()
	wsRoot := filepath.Join(tmp, "ws")
	redirectConfig(t, filepath.Join(tmp, "cfg"))
	t.Chdir(repoRoot(t))

	m := newFirstRun()
	m.input.SetValue(wsRoot)

	// step 0 → 1: Enter advances from path input to the method picker.
	nx, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(firstRunModel)
	if m.step != firstRunStepMethod {
		t.Fatalf("step = %d, want %d (method)", m.step, firstRunStepMethod)
	}
	if !strings.Contains(m.View(), "How should this root be initialised?") {
		t.Errorf("step 1 view missing method picker:\n%s", m.View())
	}

	// step 1 → done: '2' picks "copy bundled samples" and finalizes.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if cmd == nil {
		t.Fatal("'2' should produce a finalize Cmd")
	}
	done := runCmd(t, cmd).(screenDoneMsg)
	if _, ok := done.next.(*layoutModel); !ok {
		t.Errorf("expected *layoutModel, got %T", done.next)
	}

	tpls, err := launcher.ListTemplates(wsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(tpls) < 2 {
		t.Errorf("expected at least 2 seeded templates, got %d", len(tpls))
	}
}

func TestFirstRun_MethodEmptyCreatesEmptyDirs(t *testing.T) {
	// Method [1] "empty folder" creates chats/ and templates/ without
	// copying any samples.
	tmp := t.TempDir()
	wsRoot := filepath.Join(tmp, "ws")
	redirectConfig(t, filepath.Join(tmp, "cfg"))
	t.Chdir(repoRoot(t))

	m := newFirstRun()
	m.input.SetValue(wsRoot)
	nx, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(firstRunModel)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	if cmd == nil {
		t.Fatal("'1' (empty folder) should produce a finalize Cmd")
	}
	done := runCmd(t, cmd).(screenDoneMsg)
	if _, ok := done.next.(*layoutModel); !ok {
		t.Errorf("expected *layoutModel, got %T", done.next)
	}

	// Templates dir exists but empty.
	tpls, err := launcher.ListTemplates(wsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(tpls) != 0 {
		t.Errorf("expected 0 templates when 'n' is chosen, got %d", len(tpls))
	}
	// chats/ dir also exists so ListChats doesn't error later.
	if _, err := os.Stat(filepath.Join(wsRoot, launcher.ChatsDir)); err != nil {
		t.Errorf("chats dir not created when seeding was skipped: %v", err)
	}
}

// TestFirstRun_MethodEmpty_LeavesBackupDormant pins the opt-in
// guarantee: picking [1] empty or [2] samples must NOT touch any
// backup-related state — no .gitignore, no .gitattributes, no .git
// dir, and Config.BackupEnabled remains false. Users who never open
// the Backup tab see exactly the pre-0.1.11 behavior.
func TestFirstRun_MethodEmpty_LeavesBackupDormant(t *testing.T) {
	tmp := t.TempDir()
	wsRoot := filepath.Join(tmp, "ws")
	redirectConfig(t, filepath.Join(tmp, "cfg"))
	t.Chdir(repoRoot(t))

	m := newFirstRun()
	m.input.SetValue(wsRoot)
	nx, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(firstRunModel)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	if cmd == nil {
		t.Fatal("'1' should produce a finalize Cmd")
	}
	done := runCmd(t, cmd).(screenDoneMsg)
	if _, ok := done.next.(*layoutModel); !ok {
		t.Fatalf("expected *layoutModel, got %T", done.next)
	}

	// Loaded config: BackupEnabled false, no remote URL.
	cfg, err := launcher.LoadConfig()
	if err != nil || cfg == nil {
		t.Fatalf("LoadConfig: %v %v", err, cfg)
	}
	if cfg.BackupEnabled {
		t.Error("BackupEnabled should be false after method=Empty first-run")
	}
	if cfg.BackupRemoteURL != "" {
		t.Errorf("BackupRemoteURL should be empty, got %q", cfg.BackupRemoteURL)
	}
	if cfg.BackupAutoSync {
		t.Error("BackupAutoSync should be false")
	}

	// Workspaces root must NOT have any backup-managed files.
	for _, leaked := range []string{".gitignore", ".gitattributes", ".git"} {
		if _, err := os.Stat(filepath.Join(wsRoot, leaked)); err == nil {
			t.Errorf("backup left %s in the workspaces root despite never being enabled", leaked)
		}
	}
}

// TestFirstRun_CloneFailurePreservesURL pins the round-trip: a user
// who picks option [3], types a URL, but whose URL fails the
// connection test should land on an empty folder with the URL still
// recorded in config (backup off). Re-opening the Backup tab shows
// the URL pre-filled for retry.
func TestFirstRun_CloneFailurePreservesURL(t *testing.T) {
	tmp := t.TempDir()
	wsRoot := filepath.Join(tmp, "ws")
	redirectConfig(t, filepath.Join(tmp, "cfg"))

	// Drive the fallback path directly by feeding the model a
	// failing clone result. We bypass the live network so the test
	// is hermetic.
	m := newFirstRun()
	m.input.SetValue(wsRoot)
	m.root = wsRoot
	m.cloneURL = "https://invalid.example.test/repo.git"
	m.method = firstRunMethodClone
	_, cmd := m.Update(firstRunCloneResultMsg{
		err: fmt.Errorf("remote repository is not reachable"),
	})
	if cmd == nil {
		t.Fatal("clone failure should still produce a finalize Cmd (fallback to empty)")
	}
	msg := runCmd(t, cmd)
	if _, ok := msg.(screenDoneMsg); !ok {
		t.Fatalf("expected screenDoneMsg from fallback finalize, got %T", msg)
	}

	cfg, err := launcher.LoadConfig()
	if err != nil || cfg == nil {
		t.Fatalf("LoadConfig: %v %v", err, cfg)
	}
	if cfg.BackupEnabled {
		t.Error("BackupEnabled must remain false when the clone failed")
	}
	if cfg.BackupRemoteURL != "https://invalid.example.test/repo.git" {
		t.Errorf("BackupRemoteURL should be preserved for retry, got %q", cfg.BackupRemoteURL)
	}
}

func TestFirstRun_MethodCursorNavigation(t *testing.T) {
	// The first-run wizard's method picker is a 3-row list (empty /
	// samples / clone). The default cursor seeds on "samples" (the
	// recommended choice); arrows move it; the number keys also
	// jump directly.
	tmp := t.TempDir()
	redirectConfig(t, filepath.Join(tmp, "cfg"))

	m := newFirstRun()
	m.input.SetValue(filepath.Join(tmp, "ws"))
	nx, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(firstRunModel)
	if m.cursor != int(firstRunMethodSamples) {
		t.Errorf("cursor should default to %d (samples), got %d",
			firstRunMethodSamples, m.cursor)
	}
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = nx.(firstRunModel)
	if m.cursor != int(firstRunMethodEmpty) {
		t.Errorf("up should move cursor to %d (empty), got %d",
			firstRunMethodEmpty, m.cursor)
	}
}

func TestChatListModel_EmptyRoot_ShowsNewChatEntry(t *testing.T) {
	tmp := seededRoot(t)
	redirectConfig(t, t.TempDir())

	cfg := &launcher.Config{WorkspacesRoot: tmp}
	m := newChatListModel(cfg)
	loaded := runCmd(t, m.Init())
	next, _ := m.Update(loaded)
	m = next.(chatListModel)

	if len(m.items) != 0 {
		t.Errorf("expected 0 chats in a freshly-seeded root, got %d", len(m.items))
	}
	view := m.View()
	if !strings.Contains(view, "+ new chat") {
		t.Errorf("home should expose '+ new chat'; got:\n%s", view)
	}
}

func TestChatListModel_EnterOnNewOpensAgentsPane(t *testing.T) {
	tmp := seededRoot(t)
	redirectConfig(t, t.TempDir())
	cfg := &launcher.Config{WorkspacesRoot: tmp}
	m := newChatListModel(cfg)
	next, _ := m.Update(runCmd(t, m.Init()))
	m = next.(chatListModel)
	// Cursor is at 0 == "+ new chat" since items is empty.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter on '+ new chat' should produce a Cmd")
	}
	done := runCmd(t, cmd).(screenDoneMsg)
	if _, ok := done.next.(recipesModel); !ok {
		t.Errorf("expected recipesModel, got %T", done.next)
	}
}

func TestChatListModel_HelpAdaptsToCursor(t *testing.T) {
	tmp := seededRoot(t)
	redirectConfig(t, t.TempDir())
	cfg := &launcher.Config{WorkspacesRoot: tmp}
	tpl, _ := launcher.LoadTemplate(tmp, "reversing")
	_, _ = launcher.CreateChat(tmp, *tpl, "ctxhelp", launcher.AgentClaude)

	m := newChatListModel(cfg)
	next, _ := m.Update(runCmd(t, m.Init()))
	m = next.(chatListModel)

	// Cursor on a real chat → chat-only keys should appear. Ollama/agent
	// were moved into the settings menu so they no longer appear here.
	help := chatListHelp(m)
	for _, key := range []string{"e settings", "d delete", "f files"} {
		if !strings.Contains(help, key) {
			t.Errorf("on a real chat the help should mention %q; got:\n%s", key, help)
		}
	}

	// Move to the "+ new chat" row → chat-only keys should disappear.
	nx, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = nx.(chatListModel)
	help = chatListHelp(m)
	for _, key := range []string{"e settings", "d delete", "f files"} {
		if strings.Contains(help, key) {
			t.Errorf("on extra row the help should NOT mention %q; got:\n%s", key, help)
		}
	}
	if !strings.Contains(help, "enter new chat") {
		t.Errorf("on the new-chat row, help should explain Enter's effect:\n%s", help)
	}
}

func TestChatListModel_EnterOnExistingGoesToLaunchingScreen(t *testing.T) {
	tmp := seededRoot(t)
	redirectConfig(t, t.TempDir())
	cfg := &launcher.Config{WorkspacesRoot: tmp}
	tpl, _ := launcher.LoadTemplate(tmp, "reversing")
	_, err := launcher.CreateChat(tmp, *tpl, "direct-launch", launcher.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	m := newChatListModel(cfg)
	next, _ := m.Update(runCmd(t, m.Init()))
	m = next.(chatListModel)
	if len(m.items) != 1 {
		t.Fatalf("expected 1 chat, got %d", len(m.items))
	}

	// Enter must transition to the launching screen — never to agentsModel
	// directly (that's the bug we removed).
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter on existing chat should produce a Cmd")
	}
	done := runCmd(t, cmd).(screenDoneMsg)
	if _, ok := done.next.(launchingModel); !ok {
		t.Errorf("expected launchingModel after Enter, got %T", done.next)
	}
}

func TestChatListModel_OKeyNoLongerOpensOllama(t *testing.T) {
	// Ollama / local-endpoint config moved into the settings menu
	// (`e` key → "Local endpoint" row). Pressing `o` on the chat list
	// should be a no-op now (or fall through to the layout's own `o`
	// handler if one exists — there isn't one today).
	tmp := seededRoot(t)
	redirectConfig(t, t.TempDir())
	cfg := &launcher.Config{WorkspacesRoot: tmp}
	tpl, _ := launcher.LoadTemplate(tmp, "reversing")
	_, _ = launcher.CreateChat(tmp, *tpl, "ollama-test", launcher.AgentClaude)

	m := newChatListModel(cfg)
	next, _ := m.Update(runCmd(t, m.Init()))
	m = next.(chatListModel)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if cmd == nil {
		return // no-op — what we want
	}
	msg := runCmd(t, cmd)
	if done, ok := msg.(screenDoneMsg); ok {
		if _, isOllama := done.next.(ollamaModel); isOllama {
			t.Errorf("`o` on chat list still opens ollama screen — should be moved into settings menu")
		}
	}
}

func TestChatListModel_EKeyOpensSettings(t *testing.T) {
	tmp := seededRoot(t)
	redirectConfig(t, t.TempDir())
	cfg := &launcher.Config{WorkspacesRoot: tmp}
	tpl, _ := launcher.LoadTemplate(tmp, "reversing")
	_, _ = launcher.CreateChat(tmp, *tpl, "settings-test", launcher.AgentClaude)

	m := newChatListModel(cfg)
	next, _ := m.Update(runCmd(t, m.Init()))
	m = next.(chatListModel)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if cmd == nil {
		t.Fatal("'e' should open settings screen")
	}
	done := runCmd(t, cmd).(screenDoneMsg)
	if _, ok := done.next.(settingsModel); !ok {
		t.Errorf("expected settingsModel, got %T", done.next)
	}
}

func TestChatListModel_AKeyDoesNotOpenAgentsPicker(t *testing.T) {
	// The per-chat agent picker was moved into the settings menu
	// (`e` key) — pressing `a` on the chat list should NOT open it
	// anymore. The layout-level `a` handler still navigates to the
	// (install-only) Agents tab; that's tested elsewhere.
	tmp := seededRoot(t)
	redirectConfig(t, t.TempDir())
	cfg := &launcher.Config{WorkspacesRoot: tmp}
	tpl, _ := launcher.LoadTemplate(tmp, "reversing")
	_, _ = launcher.CreateChat(tmp, *tpl, "agents-test", launcher.AgentClaude)

	m := newChatListModel(cfg)
	next, _ := m.Update(runCmd(t, m.Init()))
	m = next.(chatListModel)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd == nil {
		return // no-op — what we want
	}
	msg := runCmd(t, cmd)
	if done, ok := msg.(screenDoneMsg); ok {
		if _, isOverride := done.next.(agentsModel); isOverride {
			t.Errorf("`a` on chat list still opens per-chat agent picker — should be moved to settings")
		}
	}
}

func TestChatListModel_DeleteFlowConfirmsAndRemoves(t *testing.T) {
	tmp := seededRoot(t)
	redirectConfig(t, t.TempDir())
	cfg := &launcher.Config{WorkspacesRoot: tmp}
	tpl, _ := launcher.LoadTemplate(tmp, "reversing")
	_, err := launcher.CreateChat(tmp, *tpl, "throwaway", launcher.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	m := newChatListModel(cfg)
	next, _ := m.Update(runCmd(t, m.Init()))
	m = next.(chatListModel)
	if len(m.items) != 1 {
		t.Fatalf("expected 1 chat, got %d", len(m.items))
	}

	// 'd' → confirm prompt
	nx, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = nx.(chatListModel)
	if !m.deleteAsk {
		t.Fatal("'d' should ask for confirmation")
	}
	// 'y' → actually delete + refresh
	nx, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = nx.(chatListModel)
	if cmd == nil {
		t.Fatal("confirm should trigger reload Cmd")
	}
	runCmd(t, cmd)
	chats, _ := launcher.ListChats(tmp)
	if len(chats) != 0 {
		t.Errorf("expected chat deleted, got %d remaining", len(chats))
	}
}

func TestAgentsScreen_EnterOnAvailableProducesLaunch(t *testing.T) {
	tmp := seededRoot(t)
	redirectConfig(t, t.TempDir())
	tpl, _ := launcher.LoadTemplate(tmp, "reversing")
	chat, err := launcher.CreateChat(tmp, *tpl, "launch-test", launcher.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &launcher.Config{WorkspacesRoot: tmp}
	ws := chat.AsWorkspace()
	m := newAgentsModel(cfg, ws)
	m.items = []launcher.Agent{
		{
			ID: launcher.AgentClaude, Label: "Claude Code (fake)",
			Binary: fakeCmd(), WpcTarget: "claude", Available: true, Version: "fake",
		},
	}
	m.loading = false

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
}

func TestAgentsScreen_OverridePersistsAgentSwap(t *testing.T) {
	// Per-chat agent override: when the picker is opened on an
	// existing chat (newAgentsModelForChatOverride) and the user
	// picks a *different* agent, the new agent must be written into
	// chat.json so the swap survives a restart.
	tmp := seededRoot(t)
	redirectConfig(t, t.TempDir())
	tpl, _ := launcher.LoadTemplate(tmp, "reversing")
	chat, err := launcher.CreateChat(tmp, *tpl, "swap-test", launcher.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &launcher.Config{WorkspacesRoot: tmp}

	m := newAgentsModelForChatOverride(cfg, chat)
	// Hand the model the agent list directly so Update treats it as
	// already loaded. Cursor points at Codex (the swap target).
	m.items = []launcher.Agent{
		{ID: launcher.AgentClaude, Label: "Claude (fake)", Binary: fakeCmd(),
			WpcTarget: "claude", Available: true, Version: "fake"},
		{ID: launcher.AgentCodex, Label: "Codex (fake)", Binary: fakeCmd(),
			WpcTarget: "codex", Available: true, Version: "fake"},
	}
	m.loading = false
	m.cursor = 1 // Codex

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := runCmd(t, cmd)
	done, ok := msg.(screenDoneMsg)
	if !ok {
		t.Fatalf("expected screenDoneMsg, got %T", msg)
	}
	if _, isLaunching := done.next.(launchingModel); !isLaunching {
		t.Errorf("override Enter should route through launchingModel, got %T", done.next)
	}

	// And the chat manifest on disk should now say Codex.
	reloaded, err := launcher.LoadChat(tmp, chat.ID)
	if err != nil || reloaded == nil {
		t.Fatalf("LoadChat err: %v", err)
	}
	if reloaded.AgentID != launcher.AgentCodex {
		t.Errorf("chat.AgentID = %q, want %q (override didn't persist)",
			reloaded.AgentID, launcher.AgentCodex)
	}
}

// TestAgentsScreen_CursorAgreesWithSortedRender locks in the
// chat-agent-swap bug fix. When DetectAgents returns the canonical
// order (claude, codex, opencode, gemini, deepseek) but some agents
// are unavailable, the picker sorts available-first BEFORE seating
// the cursor. The Enter handler must resolve to the agent the user
// visually picked — not to whatever sat at that index in the
// unsorted detection order.
//
// Pre-fix bug: Body() sorted at render time; m.cursor + Enter
// resolved against unsorted m.items → user clicked an installed
// agent, got the install screen for a different (uninstalled) one.
func TestAgentsScreen_CursorAgreesWithSortedRender(t *testing.T) {
	tmp := seededRoot(t)
	redirectConfig(t, t.TempDir())
	tpl, _ := launcher.LoadTemplate(tmp, "reversing")
	chat, _ := launcher.CreateChat(tmp, *tpl, "swap-sorted", launcher.AgentClaude)
	cfg := &launcher.Config{WorkspacesRoot: tmp}

	m := newAgentsModelForChatOverride(cfg, chat)
	// Mid-list available agent: in detection order [claude(✓), codex(✗),
	// opencode(✓), gemini(✗), deepseek(✗)]. After available-first sort
	// the user expects [claude, opencode, codex, gemini, deepseek].
	loaded := agentsLoadedMsg{items: []launcher.Agent{
		{ID: launcher.AgentClaude, Label: "Claude", Binary: fakeCmd(),
			WpcTarget: "claude", Available: true, Version: "fake"},
		{ID: launcher.AgentCodex, Label: "Codex", Binary: "codex",
			WpcTarget: "codex", Available: false},
		{ID: launcher.AgentOpenCode, Label: "OpenCode", Binary: fakeCmd(),
			WpcTarget: "opencode", Available: true, Version: "fake"},
	}}
	nx, _ := m.Update(loaded)
	m = nx.(agentsModel)

	// After load: cursor must seed on Claude (override.current) at
	// index 0 of the sorted slice.
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (Claude is current and now at sorted index 0)", m.cursor)
	}
	if m.items[0].ID != launcher.AgentClaude {
		t.Errorf("items[0] = %s, want claude", m.items[0].ID)
	}
	// Available-first: index 1 must be OpenCode (the other available),
	// not Codex (canonical-order neighbor but unavailable).
	if m.items[1].ID != launcher.AgentOpenCode {
		t.Fatalf("items[1] = %s, want opencode after available-first sort", m.items[1].ID)
	}

	// User arrows down to index 1 and hits Enter — they SHOULD launch
	// OpenCode, not be sent to the Codex install screen.
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = nx.(agentsModel)
	if m.cursor != 1 {
		t.Fatalf("after Down, cursor = %d, want 1", m.cursor)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := runCmd(t, cmd)
	done, ok := msg.(screenDoneMsg)
	if !ok {
		t.Fatalf("expected screenDoneMsg, got %T (would mean Enter routed to install instead of swap+launch)", msg)
	}
	if _, isLaunching := done.next.(launchingModel); !isLaunching {
		t.Errorf("Enter on available OpenCode should route through launchingModel, got %T", done.next)
	}
	// And the chat manifest now says opencode, not codex.
	reloaded, _ := launcher.LoadChat(tmp, chat.ID)
	if reloaded.AgentID != launcher.AgentOpenCode {
		t.Errorf("chat.AgentID = %q, want opencode", reloaded.AgentID)
	}
}

func TestAgentsScreen_EnterOnUnavailableRoutesToInstall(t *testing.T) {
	tmp := seededRoot(t)
	redirectConfig(t, t.TempDir())
	tpl, _ := launcher.LoadTemplate(tmp, "reversing")
	chat, _ := launcher.CreateChat(tmp, *tpl, "install-test", launcher.AgentCodex)
	cfg := &launcher.Config{WorkspacesRoot: tmp}
	m := newAgentsModel(cfg, chat.AsWorkspace())
	m.items = []launcher.Agent{
		{ID: launcher.AgentCodex, Label: "Codex", Binary: "codex", WpcTarget: "codex", Available: false},
	}
	m.loading = false

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("greyed agent + Enter should route to install screen")
	}
	done := runCmd(t, cmd).(screenDoneMsg)
	if _, ok := done.next.(installModel); !ok {
		t.Errorf("expected installModel, got %T", done.next)
	}
}

func TestRootModel_LaunchReturnsExecProcessCmd(t *testing.T) {
	// rootModel.Update no longer quits on a launch — it returns a
	// tea.ExecProcess command that runs the agent inside the running
	// Bubbletea program (stay-in-PrAImate). We just confirm cfg gets
	// persisted onto the rootModel and a non-nil Cmd is returned.
	root := &rootModel{screen: newFirstRun()}
	plan := launcher.LaunchPlan{Command: fakeCmd(), Args: fakeArgs(), Dir: t.TempDir()}
	cfg := &launcher.Config{WorkspacesRoot: "/tmp/x", LastAgent: "codex"}

	_, cmd := root.Update(screenDoneMsg{launch: &plan, updateCfg: cfg})
	if cmd == nil {
		t.Fatal("expected a Cmd (tea.ExecProcess)")
	}
	if root.cfg == nil || root.cfg.LastAgent != "codex" {
		t.Errorf("root.cfg not updated: %+v", root.cfg)
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

func TestBuildAgentCmd_PreservesPlan(t *testing.T) {
	plan := launcher.LaunchPlan{
		Command: fakeCmd(),
		Args:    fakeArgs(),
		Dir:     t.TempDir(),
		Env:     map[string]string{"X_CLADE_TEST": "1"},
	}
	cmd := buildAgentCmd(plan)
	if cmd.Path == "" || filepath.Base(cmd.Path) != filepath.Base(plan.Command) {
		t.Errorf("cmd.Path = %q, want a path to %q", cmd.Path, plan.Command)
	}
	if cmd.Dir != plan.Dir {
		t.Errorf("cmd.Dir = %q, want %q", cmd.Dir, plan.Dir)
	}
	gotEnv := false
	for _, kv := range cmd.Env {
		if kv == "X_CLADE_TEST=1" {
			gotEnv = true
			break
		}
	}
	if !gotEnv {
		t.Error("plan.Env not merged into cmd.Env")
	}
}

func TestExtractExitCode(t *testing.T) {
	if extractExitCode(nil) != 0 {
		t.Error("nil err → 0")
	}
}

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
