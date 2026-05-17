package ollama

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNormalizeEndpoint(t *testing.T) {
	cases := map[string]string{
		"192.168.1.10:11434":         "http://192.168.1.10:11434",
		"http://x.local:11434/":      "http://x.local:11434",
		"  http://x.local:11434  ":   "http://x.local:11434",
		"https://ollama.example.com": "https://ollama.example.com",
		"":                           "",
		// Real bug the user hit: a missing-slashes typo in the scheme.
		// Without the fix, the value would later be prepended with
		// another "http://" downstream, producing "http://http:host:port".
		"http:192.168.100.242:11434":  "http://192.168.100.242:11434",
		"https:example.com":           "http://example.com",
		"HTTP:192.168.1.10:11434":     "http://192.168.1.10:11434",
		"http:/lonely.slash:11434":    "http://lonely.slash:11434",
	}
	for in, want := range cases {
		if got := NormalizeEndpoint(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestListModels_PrefersOllamaTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]string{
					{"name": "qwen3-coder"},
					{"name": "llama3.1:8b"},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	models, err := ListModels(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0] != "llama3.1:8b" || models[1] != "qwen3-coder" {
		t.Errorf("got %v, want sorted [llama3.1:8b qwen3-coder]", models)
	}
}

func TestListModels_FallsBackToOpenAI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			http.NotFound(w, r) // simulate non-Ollama backend
			return
		}
		if r.URL.Path == "/v1/models" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{{"id": "phi3"}, {"id": "qwen"}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	models, _ := ListModels(context.Background(), srv.URL)
	if len(models) != 2 {
		t.Fatalf("got %v", models)
	}
}

func TestClaudeEnv(t *testing.T) {
	env := ClaudeEnv(Settings{Endpoint: "192.168.1.10:11434", Model: "qwen"})
	if env["ANTHROPIC_BASE_URL"] != "http://192.168.1.10:11434" {
		t.Errorf("ANTHROPIC_BASE_URL = %q", env["ANTHROPIC_BASE_URL"])
	}
	if env["ANTHROPIC_AUTH_TOKEN"] != "ollama" {
		t.Error("ANTHROPIC_AUTH_TOKEN not set")
	}
	if env["OPENAI_API_KEY"] != "ollama" {
		t.Error("OPENAI_API_KEY not set")
	}
}

func TestClaudeEnv_EmptyOnIncomplete(t *testing.T) {
	if e := ClaudeEnv(Settings{}); e != nil {
		t.Errorf("expected nil for empty settings, got %v", e)
	}
}

// redirectHome points UserHomeDir at a temp dir for this test only.
func redirectHome(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", dir)
	} else {
		t.Setenv("HOME", dir)
	}
}

