package main

// PrAImate Code surfaced in the CLIs browser. It's a coding CLI we
// provide (a version-pinned build of OpenCode), not a chat-launch agent
// nor a companion tool — so it appears as an install-only row in the
// CLIs section with its own download installer.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/sPROFFEs/PrAImate/internal/installer"
	"github.com/sPROFFEs/PrAImate/internal/launcher"
)

// praimateCodeAgentID is a sentinel used ONLY inside the CLIs browser to
// recognise the PrAImate Code row. It is not a real launcher agent and
// never reaches Plan()/DetectAgents() — the Enter/i handlers special-
// case it before the normal agent flow.
const praimateCodeAgentID launcher.AgentID = "praimate-code"

// praimateCodeInstalled reports whether the praimate-code binary resolves
// (managed bin dir or PATH).
func praimateCodeInstalled() bool {
	if dir, err := installer.PraimateBinDir(); err == nil {
		name := "praimate-code"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		if fi, e := os.Stat(filepath.Join(dir, name)); e == nil && !fi.IsDir() {
			return true
		}
	}
	if _, err := exec.LookPath("praimate-code"); err == nil {
		return true
	}
	return false
}

// praimateCodeBrowserEntry is the synthetic launcher.Agent row shown in
// the CLIs browser. Available reflects whether it's installed.
func praimateCodeBrowserEntry() launcher.Agent {
	return launcher.Agent{
		ID:        praimateCodeAgentID,
		Label:     "PrAImate Code (bundled coding CLI · run with `praimate code`)",
		Binary:    "praimate-code",
		Available: praimateCodeInstalled(),
	}
}
