package core

// Ollama distiller — POSTs the rendered prompt to /api/generate with
// format=json and parses the {"response":"..."} envelope into a
// DistillResult.
//
// We use /api/generate rather than /api/chat because the prompt is a
// single self-contained block (DistillPrompt + transcript) — chat
// framing would just add token overhead.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ollamaDistiller is the concrete Distiller for DistillKindOllama.
type ollamaDistiller struct {
	baseURL string
	model   string
	apiKey  string

	// httpClient is exposed for test injection. Production callers
	// shouldn't touch it; the zero value (filled in newOllamaDistiller)
	// uses a 60s timeout which is plenty for a single distill call.
	httpClient *http.Client
}

func newOllamaDistiller(ep DistillEndpoint) (*ollamaDistiller, error) {
	if ep.BaseURL == "" {
		return nil, errors.New("ollama distiller: BaseURL required")
	}
	if ep.Model == "" {
		return nil, errors.New("ollama distiller: Model required")
	}
	return &ollamaDistiller{
		baseURL:    strings.TrimRight(ep.BaseURL, "/"),
		model:      ep.Model,
		apiKey:     ep.APIKey,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (o *ollamaDistiller) Name() string { return "ollama:" + o.model }

// Available pings /api/tags to confirm the server is reachable. It
// does NOT verify that o.model is loaded — Ollama auto-pulls on first
// request, so absence here is not necessarily fatal.
func (o *ollamaDistiller) Available(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", o.baseURL+"/api/tags", nil)
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
	resp, err := o.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ollama unreachable at %s: %w", o.baseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("ollama /api/tags returned %d", resp.StatusCode)
	}
	return nil
}

type ollamaGenerateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
	Format string `json:"format"`
}

type ollamaGenerateResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

func (o *ollamaDistiller) Distill(ctx context.Context, messages []DistillMessage) (*DistillResult, error) {
	body, _ := json.Marshal(ollamaGenerateRequest{
		Model:  o.model,
		Prompt: RenderDistillInput(messages),
		Stream: false,
		Format: "json",
	})
	req, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama POST: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("ollama returned %d: %s", resp.StatusCode, truncDist(string(raw)))
	}
	var env ollamaGenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("decode ollama envelope: %w", err)
	}
	return ParseDistillJSON(env.Response)
}
