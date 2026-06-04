package workpath

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func scaffoldWorkpathWithHooks(t *testing.T, hooksJSON string) string {
	t.Helper()
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "mission.md"), "Hook test.\n")
	writeTestFile(t, filepath.Join(dir, "workpath.json"), `{"description":"hooks test"}`)
	writeTestFile(t, filepath.Join(dir, "hooks.json"), hooksJSON)
	return dir
}

func TestLoadHooks_HappyPath(t *testing.T) {
	dir := scaffoldWorkpathWithHooks(t, `{
  "hooks": [
    {"event": "pre_tool",      "matcher": "Bash",  "command": "scripts/audit.sh"},
    {"event": "session_start",                       "command": "echo hello"}
  ]
}`)
	wp, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(wp.Hooks) != 2 {
		t.Fatalf("Hooks = %d, want 2", len(wp.Hooks))
	}
	if wp.Hooks[0].Event != HookPreTool || wp.Hooks[0].Matcher != "Bash" {
		t.Errorf("Hooks[0] = %+v", wp.Hooks[0])
	}
	if wp.Hooks[1].Event != HookSessionStart || wp.Hooks[1].Matcher != "" {
		t.Errorf("Hooks[1] = %+v", wp.Hooks[1])
	}
}

func TestLoadHooks_UnknownEventErrors(t *testing.T) {
	dir := scaffoldWorkpathWithHooks(t, `{
  "hooks": [
    {"event": "open_repo_panel", "command": "scripts/x.sh"}
  ]
}`)
	_, err := LoadDir(dir)
	if err == nil {
		t.Fatal("LoadDir should reject unknown event; got nil")
	}
	if !strings.Contains(err.Error(), "open_repo_panel") {
		t.Errorf("error should mention the bad event, got: %v", err)
	}
}

func TestLoadHooks_EmptyCommandErrors(t *testing.T) {
	dir := scaffoldWorkpathWithHooks(t, `{
  "hooks": [
    {"event": "pre_tool", "matcher": "Bash", "command": "   "}
  ]
}`)
	_, err := LoadDir(dir)
	if err == nil {
		t.Fatal("LoadDir should reject empty command; got nil")
	}
}

func TestLoadHooks_MalformedJsonErrors(t *testing.T) {
	dir := scaffoldWorkpathWithHooks(t, `{ "hooks": [ malformed }`)
	_, err := LoadDir(dir)
	if err == nil {
		t.Fatal("LoadDir should reject malformed hooks.json; got nil")
	}
	if !strings.Contains(err.Error(), "hooks.json") {
		t.Errorf("error should mention hooks.json, got: %v", err)
	}
}

func TestLoadHooks_MissingFileIsNoop(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "mission.md"), "no hooks.\n")
	writeTestFile(t, filepath.Join(dir, "workpath.json"), `{"description":"no hooks"}`)
	wp, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(wp.Hooks) != 0 {
		t.Errorf("Hooks should be empty, got %d", len(wp.Hooks))
	}
}

func TestImportHooks_MergedWithCollisionConsumerWins(t *testing.T) {
	root := t.TempDir()
	// Import contributes two hooks: pre_tool/Bash + post_tool/Edit.
	imp := filepath.Join(root, "_common", "foo")
	writeTestFile(t, filepath.Join(imp, "hooks.json"), `{
  "hooks": [
    {"event": "pre_tool",  "matcher": "Bash", "command": "echo imported-bash"},
    {"event": "post_tool", "matcher": "Edit", "command": "echo imported-edit"}
  ]
}`)
	// Consumer has its own pre_tool/Bash hook (should override the import's)
	// and no post_tool/Edit (should pick up the import's).
	consumer := filepath.Join(root, "consumer")
	writeTestFile(t, filepath.Join(consumer, "workpath.json"), `{
  "description":"hook collision test",
  "imports":["_common/foo"]
}`)
	writeTestFile(t, filepath.Join(consumer, "mission.md"), "test.\n")
	writeTestFile(t, filepath.Join(consumer, "hooks.json"), `{
  "hooks": [
    {"event": "pre_tool", "matcher": "Bash", "command": "echo consumer-bash"}
  ]
}`)
	wp, err := LoadDir(consumer)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(wp.Hooks) != 2 {
		t.Fatalf("merged Hooks = %d, want 2", len(wp.Hooks))
	}
	// Find each by event+matcher.
	var bash, edit *Hook
	for i := range wp.Hooks {
		switch {
		case wp.Hooks[i].Event == HookPreTool && wp.Hooks[i].Matcher == "Bash":
			bash = &wp.Hooks[i]
		case wp.Hooks[i].Event == HookPostTool && wp.Hooks[i].Matcher == "Edit":
			edit = &wp.Hooks[i]
		}
	}
	if bash == nil || edit == nil {
		t.Fatalf("expected both hooks present, got %+v", wp.Hooks)
	}
	// Consumer wins on collision.
	if bash.Command != "echo consumer-bash" {
		t.Errorf("pre_tool/Bash should be consumer's; got %q", bash.Command)
	}
	if bash.ImportedFrom != "" {
		t.Errorf("consumer hook should have empty ImportedFrom, got %q", bash.ImportedFrom)
	}
	// Imported one survives when no collision.
	if edit.Command != "echo imported-edit" {
		t.Errorf("post_tool/Edit should be imported's; got %q", edit.Command)
	}
	if edit.ImportedFrom == "" {
		t.Errorf("imported hook should carry ImportedFrom; got empty")
	}
}

// TestClaudeTarget_EmitsSettingsJSONIntegration is in pkg/targets but
// needs the workpath fixture from here; keep the workpath-side check
// targeted to the merged struct.
func TestHookEvents_AllValidatorEntries(t *testing.T) {
	// AllHookEvents must list every named constant so the validator
	// matches the public surface.
	all := []HookEvent{
		HookPreTool, HookPostTool, HookUserInput,
		HookSessionStart, HookSessionStop, HookSubagentStop, HookNotification,
	}
	for _, e := range all {
		if !AllHookEvents[e] {
			t.Errorf("AllHookEvents missing %q", e)
		}
	}
	if len(AllHookEvents) != len(all) {
		t.Errorf("AllHookEvents has %d entries, constants list has %d", len(AllHookEvents), len(all))
	}
}

// Use os.Stat just to anchor the testfile imports.
var _ = os.Stat
