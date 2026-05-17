package launcher

import (
	"os"
	"path/filepath"
	"testing"
)

// chatFromSeededReversing seeds the bundled samples as templates, then
// creates a fresh chat from the "reversing" template — the canonical
// fixture for launch-path tests.
func chatFromSeededReversing(t *testing.T) Chat {
	t.Helper()
	src := samplesDir(t)
	if _, err := os.Stat(src); err != nil {
		t.Skipf("no samples dir at %s", src)
	}
	root := t.TempDir()
	if _, err := SeedSamples(root, []string{src}); err != nil {
		t.Fatal(err)
	}
	tpl, err := LoadTemplate(root, "reversing")
	if err != nil || tpl == nil {
		t.Fatalf("LoadTemplate(reversing): %v %v", err, tpl)
	}
	chat, err := CreateChat(root, *tpl, "test-chat", AgentClaude)
	if err != nil {
		t.Fatalf("CreateChat: %v", err)
	}
	return chat
}

func TestPrepareSandbox_CompilesCodexTarget(t *testing.T) {
	chat := chatFromSeededReversing(t)
	ws := chat.AsWorkspace()
	// We compile reversing for Codex even though chat.AgentID is Claude —
	// the test just exercises the codex target path.
	agent := Agent{
		ID: AgentCodex, Label: "Codex CLI", Binary: "codex",
		WpcTarget: "codex", Available: true,
	}
	if err := PrepareSandbox(ws, agent); err != nil {
		t.Fatalf("PrepareSandbox: %v", err)
	}
	for _, rel := range []string{"AGENTS.md", "AGENTS.assets/tools/file_summary.sh", "SANDBOX.md"} {
		if _, err := os.Stat(filepath.Join(ws.SandboxDir, rel)); err != nil {
			t.Errorf("expected %s in sandbox: %v", rel, err)
		}
	}
}

func TestPrepareSandbox_CompilesClaudeTarget(t *testing.T) {
	chat := chatFromSeededReversing(t)
	ws := chat.AsWorkspace()
	agent := Agent{
		ID: AgentClaude, Label: "Claude Code", Binary: "claude",
		WpcTarget: "claude", Available: true,
	}
	if err := PrepareSandbox(ws, agent); err != nil {
		t.Fatalf("PrepareSandbox: %v", err)
	}
	// Claude target writes <sandbox>/.claude/skills/<template>/SKILL.md —
	// the template name flows through AsWorkspace.Name on purpose so two
	// chats cloned from the same template both compile a skill of that
	// name into their respective sandboxes.
	skill := filepath.Join(ws.SandboxDir, ".claude", "skills", "reversing", "SKILL.md")
	if _, err := os.Stat(skill); err != nil {
		t.Errorf("expected Claude skill at %s: %v", skill, err)
	}
}

func TestPlan_AppendsModelArgForClaudeWhenOllamaConfigured(t *testing.T) {
	chat := chatFromSeededReversing(t)
	chat.Settings = WorkspaceSettings{
		Ollama: OllamaSettings{Endpoint: "http://10.0.0.1:11434", Model: "qwen3"},
	}
	if err := SaveChatSettings(chat); err != nil {
		t.Fatal(err)
	}
	loaded, _ := LoadChat(filepath.Dir(filepath.Dir(chat.Root)), chat.ID)
	ws := loaded.AsWorkspace()
	agent := Agent{ID: AgentClaude, Binary: "claude", WpcTarget: "claude", Available: true}
	plan, err := Plan(ws, agent)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--model", "qwen3"}
	if !equalStrings(plan.Args, want) {
		t.Errorf("Args = %v, want %v", plan.Args, want)
	}
	if plan.Env["ANTHROPIC_BASE_URL"] != "http://10.0.0.1:11434" {
		t.Errorf("ANTHROPIC_BASE_URL = %q", plan.Env["ANTHROPIC_BASE_URL"])
	}
}

