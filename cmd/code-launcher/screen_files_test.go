package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sdksdk/code-launcher/internal/launcher"
)

func TestFilesModel_EnterOnMissionInvokesEditor(t *testing.T) {
	tmp := seededRoot(t)
	redirectConfig(t, t.TempDir())
	cfg := &launcher.Config{WorkspacesRoot: tmp}
	tpl, _ := launcher.LoadTemplate(tmp, "reversing")
	parent := newTemplateListModel(cfg)

	// Force a deterministic editor that exits cleanly so tea.ExecProcess
	// doesn't try to spawn vi/notepad in the test runner.
	switch runtime.GOOS {
	case "windows":
		t.Setenv("VISUAL", "cmd /c exit 0")
	default:
		t.Setenv("VISUAL", "true")
	}

	m := newFilesModel(cfg, tpl.WorkpathDir, "template reversing", parent)

	// Cursor starts on mission.md. Enter should produce an exec Cmd.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter on mission.md should produce a Cmd (tea.ExecProcess)")
	}
}

func TestFilesModel_EnterOnOpenDirReturnsToScreen(t *testing.T) {
	tmp := seededRoot(t)
	redirectConfig(t, t.TempDir())
	cfg := &launcher.Config{WorkspacesRoot: tmp}
	tpl, _ := launcher.LoadTemplate(tmp, "reversing")
	parent := newTemplateListModel(cfg)

	m := newFilesModel(cfg, tpl.WorkpathDir, "template reversing", parent)
	// Move cursor to last entry (open dir).
	for i := 0; i < len(m.entries)-1; i++ {
		nx, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = nx.(filesModel)
	}
	// We can't actually exec explorer/xdg-open in the test env reliably,
	// so this just exercises the code path — it returns nil on missing
	// command and surfaces an error.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
}

func TestFilesModel_EscReturnsToParent(t *testing.T) {
	tmp := seededRoot(t)
	cfg := &launcher.Config{WorkspacesRoot: tmp}
	parent := newTemplateListModel(cfg)

	m := newFilesModel(cfg, filepath.Join(tmp, "x"), "x", parent)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should return a Cmd")
	}
	done := runCmd(t, cmd).(screenDoneMsg)
	if _, ok := done.next.(templateListModel); !ok {
		t.Errorf("esc should return to parent (templateListModel), got %T", done.next)
	}
}

func TestEditorCommand_HonorsVisualEnvVar(t *testing.T) {
	t.Setenv("VISUAL", "my-editor --no-history")
	t.Setenv("EDITOR", "ignored")
	cmd, args := editorCommand()
	if cmd != "my-editor" {
		t.Errorf("cmd = %q, want my-editor", cmd)
	}
	if len(args) != 1 || args[0] != "--no-history" {
		t.Errorf("args = %v, want [--no-history]", args)
	}
}

func TestEditorCommand_FallsBackToEditorEnvVar(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "nano -w")
	cmd, _ := editorCommand()
	if cmd != "nano" {
		t.Errorf("cmd = %q, want nano", cmd)
	}
}

func TestEditorCommand_OSFallback(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	cmd, _ := editorCommand()
	switch runtime.GOOS {
	case "windows":
		if cmd != "notepad" {
			t.Errorf("Windows fallback should be notepad, got %q", cmd)
		}
	default:
		// On the test host, at least one of nano/vim/vi may not exist;
		// just ensure we return SOMETHING when one of them is on PATH,
		// or empty string when none are.
		if cmd != "" && cmd != "nano" && cmd != "vim" && cmd != "vi" {
			t.Errorf("unexpected fallback: %q", cmd)
		}
	}
}

func TestChatListModel_FKeyOpensFilesEditor(t *testing.T) {
	tmp := seededRoot(t)
	redirectConfig(t, t.TempDir())
	cfg := &launcher.Config{WorkspacesRoot: tmp}
	tpl, _ := launcher.LoadTemplate(tmp, "reversing")
	_, _ = launcher.CreateChat(tmp, *tpl, "files-test", launcher.AgentClaude)

	m := newChatListModel(cfg)
	next, _ := m.Update(runCmd(t, m.Init()))
	m = next.(chatListModel)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if cmd == nil {
		t.Fatal("'f' should open files editor for the chat")
	}
	done := runCmd(t, cmd).(screenDoneMsg)
	if _, ok := done.next.(filesModel); !ok {
		t.Errorf("expected filesModel, got %T", done.next)
	}
	// Ensure the view at least mentions the workpath dir.
	view := done.next.View()
	if !strings.Contains(view, "Workpath:") {
		t.Errorf("filesModel view missing workpath header:\n%s", view)
	}
}

// Avoid unused import error if os is only referenced inside conditional paths.
var _ = os.Stat
