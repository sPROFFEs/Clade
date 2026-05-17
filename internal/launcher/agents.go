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
	Available bool
	Version   string // best-effort, may be empty
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
// with Available/Version populated.
func DetectAgents(ctx context.Context) []Agent {
	agents := KnownAgents()
	for i := range agents {
		path, err := exec.LookPath(agents[i].Binary)
		if err != nil {
			continue
		}
		agents[i].Available = true
		agents[i].Version = probeVersion(ctx, path)
	}
	return agents
}

// probeVersion runs `<bin> --version` with a short timeout. Anything
// that doesn't return cleanly within the timeout is recorded as an
// empty version, not as unavailable — the binary is on PATH, we just
// don't know which version.
func probeVersion(parent context.Context, path string) string {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return ""
	}
	line := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	return strings.TrimSpace(line)
}
