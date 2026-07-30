package main

// Resolution from a PrAImate CLI name to the actual interactive
// command to spawn in a PTY, plus exporting an agent's instructions
// into the project folder's native context file so the launched CLI
// adopts the agent persona without us touching its loop.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"git.jtsec.local/lab/PrAImate/internal/core"
	"git.jtsec.local/lab/PrAImate/internal/ollama"
)

// terminalCommand maps a PrAImate CLI id to the binary + interactive
// args to launch. We deliberately launch the CLI in its normal
// interactive mode — the whole point is the user gets the real tool.
// model, when non-empty, is passed with the CLI's own model flag
// when non-empty, is passed with the CLI's own model flag.
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
	case "":
		return "", nil, fmt.Errorf("no CLI selected for the terminal")
	default:
		return "", nil, fmt.Errorf("unknown CLI %q", cli)
	}
}

// terminalResumeCommand reopens the most recent native interactive session in
// cwd for CLIs that expose a deterministic non-picker flag. The caller still
// supplies cwd through Cmd.Dir, so "most recent" is folder-scoped.
func terminalResumeCommand(cli, model string) (name string, args []string, supported bool, err error) {
	name, args, err = terminalCommand(cli, model)
	if err != nil {
		return "", nil, false, err
	}
	switch cli {
	case "claude", "openclaude", "opencode", "praimate-code":
		return name, append(args, "--continue"), true, nil
	case "codex":
		args = []string{"resume", "--last"}
		if model != "" {
			args = append(args, "--model", model)
		}
		return name, args, true, nil
	default:
		return name, args, false, nil
	}
}

// terminalLocalEnv builds the environment that points a terminal CLI at
// a local LLM endpoint (Ollama / vLLM / GPUStack / LiteLLM). Mirrors the
// launcher's per-CLI env for the agent-launch path, scoped to the CLIs
// that route by env alone:
//
//	claude                 → ANTHROPIC_BASE_URL + auth token (Anthropic-compat proxy)
//	openclaude             → CLAUDE_CODE_USE_OPENAI + OPENAI_* (OpenAI-compat proxy)
//	opencode/praimate-code → OPENAI_BASE_URL + key (its openai provider)
//
// codex needs config-file rewrites the terminal path doesn't do —
// callers should steer those to a Chat (which goes through the full
// launcher machinery). Returns nil when cli isn't env-routable.
func terminalLocalEnv(cli, endpoint, apiKey, model string) []string {
	if strings.TrimSpace(endpoint) == "" {
		return nil
	}
	token := apiKey
	if token == "" {
		token = "ollama" // vanilla Ollama ignores the key; proxies want a non-empty bearer
	}
	switch cli {
	case "claude":
		return []string{
			"ANTHROPIC_BASE_URL=" + strings.TrimRight(ollama.NormalizeEndpoint(endpoint), "/"),
			"ANTHROPIC_AUTH_TOKEN=" + token,
			"ANTHROPIC_API_KEY=",
			"OPENAI_API_KEY=" + token,
		}
	case "openclaude":
		base := openAIBaseURL(endpoint)
		env := []string{
			"CLAUDE_CODE_USE_OPENAI=1",
			"OPENAI_API_KEY=" + token,
			"OPENAI_BASE_URL=" + base,
			"OPENAI_API_BASE=" + base,
		}
		if model != "" {
			env = append(env, "OPENAI_MODEL="+model)
		}
		return env
	case "opencode", "praimate-code":
		base := openAIBaseURL(endpoint)
		return []string{
			"OPENAI_API_KEY=" + token,
			"OPENAI_BASE_URL=" + base,
			"OPENAI_API_BASE=" + base,
		}
	default:
		return nil
	}
}

// terminalLocalRoutable reports whether a terminal session can route cli
// to a local endpoint via env alone (see terminalLocalEnv).
func terminalLocalRoutable(cli string) bool {
	switch cli {
	case "claude", "openclaude", "opencode", "praimate-code":
		return true
	}
	return false
}

// appendEnvMap folds generated launch secrets into an env overlay,
// replacing any existing value for the same key.
func appendEnvMap(env []string, extra map[string]string) []string {
	if len(extra) == 0 {
		return env
	}
	index := map[string]int{}
	for i, kv := range env {
		if key, _, ok := strings.Cut(kv, "="); ok {
			index[key] = i
		}
	}
	keys := make([]string, 0, len(extra))
	for key := range extra {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		kv := key + "=" + extra[key]
		if i, ok := index[key]; ok {
			env[i] = kv
			continue
		}
		index[key] = len(env)
		env = append(env, kv)
	}
	return env
}

// exportAgentContext writes the agent's instructions into the project
// folder's native context file for the chosen CLI, so the launched CLI
// picks them up automatically. We do NOT clobber an existing file the
// user authored — only create it when absent, and tag PrAImate-written
// ones so a later run can refresh them.
//
//	claude / openclaude           → CLAUDE.md
//	codex / opencode/praimate-code → AGENTS.md
//	others                        → no native convention; skipped
func exportAgentContext(cwd, cli string, agent *core.Agent) error {
	if agent == nil || strings.TrimSpace(agent.Instructions) == "" {
		return nil
	}
	var fname string
	switch cli {
	case "claude", "openclaude":
		fname = "CLAUDE.md"
	case "codex", "opencode", "praimate-code":
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
