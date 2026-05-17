package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sdksdk/code-launcher/internal/launcher"
)

// TestOllamaFlow_FullWizardWritesPerWorkspaceClaudeSettings drives every
// step of the Ollama screen against a fake Ollama server. End state:
// workspace.json has the right Ollama block, and Plan() for Claude
// injects ANTHROPIC_BASE_URL.
func TestOllamaFlow_FullWizardWritesPerWorkspaceClaudeSettings(t *testing.T) {
	// Fake Ollama: returns two models via /api/tags.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]string{{"name": "llama3"}, {"name": "qwen3"}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	tmp := t.TempDir()
	redirectConfig(t, filepath.Join(tmp, "cfg"))
	t.Chdir(repoRoot(t))
	if _, err := launcher.SeedSamples(tmp, []string{filepath.Join(repoRoot(t), "samples", "workpaths")}); err != nil {
		t.Fatal(err)
	}
	ws, _ := launcher.LoadWorkspace(tmp, "reversing")
	cfg := &launcher.Config{WorkspacesRoot: tmp}

	m := newOllamaModel(cfg, *ws)

	// Step 0 — endpoint
	m.endpoint.SetValue(srv.URL)
	nx, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(ollamaModel)
	if cmd == nil {
		t.Fatal("Enter on endpoint should produce probe Cmd")
	}
	probe := runCmd(t, cmd)
	nx, _ = m.Update(probe)
	m = nx.(ollamaModel)
	if m.step != ollamaStepModel {
		t.Fatalf("step = %d, want %d (model)", m.step, ollamaStepModel)
	}
	if len(m.probedModels) != 2 {
		t.Fatalf("probedModels = %v", m.probedModels)
	}

	// Step 1 — pick second model (down + enter)
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = nx.(ollamaModel)
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(ollamaModel)
	if m.step != ollamaStepAgents {
		t.Fatalf("step = %d, want %d (agents)", m.step, ollamaStepAgents)
	}
	if m.modelInput.Value() != "qwen3" {
		t.Errorf("modelInput = %q, want qwen3", m.modelInput.Value())
	}

	// Step 2 — only Claude is pre-checked. Enter to apply.
	nx, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(ollamaModel)
	if cmd == nil {
		t.Fatal("Enter on agents step should produce apply Cmd")
	}
	apply := runCmd(t, cmd)
	nx, _ = m.Update(apply)
	m = nx.(ollamaModel)
	if !m.applied {
		t.Fatal("expected applied=true after apply")
	}
	joined := strings.Join(m.results, "\n")
	if !strings.Contains(joined, "claude") {
		t.Errorf("expected claude result, got:\n%s", joined)
	}

	// Workspace settings persisted.
	wsPath := filepath.Join(ws.Root, "workspace.json")
	raw, err := os.ReadFile(wsPath)
	if err != nil {
		t.Fatalf("workspace.json missing: %v", err)
	}
	if !strings.Contains(string(raw), srv.URL) || !strings.Contains(string(raw), "qwen3") {
		t.Errorf("workspace.json doesn't contain Ollama settings:\n%s", raw)
	}

	// Plan() for Claude now auto-injects the env vars.
	ws2, _ := launcher.LoadWorkspace(tmp, "reversing")
	agent := launcher.Agent{
		ID: launcher.AgentClaude, Label: "Claude", Binary: "claude",
		WpcTarget: "claude", Available: true,
	}
	plan, err := launcher.Plan(*ws2, agent)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Env["ANTHROPIC_BASE_URL"] != srv.URL {
		t.Errorf("ANTHROPIC_BASE_URL = %q, want %q", plan.Env["ANTHROPIC_BASE_URL"], srv.URL)
	}
	if plan.Env["ANTHROPIC_AUTH_TOKEN"] != "ollama" {
		t.Errorf("ANTHROPIC_AUTH_TOKEN = %q", plan.Env["ANTHROPIC_AUTH_TOKEN"])
	}
}

func TestOllamaFlow_FallsBackToManualEntryOnProbeError(t *testing.T) {
	// Endpoint that's not even a valid HTTP server — probe will fail.
	tmp := t.TempDir()
	redirectConfig(t, filepath.Join(tmp, "cfg"))
	t.Chdir(repoRoot(t))
	_, _ = launcher.SeedSamples(tmp, []string{filepath.Join(repoRoot(t), "samples", "workpaths")})
	ws, _ := launcher.LoadWorkspace(tmp, "reversing")
	cfg := &launcher.Config{WorkspacesRoot: tmp}

	m := newOllamaModel(cfg, *ws)
	m.endpoint.SetValue("http://127.0.0.1:1") // refuses connection
	nx, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(ollamaModel)
	probe := runCmd(t, cmd)
	nx, _ = m.Update(probe)
	m = nx.(ollamaModel)

	// Falls through to manual model entry.
	if m.step != ollamaStepModel {
		t.Errorf("step = %d, want %d", m.step, ollamaStepModel)
	}
	if len(m.probedModels) != 0 {
		t.Errorf("probedModels should be empty, got %v", m.probedModels)
	}
	if m.probeErr == "" {
		t.Error("expected probeErr to be set")
	}
}
