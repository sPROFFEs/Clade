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
	// Endpoint Enter → API-key step (no probe yet).
	nx, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(ollamaModel)
	if m.step != ollamaStepAPIKey {
		t.Fatalf("after endpoint Enter, step = %d, want %d (apiKey)", m.step, ollamaStepAPIKey)
	}
	// API-key Enter (blank = no auth, Ollama path) → probe → model step.
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

// TestOllamaFlow_PersistsAPIKeyAndPlanUsesIt: end-to-end check of the
// GPUStack-style path. User enters an endpoint, an API key, picks a
// model from the probe, ticks Claude, applies, then Plan() injects the
// real key as ANTHROPIC_AUTH_TOKEN / OPENAI_API_KEY.
func TestOllamaFlow_PersistsAPIKeyAndPlanUsesIt(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path == "/api/tags" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "qwen3-0.6b"}},
		})
	}))
	defer srv.Close()

	tmp := t.TempDir()
	redirectConfig(t, filepath.Join(tmp, "cfg"))
	t.Chdir(repoRoot(t))
	if _, err := launcher.SeedSamples(tmp, []string{filepath.Join(repoRoot(t), "samples", "workpaths")}); err != nil {
		t.Fatal(err)
	}
	tpl, _ := launcher.LoadTemplate(tmp, "reversing")
	chat, err := launcher.CreateChat(tmp, *tpl, "gpustack-test", launcher.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &launcher.Config{WorkspacesRoot: tmp}
	m := newOllamaModel(cfg, chat.AsWorkspace())

	m.endpoint.SetValue(srv.URL)
	nx, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(ollamaModel)
	// Type a key, then Enter to probe.
	m.apiKey.SetValue("sk-gpustack-secret")
	nx, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(ollamaModel)
	probe := runCmd(t, cmd)
	nx, _ = m.Update(probe)
	m = nx.(ollamaModel)

	if gotAuth != "Bearer sk-gpustack-secret" {
		t.Errorf("probe Auth header = %q, want Bearer sk-gpustack-secret", gotAuth)
	}
	if m.step != ollamaStepModel {
		t.Fatalf("step = %d, want %d (model)", m.step, ollamaStepModel)
	}

	// Pick the only model, tick Claude, apply.
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(ollamaModel)
	nx, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = nx.(ollamaModel)
	nx, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(ollamaModel)
	apply := runCmd(t, cmd)
	nx, _ = m.Update(apply)
	m = nx.(ollamaModel)
	if !m.applied {
		t.Fatal("expected applied=true")
	}

	// chat.json persists the key.
	raw, _ := os.ReadFile(filepath.Join(chat.AsWorkspace().Root, "chat.json"))
	if !strings.Contains(string(raw), "sk-gpustack-secret") {
		t.Errorf("chat.json should persist the apiKey:\n%s", raw)
	}

	// Plan() now injects the real key, not the "ollama" placeholder.
	chat2, _ := launcher.LoadChat(tmp, chat.ID)
	plan, err := launcher.Plan(chat2.AsWorkspace(), launcher.Agent{
		ID: launcher.AgentClaude, Label: "Claude", Binary: "claude",
		WpcTarget: "claude", Available: true,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Env["ANTHROPIC_AUTH_TOKEN"] != "sk-gpustack-secret" {
		t.Errorf("ANTHROPIC_AUTH_TOKEN = %q, want sk-gpustack-secret", plan.Env["ANTHROPIC_AUTH_TOKEN"])
	}
	if plan.Env["OPENAI_API_KEY"] != "sk-gpustack-secret" {
		t.Errorf("OPENAI_API_KEY = %q, want sk-gpustack-secret", plan.Env["OPENAI_API_KEY"])
	}
}

// TestApplyOllama_UntickingRemovesPreviouslyConfigured pins the bug
// where the wizard's checkboxes re-ticked themselves on re-open: the
// previous apply wrote a global config, the un-tick was a silent
// no-op, and the disk-state-based init read "still configured" and
// re-ticked the box.
//
// With the fix, unticking an agent that was previously configured
// actively strips the global block (Disable*) — or clears the
// chat-level Ollama block (claude). Round-trip works.
func TestApplyOllama_UntickingRemovesPreviouslyConfigured(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpHome, "xdg"))

	root := seededRoot(t)
	tpl, _ := launcher.LoadTemplate(root, "reversing")
	chat, _ := launcher.CreateChat(root, *tpl, "untick-test", launcher.AgentClaude)
	cfg := &launcher.Config{WorkspacesRoot: root}

	// First apply: tick everything. Writes the chat-level claude block
	// + every global agent config.
	settings := ollama.Settings{Endpoint: "http://host:1234", Model: "qwen"}
	all := applyPicks{claude: true, codex: true, opencode: true, deepseek: true}
	// Skip codex (would block on the network probe). Drive the other
	// three through the actual applier.
	tickedNoCodex := applyPicks{claude: true, opencode: true, deepseek: true}
	_ = applyOllama(chat.AsWorkspace(), settings, tickedNoCodex)
	_ = all // keep symbol live for the test-reader

	// Confirm everything's on disk now.
	loaded, _ := launcher.LoadChat(root, chat.ID)
	if loaded.Settings.Ollama.Endpoint == "" {
		t.Fatal("baseline: claude chat-level Ollama settings should be set")
	}
	if !ollama.OpenCodeConfigured() {
		t.Fatal("baseline: opencode should report configured")
	}
	if !ollama.DeepSeekConfigured() {
		t.Fatal("baseline: deepseek should report configured")
	}

	// Second apply: untick all three. Should actively disable each.
	out := applyOllama(loaded.AsWorkspace(), settings, applyPicks{})
	joined := strings.Join(out, "\n")
	for _, want := range []string{"claude: cleared", "opencode: removed", "deepseek: removed"} {
		if !strings.Contains(joined, want) {
			t.Errorf("apply log should mention %q after unticking; got:\n%s", want, joined)
		}
	}

	// Re-read disk: nothing should still claim to be configured.
	loaded2, _ := launcher.LoadChat(root, chat.ID)
	if loaded2.Settings.Ollama.Endpoint != "" {
		t.Errorf("claude chat settings should be cleared; got %+v", loaded2.Settings.Ollama)
	}
	if ollama.OpenCodeConfigured() {
		t.Error("opencode should report unconfigured after untick")
	}
	if ollama.DeepSeekConfigured() {
		t.Error("deepseek should report unconfigured after untick")
	}

	// And a fresh wizard opens with all boxes off — the round-trip the
	// user actually cares about.
	wsFresh := loaded2.AsWorkspace()
	wiz := newOllamaModelWithReturn(cfg, wsFresh, nil)
	if wiz.pickClaude || wiz.pickOpenCode || wiz.pickDeepSeek {
		t.Errorf("re-opened wizard re-ticked unconfigured agents: claude=%v opencode=%v deepseek=%v",
			wiz.pickClaude, wiz.pickOpenCode, wiz.pickDeepSeek)
	}
}

