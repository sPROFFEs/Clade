package launcher

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestDetectAgents_BrokenBinaryNotMarkedAvailable: when an agent's
// binary is on PATH but `--version` fails (e.g. the opencode-ai npm
// package shipped for an incompatible Windows arch), DetectAgents must
// NOT mark it Available — otherwise the user picks it and the launch
// crashes. The probe error is captured in ProbeError so the picker can
// explain why.
func TestDetectAgents_BrokenBinaryNotMarkedAvailable(t *testing.T) {
	// Create a fake "claude" binary that always exits 1 to simulate a
	// broken install.
	binDir := t.TempDir()
	binName := "claude"
	if runtime.GOOS == "windows" {
		binName = "claude.bat"
	}
	binPath := filepath.Join(binDir, binName)
	body := "#!/bin/sh\nexit 1\n"
	if runtime.GOOS == "windows" {
		body = "@echo off\r\nexit /b 1\r\n"
	}
	if err := os.WriteFile(binPath, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	// Point PATH at ONLY our fake bin dir so the real claude (if any) isn't found.
	t.Setenv("PATH", binDir)
	// On Windows, exec.LookPath honors PATHEXT — make sure .bat is included.
	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", ".BAT;.CMD;.EXE")
	}

	agents := DetectAgents(context.Background())
	var claude *Agent
	for i := range agents {
		if agents[i].ID == AgentClaude {
			claude = &agents[i]
			break
		}
	}
	if claude == nil {
		t.Fatal("Claude entry missing from catalog")
	}
	if claude.Available {
		t.Error("broken binary should NOT be marked Available")
	}
	if claude.ProbeError == "" {
		t.Error("ProbeError should be populated for broken --version")
	}
}

func TestDetectAgents_WorkingBinaryMarkedAvailableWithVersion(t *testing.T) {
	// Fake binary that prints a version and exits 0.
	binDir := t.TempDir()
	binName := "claude"
	if runtime.GOOS == "windows" {
		binName = "claude.bat"
	}
	binPath := filepath.Join(binDir, binName)
	body := "#!/bin/sh\necho fake-claude 9.9.9\n"
	if runtime.GOOS == "windows" {
		body = "@echo off\r\necho fake-claude 9.9.9\r\n"
	}
	if err := os.WriteFile(binPath, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", ".BAT;.CMD;.EXE")
	}

	agents := DetectAgents(context.Background())
	var claude *Agent
	for i := range agents {
		if agents[i].ID == AgentClaude {
			claude = &agents[i]
		}
	}
	if claude == nil || !claude.Available {
		t.Fatalf("Claude should be Available; got %+v", claude)
	}
	if !strings.Contains(claude.Version, "fake-claude 9.9.9") {
		t.Errorf("Version = %q, want to contain fake-claude 9.9.9", claude.Version)
	}
	if claude.ProbeError != "" {
		t.Errorf("ProbeError should be empty for working binary; got %q", claude.ProbeError)
	}
}
