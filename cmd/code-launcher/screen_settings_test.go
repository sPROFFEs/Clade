package main

import (
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sdksdk/code-launcher/internal/launcher"
)

func TestSettingsScreen_FullFlowRoundTrips(t *testing.T) {
	tmp := t.TempDir()
	redirectConfig(t, filepath.Join(tmp, "cfg"))
	t.Chdir(repoRoot(t))
	if _, err := launcher.SeedSamples(tmp, []string{filepath.Join(repoRoot(t), "samples", "workpaths")}); err != nil {
		t.Fatal(err)
	}
	ws, _ := launcher.LoadWorkspace(tmp, "reversing")
	cfg := &launcher.Config{WorkspacesRoot: tmp}

	m := newSettingsModel(cfg, *ws)

	// step 0 — language
	m.language.SetValue("Spanish")
	nx, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(settingsModel)
	if m.step != 1 {
		t.Fatalf("after lang step = %d", m.step)
	}

	// step 1 — memory: y
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = nx.(settingsModel)
	if !m.memory || m.step != 2 {
		t.Fatalf("after memory: mem=%v step=%d", m.memory, m.step)
	}

	// step 2 — add a skill, then blank Enter to save
	m.skillInput.SetValue("https://example.com/a.git")
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(settingsModel)
	if len(m.skills) != 1 {
		t.Fatalf("skills = %v", m.skills)
	}
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(settingsModel)
	if m.step != 3 {
		t.Fatalf("after save step = %d, want 3", m.step)
	}
	if m.err != "" {
		t.Errorf("unexpected err: %s", m.err)
	}

	// Persisted to workspace.json.
	loaded, _ := launcher.LoadWorkspace(tmp, "reversing")
	if loaded.Settings.Language != "Spanish" {
		t.Errorf("Language = %q", loaded.Settings.Language)
	}
	if !loaded.Settings.MemoryEnabled {
		t.Error("MemoryEnabled not saved")
	}
	if len(loaded.Settings.OnlineSkills) != 1 {
		t.Errorf("OnlineSkills = %v", loaded.Settings.OnlineSkills)
	}

	// Enter on the done screen returns to workspaces.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	done := runCmd(t, cmd).(screenDoneMsg)
	if _, ok := done.next.(workspacesModel); !ok {
		t.Errorf("expected workspacesModel after settings, got %T", done.next)
	}
}

func TestWorkspacesScreen_SKeyOpensSettings(t *testing.T) {
	tmp := t.TempDir()
	redirectConfig(t, filepath.Join(tmp, "cfg"))
	t.Chdir(repoRoot(t))
	_, _ = launcher.SeedSamples(tmp, []string{filepath.Join(repoRoot(t), "samples", "workpaths")})

	cfg := &launcher.Config{WorkspacesRoot: tmp}
	m := newWorkspacesModel(cfg)
	loaded := runCmd(t, m.Init())
	next, _ := m.Update(loaded)
	m = next.(workspacesModel)
	if len(m.items) < 1 {
		t.Fatal("no items")
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd == nil {
		t.Fatal("'s' should open settings screen")
	}
	done := runCmd(t, cmd).(screenDoneMsg)
	if _, ok := done.next.(settingsModel); !ok {
		t.Errorf("expected settingsModel, got %T", done.next)
	}
}
