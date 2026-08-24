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

// Detection is invoked after installs and from the GUI's background prefetch.
// Each CLI has its own probe deadline, so one cold Node/Bun process must not
// delay every CLI that follows it in the catalogue.
func TestDetectAgents_ProbesCLIsConcurrently(t *testing.T) {
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

	for _, agent := range KnownAgents() {
		name := agent.Binary
		body := "#!/bin/sh\n/bin/sleep 0.4\necho " + string(agent.ID) + " 9.9.9\n"
		if runtime.GOOS == "windows" {
			name += ".bat"
			body = "@echo off\r\nping -n 2 127.0.0.1 >nul\r\necho " + string(agent.ID) + " 9.9.9\r\n"
		}
		must(t, os.WriteFile(filepath.Join(binDir, name), []byte(body), 0o755))
	}

	started := time.Now()
	agents := DetectAgents(context.Background())
	elapsed := time.Since(started)

	limit := 1500 * time.Millisecond
	if runtime.GOOS == "windows" {
		limit = 3 * time.Second
	}
	if elapsed >= limit {
		t.Fatalf("CLI probes ran serially: elapsed %s, want less than %s", elapsed, limit)
	}
	for _, agent := range agents {
		if !agent.Available {
			t.Errorf("%s was not detected: %+v", agent.ID, agent)
		}
	}
}
