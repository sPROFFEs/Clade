package main

// Integration tests that drive every Bubble Tea screen with synthetic
// messages, verifying transitions end-to-end without needing a TTY.
// Rewritten for the chats/templates split (v0.2): the home screen is
// chatListModel, new chats clone from templates via newPickTemplateModel.

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

func TestFirstRun_EnterSeedsTemplatesAndAdvances(t *testing.T) {
	tmp := t.TempDir()
	wsRoot := filepath.Join(tmp, "ws")
	redirectConfig(t, filepath.Join(tmp, "cfg"))
	t.Chdir(repoRoot(t))

	m := newFirstRun()
	m.input.SetValue(wsRoot)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("firstRun Enter should return a Cmd")
	}
	if !strings.Contains(next.View(), "Seeding samples") {
		t.Errorf("expected status line; got: %s", next.View())
	}

	msg := runCmd(t, cmd)
	done, ok := msg.(screenDoneMsg)
	if !ok {
		t.Fatalf("expected screenDoneMsg, got %T", msg)
	}
	if _, isChatList := done.next.(chatListModel); !isChatList {
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

	nx, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(newChatFromTemplateModel)
	if cmd == nil {
		t.Fatal("expected create-chat Cmd")
	}
	msg := runCmd(t, cmd)
	done, ok := msg.(screenDoneMsg)
	if !ok {
		t.Fatalf("expected screenDoneMsg, got %T %+v", msg, msg)
	}
	if _, ok := done.next.(agentsModel); !ok {
		t.Fatalf("expected agentsModel after chat creation, got %T", done.next)
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