func TestPlan_AppendsOpenAIEnvAndModelArgForGeminiWhenOllamaConfigured(t *testing.T) {
	chat := chatFromSeededReversing(t)
	chat.Settings = WorkspaceSettings{
		Ollama: OllamaSettings{Endpoint: "http://10.0.0.1:11434", Model: "qwen3"},
	}
	_ = SaveChatSettings(chat)
	loaded, _ := LoadChat(filepath.Dir(filepath.Dir(chat.Root)), chat.ID)
	ws := loaded.AsWorkspace()
	agent := Agent{ID: AgentGemini, Binary: "gemini", WpcTarget: "gemini", Available: true}
	plan, err := Plan(ws, agent)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--model", "qwen3"}
	if !equalStrings(plan.Args, want) {
		t.Errorf("Args = %v, want %v", plan.Args, want)
	}
	if plan.Env["OPENAI_BASE_URL"] != "http://10.0.0.1:11434/v1" {
		t.Errorf("OPENAI_BASE_URL = %q, want http://10.0.0.1:11434/v1", plan.Env["OPENAI_BASE_URL"])
	}
	if plan.Env["OPENAI_API_KEY"] != "ollama" {
		t.Errorf("OPENAI_API_KEY = %q", plan.Env["OPENAI_API_KEY"])
	}
	// Gemini routing is OpenAI-style; no ANTHROPIC_* env vars should leak.
	if plan.Env["ANTHROPIC_BASE_URL"] != "" {
		t.Errorf("Gemini plan shouldn't carry ANTHROPIC_BASE_URL; got %q", plan.Env["ANTHROPIC_BASE_URL"])
	}
}

func TestPlan_AppendsProfileArgForCodexWhenOllamaConfigured(t *testing.T) {
	chat := chatFromSeededReversing(t)
	chat.Settings = WorkspaceSettings{
		Ollama: OllamaSettings{Endpoint: "http://10.0.0.1:11434", Model: "qwen3"},
	}
	_ = SaveChatSettings(chat)
	loaded, _ := LoadChat(filepath.Dir(filepath.Dir(chat.Root)), chat.ID)
	ws := loaded.AsWorkspace()
	agent := Agent{ID: AgentCodex, Binary: "codex", WpcTarget: "codex", Available: true}
	plan, err := Plan(ws, agent)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-p", "ollama_remote"}
	if !equalStrings(plan.Args, want) {
		t.Errorf("Args = %v, want %v", plan.Args, want)
	}
	// Codex routes via config.toml, not env — Plan should NOT inject ANTHROPIC_*.
	if plan.Env["ANTHROPIC_BASE_URL"] != "" {
		t.Errorf("Codex shouldn't get ANTHROPIC_BASE_URL env injection; got %q", plan.Env["ANTHROPIC_BASE_URL"])
	}
}

func TestPlan_NoExtraArgsWhenOllamaNotConfigured(t *testing.T) {
	chat := chatFromSeededReversing(t)
	ws := chat.AsWorkspace()
	for _, agent := range []Agent{
		{ID: AgentClaude, Binary: "claude", WpcTarget: "claude", Available: true},
		{ID: AgentCodex, Binary: "codex", WpcTarget: "codex", Available: true},
		{ID: AgentGemini, Binary: "gemini", WpcTarget: "gemini", Available: true},
	} {
		plan, err := Plan(ws, agent)
		if err != nil {
			t.Fatalf("Plan(%s): %v", agent.ID, err)
		}
		if len(plan.Args) != 0 {
			t.Errorf("%s without ollama should have no Args; got %v", agent.ID, plan.Args)
		}
	}
}

func TestPrepareSandbox_RefusesUnavailableAgent(t *testing.T) {
	root := t.TempDir()
	tpl, err := CreateTemplate(root, "empty", "x")
	if err != nil {
		t.Fatal(err)
	}
	chat, err := CreateChat(root, tpl, "x-chat", AgentCodex)
	if err != nil {
		t.Fatal(err)
	}
	agent := Agent{ID: AgentCodex, Binary: "codex", WpcTarget: "codex", Available: false}
	if err := PrepareSandbox(chat.AsWorkspace(), agent); err == nil {
		t.Error("expected error for unavailable agent")
	}
}

func TestDetectAgents_PopulatesEntries(t *testing.T) {
	// We can't assume anything about which CLIs are installed on the test
	// host; just check the catalog comes back with the expected IDs and
	// that Available is a deterministic bool (not panicking, etc.).
	agents := DetectAgents(t.Context())
	if len(agents) != 4 {
		t.Fatalf("expected 4 agents, got %d", len(agents))
	}
	wantIDs := map[AgentID]bool{AgentClaude: true, AgentCodex: true, AgentOpenCode: true, AgentGemini: true}
	for _, a := range agents {
		if !wantIDs[a.ID] {
			t.Errorf("unexpected agent ID %q", a.ID)
		}
		if a.Binary == "" || a.WpcTarget == "" {
			t.Errorf("agent %s has empty binary or target", a.ID)
		}
	}
}
