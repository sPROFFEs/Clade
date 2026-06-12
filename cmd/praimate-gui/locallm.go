package main

// Local LLM tab bindings — the GUI counterpart of the TUI's local-LLM
// screen. Reads/writes the GLOBAL default endpoint in launcher.Config
// (the same fields the TUI's wizard offers as "use the saved
// endpoint"), and probes the endpoint's model list for the Test button.

import (
	"context"
	"time"

	"github.com/sPROFFEs/PrAImate/internal/launcher"
	"github.com/sPROFFEs/PrAImate/internal/ollama"
)

// LocalLLMDefaults mirrors launcher.Config's DefaultLocal* slice.
type LocalLLMDefaults struct {
	Endpoint      string `json:"endpoint"`
	APIKey        string `json:"apiKey"`
	WireAPI       string `json:"wireApi"`        // "", "responses", "chat"
	ContextTokens int    `json:"contextTokens"`
	OutputTokens  int    `json:"outputTokens"`
}

// GetLocalLLM returns the saved global default endpoint (zero values
// when none configured).
func (a *App) GetLocalLLM() (*LocalLLMDefaults, error) {
	cfg, err := launcher.LoadConfig()
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return &LocalLLMDefaults{}, nil
	}
	return &LocalLLMDefaults{
		Endpoint:      cfg.DefaultLocalEndpoint,
		APIKey:        cfg.DefaultLocalAPIKey,
		WireAPI:       cfg.DefaultLocalWireAPI,
		ContextTokens: cfg.DefaultLocalContextTokens,
		OutputTokens:  cfg.DefaultLocalOutputTokens,
	}, nil
}

// SetLocalLLM persists the global default endpoint. Shared with the
// TUI (same config.json) and with other machines via the backup's
// shareable-config sync.
func (a *App) SetLocalLLM(d LocalLLMDefaults) error {
	cfg, err := launcher.LoadConfig()
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = &launcher.Config{}
	}
	cfg.DefaultLocalEndpoint = d.Endpoint
	cfg.DefaultLocalAPIKey = d.APIKey
	cfg.DefaultLocalWireAPI = d.WireAPI
	cfg.DefaultLocalContextTokens = d.ContextTokens
	cfg.DefaultLocalOutputTokens = d.OutputTokens
	return launcher.SaveConfig(cfg)
}

// TestLocalLLM probes the endpoint and returns its model list
// (Ollama /api/tags or OpenAI-compatible /v1/models — ollama.ListModels
// tries both).
func (a *App) TestLocalLLM(endpoint, apiKey string) ([]string, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	return ollama.ListModels(ctx, ollama.NormalizeEndpoint(endpoint), apiKey)
}
