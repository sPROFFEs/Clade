package launcher

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/sPROFFEs/Clade/pkg/targets"
	"github.com/sPROFFEs/Clade/pkg/workpath"
)

// PrepareSandbox loads the workpath, validates it, and compiles it into
// the workspace sandbox using the agent's matching wpc target. After this
// returns, the sandbox is ready for the agent to be spawned with that as
// its cwd. We never write into the workpath source — that stays read-only
// as far as the launcher is concerned.
func PrepareSandbox(ws Workspace, agent Agent) error {
	if !agent.Available {
		return fmt.Errorf("agent %s is not installed", agent.ID)
	}

	if err := EnsureSandbox(ws); err != nil {
		return err
	}

	wp, err := workpath.LoadDir(ws.WorkpathDir)
	if err != nil {
		return fmt.Errorf("load workpath: %w", err)
	}
	// The wpc loader defaults Name to the directory's basename, which is
	// always literally "workpath" under our layout. Override so the
	// compiled output is identified by the workspace name (matters for
	// Claude skill dirs, mika modules, and the generic <name>.md target).
	wp.Name = ws.Name
	if err := workpath.Validate(wp); err != nil {
		return fmt.Errorf("validate workpath: %w", err)
	}

	t, err := targets.Get(agent.WpcTarget)
	if err != nil {
		return err
	}
	if err := t.Compile(wp, ws.SandboxDir); err != nil {
		return fmt.Errorf("compile %s into sandbox: %w", agent.WpcTarget, err)
	}

	if err := writeSandboxReadme(ws, agent); err != nil {
		return err
	}

	// Apply per-workspace decorations (language directive, memory
	// staging, online-skill clone, chat-log dir). Errors here are
	// surfaced via the LastDecorationNotes accessor — non-fatal so a
	// flaky network fetch doesn't block the launch.
	notes := applyDecorations(ws, agent)
	if len(notes) > 0 {
		LastDecorationNotes = notes
	} else {
		LastDecorationNotes = nil
	}
	return nil
}

// LastDecorationNotes is set by PrepareSandbox after each call so the TUI
// can surface non-fatal warnings (online-skill clone failed, memory copy
// failed, etc.) on the launching screen.
var LastDecorationNotes []string

// writeSandboxReadme drops an orientation file on first compile so the
// user lands in something self-explanatory when they cd into the sandbox.
func writeSandboxReadme(ws Workspace, agent Agent) error {
	path := filepath.Join(ws.SandboxDir, "SANDBOX.md")
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	body := fmt.Sprintf(`# Sandbox for %s

Knowledge base: %s
Agent: %s (binary: %s)

This directory is the agent's working directory. The launcher compiled the
workpath into it using the %q wpc target, so the agent loads the mission,
playbook, rules, and any tools/subagents on startup.

Anything you create here is gitignored by default. Treat the parent
workpath (../workpath/) as read-only — edit it via the launcher (or by
hand) and re-run the launcher to recompile.
`, ws.Name, ws.WorkpathDir, agent.Label, agent.Binary, agent.WpcTarget)
	return os.WriteFile(path, []byte(body), 0o644)
}

// LaunchPlan describes what the TUI hands off to the OS once Bubble Tea
// has released the terminal: which binary to exec, with what args, in
// which directory, plus optional env overrides.
type LaunchPlan struct {
	Command string
	Args    []string
	Dir     string
	// Env, if non-empty, is merged on top of os.Environ() when spawning
	// the agent. Used for Claude + Ollama: ANTHROPIC_BASE_URL, etc.
	Env map[string]string
}

// Plan builds the LaunchPlan but does not actually exec. The TUI layer
// must run Plan first (so the workpath is compiled and any error
// surfaces inside the UI), then quit the Bubble Tea program, then exec
// the plan from main() while the terminal is no longer being driven.
//
// If the workspace has Ollama settings AND the picked agent is Claude,
// Plan auto-injects the ANTHROPIC_* env vars so Claude routes to the
// local endpoint instead of Anthropic. Codex and OpenCode get their
// routing from their own config files (written via the Ollama screen);
// the launcher doesn't override their env.
func Plan(ws Workspace, agent Agent) (LaunchPlan, error) {
	if err := PrepareSandbox(ws, agent); err != nil {
		return LaunchPlan{}, err
	}
	plan := LaunchPlan{
		Command: agent.Binary,
		Args:    nil,
		Dir:     ws.SandboxDir,
	}
	o := ws.Settings.Ollama
	ollamaConfigured := o.Endpoint != "" && o.Model != ""

	switch agent.ID {
	case AgentClaude:
		if ollamaConfigured {
			plan.Env = map[string]string{
				"ANTHROPIC_AUTH_TOKEN": "ollama",
				"ANTHROPIC_API_KEY":    "",
				"ANTHROPIC_BASE_URL":   o.Endpoint,
				"OPENAI_API_KEY":       "ollama",
			}
			// Without --model Claude sends its default model name to
			// the Ollama proxy, which doesn't have it → request fails.
			plan.Args = []string{"--model", o.Model}
		}
	case AgentCodex:
		if ollamaConfigured {
			// The Ollama screen wrote [profiles.ollama_remote] into
			// ~/.codex/config.toml; tell codex to actually use it.
			plan.Args = []string{"-p", "ollama_remote"}
		}
	case AgentOpenCode:
		// OpenCode picks up routing from opencode.json's model/provider
		// fields, set by the Ollama screen — nothing to inject here.

	case AgentGemini:
		// Intentionally no Ollama handling here. Tried OPENAI_* env
		// vars (the convention that works for Codex/OpenCode) — Gemini
		// CLI 0.42+ continues to hit Google's API via cached OAuth and
		// rejects the Ollama model name with "Model ... was not found".
		// The real switch happens through ~/.gemini/settings.json
		// (selectedAuthType + provider section), whose schema isn't
		// stable across CLI versions. Until we can match the installed
		// version's schema reliably, we leave Gemini launching with
		// its default Google auth — and refuse to pass --model with
		// the Ollama model name, which would just produce the same
		// "not found" error you'd otherwise get.
	}
	return plan, nil
}

// PlanWithEnv is Plan + env overrides — the launcher injects these when
// it spawns the agent so users get e.g. Ollama routing without us
// touching their shell rc.
func PlanWithEnv(ws Workspace, agent Agent, env map[string]string) (LaunchPlan, error) {
	plan, err := Plan(ws, agent)
	if err != nil {
		return LaunchPlan{}, err
	}
	plan.Env = env
	return plan, nil
}
