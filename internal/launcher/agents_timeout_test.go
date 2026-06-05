package launcher

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestDetectAgents_SlowVersionProbeStillAvailable(t *testing.T) {
	tmpHome := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", tmpHome)
	} else {
		t.Setenv("HOME", tmpHome)
	}

	binDir := t.TempDir()
	t.Setenv("PATH", binDir)
	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", ".BAT;.CMD;.EXE")
	}

	binName := "opencode"
	body := "#!/bin/sh\n/bin/sleep 1\necho opencode 9.9.9\n"
	if runtime.GOOS == "windows" {
		binName = "opencode.bat"
		body = "@echo off\r\nping -n 2 127.0.0.1 >nul\r\necho opencode 9.9.9\r\n"
	}
	must(t, os.WriteFile(filepath.Join(binDir, binName), []byte(body), 0o755))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	agents := DetectAgents(ctx)
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
		t.Fatalf("slow --version timeout should still be available; got %+v", oc)
	}
	if oc.ProbeError != "" {
		t.Fatalf("slow --version timeout should not be shown as broken; ProbeError=%q", oc.ProbeError)
	}
	if oc.Version != "" {
		t.Fatalf("timed-out version probe should leave version unknown; got %q", oc.Version)
	}
}