// TestOllamaModelWithReturn_NoUnconditionalClaudePreTick: the old
// fallback ("nothing configured? pre-tick claude") was removed. A
// brand-new chat with no Ollama config anywhere should open the
// wizard with EVERY box off. preTickAgentForChat is the only thing
// that should add ticks now, and only for the chat's locked agent.
func TestOllamaModelWithReturn_NoUnconditionalClaudePreTick(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpHome, "xdg"))

	root := seededRoot(t)
	tpl, _ := launcher.LoadTemplate(root, "reversing")
	chat, _ := launcher.CreateChat(root, *tpl, "no-pretick", launcher.AgentCodex)
	cfg := &launcher.Config{WorkspacesRoot: root}

	wiz := newOllamaModelWithReturn(cfg, chat.AsWorkspace(), nil)
	if wiz.pickClaude {
		t.Error("claude should NOT be pre-ticked on a brand-new chat with nothing configured")
	}
	if wiz.pickCodex || wiz.pickOpenCode || wiz.pickDeepSeek {
		t.Errorf("nothing should be pre-ticked; got codex=%v opencode=%v deepseek=%v",
			wiz.pickCodex, wiz.pickOpenCode, wiz.pickDeepSeek)
	}
}

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
	// endpoint Enter → API-key step → Enter again triggers probe.
	nx, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nx.(ollamaModel)
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
