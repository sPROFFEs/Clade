package launcher

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestE2E_PlanForRealInstalledAgents walks the full launcher pipeline
// against the actual agent CLIs on this host: seed samples, load the
// "reversing" workspace, detect agents, and for every one that's
// available, build a LaunchPlan and confirm its Command is resolvable.
// Skips if no agents are installed (CI hosts without any of the three).
func TestE2E_PlanForRealInstalledAgents(t *testing.T) {
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
		t.Fatalf("LoadTemplate: %v %v", err, tpl)
	}
	chat, err := CreateChat(root, *tpl, "e2e-test", AgentClaude)
	if err != nil {
		t.Fatalf("CreateChat: %v", err)
	}
	wsLocal := chat.AsWorkspace()
	ws := &wsLocal

	agents := DetectAgents(context.Background())
	anyAvailable := false
	for _, a := range agents {
		if !a.Available {
			t.Logf("skipping %s: not on PATH", a.ID)
			continue
		}
		anyAvailable = true
		t.Run(string(a.ID), func(t *testing.T) {
			plan, err := Plan(*ws, a)
			if err != nil {
				t.Fatalf("Plan(%s): %v", a.ID, err)
			}
			if plan.Command == "" {
				t.Fatal("empty Command")
			}
			if plan.Dir != ws.SandboxDir {
				t.Errorf("Dir = %q, want %q", plan.Dir, ws.SandboxDir)
			}
			// LookPath must succeed — that's how the OS will find this binary
			// when execAgent fires.
			resolved, err := exec.LookPath(plan.Command)
			if err != nil {
				t.Fatalf("LookPath(%s): %v", plan.Command, err)
			}
			t.Logf("plan ok: %s → %s (cwd=%s)", a.ID, resolved, plan.Dir)

			// Confirm the agent-readable artifact is on disk in the sandbox.
			var marker string
			switch a.WpcTarget {
			case "claude":
				marker = filepath.Join(plan.Dir, ".claude", "skills", "reversing", "SKILL.md")
			case "codex":
				marker = filepath.Join(plan.Dir, "AGENTS.md")
			case "gemini":
				marker = filepath.Join(plan.Dir, "GEMINI.md")
			default:
				t.Fatalf("unknown wpc target %q", a.WpcTarget)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Errorf("expected marker %s in sandbox: %v", marker, err)
			}
		})
	}
	if !anyAvailable {
		t.Skip("no agent CLIs installed; nothing to verify end-to-end")
	}
}
