package main

import (
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sdksdk/code-launcher/internal/launcher"
)

func TestSettingsScreen_FullFlowRoundTripsOnTemplate(t *testing.T) {
	tmp := t.TempDir()
	redirectConfig(t, filepath.Join(tmp, "cfg"))
	t.Chdir(repoRoot(t))
	if _, err := launcher.SeedSamples(tmp, []string{filepath.Join(repoRoot(t), "samples", "workpaths")}); err != nil {
		t.Fatal(err)
	}
	tpl, err := launcher.LoadTemplate(tmp, "reversing")
	if err != nil || tpl == nil {
		t.Fatalf("LoadTemplate: %v %v", err, tpl)
	}
	cfg := &launcher.Config{WorkspacesRoot: tmp}

	// settingsModel takes a Workspace; the smart saver routes to
	// template.json because the Root is under <root>/templates/.
	ws := launcher.Workspace{
		Name: tpl.Name, Root: tpl.Root, WorkpathDir: tpl.WorkpathDir,
		Description: tpl.Description, Settings: tpl.Settings,
	}
	m := newSettingsModel(cfg, ws)

	m.language.SetValue("Spanish")
	nx, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(settingsModel)
	if m.step != 1 {
		t.Fatalf("after lang step = %d", m.step)
	}
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = nx.(settingsModel)
	if !m.memory || m.step != 2 {
		t.Fatalf("after memory: mem=%v step=%d", m.memory, m.step)
	}
	m.skillInput.SetValue("https://example.com/a.git")
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(settingsModel)
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(settingsModel)
	if m.step != 3 {
		t.Fatalf("after save step = %d, want 3", m.step)
	}
	if m.err != "" {
		t.Errorf("unexpected err: %s", m.err)
	}

	// Settings written to template.json (not workspace.json) because of
	// the smart saver.
	loaded, _ := launcher.LoadTemplate(tmp, "reversing")
	if loaded.Settings.Language != "Spanish" {
		t.Errorf("Language = %q", loaded.Settings.Language)
	}
	if !loaded.Settings.MemoryEnabled {
		t.Error("MemoryEnabled not saved")
	}
	if len(loaded.Settings.OnlineSkills) != 1 {
		t.Errorf("OnlineSkills = %v", loaded.Settings.OnlineSkills)
	}

	// Enter on the done screen returns to chat list.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	done := runCmd(t, cmd).(screenDoneMsg)
	if _, ok := done.next.(chatListModel); !ok {
		t.Errorf("expected chatListModel after settings done, got %T", done.next)
	}
}

func TestTemplateListScreen_EnterOpensSettings(t *testing.T) {
	tmp := t.TempDir()
	redirectConfig(t, filepath.Join(tmp, "cfg"))
	t.Chdir(repoRoot(t))
	_, _ = launcher.SeedSamples(tmp, []string{filepath.Join(repoRoot(t), "samples", "workpaths")})

	cfg := &launcher.Config{WorkspacesRoot: tmp}
	m := newTemplateListModel(cfg)
	loaded := runCmd(t, m.Init())
	next, _ := m.Update(loaded)
	m = next.(templateListModel)
	if len(m.items) < 1 {
		t.Fatal("no template items")
	}

	// Cursor is at 0 (first template), Enter opens its settings screen.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter on a template row should open settings")
	}
	done := runCmd(t, cmd).(screenDoneMsg)
	if _, ok := done.next.(settingsModel); !ok {
		t.Errorf("expected settingsModel, got %T", done.next)
	}
}
