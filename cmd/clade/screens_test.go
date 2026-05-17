package main

// Integration tests that drive every Bubble Tea screen with synthetic
// messages, verifying transitions end-to-end without needing a TTY.
// Rewritten for the chats/templates split (v0.2): the home screen is
// chatListModel, new chats clone from templates via newPickTemplateModel.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sPROFFEs/Clade/internal/launcher"
)

// repoRoot is two dirs above the package dir (cmd/clade).
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

	// step 0 → 1: Enter advances from path input to the seed prompt.
	nx, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(firstRunModel)
	if m.step != firstRunStepSeed {
		t.Fatalf("step = %d, want %d (seed)", m.step, firstRunStepSeed)
	}
	if !strings.Contains(m.View(), "Seed example templates") {
		t.Errorf("step 1 view missing seed prompt:\n%s", m.View())
	}

	// step 1 → done: 'y' seeds + saves config + transitions.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd == nil {
		t.Fatal("'y' should produce a finalize Cmd")
	}
	done := runCmd(t, cmd).(screenDoneMsg)
	if _, ok := done.next.(chatListModel); !ok {
		t.Errorf("expected chatListModel, got %T", done.next)
	}

	tpls, err := launcher.ListTemplates(wsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(tpls) < 2 {
		t.Errorf("expected at least 2 seeded templates, got %d", len(tpls))
	}
}

