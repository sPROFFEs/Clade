package launcher

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestDetectAgents_FallsBackToKnownInstallPaths: the official OpenCode
// installer drops its binary at ~/.opencode/bin/ and updates ~/.bashrc.
// On Windows, a launcher started from PowerShell or cmd.exe never sees
// the bashrc PATH update — exec.LookPath fails. Detection should fall
// back to the known install dir and still mark the agent available.
func TestDetectAgents_FallsBackToKnownInstallPaths(t *testing.T) {
	tmp := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("USERPROFILE", tmp)
	default:
		t.Setenv("HOME", tmp)
	}
	// Empty PATH so exec.LookPath cannot find any real opencode.
	t.Setenv("PATH", "")
	t.Setenv("PATHEXT", ".BAT;.CMD;.EXE")

	binDir := filepath.Join(tmp, ".opencode", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binName := "opencode"
	body := "#!/bin/sh\necho fake-opencode 9.9.9\n"
	if runtime.GOOS == "windows" {
		binName = "opencode.bat"
		body = "@echo off\r\necho fake-opencode 9.9.9\r\n"
	}
	if err := os.WriteFile(filepath.Join(binDir, binName), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	agents := DetectAgents(context.Background())
	var oc *Agent
	for i := range agents {
		if agents[i].ID == AgentOpenCode {
			oc = &agents[i]
		}
	}
	if oc == nil {
		t.Fatal("OpenCode missing from catalog")
	}
	if !oc.Available {
		t.Errorf("OpenCode should be available via the install-dir fallback; got %+v", oc)
	}
	if oc.Binary == "opencode" {
		t.Error("Binary should have been rewritten to the absolute fallback path")
	}
}
