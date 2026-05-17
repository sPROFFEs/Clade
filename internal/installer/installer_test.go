package installer

import (
	"context"
	"os"
	"runtime"
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

func TestExtractPnpmHome(t *testing.T) {
	// Real-world pnpm setup output the user reported.
	out := `Next configuration changes were made:
PNPM_HOME=C:\Users\user\AppData\Local\pnpm
Path=%PNPM_HOME%;C:\Users\user\.cargo\bin;C:\Users\user\AppData\Local\Microsoft\WindowsApps
Setup complete. Open a new terminal to start using pnpm.`

	got := extractPnpmHome(out)
	if got != `C:\Users\user\AppData\Local\pnpm` {
		t.Errorf("extractPnpmHome = %q, want the Windows path", got)
	}
}

func TestExtractPnpmHome_UnixLayout(t *testing.T) {
	out := `Updating /Users/me/.zshrc
PNPM_HOME=/Users/me/Library/pnpm
PATH=$PNPM_HOME:$PATH
Setup complete. Open a new terminal to start using pnpm.`
	if got := extractPnpmHome(out); got != "/Users/me/Library/pnpm" {
		t.Errorf("extractPnpmHome = %q", got)
	}
}

func TestExtractPnpmHome_AbsentReturnsEmpty(t *testing.T) {
	if got := extractPnpmHome("nothing here"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestImportPnpmPathIfPresent_AddsExistingDirToPath(t *testing.T) {
	// Build a fake pnpm bin dir and point defaultPnpmHome's env at it
	// (Linux uses XDG_DATA_HOME, Windows uses LOCALAPPDATA, macOS uses
	// HOME). Whichever applies, the test ensures the dir gets prepended.
	dir := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("LOCALAPPDATA", dir)
		// The function joins LOCALAPPDATA + "pnpm".
		if err := os.MkdirAll(dir+`\pnpm`, 0o755); err != nil {
			t.Fatal(err)
		}
	case "darwin":
		t.Setenv("HOME", dir)
		if err := os.MkdirAll(dir+`/Library/pnpm`, 0o755); err != nil {
			t.Fatal(err)
		}
	default:
		t.Setenv("XDG_DATA_HOME", dir)
		if err := os.MkdirAll(dir+`/pnpm`, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("PATH", "/nowhere")
	t.Setenv("PNPM_HOME", "")
	ImportPnpmPathIfPresent()
	got := os.Getenv("PATH")
	expected := defaultPnpmHome()
	if !strings.Contains(got, expected) {
		t.Errorf("PATH = %q, expected to contain %q", got, expected)
	}
	if os.Getenv("PNPM_HOME") != expected {
		t.Errorf("PNPM_HOME = %q, want %q", os.Getenv("PNPM_HOME"), expected)
	}

	// Second call is a no-op.
	pathBefore := os.Getenv("PATH")
	ImportPnpmPathIfPresent()
	if os.Getenv("PATH") != pathBefore {
		t.Errorf("ImportPnpmPathIfPresent should be idempotent")
	}
}

func TestImportPnpmPathIfPresent_NoopWhenDirMissing(t *testing.T) {
	// Point env at a tmp dir that has NO pnpm subdir.
	dir := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("LOCALAPPDATA", dir)
	case "darwin":
		t.Setenv("HOME", dir)
	default:
		t.Setenv("XDG_DATA_HOME", dir)
	}
	pathBefore := "/preexisting"
	t.Setenv("PATH", pathBefore)
	ImportPnpmPathIfPresent()
	if os.Getenv("PATH") != pathBefore {
		t.Errorf("PATH should be untouched when pnpm dir doesn't exist; got %q", os.Getenv("PATH"))
	}
}

func TestDefaultPnpmHome_NonEmptyOnEveryOS(t *testing.T) {
	// We don't assert the exact path (env-dependent) — just that we
	// always have *some* fallback so EnsurePnpmReady never bails for
	// "couldn't resolve PNPM_HOME" when only the env-propagation step
	// failed.
	if defaultPnpmHome() == "" {
		t.Error("defaultPnpmHome returned empty on this OS")
	}
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
