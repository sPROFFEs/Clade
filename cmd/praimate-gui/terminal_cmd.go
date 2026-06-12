package main

// Resolution from a PrAImate CLI name to the actual interactive
// command to spawn in a PTY, plus exporting an agent's instructions
// into the project folder's native context file so the launched CLI
// adopts the agent persona without us touching its loop.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sPROFFEs/PrAImate/internal/core"
)

// terminalCommand maps a PrAImate CLI id to the binary + interactive
// args to launch. We deliberately launch the CLI in its normal
// interactive mode — the whole point is the user gets the real tool.
// model, when non-empty, is passed with the CLI's own model flag
// (deepseek has none; its config picks the model).
func terminalCommand(cli, model string) (name string, args []string, err error) {
	switch cli {
	case "claude", "openclaude":
		if model != "" {
			args = []string{"--model", model}
		}
		return cli, args, nil
	case "codex":
		if model != "" {
			args = []string{"-m", model}
		}
		return "codex", args, nil
	case "opencode", "praimate-code":
		if model != "" {
			args = []string{"--model", model}
		}
		return cli, args, nil
	case "gemini":
		if model != "" {
			args = []string{"-m", model}
		}
		return "gemini", args, nil
	case "deepseek":
		return "deepseek-tui", nil, nil
	case "":
		return "", nil, fmt.Errorf("no CLI selected for the terminal")
	default:
		return "", nil, fmt.Errorf("unknown CLI %q", cli)
	}
}

// exportAgentContext writes the agent's instructions into the project
// folder's native context file for the chosen CLI, so the launched CLI
// picks them up automatically. We do NOT clobber an existing file the
// user authored — only create it when absent, and tag PrAImate-written
// ones so a later run can refresh them.
//
//	claude / openclaude → CLAUDE.md
//	codex / opencode    → AGENTS.md
//	others              → no native convention; skipped
func exportAgentContext(cwd, cli string, agent *core.Agent) error {
	if agent == nil || strings.TrimSpace(agent.Instructions) == "" {
		return nil
	}
	var fname string
	switch cli {
	case "claude", "openclaude":
		fname = "CLAUDE.md"
	case "codex", "opencode":
		fname = "AGENTS.md"
	default:
		return nil
	}
	path := filepath.Join(cwd, fname)

	const marker = "<!-- praimate:agent -->"
	if existing, err := os.ReadFile(path); err == nil {
		// Only refresh files we wrote; never overwrite the user's own.
		if !strings.Contains(string(existing), marker) {
			return nil
		}
	}
	// AgentSystemPrompt = instructions + the knowledge-base pointer, so
	// terminal sessions get the same context as chats and the studio.
	body := fmt.Sprintf("%s\n# %s\n\n%s\n", marker, agent.Name, strings.TrimSpace(core.AgentSystemPrompt(agent)))
	return os.WriteFile(path, []byte(body), 0o644)
}
