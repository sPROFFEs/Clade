package launcher

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// AgentID is one of "claude", "codex", "opencode".
type AgentID string

const (
	AgentClaude   AgentID = "claude"
	AgentCodex    AgentID = "codex"
	AgentOpenCode AgentID = "opencode"
)

// Agent describes one supported CLI agent. WpcTarget is the wpc target
// the launcher uses to compile the workpath into the sandbox before
// launching this agent.
type Agent struct {
	ID        AgentID
	Label     string
	Binary    string
	WpcTarget string // "claude" or "codex"
	// Available is true only when the binary is on PATH AND `--version`
	// exits cleanly. We require the version probe to succeed because a
	// binary that exists but crashes on launch (e.g. an opencode-ai npm
	// package shipped for an incompatible Windows build) shouldn't appear
	// launchable.
	Available bool
	Version   string
	// ProbeError, when set, is the reason --version failed even though the
	// binary was on PATH. Surfaced in the picker so the user understands
	// why a "found but broken" install is greyed out.
	ProbeError string
	// InstallHint is a single command the user can run to install the
	// agent themselves. Surfaced in the picker for greyed-out entries.
	InstallHint string
}

// KnownAgents returns the static catalog. Availability is filled by
// DetectAgents.
func KnownAgents() []Agent {
	return []Agent{
		{
			ID:          AgentClaude,
			Label:       "Claude Code",
			Binary:      "claude",
			WpcTarget:   "claude",
			InstallHint: "curl -fsSL https://claude.ai/install.sh | bash   (macOS/Linux)  |  winget install Anthropic.ClaudeCode  (Windows)",
		},
		{
			ID:          AgentCodex,
			Label:       "Codex CLI",
			Binary:      "codex",
			WpcTarget:   "codex",
			InstallHint: "pnpm add -g @openai/codex   (npm package; needs Node + pnpm)",
		},
		{
			ID:          AgentOpenCode,
			Label:       "OpenCode",
			Binary:      "opencode",
			WpcTarget:   "codex",
			InstallHint: "curl -fsSL https://opencode.ai/install | bash   |  pnpm add -g opencode-ai",
		},
	}
}

// DetectAgents probes PATH for each known agent and returns a fresh slice
// with Available/Version/ProbeError populated.
func DetectAgents(ctx context.Context) []Agent {
	agents := KnownAgents()
	for i := range agents {
		path, err := exec.LookPath(agents[i].Binary)
		if err != nil {
			continue
		}
		version, perr := probeVersion(ctx, path)
		if perr != nil {
			// Binary exists but `--version` failed — likely an incompatible
			// or broken install. Don't mark available; surface the reason.
			agents[i].ProbeError = trimErr(perr)
			continue
		}
		agents[i].Available = true
		agents[i].Version = version
	}
	return agents
}

// probeVersion runs `<bin> --version` with a short timeout. Returns the
// version line and a non-nil error if the binary couldn't even produce
// a clean exit — caller uses that to decide whether the install is
// actually usable.
func probeVersion(parent context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	if err != nil {
		return "", err
	}
	line := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	return strings.TrimSpace(line), nil
}

// trimErr keeps Windows error blobs short enough to render in the picker.
func trimErr(err error) string {
	s := err.Error()
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:117] + "..."
	}
	return s
}
