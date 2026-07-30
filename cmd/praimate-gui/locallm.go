package main

// Local LLM tab bindings. Reads/writes the global default endpoint in
// launcher.Config and probes the endpoint's model list for the Test button.

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"git.jtsec.local/lab/PrAImate/internal/core"
	"git.jtsec.local/lab/PrAImate/internal/launcher"
	"git.jtsec.local/lab/PrAImate/internal/ollama"
)

const localLLMAPIKeySetting = "local_llm.api_key"

// LocalLLMDefaults mirrors launcher.Config's DefaultLocal* slice.
type LocalLLMDefaults struct {
	Endpoint      string `json:"endpoint"`
	APIKey        string `json:"apiKey"`
	WireAPI       string `json:"wireApi"` // "", "responses", "chat"
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
	apiKey := ""
	if a.core != nil {
		apiKey, err = loadLocalLLMAPIKey(a.core)
		if err != nil {
			return nil, err
		}
	} else {
		// Startup failures may leave Core unavailable. Preserve read-only
		// access to a legacy config until the next successful migration.
		apiKey = cfg.DefaultLocalAPIKey
	}
	return &LocalLLMDefaults{
		Endpoint:      cfg.DefaultLocalEndpoint,
		APIKey:        apiKey,
		WireAPI:       cfg.DefaultLocalWireAPI,
		ContextTokens: cfg.DefaultLocalContextTokens,
		OutputTokens:  cfg.DefaultLocalOutputTokens,
	}, nil
}

// SetLocalLLM persists the global default endpoint and shares it with
// other machines via the backup's shareable-config sync.
func (a *App) SetLocalLLM(d LocalLLMDefaults) error {
	c, err := a.requireCore()
	if err != nil {
		return err
	}
	cfg, err := launcher.LoadConfig()
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = &launcher.Config{}
	}
	cfg.DefaultLocalEndpoint = d.Endpoint
	cfg.DefaultLocalAPIKey = "" // migrated secret must never return to plaintext config
	cfg.DefaultLocalWireAPI = d.WireAPI
	cfg.DefaultLocalContextTokens = d.ContextTokens
	cfg.DefaultLocalOutputTokens = d.OutputTokens
	if err := saveLocalLLMAPIKey(c, d.APIKey); err != nil {
		return err
	}
	return launcher.SaveConfig(cfg)
}

func loadLocalLLMAPIKey(c *core.Core) (string, error) {
	raw, err := c.GetSetting(context.Background(), core.ScopeCLI, localLLMAPIKeySetting)
	if err != nil || raw == nil {
		return "", err
	}
	var key string
	if err := json.Unmarshal(raw, &key); err != nil {
		return "", err
	}
	return key, nil
}

func saveLocalLLMAPIKey(c *core.Core, key string) error {
	if c == nil {
		return errors.New("save local LLM API key: core unavailable")
	}
	if key == "" {
		return c.DeleteSetting(context.Background(), core.ScopeCLI, localLLMAPIKeySetting)
	}
	raw, err := json.Marshal(key)
	if err != nil {
		return err
	}
	return c.SetSetting(context.Background(), core.ScopeCLI, localLLMAPIKeySetting, raw)
}

// migrateLegacyLocalLLMAPIKey moves the pre-1.1 plaintext config field into
// the encrypted database. Saving config last makes the migration retry-safe.
func migrateLegacyLocalLLMAPIKey(c *core.Core, cfg *launcher.Config) error {
	if c == nil || cfg == nil || cfg.DefaultLocalAPIKey == "" {
		return nil
	}
	existing, err := loadLocalLLMAPIKey(c)
	if err != nil {
		return err
	}
	if existing == "" {
		if err := saveLocalLLMAPIKey(c, cfg.DefaultLocalAPIKey); err != nil {
			return err
		}
	}
	cfg.DefaultLocalAPIKey = ""
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

// LocalLLMOption bundles the saved default endpoint with its live model
// list, so the new-chat / new-code-session pickers can offer the local
// LLM in one call. Models is best-effort — Error carries a probe failure
// without failing the whole call (the endpoint may just be offline).
type LocalLLMOption struct {
	Configured bool     `json:"configured"`
	Endpoint   string   `json:"endpoint"`
	APIKey     string   `json:"apiKey"`
	WireAPI    string   `json:"wireApi"`
	Models     []string `json:"models"`
	Error      string   `json:"error,omitempty"`
}

// LocalLLMModels returns the configured global local endpoint and its
// available models, for the new-chat and new-code-session model
// pickers. Configured is false when no endpoint is set in Settings.
func (a *App) LocalLLMModels() (*LocalLLMOption, error) {
	d, err := a.GetLocalLLM()
	if err != nil {
		return nil, err
	}
	opt := &LocalLLMOption{
		Configured: d.Endpoint != "",
		Endpoint:   d.Endpoint,
		APIKey:     d.APIKey,
		WireAPI:    d.WireAPI,
	}
	if !opt.Configured {
		return opt, nil
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	models, err := ollama.ListModels(ctx, ollama.NormalizeEndpoint(d.Endpoint), d.APIKey)
	if err != nil {
		opt.Error = err.Error()
		return opt, nil
	}
	opt.Models = models
	return opt, nil
}
