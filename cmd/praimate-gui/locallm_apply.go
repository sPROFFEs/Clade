package main

// Apply the saved local endpoint to OpenCode-compatible CLIs from the GUI.
// Claude/OpenClaude route per launch through environment variables.

import (
	"fmt"
	"strings"

	"git.jtsec.local/lab/PrAImate/internal/ollama"
)

// LocalCLIStatus reports whether the OpenCode config currently has the local
// ollama_remote route applied. praimate-code is
// the OpenCode fork (name-only rebrand) and reads the SAME opencode.json,
// so the opencode route covers it.
type LocalCLIStatus struct {
	Opencode bool `json:"opencode"` // also governs praimate-code (shared config)
}

// LocalCLIStatusNow probes the on-disk config of the config-file CLIs.
func (a *App) LocalCLIStatusNow() LocalCLIStatus {
	return LocalCLIStatus{
		Opencode: ollama.OpenCodeConfigured(),
	}
}

// ApplyLocalToCLI writes the saved local endpoint into opencode.json so
// OpenCode and PrAImate Code route to the local model. model is
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
	apiKey, err := loadLocalLLMAPIKey(a.core)
	if err != nil {
		return "", fmt.Errorf("load local LLM credential: %w", err)
	}
	s := ollama.Settings{
		Endpoint:      d.Endpoint,
		APIKey:        apiKey,
		Model:         strings.TrimSpace(model),
		ContextTokens: d.ContextTokens,
		OutputTokens:  d.OutputTokens,
	}
	switch cli {
	case "opencode", "praimate-code":
		// Shared config: praimate-code is OpenCode rebranded name-only and
		// reads the same opencode.json, so one write routes both.
		path, err := ollama.ApplyOpenCode(s, true)
		if err != nil {
			return "", err
		}
		return "opencode + praimate-code routed to the local model — wrote " + path, nil
	default:
		return "", fmt.Errorf("apply-to-local supports only opencode/praimate-code — claude/openclaude use the per-launch toggle")
	}
}

// DisableLocalForCLI removes the local ollama_remote route from a CLI's
// global config, returning it to its cloud default.
func (a *App) DisableLocalForCLI(cli string) (string, error) {
	switch cli {
	case "opencode", "praimate-code":
		path, err := ollama.DisableOpenCode()
		if err != nil {
			return "", err
		}
		return "opencode + praimate-code local route removed — " + path, nil
	default:
		return "", fmt.Errorf("nothing to disable for %s", cli)
	}
}