func TestFirstRun_NoSkipsSeedingAndCreatesEmptyDirs(t *testing.T) {
	tmp := t.TempDir()
	wsRoot := filepath.Join(tmp, "ws")
	redirectConfig(t, filepath.Join(tmp, "cfg"))
	t.Chdir(repoRoot(t))

	m := newFirstRun()
	m.input.SetValue(wsRoot)
	nx, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(firstRunModel)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if cmd == nil {
		t.Fatal("'n' should also produce a finalize Cmd")
	}
	done := runCmd(t, cmd).(screenDoneMsg)
	if _, ok := done.next.(chatListModel); !ok {
		t.Errorf("expected chatListModel, got %T", done.next)
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

func TestFirstRun_SpaceTogglesSeedChoice(t *testing.T) {
	tmp := t.TempDir()
	redirectConfig(t, filepath.Join(tmp, "cfg"))

	m := newFirstRun()
	m.input.SetValue(filepath.Join(tmp, "ws"))
	nx, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(firstRunModel)
	if !m.seed {
		t.Fatal("seed should default to true (recommended)")
	}
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = nx.(firstRunModel)
	if m.seed {
		t.Error("space should toggle seed off")
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

func TestChatListModel_EnterOnNewOpensTemplatePicker(t *testing.T) {
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
	if _, ok := done.next.(pickTemplateModel); !ok {
		t.Errorf("expected pickTemplateModel, got %T", done.next)
	}
}

func TestChatListModel_ManageTemplatesRowIsVisibleAndOpensList(t *testing.T) {
	// Empty root → cursor 0 = "+ new chat", cursor 1 = "Manage templates".
	tmp := seededRoot(t)
	redirectConfig(t, t.TempDir())
	cfg := &launcher.Config{WorkspacesRoot: tmp}
	m := newChatListModel(cfg)
	next, _ := m.Update(runCmd(t, m.Init()))
	m = next.(chatListModel)
	if !strings.Contains(m.View(), "Manage templates") {
		t.Errorf("home should always show 'Manage templates' row:\n%s", m.View())
	}

	// down → cursor on Manage templates → enter → templateListModel.
	nx, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = nx.(chatListModel)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter on Manage templates should produce a Cmd")
	}
	done := runCmd(t, cmd).(screenDoneMsg)
	if _, ok := done.next.(templateListModel); !ok {
		t.Errorf("expected templateListModel, got %T", done.next)
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

	// Cursor on a real chat → e/f/o/a/d should appear.
	help := chatListHelp(m)
	for _, key := range []string{"e settings", "d delete", "o ollama", "f files"} {
		if !strings.Contains(help, key) {
			t.Errorf("on a real chat the help should mention %q; got:\n%s", key, help)
		}
	}

	// Move to "Manage templates" row → chat-only keys should disappear.
	for i := 0; i < 2; i++ {
		nx, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = nx.(chatListModel)
	}
	help = chatListHelp(m)
	for _, key := range []string{"e settings", "d delete", "o ollama", "f files"} {
		if strings.Contains(help, key) {
			t.Errorf("on extra row the help should NOT mention %q; got:\n%s", key, help)
		}
	}
	if !strings.Contains(help, "enter manage templates") {
		t.Errorf("on Manage templates row, help should explain Enter's effect:\n%s", help)
	}
}

func TestChatListModel_TKeyOpensTemplateList(t *testing.T) {
	tmp := seededRoot(t)
	redirectConfig(t, t.TempDir())
	cfg := &launcher.Config{WorkspacesRoot: tmp}
	m := newChatListModel(cfg)
	next, _ := m.Update(runCmd(t, m.Init()))
	m = next.(chatListModel)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if cmd == nil {
		t.Fatal("'t' should open template list")
	}
	done := runCmd(t, cmd).(screenDoneMsg)
	if _, ok := done.next.(templateListModel); !ok {
		t.Errorf("expected templateListModel, got %T", done.next)
	}
}

func TestNewChatFromTemplate_LabelPlusAgentCreatesChat(t *testing.T) {
	tmp := seededRoot(t)
	redirectConfig(t, t.TempDir())
	cfg := &launcher.Config{WorkspacesRoot: tmp}

	tpl, err := launcher.LoadTemplate(tmp, "reversing")
	if err != nil || tpl == nil {
		t.Fatalf("LoadTemplate: %v %v", err, tpl)
	}
	m := newNewChatFromTemplateModel(cfg, *tpl)
	m.label.SetValue("integration-chat")

	// step 0 → 1 (Enter triggers agent detection)
	nx, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(newChatFromTemplateModel)
	if cmd == nil {
		t.Fatal("expected agent-detect Cmd")
	}
	// Skip the real detection: inject a known-available fake.
	m.agents = []launcher.Agent{
		{ID: launcher.AgentClaude, Label: "Claude", Binary: fakeCmd(), WpcTarget: "claude", Available: true},
	}

	// Step 1 (agent) → Enter advances to step 2 (Ollama y/n), it does
	// NOT immediately spawn the create-chat Cmd anymore.
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(newChatFromTemplateModel)
	if m.step != 2 {
		t.Fatalf("after picking agent, step = %d, want 2 (ollama)", m.step)
	}
	// Pick "no" to launch immediately without going through the Ollama wizard.
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if cmd == nil {
		t.Fatal("'n' on Ollama step should produce a finalize Cmd")
	}
	msg := runCmd(t, cmd)
	done, ok := msg.(screenDoneMsg)
	if !ok {
		t.Fatalf("expected screenDoneMsg, got %T %+v", msg, msg)
	}
	if done.launch == nil {
		t.Fatalf("expected launch plan after new-chat (Ollama=no), got next=%T", done.next)
	}
	if done.launchedWS == nil {
		t.Fatal("expected launchedWS to be set so main() can sync MEMORY.md back")
	}

	chats, err := launcher.ListChats(tmp)
	if err != nil || len(chats) != 1 {
		t.Fatalf("ListChats: chats=%+v err=%v", chats, err)
	}
	if chats[0].Label != "integration-chat" {
		t.Errorf("label = %q", chats[0].Label)
	}
	if chats[0].Template != "reversing" {
		t.Errorf("template = %q", chats[0].Template)
	}
	if chats[0].AgentID != launcher.AgentClaude {
		t.Errorf("agent = %q", chats[0].AgentID)
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

func TestChatListModel_OKeyOpensOllama(t *testing.T) {
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
		t.Fatal("'o' should open Ollama screen for the highlighted chat")
	}
	done := runCmd(t, cmd).(screenDoneMsg)
	if _, ok := done.next.(ollamaModel); !ok {
		t.Errorf("expected ollamaModel, got %T", done.next)
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

func TestChatListModel_AKeyOpensAgentsPicker(t *testing.T) {
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
		t.Fatal("'a' should open agents picker")
	}
	done := runCmd(t, cmd).(screenDoneMsg)
	if _, ok := done.next.(agentsModel); !ok {
		t.Errorf("expected agentsModel via 'a', got %T", done.next)
	}
}

func TestNewChatFromTemplate_OllamaYesRoutesToOllamaScreen(t *testing.T) {
	tmp := seededRoot(t)
	redirectConfig(t, t.TempDir())
	cfg := &launcher.Config{WorkspacesRoot: tmp}
	tpl, _ := launcher.LoadTemplate(tmp, "reversing")

	m := newNewChatFromTemplateModel(cfg, *tpl)
	m.label.SetValue("with-ollama")
	// step 0 → 1
	nx, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(newChatFromTemplateModel)
	m.agents = []launcher.Agent{
		{ID: launcher.AgentClaude, Binary: fakeCmd(), WpcTarget: "claude", Available: true},
	}
	// step 1 → 2
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(newChatFromTemplateModel)
	if m.step != 2 {
		t.Fatalf("step = %d, want 2", m.step)
	}
	// 'y' triggers finalize → returns screenDoneMsg with next=ollamaModel
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd == nil {
		t.Fatal("'y' should produce a finalize Cmd")
	}
	done := runCmd(t, cmd).(screenDoneMsg)
	if done.launch != nil {
		t.Errorf("'y' should NOT launch directly — it should route to Ollama screen first; got launch=%+v", done.launch)
	}
	if _, ok := done.next.(ollamaModel); !ok {
		t.Errorf("expected ollamaModel as the next screen, got %T", done.next)
	}
	// Chat was still created so the Ollama screen has a real ws to save into.
	chats, _ := launcher.ListChats(tmp)
	if len(chats) != 1 || chats[0].Label != "with-ollama" {
		t.Errorf("expected the chat to be created before the Ollama screen; got %+v", chats)
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

func TestRootModel_RoutesScreensAndCapturesLaunch(t *testing.T) {
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
	plan := launcher.LaunchPlan{Command: fakeCmd(), Args: fakeArgs(), Dir: t.TempDir()}
	if code := execAgent(plan); code != 0 {
		t.Errorf("execAgent returned %d for a successful command", code)
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
