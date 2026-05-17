package launcher

import (
	"os"
	"path/filepath"
	"testing"
)

// chatFromSeededReversing seeds the bundled samples as templates, then
// creates a fresh chat from the "reversing" template — the canonical
// fixture for launch-path tests.
func chatFromSeededReversing(t *testing.T) Chat {
	t.Helper()
	src := samplesDir(t)
	if _, err := os.Stat(src); err != nil {
		t.Skipf("no samples dir at %s", src)
	}
	root := t.TempDir()
	if _, err := SeedSamples(root, []string{src}); err != nil {
		t.Fatal(err)
	}
	tpl, err := LoadTemplate(root, "reversing")
	if err != nil || tpl == nil {
		t.Fatalf("LoadTemplate(reversing): %v %v", err, tpl)
	}
	chat, err := CreateChat(root, *tpl, "test-chat", AgentClaude)
	if err != nil {
		t.Fatalf("CreateChat: %v", err)
	}
	return chat
}

func TestPrepareSandbox_CompilesCodexTarget(t *testing.T) {
	chat := chatFromSeededReversing(t)
	ws := chat.AsWorkspace()
	// We compile reversing for Codex even though chat.AgentID is Claude —
	// the test just exercises the codex target path.
	agent := Agent{
		ID: AgentCodex, Label: "Codex CLI", Binary: "codex",
		WpcTarget: "codex", Available: true,
	}
	if err := PrepareSandbox(ws, agent); err != nil {
		t.Fatalf("PrepareSandbox: %v", err)
	}
	for _, rel := range []string{"AGENTS.md", "AGENTS.assets/tools/file_summary.sh", "SANDBOX.md"} {
		if _, err := os.Stat(filepath.Join(ws.SandboxDir, rel)); err != nil {
			t.Errorf("expected %s in sandbox: %v", rel, err)
		}
	}
}

func TestPrepareSandbox_CompilesClaudeTarget(t *testing.T) {
	chat := chatFromSeededReversing(t)
	ws := chat.AsWorkspace()
	agent := Agent{
		ID: AgentClaude, Label: "Claude Code", Binary: "claude",
		WpcTarget: "claude", Available: true,
	}
	if err := PrepareSandbox(ws, agent); err != nil {
		t.Fatalf("PrepareSandbox: %v", err)
	}
	// Claude target writes <sandbox>/.claude/skills/<template>/SKILL.md —
	// the template name flows through AsWorkspace.Name on purpose so two
	// chats cloned from the same template both compile a skill of that
	// name into their respective sandboxes.
	skill := filepath.Join(ws.SandboxDir, ".claude", "skills", "reversing", "SKILL.md")
	if _, err := os.Stat(skill); err != nil {
		t.Errorf("expected Claude skill at %s: %v", skill, err)
	}
}

func TestPrepareSandbox_RefusesUnavailableAgent(t *testing.T) {
	root := t.TempDir()
	tpl, err := CreateTemplate(root, "empty", "x")
	if err != nil {
		t.Fatal(err)
	}
	chat, err := CreateChat(root, tpl, "x-chat", AgentCodex)
	if err != nil {
		t.Fatal(err)
	}
	agent := Agent{ID: AgentCodex, Binary: "codex", WpcTarget: "codex", Available: false}
	if err := PrepareSandbox(chat.AsWorkspace(), agent); err == nil {
		t.Error("expected error for unavailable agent")
	}
}

func TestDetectAgents_PopulatesEntries(t *testing.T) {
	// We can't assume anything about which CLIs are installed on the test
	// host; just check the catalog comes back with the expected IDs and
	// that Available is a deterministic bool (not panicking, etc.).
	agents := DetectAgents(t.Context())
	if len(agents) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(agents))
	}
	wantIDs := map[AgentID]bool{AgentClaude: true, AgentCodex: true, AgentOpenCode: true}
	for _, a := range agents {
		if !wantIDs[a.ID] {
			t.Errorf("unexpected agent ID %q", a.ID)
		}
		if a.Binary == "" || a.WpcTarget == "" {
			t.Errorf("agent %s has empty binary or target", a.ID)
		}
	}
}
