package launcher

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// helper: minimal workspace with sandbox prepared (no agent yet, just dirs).
func freshWorkspace(t *testing.T, settings WorkspaceSettings) Workspace {
	t.Helper()
	src := samplesDir(t)
	if _, err := os.Stat(src); err != nil {
		t.Skipf("no samples at %s", src)
	}
	root := t.TempDir()
	if _, err := SeedSamples(root, []string{src}); err != nil {
		t.Fatal(err)
	}
	ws, err := LoadWorkspace(root, "reversing")
	if err != nil || ws == nil {
		t.Fatalf("LoadWorkspace: %v %v", err, ws)
	}
	ws.Settings = settings
	if err := SaveWorkspaceSettings(*ws); err != nil {
		t.Fatal(err)
	}
	return *ws
}

func TestDecorate_LanguageDirectivePrependedToSKILLmd(t *testing.T) {
	ws := freshWorkspace(t, WorkspaceSettings{Language: "Italian"})
	claude := Agent{
		ID: AgentClaude, Label: "Claude", Binary: "claude",
		WpcTarget: "claude", Available: true,
	}
	if err := PrepareSandbox(ws, claude); err != nil {
		t.Fatalf("PrepareSandbox: %v", err)
	}
	path := filepath.Join(ws.SandboxDir, ".claude", "skills", "reversing", "SKILL.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "respond in Italian") {
		t.Errorf("SKILL.md missing language directive:\n%s", raw)
	}
	// Frontmatter still intact.
	if !strings.HasPrefix(string(raw), "---\n") {
		t.Error("frontmatter clobbered")
	}
}

func TestDecorate_LanguageDirectiveInAGENTSmd(t *testing.T) {
	ws := freshWorkspace(t, WorkspaceSettings{Language: "Japanese"})
	codex := Agent{
		ID: AgentCodex, Label: "Codex", Binary: "codex",
		WpcTarget: "codex", Available: true,
	}
	if err := PrepareSandbox(ws, codex); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(ws.SandboxDir, "AGENTS.md"))
	if !strings.Contains(string(raw), "respond in Japanese") {
		t.Errorf("AGENTS.md missing language directive:\n%s", raw)
	}
}

func TestDecorate_MemoryStagedAndSyncedBack(t *testing.T) {
	ws := freshWorkspace(t, WorkspaceSettings{MemoryEnabled: true})
	claude := Agent{ID: AgentClaude, Binary: "claude", WpcTarget: "claude", Available: true}
	if err := PrepareSandbox(ws, claude); err != nil {
		t.Fatal(err)
	}
	// MEMORY.md initialized at the workspace root and copied to sandbox.
	if _, err := os.Stat(filepath.Join(ws.Root, "MEMORY.md")); err != nil {
		t.Errorf("workspace MEMORY.md missing: %v", err)
	}
	sandboxMem := filepath.Join(ws.SandboxDir, "MEMORY.md")
	if _, err := os.Stat(sandboxMem); err != nil {
		t.Errorf("sandbox MEMORY.md missing: %v", err)
	}

	// Simulate agent editing the sandbox memory.
	time.Sleep(20 * time.Millisecond) // ensure mtime difference
	if err := os.WriteFile(sandboxMem, []byte("# Memory\n\n- agent wrote this\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SyncMemoryBack(ws); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(ws.Root, "MEMORY.md"))
	if !strings.Contains(string(raw), "agent wrote this") {
		t.Errorf("memory not synced back:\n%s", raw)
	}
}

func TestDecorate_ChatLogDirCreatedPerLaunch(t *testing.T) {
	ws := freshWorkspace(t, WorkspaceSettings{})
	codex := Agent{ID: AgentCodex, Binary: "codex", WpcTarget: "codex", Available: true}
	if err := PrepareSandbox(ws, codex); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(ws.ChatsDir)
	if err != nil {
		t.Fatalf("ChatsDir not created: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no chat session dir created")
	}
	// session.json valid.
	sess := filepath.Join(ws.ChatsDir, entries[0].Name(), "session.json")
	raw, err := os.ReadFile(sess)
	if err != nil {
		t.Fatalf("session.json missing: %v", err)
	}
	var rec SessionRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("session.json not valid: %v", err)
	}
	if rec.Agent != "codex" || rec.WpcTarget != "codex" {
		t.Errorf("rec = %+v", rec)
	}
}

func TestDecorate_OnlineSkillsClonedIntoClaudeSkillsDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	// Build a local git "remote" repo.
	src := filepath.Join(t.TempDir(), "demoskill")
	_ = os.MkdirAll(src, 0o755)
	_ = os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("# demoskill\n"), 0o644)
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"add", "."},
		{"-c", "user.email=t@e", "-c", "user.name=t", "commit", "-qm", "init"},
	} {
		c := exec.Command("git", args...)
		c.Dir = src
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	ws := freshWorkspace(t, WorkspaceSettings{OnlineSkills: []string{src}})
	claude := Agent{ID: AgentClaude, Binary: "claude", WpcTarget: "claude", Available: true}
	if err := PrepareSandbox(ws, claude); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(ws.SandboxDir, ".claude", "skills", "demoskill", "SKILL.md")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected cloned skill at %s: %v", want, err)
	}
}

func TestDecorate_FailedOnlineSkillIsNonFatal(t *testing.T) {
	ws := freshWorkspace(t, WorkspaceSettings{OnlineSkills: []string{"/nonexistent/repo"}})
	claude := Agent{ID: AgentClaude, Binary: "claude", WpcTarget: "claude", Available: true}
	if err := PrepareSandbox(ws, claude); err != nil {
		// A failed online-skill clone must not break the launch.
		t.Fatalf("PrepareSandbox should not fail on bad skill URL, got: %v", err)
	}
	if len(LastDecorationNotes) == 0 {
		t.Error("expected LastDecorationNotes to record the failure")
	}
}

// Use context to silence unused-import warnings when subtests get skipped.
var _ = context.Background
