package main

import (
	"path/filepath"
	"slices"
	"testing"

	"git.jtsec.local/lab/PrAImate/internal/core"
	"git.jtsec.local/lab/PrAImate/internal/launcher"
	"git.jtsec.local/lab/PrAImate/internal/ollama"
	"git.jtsec.local/lab/PrAImate/internal/store"
)

func TestWorkflowModelAndEnvInjectsEncryptedKeyForManagedOpenCode(t *testing.T) {
	root := filepath.Join(t.TempDir(), "praimate")
	t.Setenv("PRAIMATE_HOME", root)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := launcher.SaveConfig(&launcher.Config{DefaultLocalEndpoint: "https://llm.example"}); err != nil {
		t.Fatal(err)
	}
	st, err := store.InitializeWithPassword(filepath.Join(root, "db.sqlite"), "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	c, _ := core.New(core.Options{Store: st})
	if err := saveLocalLLMAPIKey(c, "db-secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := ollama.ApplyOpenCode(ollama.Settings{
		Endpoint: "https://llm.example", Model: "qwen3", APIKey: "db-secret",
	}, true); err != nil {
		t.Fatal(err)
	}

	a := &App{core: c}
	model, env, err := a.workflowModelAndEnv("praimate-code", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if model != "" {
		t.Fatalf("model = %q, want CLI configured default", model)
	}
	if got := env["OPENAI_API_KEY"]; got != "db-secret" {
		t.Fatalf("OPENAI_API_KEY = %q, want encrypted DB credential", got)
	}
}

func TestTerminalRoutingEnvInjectsEncryptedKeyForManagedOpenCode(t *testing.T) {
	root := filepath.Join(t.TempDir(), "praimate")
	t.Setenv("PRAIMATE_HOME", root)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := launcher.SaveConfig(&launcher.Config{DefaultLocalEndpoint: "https://llm.example"}); err != nil {
		t.Fatal(err)
	}
	st, err := store.InitializeWithPassword(filepath.Join(root, "db.sqlite"), "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	c, _ := core.New(core.Options{Store: st})
	if err := saveLocalLLMAPIKey(c, "terminal-secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := ollama.ApplyOpenCode(ollama.Settings{
		Endpoint: "https://llm.example", Model: "qwen3", APIKey: "terminal-secret",
	}, true); err != nil {
		t.Fatal(err)
	}

	env, err := (&App{core: c}).terminalRoutingEnv("opencode", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(env, "OPENAI_API_KEY=terminal-secret") {
		t.Fatalf("managed OpenCode terminal env = %q", env)
	}
}

func TestApplyLocalToCLIRejectsCodex(t *testing.T) {
	root := filepath.Join(t.TempDir(), "praimate")
	t.Setenv("PRAIMATE_HOME", root)
	if err := launcher.SaveConfig(&launcher.Config{DefaultLocalEndpoint: "https://llm.example"}); err != nil {
		t.Fatal(err)
	}
	st, err := store.InitializeWithPassword(filepath.Join(root, "db.sqlite"), "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	c, _ := core.New(core.Options{Store: st})
	a := &App{core: c}
	if _, err := a.ApplyLocalToCLI("codex", "qwen3"); err == nil {
		t.Fatal("Codex local routing must not be accepted")
	}
}
