package main

// Apply the saved local endpoint to the config-file CLIs (opencode /
// codex) from the GUI — the counterpart of the TUI's local-endpoint
// wizard. claude/openclaude route by env (the per-chat "Use local LLM"
// toggle); codex/opencode read a provider block from their own config
// files (~/.codex/config.toml, opencode.json), which only the TUI wrote
// until now. ApplyLocalToCLI writes that block so opencode/codex GUI
// chats, code sessions and studio use the local model too.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sPROFFEs/PrAImate/internal/ollama"
)

// LocalCLIStatus reports which config-file CLIs currently have the local
// ollama_remote route applied to their global config.
type LocalCLIStatus struct {
	Codex    bool `json:"codex"`
	Opencode bool `json:"opencode"`
}

// LocalCLIStatusNow probes the on-disk config of codex + opencode.
func (a *App) LocalCLIStatusNow() LocalCLIStatus {
	return LocalCLIStatus{
		Codex:    ollama.CodexConfigured(),
		Opencode: ollama.OpenCodeConfigured(),
	}
}

// ApplyLocalToCLI writes the saved local endpoint (Settings → Local LLM)
// into the global config of codex or opencode so the CLI routes to the
// local model everywhere — including GUI chats/code/studio. model is
// required (it becomes the provider's default model). Returns a status
// line for the UI.
func (a *App) ApplyLocalToCLI(cli, model string) (string, error) {
	d, err := a.GetLocalLLM()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(d.Endpoint) == "" {
		return "", fmt.Errorf("no local endpoint saved — set and save one above first")
	}
	if strings.TrimSpace(model) == "" {
		return "", fmt.Errorf("pick a model to route %s to", cli)
	}
	s := ollama.Settings{
		Endpoint:      d.Endpoint,
		APIKey:        d.APIKey,
		Model:         strings.TrimSpace(model),
		WireAPI:       d.WireAPI,
		ContextTokens: d.ContextTokens,
		OutputTokens:  d.OutputTokens,
	}
	switch cli {
	case "opencode":
		path, err := ollama.ApplyOpenCode(s, true)
		if err != nil {
			return "", err
		}
		return "opencode routed to the local model — wrote " + path, nil
	case "codex":
		// codex needs an OpenAI /v1/responses-compatible endpoint; probe
		// before writing so the user gets a clear error instead of codex
		// choking at launch.
		ctx, cancel := context.WithTimeout(a.ctx, 12*time.Second)
		warn, perr := ollama.ProbeCodexCompat(ctx, s.Endpoint, s.APIKey, s.Model)
		cancel()
		if perr != nil {
			_, _ = ollama.DisableCodex() // strip any stale block
			return "", fmt.Errorf("codex needs an OpenAI /v1/responses endpoint: %w", perr)
		}
		if s.WireAPI == "" {
			s.WireAPI = "responses" // codex ≥0.130 requires it
		}
		path, err := ollama.ApplyCodex(s)
		if err != nil {
			return "", err
		}
		msg := "codex routed to the local model — wrote " + path
		if warn != "" {
			msg += " (note: " + warn + ")"
		}
		return msg, nil
	default:
		return "", fmt.Errorf("apply-to-local is for opencode/codex only — claude/openclaude use the per-chat toggle")
	}
}

// DisableLocalForCLI removes the local ollama_remote route from a CLI's
// global config, returning it to its cloud default.
func (a *App) DisableLocalForCLI(cli string) (string, error) {
	switch cli {
	case "opencode":
		path, err := ollama.DisableOpenCode()
		if err != nil {
			return "", err
		}
		return "opencode local route removed — " + path, nil
	case "codex":
		path, err := ollama.DisableCodex()
		if err != nil {
			return "", err
		}
		return "codex local route removed — " + path, nil
	default:
		return "", fmt.Errorf("nothing to disable for %s", cli)
	}
}
