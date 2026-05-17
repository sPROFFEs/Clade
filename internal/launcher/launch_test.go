package launcher

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareSandbox_CompilesCodexTarget(t *testing.T) {
	src := samplesDir(t)
	if _, err := os.Stat(src); err != nil {
		t.Skipf("no samples dir at %s", src)
	}
	root := t.TempDir()
	if _, err := SeedSamples(root, []string{src}); err != nil {
		t.Fatal(err)
	}
	ws, err := LoadWorkspace(root, "reversing")
	if err != nil || ws == nil {
		t.Fatalf("LoadWorkspace: %v ws=%v", err, ws)
	}

	agent := Agent{
		ID:        AgentCodex,
		Label:     "Codex CLI",
		Binary:    "codex",
		WpcTarget: "codex",
		Available: true,
	}

	if err := PrepareSandbox(*ws, agent); err != nil {
		t.Fatalf("PrepareSandbox: %v", err)
	}

	// Codex target writes AGENTS.md + AGENTS.assets at sandbox root.
	for _, rel := range []string{"AGENTS.md", "AGENTS.assets/tools/file_summary.sh", "SANDBOX.md"} {
		if _, err := os.Stat(filepath.Join(ws.SandboxDir, rel)); err != nil {
			t.Errorf("expected %s in sandbox: %v", rel, err)
		}
	}
}

func TestPrepareSandbox_CompilesClaudeTarget(t *testing.T) {
	src := samplesDir(t)
	if _, err := os.Stat(src); err != nil {
		t.Skipf("no samples dir at %s", src)
	}
	root := t.TempDir()
	if _, err := SeedSamples(root, []string{src}); err != nil {
		t.Fatal(err)
	}
	ws, err := LoadWorkspace(root, "reversing")
	if err != nil || ws == nil {
		t.Fatalf("LoadWorkspace: %v ws=%v", err, ws)
	}

	agent := Agent{
		ID:        AgentClaude,
		Label:     "Claude Code",
		Binary:    "claude",
		WpcTarget: "claude",
		Available: true,
	}
	if err := PrepareSandbox(*ws, agent); err != nil {
		t.Fatalf("PrepareSandbox: %v", err)
	}
	skill := filepath.Join(ws.SandboxDir, ".claude", "skills", "reversing", "SKILL.md")
	if _, err := os.Stat(skill); err != nil {
		t.Errorf("expected Claude skill at %s: %v", skill, err)
	}
}

func TestPrepareSandbox_RefusesUnavailableAgent(t *testing.T) {
	root := t.TempDir()
	ws, err := CreateWorkspace(root, "empty", "x")
	if err != nil {
		t.Fatal(err)
	}
	agent := Agent{ID: AgentCodex, Binary: "codex", WpcTarget: "codex", Available: false}
	if err := PrepareSandbox(ws, agent); err == nil {
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
