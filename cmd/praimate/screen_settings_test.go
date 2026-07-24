package main

import (
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"git.jtsec.local/lab/PrAImate/internal/launcher"
)

func TestSettingsScreen_MenuRoundTripsOnTemplate(t *testing.T) {
	// Settings is now a menu, not a wizard. Drive it like a user would:
	// cursor to language → edit; cursor to memory → toggle with space;
	// cursor to skills → add one URL; Esc to save & return to chat list.
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
	ws := launcher.Workspace{
		Name: tpl.Name, Root: tpl.Root, WorkpathDir: tpl.WorkpathDir,
		Description: tpl.Description, Settings: tpl.Settings,
	}
	m := newSettingsModel(cfg, ws)

	// Cursor on Language (index 0) — Enter to open the editor, type a
	// value, Enter to accept.
	nx, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(settingsModel)
	if m.mode != settingsModeEditLanguage {
		t.Fatalf("mode = %d, want EditLanguage", m.mode)
	}
	m.textInput.SetValue("Spanish")
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(settingsModel)
	if m.mode != settingsModeList {
		t.Fatalf("mode = %d, want List after lang accept", m.mode)
	}
	if m.language != "Spanish" {
		t.Errorf("language = %q, want Spanish", m.language)
	}

	// Down to Memory (index 1) and toggle with space.
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = nx.(settingsModel)
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = nx.(settingsModel)
	if !m.memory {
		t.Errorf("memory should be on after space, got %v", m.memory)
	}

	// Down to Skills (lang, mem, primer, mirror, agent, endpoint,
	// skills — five downs from mem).
	for i := 0; i < 5; i++ {
		nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = nx.(settingsModel)
	}
	if settingsItem(m.cursor) != settingsItemSkills {
		t.Fatalf("cursor = %d, want skills (%d)", m.cursor, settingsItemSkills)
	}
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(settingsModel)
	m.skillInput.SetValue("https://example.com/a.git")
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(settingsModel)
	if len(m.skills) != 1 {
		t.Errorf("skills = %v, want one entry", m.skills)
	}
	// Blank Enter exits skills editor.
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(settingsModel)
	if m.mode != settingsModeList {
		t.Fatalf("mode after blank-enter = %d, want List", m.mode)
	}

	// Esc on the list saves and returns to chat list.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	done := runCmd(t, cmd).(screenDoneMsg)
	if _, ok := done.next.(chatListModel); !ok {
		t.Errorf("expected chatListModel after Esc, got %T", done.next)
	}

	// Persisted to template.json via the smart saver.
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
}
