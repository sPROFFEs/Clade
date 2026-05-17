package installer

import (
	"context"
	"strings"
	"testing"
)

// allMethods is internal — we test the static catalog directly, not the
// filtered Methods(), so the test result doesn't depend on what's
// installed on the dev box.

func TestCatalog_EveryAgentHasAtLeastOneMethodPerOS(t *testing.T) {
	osList := []OS{OSMacOS, OSLinux, OSWSL, OSWindows}
	agents := []AgentID{AgentClaude, AgentCodex, AgentOpenCode}
	for _, a := range agents {
		for _, o := range osList {
			for _, act := range []Action{ActionInstall, ActionUpdate} {
				got := allMethods(a, act, o)
				if len(got) == 0 {
					t.Errorf("no methods for agent=%s os=%s action=%s", a, o, act)
				}
			}
		}
	}
}

func TestCatalog_ExactlyOneRecommendedPerEntry(t *testing.T) {
	agents := []AgentID{AgentClaude, AgentCodex, AgentOpenCode}
	for _, a := range agents {
		for _, o := range []OS{OSMacOS, OSLinux, OSWSL, OSWindows} {
			got := allMethods(a, ActionInstall, o)
			n := 0
			for _, m := range got {
				if m.Recommended {
					n++
				}
			}
			if n != 1 {
				t.Errorf("agent=%s os=%s has %d recommended methods, want 1", a, o, n)
			}
		}
	}
}

func TestCatalog_PnpmMethodsCarryNodePrereq(t *testing.T) {
	// Any method using pnpm must declare node + pnpm as prerequisites,
	// otherwise the launcher won't know to surface the warning.
	for _, a := range []AgentID{AgentClaude, AgentCodex, AgentOpenCode} {
		for _, o := range []OS{OSMacOS, OSLinux, OSWSL, OSWindows} {
			for _, m := range allMethods(a, ActionInstall, o) {
				if !strings.HasPrefix(m.Command, "pnpm ") {
					continue
				}
				hasNode, hasPnpm := false, false
				for _, p := range m.Prereqs {
					if p == "node" {
						hasNode = true
					}
					if p == "pnpm" {
						hasPnpm = true
					}
				}
				if !hasNode || !hasPnpm {
					t.Errorf("agent=%s os=%s method=%s missing prereqs (got %v)", a, o, m.ID, m.Prereqs)
				}
			}
		}
	}
}

func TestCatalog_UpdateUsesLatestForPnpmInstalls(t *testing.T) {
	for _, m := range allMethods(AgentCodex, ActionUpdate, OSLinux) {
		if m.ID != "pnpm" {
			continue
		}
		if !strings.Contains(m.Command, "@latest") {
			t.Errorf("codex update via pnpm should pin @latest; got %q", m.Command)
		}
	}
}

func TestCatalog_WindowsHasNoBashOnlyMethods(t *testing.T) {
	// curl|bash methods don't work in default Windows environments —
	// agents would be unusable from a fresh Windows machine. Make sure
	// every agent has at least one Shell-Direct or PowerShell method.
	for _, a := range []AgentID{AgentClaude, AgentCodex, AgentOpenCode} {
		got := allMethods(a, ActionInstall, OSWindows)
		ok := false
		for _, m := range got {
			if m.Shell != ShellBash {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("agent=%s on Windows has only bash-shell methods; user can't install without WSL/Git Bash", a)
		}
	}
}

func TestBuildCmd_DirectMode(t *testing.T) {
	m := Method{Command: "echo hello world"}
	cmd := buildCmd(context.Background(), m)
	if !strings.Contains(cmd.Path, "echo") {
		t.Errorf("Path = %q, want to contain %q", cmd.Path, "echo")
	}
	if len(cmd.Args) < 3 {
		t.Fatalf("Args = %v", cmd.Args)
	}
	if cmd.Args[len(cmd.Args)-2] != "hello" || cmd.Args[len(cmd.Args)-1] != "world" {
		t.Errorf("Args tail = %v, want [hello world]", cmd.Args)
	}
}

func TestBuildCmd_BashShell(t *testing.T) {
	m := Method{Shell: ShellBash, Command: "echo $HOME"}
	cmd := buildCmd(context.Background(), m)
	if !strings.Contains(cmd.Path, "bash") {
		t.Errorf("Path = %q, want to contain bash", cmd.Path)
	}
	last := cmd.Args[len(cmd.Args)-1]
	if last != "echo $HOME" {
		t.Errorf("Args last = %q, want %q", last, "echo $HOME")
	}
}

func TestAutoFixable_SplitsByCapability(t *testing.T) {
	missing := []string{"node", "pnpm", "go"}
	fix := AutoFixable(missing)
	unfix := UnfixableMissing(missing)

	// Only pnpm is auto-fixable today.
	if !equal(fix, []string{"pnpm"}) {
		t.Errorf("AutoFixable = %v, want [pnpm]", fix)
	}
	// node + go (anything else) → user must install.
	if !equal(unfix, []string{"node", "go"}) {
		t.Errorf("UnfixableMissing = %v, want [node go]", unfix)
	}
}

func TestAutoFixable_EmptyInputs(t *testing.T) {
	if got := AutoFixable(nil); len(got) != 0 {
		t.Errorf("AutoFixable(nil) = %v", got)
	}
	if got := UnfixableMissing(nil); len(got) != 0 {
		t.Errorf("UnfixableMissing(nil) = %v", got)
	}
}

func TestAutoFixable_OnlyPnpmMissing_NothingToBlock(t *testing.T) {
	// This is the exact scenario from the user bug report: pnpm is
	// auto-fixable, so the install screen should NOT block on it.
	if got := UnfixableMissing([]string{"pnpm"}); len(got) != 0 {
		t.Errorf("UnfixableMissing([pnpm]) = %v, expected empty (auto-fixable)", got)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDetectOS_ReturnsKnownValue(t *testing.T) {
	o := DetectOS()
	switch o {
	case OSMacOS, OSLinux, OSWSL, OSWindows:
		// ok
	default:
		t.Errorf("DetectOS returned %q, want one of macos/linux/wsl/windows", o)
	}
}
