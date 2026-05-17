package launcher

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateTemplate_ScaffoldsAndAppearsInList(t *testing.T) {
	root := t.TempDir()
	tpl, err := CreateTemplate(root, "scratch", "scratch template for the test")
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	for _, f := range []string{"workpath.json", "mission.md", "playbook.md", "rules.md"} {
		if _, err := os.Stat(filepath.Join(tpl.WorkpathDir, f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	got, err := ListTemplates(root)
	if err != nil || len(got) != 1 {
		t.Fatalf("ListTemplates: %v %v", err, got)
	}
}

func TestCreateChat_ClonesWorkpathAndPersistsManifest(t *testing.T) {
	root := t.TempDir()
	tpl, err := CreateTemplate(root, "src-template", "x")
	if err != nil {
		t.Fatal(err)
	}
	// Mutate the template's mission to verify the chat gets the COPY, not a link.
	missionPath := filepath.Join(tpl.WorkpathDir, "mission.md")
	if err := os.WriteFile(missionPath, []byte("ORIGINAL\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	chat, err := CreateChat(root, tpl, "My Chat / Test", AgentCodex)
	if err != nil {
		t.Fatalf("CreateChat: %v", err)
	}
	// ID is timestamp + slugified label.
	if !strings.Contains(chat.ID, "my-chat-test") {
		t.Errorf("ID should contain slug; got %q", chat.ID)
	}
	if chat.AgentID != AgentCodex {
		t.Errorf("AgentID = %q", chat.AgentID)
	}
	if chat.Template != "src-template" {
		t.Errorf("Template = %q", chat.Template)
	}

	// Workpath was actually copied, not symlinked.
	cloned := filepath.Join(chat.WorkpathDir, "mission.md")
	if raw, err := os.ReadFile(cloned); err != nil || string(raw) != "ORIGINAL\n" {
		t.Errorf("chat workpath not cloned correctly: err=%v body=%q", err, raw)
	}
	// Mutate the template AFTER cloning — chat's copy must stay original.
	if err := os.WriteFile(missionPath, []byte("MUTATED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(cloned)
	if string(raw) != "ORIGINAL\n" {
		t.Errorf("chat workpath shouldn't see template mutations; got %q", raw)
	}
}

func TestListChats_SortsByLastUsedDesc(t *testing.T) {
	root := t.TempDir()
	tpl, _ := CreateTemplate(root, "t", "x")
	// Create three chats; touch them in a specific order to vary lastUsed.
	c1, _ := CreateChat(root, tpl, "first", AgentClaude)
	c2, _ := CreateChat(root, tpl, "second", AgentClaude)
	c3, _ := CreateChat(root, tpl, "third", AgentClaude)

	// c2 most recent, then c1, then c3.
	_ = TouchChat(&c3)
	_ = TouchChat(&c1)
	_ = TouchChat(&c2)

	got, err := ListChats(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d chats", len(got))
	}
	if got[0].ID != c2.ID || got[1].ID != c1.ID || got[2].ID != c3.ID {
		t.Errorf("order = [%s %s %s], want [%s %s %s]",
			got[0].ID, got[1].ID, got[2].ID, c2.ID, c1.ID, c3.ID)
	}
}

func TestDeleteChat_RemovesDirEntirely(t *testing.T) {
	root := t.TempDir()
	tpl, _ := CreateTemplate(root, "t", "x")
	chat, _ := CreateChat(root, tpl, "doomed", AgentClaude)
	if _, err := os.Stat(chat.Root); err != nil {
		t.Fatal(err)
	}
	if err := DeleteChat(root, chat.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(chat.Root); err == nil {
		t.Errorf("expected chat dir gone, still exists")
	}
}

func TestDeleteTemplate_LeavesExistingChatsAlone(t *testing.T) {
	root := t.TempDir()
	tpl, _ := CreateTemplate(root, "soon-deleted", "x")
	chat, _ := CreateChat(root, tpl, "survives", AgentClaude)
	if err := DeleteTemplate(root, "soon-deleted"); err != nil {
		t.Fatal(err)
	}
	// Chat dir must still be there.
	if _, err := os.Stat(chat.Root); err != nil {
		t.Errorf("chat dir removed when template was deleted: %v", err)
	}
	loaded, _ := LoadChat(root, chat.ID)
	if loaded == nil {
		t.Error("chat shouldn't depend on template")
	}
}

func TestMigrateLegacyLayout_PromotesOldWorkspaces(t *testing.T) {
	root := t.TempDir()
	// Simulate the v0.1 layout: <root>/<name>/workpath/
	for _, name := range []string{"legacy-a", "legacy-b"} {
		dir := filepath.Join(root, name, "workpath")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "mission.md"), []byte("# x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Also a stray dir that shouldn't be touched.
	_ = os.MkdirAll(filepath.Join(root, "not-a-workspace"), 0o755)

	res, err := MigrateLegacyLayout(root)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if len(res.Promoted) != 2 {
		t.Errorf("expected 2 promoted, got %v", res.Promoted)
	}
	// Both should now live under templates/.
	for _, name := range []string{"legacy-a", "legacy-b"} {
		if _, err := os.Stat(filepath.Join(root, TemplatesDir, name, "workpath", "mission.md")); err != nil {
			t.Errorf("legacy %s not under templates/: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			t.Errorf("legacy dir %s still exists at top level", name)
		}
	}
	// Stray dir is left alone.
	if _, err := os.Stat(filepath.Join(root, "not-a-workspace")); err != nil {
		t.Errorf("stray dir was incorrectly touched: %v", err)
	}

	// Idempotent: running again is a no-op.
	res, err = MigrateLegacyLayout(root)
	if err != nil || len(res.Promoted) != 0 {
		t.Errorf("re-run: err=%v promoted=%v", err, res.Promoted)
	}
}

func TestEnsureSandbox_BailsOnEmptyPath(t *testing.T) {
	// This is the safety net for the "mkdir : The system cannot find
	// the path specified" bug a user hit when a chat manifest somehow
	// produced an empty SandboxDir. The error should now be actionable
	// instead of an opaque OS error.
	err := EnsureSandbox(Workspace{Name: "broken-chat"})
	if err == nil {
		t.Fatal("expected error for empty SandboxDir")
	}
	if !strings.Contains(err.Error(), "empty sandbox") {
		t.Errorf("error should mention empty sandbox; got: %v", err)
	}
}

func TestOpenChat_ResolvesAgentAndBuildsPlan(t *testing.T) {
	// End-to-end: create a chat, then OpenChat should find the agent
	// (or surface ErrAgentUnavailable cleanly when it's not on PATH).
	src := samplesDir(t)
	if _, err := os.Stat(src); err != nil {
		t.Skipf("no samples at %s", src)
	}
	root := t.TempDir()
	if _, err := SeedSamples(root, []string{src}); err != nil {
		t.Fatal(err)
	}
	tpl, _ := LoadTemplate(root, "reversing")
	chat, err := CreateChat(root, *tpl, "open-test", AgentClaude)
	if err != nil {
		t.Fatal(err)
	}

	plan, picked, err := OpenChat(chat)
	if errors.Is(err, ErrAgentUnavailable) {
		// Acceptable: this host doesn't have claude installed.
		t.Skip("claude not on PATH")
	}
	if err != nil {
		t.Fatalf("OpenChat: %v", err)
	}
	if picked.ID != AgentClaude {
		t.Errorf("picked = %s, want claude", picked.ID)
	}
	if plan.Dir != chat.SandboxDir {
		t.Errorf("Dir = %q, want %q", plan.Dir, chat.SandboxDir)
	}
}

func TestOpenChat_EmptyAgentIDErrors(t *testing.T) {
	_, _, err := OpenChat(Chat{Label: "no-agent"})
	if err == nil {
		t.Fatal("expected error for chat with no AgentID")
	}
}

func TestLoadTemplate_FlatLayoutWithoutWorkpathSubdir(t *testing.T) {
	// User dropped a workpath dir directly under templates/ (mission.md
	// at the top of the dir, no workpath/ wrapper). The launcher should
	// still pick it up — that's the lenient layout fix.
	root := t.TempDir()
	flat := filepath.Join(root, TemplatesDir, "flat-tpl")
	if err := os.MkdirAll(flat, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(flat, "mission.md"),
		[]byte("# flat\n\nA flat-layout template the user dropped in.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tpl, err := LoadTemplate(root, "flat-tpl")
	if err != nil || tpl == nil {
		t.Fatalf("LoadTemplate(flat-tpl) = %v, %v — should accept flat layout", tpl, err)
	}
	if tpl.WorkpathDir != flat {
		t.Errorf("WorkpathDir = %q, want %q (the dir itself for flat layout)", tpl.WorkpathDir, flat)
	}
}

func TestListTemplatesAndSkipped_ReportsBadDirs(t *testing.T) {
	root := t.TempDir()
	// A well-formed canonical template.
	good := filepath.Join(root, TemplatesDir, "good")
	must(t, os.MkdirAll(filepath.Join(good, "workpath"), 0o755))
	must(t, os.WriteFile(filepath.Join(good, "workpath", "mission.md"), []byte("# good\n"), 0o644))
	// A flat-layout template — also fine.
	must(t, os.MkdirAll(filepath.Join(root, TemplatesDir, "flat"), 0o755))
	must(t, os.WriteFile(filepath.Join(root, TemplatesDir, "flat", "mission.md"), []byte("# flat\n"), 0o644))
	// A dir with no mission.md anywhere — must be skipped + reported.
	must(t, os.MkdirAll(filepath.Join(root, TemplatesDir, "broken"), 0o755))
	must(t, os.WriteFile(filepath.Join(root, TemplatesDir, "broken", "README.md"), []byte("not a workpath\n"), 0o644))

	items, skipped, err := ListTemplatesAndSkipped(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Errorf("got %d templates, want 2", len(items))
	}
	if len(skipped) != 1 || skipped[0].Name != "broken" {
		t.Errorf("expected exactly 'broken' to be skipped, got %+v", skipped)
	}
	if !strings.Contains(skipped[0].Reason, "mission.md") {
		t.Errorf("reason should mention mission.md, got %q", skipped[0].Reason)
	}
}

func TestSaveWorkspaceLikeSettings_RoutesByPath(t *testing.T) {
	root := t.TempDir()
	tpl, _ := CreateTemplate(root, "router", "x")
	chat, _ := CreateChat(root, tpl, "router-chat", AgentClaude)

	// Save settings using the chat's AsWorkspace shape.
	ws := chat.AsWorkspace()
	ws.Settings = WorkspaceSettings{Language: "Hindi", MemoryEnabled: true}
	if err := SaveWorkspaceLikeSettings(ws); err != nil {
		t.Fatalf("SaveWorkspaceLikeSettings(chat): %v", err)
	}
	loaded, _ := LoadChat(root, chat.ID)
	if loaded.Settings.Language != "Hindi" {
		t.Errorf("chat settings not saved via smart router")
	}
	// No stray workspace.json.
	if _, err := os.Stat(filepath.Join(chat.Root, "workspace.json")); err == nil {
		t.Error("chat root shouldn't have workspace.json")
	}

	// Now do the same for a template.
	tplWS := Workspace{
		Name: tpl.Name, Root: tpl.Root, WorkpathDir: tpl.WorkpathDir,
		Settings: WorkspaceSettings{Language: "Greek"},
	}
	if err := SaveWorkspaceLikeSettings(tplWS); err != nil {
		t.Fatalf("SaveWorkspaceLikeSettings(template): %v", err)
	}
	loadedTpl, _ := LoadTemplate(root, "router")
	if loadedTpl.Settings.Language != "Greek" {
		t.Errorf("template settings not saved via smart router; got %+v", loadedTpl.Settings)
	}
}
