package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sPROFFEs/Clade/internal/launcher"
	"github.com/sPROFFEs/Clade/internal/ollama"
)

// TestOllamaFlow_FullWizardWritesPerChatClaudeSettings: drives every step
// of the Ollama screen against a fake Ollama server. End state:
// chat.json has the right Ollama block (because SaveWorkspaceLikeSettings
// detects the Chat root), and Plan() for Claude injects ANTHROPIC_BASE_URL.
func TestOllamaFlow_FullWizardWritesPerChatClaudeSettings(t *testing.T) {
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
	tpl, _ := launcher.LoadTemplate(tmp, "reversing")
	chat, err := launcher.CreateChat(tmp, *tpl, "ollama-test", launcher.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &launcher.Config{WorkspacesRoot: tmp}
	ws := chat.AsWorkspace()
	m := newOllamaModel(cfg, ws)

	m.endpoint.SetValue(srv.URL)
	nx, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(ollamaModel)
	probe := runCmd(t, cmd)
	nx, _ = m.Update(probe)
	m = nx.(ollamaModel)
	if m.step != ollamaStepModel {
		t.Fatalf("step = %d, want %d (model)", m.step, ollamaStepModel)
	}
	if len(m.probedModels) != 2 {
		t.Fatalf("probedModels = %v", m.probedModels)
	}

	// Pick second model.
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = nx.(ollamaModel)
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(ollamaModel)
	if m.modelInput.Value() != "qwen3" {
		t.Errorf("modelInput = %q, want qwen3", m.modelInput.Value())
	}

	// Boxes are now pre-checked from disk state — a brand-new chat
	// starts with NONE checked. Tick Claude explicitly with space, then
	// Enter to apply.
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = nx.(ollamaModel)
	if !m.pickClaude {
		t.Fatal("space should toggle claude on")
	}
	nx, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(ollamaModel)
	apply := runCmd(t, cmd)
	nx, _ = m.Update(apply)
	m = nx.(ollamaModel)
	if !m.applied {
		t.Fatal("expected applied=true")
	}
	if !strings.Contains(strings.Join(m.results, "\n"), "claude") {
		t.Errorf("expected claude result; got %v", m.results)
	}

	// Persisted to chat.json (not workspace.json) because of the smart saver.
	chatJSON := filepath.Join(ws.Root, "chat.json")
	raw, err := os.ReadFile(chatJSON)
	if err != nil {
		t.Fatalf("chat.json missing: %v", err)
	}
	if !strings.Contains(string(raw), srv.URL) || !strings.Contains(string(raw), "qwen3") {
		t.Errorf("chat.json doesn't contain Ollama settings:\n%s", raw)
	}
	// And ensure we did NOT pollute with a stray workspace.json.
	if _, err := os.Stat(filepath.Join(ws.Root, "workspace.json")); err == nil {
		t.Error("settings should NOT have been written to workspace.json on a chat")
	}

	// Plan() for Claude auto-injects the env vars.
	chat2, _ := launcher.LoadChat(tmp, chat.ID)
	ws2 := chat2.AsWorkspace()
	agent := launcher.Agent{
		ID: launcher.AgentClaude, Label: "Claude", Binary: "claude",
		WpcTarget: "claude", Available: true,
	}
	plan, err := launcher.Plan(ws2, agent)
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

// TestOllamaScreen_PreChecksBoxesFromDisk: when the user has previously
// applied Ollama settings for codex/opencode, reopening the screen
// should show those boxes already checked. The bug was that only
// Claude defaulted to true and the others were unchecked even when
// their config files clearly had the ollama_remote block.
func TestOllamaScreen_PreChecksBoxesFromDisk(t *testing.T) {
	tmp := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("USERPROFILE", tmp)
		t.Setenv("APPDATA", filepath.Join(tmp, "cfg"))
	default:
		t.Setenv("HOME", tmp)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdg"))
	}

	// Pre-stage both codex + opencode configs.
	s := ollama.Settings{Endpoint: "http://10.0.0.1:11434", Model: "qwen3"}
	if _, err := ollama.ApplyCodex(s); err != nil {
		t.Fatal(err)
	}
	if _, err := ollama.ApplyOpenCode(s, true); err != nil {
		t.Fatal(err)
	}

	// Build a chat with claude already configured.
	root := seededRoot(t)
	tpl, _ := launcher.LoadTemplate(root, "reversing")
	chat, _ := launcher.CreateChat(root, *tpl, "precheck", launcher.AgentClaude)
	chat.Settings.Ollama = launcher.OllamaSettings{Endpoint: s.Endpoint, Model: s.Model}
	_ = launcher.SaveChatSettings(chat)

	loaded, _ := launcher.LoadChat(root, chat.ID)
	m := newOllamaModel(&launcher.Config{WorkspacesRoot: root}, loaded.AsWorkspace())
	if !m.pickClaude {
		t.Error("pickClaude should be true when ws.Settings.Ollama is populated")
	}
	if !m.pickCodex {
		t.Error("pickCodex should be true when ~/.codex/config.toml has the ollama_remote block")
	}
	if !m.pickOpenCode {
		t.Error("pickOpenCode should be true when opencode.json has the ollama_remote provider")
	}
}

// Pull in os/runtime for the new test.
var _ = runtime.GOOS

func TestOllamaFlow_FallsBackToManualEntryOnProbeError(t *testing.T) {
	tmp := t.TempDir()
	redirectConfig(t, filepath.Join(tmp, "cfg"))
	t.Chdir(repoRoot(t))
	_, _ = launcher.SeedSamples(tmp, []string{filepath.Join(repoRoot(t), "samples", "workpaths")})
	tpl, _ := launcher.LoadTemplate(tmp, "reversing")
	chat, _ := launcher.CreateChat(tmp, *tpl, "probe-fail-test", launcher.AgentClaude)
	cfg := &launcher.Config{WorkspacesRoot: tmp}

	m := newOllamaModel(cfg, chat.AsWorkspace())
	m.endpoint.SetValue("http://127.0.0.1:1") // refuses connection
	nx, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(ollamaModel)
	probe := runCmd(t, cmd)
	nx, _ = m.Update(probe)
	m = nx.(ollamaModel)

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