func TestApplyCodex_RoundTripAndIdempotent(t *testing.T) {
	tmp := t.TempDir()
	redirectHome(t, tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp) // belt and braces

	path, err := ApplyCodex(Settings{Endpoint: "192.168.1.10:11434", Model: "qwen3"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	body := string(raw)
	for _, want := range []string{
		"[model_providers.ollama_remote]",
		`base_url = "http://192.168.1.10:11434/v1"`,
		"[profiles.ollama_remote]",
		`model = "qwen3"`,
		`wire_api = "chat"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("config missing %q\n%s", want, body)
		}
	}

	// Apply again — should be idempotent, not duplicate blocks.
	if _, err := ApplyCodex(Settings{Endpoint: "192.168.1.10:11434", Model: "qwen3"}); err != nil {
		t.Fatal(err)
	}
	raw2, _ := os.ReadFile(path)
	if strings.Count(string(raw2), "[model_providers.ollama_remote]") != 1 {
		t.Errorf("expected exactly 1 model_providers block after re-apply, got\n%s", raw2)
	}
}

func TestApplyCodex_PreservesUnrelatedConfig(t *testing.T) {
	tmp := t.TempDir()
	redirectHome(t, tmp)
	codexDir := filepath.Join(tmp, ".codex")
	_ = os.MkdirAll(codexDir, 0o755)
	preexisting := "default_profile = \"my_profile\"\n\n[profiles.my_profile]\nmodel = \"gpt-5\"\nmodel_provider = \"openai\"\n"
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(preexisting), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := ApplyCodex(Settings{Endpoint: "10.0.0.1:11434", Model: "phi3"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	if !strings.Contains(string(raw), "[profiles.my_profile]") {
		t.Errorf("preexisting profile lost:\n%s", raw)
	}
	if !strings.Contains(string(raw), "[profiles.ollama_remote]") {
		t.Errorf("new profile not added:\n%s", raw)
	}
}

func TestDisableCodex_RemovesBlocks(t *testing.T) {
	tmp := t.TempDir()
	redirectHome(t, tmp)
	if _, err := ApplyCodex(Settings{Endpoint: "x:1", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	if _, err := DisableCodex(); err != nil {
		t.Fatal(err)
	}
	path, _ := CodexConfigPath()
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "ollama_remote") {
		t.Errorf("disable left ollama_remote:\n%s", raw)
	}
}

func TestApplyOpenCode_WritesProviderJSON(t *testing.T) {
	tmp := t.TempDir()
	redirectHome(t, tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdg"))

	path, err := ApplyOpenCode(Settings{Endpoint: "10.0.0.1:11434", Model: "qwen3"}, true)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, raw)
	}
	if cfg["$schema"] != "https://opencode.ai/config.json" {
		t.Errorf("missing $schema")
	}
	if cfg["model"] != "ollama_remote/qwen3" {
		t.Errorf("model = %v", cfg["model"])
	}
	provider := cfg["provider"].(map[string]any)
	if _, ok := provider["ollama_remote"]; !ok {
		t.Error("ollama_remote provider not added")
	}
}

func TestApplyOpenCode_PreservesOtherProviders(t *testing.T) {
	tmp := t.TempDir()
	redirectHome(t, tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdg"))
	path, _ := OpenCodeConfigPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	pre, _ := json.Marshal(map[string]any{
		"$schema":  "x",
		"model":    "openai/gpt-5",
		"provider": map[string]any{"my_provider": map[string]any{"name": "Mine"}},
	})
	if err := os.WriteFile(path, pre, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := ApplyOpenCode(Settings{Endpoint: "x:1", Model: "m"}, false); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var cfg map[string]any
	_ = json.Unmarshal(raw, &cfg)
	prov := cfg["provider"].(map[string]any)
	if _, ok := prov["my_provider"]; !ok {
		t.Error("my_provider removed")
	}
	if _, ok := prov["ollama_remote"]; !ok {
		t.Error("ollama_remote not added")
	}
	// makeDefault=false → existing model untouched.
	if cfg["model"] != "openai/gpt-5" {
		t.Errorf("model overwritten: %v", cfg["model"])
	}
}

func TestDisableOpenCode_RemovesProvider(t *testing.T) {
	tmp := t.TempDir()
	redirectHome(t, tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdg"))
	if _, err := ApplyOpenCode(Settings{Endpoint: "x:1", Model: "m"}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := DisableOpenCode(); err != nil {
		t.Fatal(err)
	}
	path, _ := OpenCodeConfigPath()
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "ollama_remote") {
		t.Errorf("disable left ollama_remote:\n%s", raw)
	}
}

// Ensure the io.Copy helper compiles + works (covers a tiny export).
func TestCopyTo(t *testing.T) {
	var buf strings.Builder
	n, err := CopyTo(&buf, strings.NewReader("hello"))
	if err != nil || n != 5 || buf.String() != "hello" {
		t.Errorf("CopyTo broken: n=%d err=%v out=%q", n, err, buf.String())
	}
}

var _ = io.Discard // keep import even if dropped in edits
