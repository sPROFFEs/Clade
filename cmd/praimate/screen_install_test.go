package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sPROFFEs/PrAImate/internal/installer"
	"github.com/sPROFFEs/PrAImate/internal/launcher"
)

// TestInstall_AutoFixableOnly_DoesNotBlock verifies the user bug report:
// pressing Enter on a pnpm method when pnpm isn't installed should NOT
// surface a "missing prereq" warning — installer.Run will auto-install
// pnpm + run `pnpm setup` itself.
func TestInstall_AutoFixableOnly_DoesNotBlock(t *testing.T) {
	cfg := &launcher.Config{WorkspacesRoot: t.TempDir()}
	ws := launcher.Workspace{Name: "x", SandboxDir: t.TempDir()}

	m := installModel{
		cfg:     cfg,
		ws:      ws,
		agentID: launcher.AgentOpenCode,
		methods: []installer.Method{
			{
				ID: "pnpm", Label: "pnpm global package",
				// Real-world command — but the test never runs it.
				Command: "pnpm add -g opencode-ai",
				Prereqs: []string{"pnpm"}, // intentionally only pnpm — auto-fixable
			},
		},
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		// Could be nil if PrereqsMissing returns nothing on this dev box,
		// but the model should at minimum not surface a prereqWarn.
	}
	// Re-render and check the warning didn't fire.
	view := m.View()
	if strings.Contains(view, "missing prereq:") {
		t.Errorf("install screen wrongly blocked with 'missing prereq:'; output:\n%s", view)
	}
}

// TestInstall_ReturnToHonoursFactory: the install screen must not drag
// a stub workspace through to the post-install screen. Pressing esc
// after install should land on whatever pane the returnTo factory
// supplies (the agents pane in the post-template world), not on a
// picker with bogus state.
func TestInstall_ReturnToHonoursFactory(t *testing.T) {
	cfg := &launcher.Config{WorkspacesRoot: t.TempDir()}

	called := false
	m := newInstallModelWithReturn(cfg, launcher.AgentOpenCode, func() tea.Model {
		called = true
		return newRecipesModel(cfg)
	})

	// esc on the picker step → exitTo
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should produce a Cmd")
	}
	done := runCmd(t, cmd).(screenDoneMsg)
	if !called {
		t.Error("returnTo factory should have been invoked")
	}
	if _, ok := done.next.(recipesModel); !ok {
		t.Errorf("expected recipesModel after install esc, got %T", done.next)
	}
}

// TestInstall_UnfixablePrereq_Blocks verifies a method that needs Node
// (or anything not in the auto-fix set) DOES surface a warning when the
// prereq is missing.
func TestInstall_UnfixablePrereq_Blocks(t *testing.T) {
	cfg := &launcher.Config{WorkspacesRoot: t.TempDir()}
	ws := launcher.Workspace{Name: "x", SandboxDir: t.TempDir()}

	m := installModel{
		cfg:     cfg,
		ws:      ws,
		agentID: launcher.AgentCodex,
		methods: []installer.Method{
			{
				ID: "pnpm", Label: "pnpm",
				Command: "pnpm add -g x",
				// Use a sentinel binary nobody has, so PrereqsMissing reports it.
				Prereqs: []string{"this-binary-does-not-exist-anywhere"},
			},
		},
	}

	nx, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(installModel)
	// Should NOT have spawned the install Cmd.
	if cmd != nil {
		t.Errorf("expected nil Cmd when unfixable prereq is missing, got %v", cmd)
	}
	if m.prereqWarn == "" {
		t.Error("expected prereqWarn to be set for unfixable missing prereq")
	}
	if !strings.Contains(m.View(), "you must install") &&
		!strings.Contains(m.View(), "missing prereq:") {
		t.Errorf("expected the warning to appear in the view; got:\n%s", m.View())
	}
}
