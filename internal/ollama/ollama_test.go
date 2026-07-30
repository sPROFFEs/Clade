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
		"http:192.168.1.42:11434":  "http://192.168.1.42:11434",
		"https:example.com":        "http://example.com",
		"HTTP:192.168.1.10:11434":  "http://192.168.1.10:11434",
		"http:/lonely.slash:11434": "http://lonely.slash:11434",
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

	models, err := ListModels(context.Background(), srv.URL, "")
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

	models, _ := ListModels(context.Background(), srv.URL, "")
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

func TestClaudeEnv_UsesAPIKeyWhenSet(t *testing.T) {
	env := ClaudeEnv(Settings{Endpoint: "h:1", Model: "m", APIKey: "sk-gpustack-xyz"})
	if env["ANTHROPIC_AUTH_TOKEN"] != "sk-gpustack-xyz" {
		t.Errorf("ANTHROPIC_AUTH_TOKEN = %q, want sk-gpustack-xyz", env["ANTHROPIC_AUTH_TOKEN"])
	}
	if env["OPENAI_API_KEY"] != "sk-gpustack-xyz" {
		t.Errorf("OPENAI_API_KEY = %q, want sk-gpustack-xyz", env["OPENAI_API_KEY"])
	}
}

// TestListModels_SendsBearerWhenKeySet verifies the probe forwards
// Authorization: Bearer <key> when the user supplied one. GPUStack and
// other gated providers reject /v1/models without it.
func TestListModels_SendsBearerWhenKeySet(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		// Refuse /api/tags so we exercise the /v1/models path too.
		if r.URL.Path == "/api/tags" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "qwen3-0.6b"}},
		})
	}))
	defer srv.Close()

	if _, err := ListModels(context.Background(), srv.URL, "sk-test-key"); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer sk-test-key" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer sk-test-key")
	}
}

// TestApplyOpenCode_ReferencesAPIKeyEnvironment: the plaintext config
// contains only an environment reference, never the credential itself.
// TestProbeCodexCompat_PassesWhenServerImplementsResponses: 2xx from
// /v1/responses → probe passes, no error, no warning.
func TestProbeCodexCompat_PassesWhenServerImplementsResponses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/responses" {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"id":"resp_x","output":[{"type":"output_text","text":"ok"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	warn, err := ProbeCodexCompat(context.Background(), srv.URL, "", "any")
	if err != nil || warn != "" {
		t.Errorf("expected (nil, \"\") on 2xx; got (%q, %v)", warn, err)
	}
}

// TestProbeCodexCompat_AuthFailureCountsAsRouteExists: 401/403 means
// the path matched, only auth's missing. Real codex launch will pass
// the right key via env_key, so treat as success for compat detection.
func TestProbeCodexCompat_AuthFailureCountsAsRouteExists(t *testing.T) {
	for _, code := range []int{401, 403} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		}))
		warn, err := ProbeCodexCompat(context.Background(), srv.URL, "", "any")
		if err != nil || warn != "" {
			t.Errorf("%d should count as clean pass; got warn=%q err=%v", code, warn, err)
		}
		srv.Close()
	}
}

// TestProbeCodexCompat_502_503_504_PassWithWarning: GPUStack's API
// gateway routes /v1/responses fine but its internal worker can be
// unreachable → 503 with a body explaining the upstream failure. The
// route IS implemented, so the probe should NOT refuse the apply.
// Pass with a warning so the user sees the backend health issue.
func TestProbeCodexCompat_502_503_504_PassWithWarning(t *testing.T) {
	for _, code := range []int{502, 503, 504} {
		body := `{"error":{"message":"Cannot connect to host 192.168.155.150:40047 ssl:default [Connect call failed]"}}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			_, _ = w.Write([]byte(body))
		}))
		warn, err := ProbeCodexCompat(context.Background(), srv.URL, "", "any")
		if err != nil {
			t.Errorf("%d should pass (route exists, upstream unhealthy); got err=%v", code, err)
		}
		if warn == "" {
			t.Errorf("%d should produce a warning so user sees the upstream issue", code)
		}
		for _, want := range []string{"speaks /v1/responses", "upstream"} {
			if !strings.Contains(warn, want) {
				t.Errorf("%d warning should mention %q; got: %s", code, want, warn)
			}
		}
		srv.Close()
	}
}

// TestProbeCodexCompat_404IsRefused: server only implements
// /v1/chat/completions, /v1/responses returns 404. Error must be
// user-facing and mention LiteLLM as the workaround.
func TestProbeCodexCompat_404IsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	_, err := ProbeCodexCompat(context.Background(), srv.URL, "", "any")
	if err == nil {
		t.Fatal("404 should produce an error")
	}
	for _, want := range []string{"/v1/responses", "0.130", "LiteLLM"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q for the user; got: %v", want, err)
		}
	}
}

// TestProbeCodexCompat_400ChatCompletionsHintIsRecognised: some
// chat-completions-only servers (LocalAI etc.) return 400 with a
// hint about /v1/chat/completions instead of 404. Catch that shape.
func TestProbeCodexCompat_400ChatCompletionsHintIsRecognised(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"unknown endpoint, try /v1/chat/completions"}`))
	}))
	defer srv.Close()
	_, err := ProbeCodexCompat(context.Background(), srv.URL, "", "any")
	if err == nil {
		t.Fatal("400 + chat/completions hint should produce an error")
	}
	if !strings.Contains(err.Error(), "chat/completions") {
		t.Errorf("error should explain it's a chat/completions-only server; got: %v", err)
	}
}

// TestProbeCodexCompat_UnreachableEndpointReportsClearly: network
// error → user-readable message naming the endpoint.
func TestProbeCodexCompat_UnreachableEndpointReportsClearly(t *testing.T) {
	// 127.0.0.1:1 is reliably refused on every OS we ship to.
	_, err := ProbeCodexCompat(context.Background(), "http://127.0.0.1:1", "", "any")
	if err == nil {
		t.Fatal("unreachable endpoint should error")
	}
	if !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Errorf("error should name the endpoint; got: %v", err)
	}
}

func TestApplyOpenCode_ReferencesAPIKeyEnvironment(t *testing.T) {
	tmp := t.TempDir()
	redirectHome(t, tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdg"))

	path, err := ApplyOpenCode(Settings{Endpoint: "10.0.0.1:11434", Model: "qwen3", APIKey: "sk-abc"}, true)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var cfg map[string]any
	_ = json.Unmarshal(raw, &cfg)
	prov := cfg["provider"].(map[string]any)
	entry := prov["ollama_remote"].(map[string]any)
	opts := entry["options"].(map[string]any)
	if opts["apiKey"] != "{env:OPENAI_API_KEY}" {
		t.Errorf("options.apiKey = %v, want environment reference", opts["apiKey"])
	}
	if strings.Contains(string(raw), "sk-abc") {
		t.Fatalf("opencode config leaked API key:\n%s", raw)
	}
}

func TestApplyOpenCode_WritesModelTokenLimits(t *testing.T) {
	tmp := t.TempDir()
	redirectHome(t, tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdg"))

	path, err := ApplyOpenCode(Settings{
		Endpoint:      "10.0.0.1:11434",
		Model:         "qwen3",
		ContextTokens: 4096,
		OutputTokens:  1024,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var cfg map[string]any
	_ = json.Unmarshal(raw, &cfg)
	prov := cfg["provider"].(map[string]any)
	entry := prov["ollama_remote"].(map[string]any)
	models := entry["models"].(map[string]any)
	model := models["qwen3"].(map[string]any)
	limit := model["limit"].(map[string]any)
	if limit["context"] != float64(4096) || limit["output"] != float64(1024) {
		t.Errorf("limit = %#v, want context=4096 output=1024\n%s", limit, raw)
	}
}

// TestApplyOpenCode_OmitsAPIKeyWhenBlank: vanilla Ollama path — no
// apiKey field should appear in the options block.
func TestApplyOpenCode_OmitsAPIKeyWhenBlank(t *testing.T) {
	tmp := t.TempDir()
	redirectHome(t, tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdg"))

	path, _ := ApplyOpenCode(Settings{Endpoint: "h:1", Model: "m"}, false)
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), `"apiKey"`) {
		t.Errorf("opencode.json should omit apiKey when blank:\n%s", raw)
	}
}

// TestApplyDeepSeek_ReferencesKeyEnvironment keeps the credential out of
// DeepSeek's plaintext TOML.
func TestApplyDeepSeek_ReferencesKeyEnvironment(t *testing.T) {
	tmp := t.TempDir()
	redirectHome(t, tmp)
	path, _ := ApplyDeepSeek(Settings{Endpoint: "h:1", Model: "m", APIKey: "sk-deepseek"})
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), `api_key_env = "OPENAI_API_KEY"`) {
		t.Errorf("expected api_key_env in managed block:\n%s", body)
	}
	if strings.Contains(string(body), "sk-deepseek") {
		t.Errorf("DeepSeek config leaked API key:\n%s", body)
	}

	// Re-apply without a key: the api_key line must disappear.
	if _, err := ApplyDeepSeek(Settings{Endpoint: "h:1", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	body2, _ := os.ReadFile(path)
	if strings.Contains(string(body2), "api_key") {
		t.Errorf("re-apply without key should drop the api_key line:\n%s", body2)
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
		`wire_api = "responses"`,
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

func TestApplyCodex_WritesContextTokenLimit(t *testing.T) {
	tmp := t.TempDir()
	redirectHome(t, tmp)

	path, err := ApplyCodex(Settings{
		Endpoint:      "192.168.1.10:11434",
		Model:         "qwen3",
		ContextTokens: 4096,
		OutputTokens:  1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	body := string(raw)
	if !strings.Contains(body, `model_context_window = 4096`) {
		t.Errorf("config missing model_context_window\n%s", body)
	}
	if strings.Contains(body, `model_max_output_tokens`) {
		t.Errorf("Codex no longer exposes model_max_output_tokens; should not write it\n%s", body)
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

func TestMigrateCodexWireAPI_RewritesChatToResponses(t *testing.T) {
	tmp := t.TempDir()
	redirectHome(t, tmp)
	// Pre-existing config.toml with our managed block using the old "chat".
	pre := `model_provider = "ollama_remote"
model = "qwen3"

[model_providers.ollama_remote]
name = "Ollama Remote"
base_url = "http://x/v1"
env_key = "OPENAI_API_KEY"
wire_api = "chat"

[profiles.ollama_remote]
model_provider = "ollama_remote"
model = "qwen3"

[other_provider]
wire_api = "chat"
`
	codexDir := filepath.Join(tmp, ".codex")
	_ = os.MkdirAll(codexDir, 0o755)
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, err := MigrateCodexWireAPI()
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("expected changed=true")
	}
	raw, _ := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	body := string(raw)
	// Our block was rewritten.
	if !strings.Contains(body, `wire_api = "responses"`) {
		t.Errorf("ollama_remote wire_api not rewritten:\n%s", body)
	}
	// The OTHER provider's wire_api was NOT touched (we only own ollama_remote).
	if !strings.Contains(body, "[other_provider]\nwire_api = \"chat\"") {
		t.Errorf("other_provider wire_api was incorrectly touched:\n%s", body)
	}

	// Idempotent.
	changed, err = MigrateCodexWireAPI()
	if err != nil || changed {
		t.Errorf("idempotent re-run: changed=%v err=%v", changed, err)
	}
}

func TestMigrateCodexWireAPI_NoopWhenAbsent(t *testing.T) {
	tmp := t.TempDir()
	redirectHome(t, tmp)
	changed, err := MigrateCodexWireAPI()
	if err != nil || changed {
		t.Errorf("no-file: changed=%v err=%v", changed, err)
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

func TestCodexConfigured_ReflectsDiskState(t *testing.T) {
	tmp := t.TempDir()
	redirectHome(t, tmp)
	if CodexConfigured() {
		t.Fatal("fresh dir: CodexConfigured should be false")
	}
	if _, err := ApplyCodex(Settings{Endpoint: "x:1", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	if !CodexConfigured() {
		t.Error("after ApplyCodex: CodexConfigured should be true")
	}
	if _, err := DisableCodex(); err != nil {
		t.Fatal(err)
	}
	if CodexConfigured() {
		t.Error("after DisableCodex: CodexConfigured should be false")
	}
}

func TestOpenCodeConfigured_ReflectsDiskState(t *testing.T) {
	tmp := t.TempDir()
	redirectHome(t, tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdg"))
	if OpenCodeConfigured() {
		t.Fatal("fresh dir: OpenCodeConfigured should be false")
	}
	if _, err := ApplyOpenCode(Settings{Endpoint: "x:1", Model: "m"}, false); err != nil {
		t.Fatal(err)
	}
	if !OpenCodeConfigured() {
		t.Error("after ApplyOpenCode: OpenCodeConfigured should be true")
	}
	if _, err := DisableOpenCode(); err != nil {
		t.Fatal(err)
	}
	if OpenCodeConfigured() {
		t.Error("after DisableOpenCode: OpenCodeConfigured should be false")
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

func TestApplyDeepSeek_WritesManagedBlock(t *testing.T) {
	tmp := t.TempDir()
	redirectHome(t, tmp)

	path, err := ApplyDeepSeek(Settings{
		Endpoint: "192.168.1.10:11434",
		Model:    "deepseek-coder:1.3b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, filepath.Join(".deepseek", "config.toml")) {
		t.Errorf("config path %q doesn't end in .deepseek/config.toml", path)
	}
	body, _ := os.ReadFile(path)
	for _, want := range []string{
		"# >>> praimate ollama config",
		`provider = "ollama"`,
		`model = "deepseek-coder:1.3b"`,
		"[providers.ollama]",
		`base_url = "http://192.168.1.10:11434/v1"`,
		"# <<< praimate ollama config",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("config missing %q\n%s", want, body)
		}
	}

	// Re-apply should be idempotent — exactly one managed block.
	if _, err := ApplyDeepSeek(Settings{
		Endpoint: "192.168.1.10:11434",
		Model:    "deepseek-coder:1.3b",
	}); err != nil {
		t.Fatal(err)
	}
	body2, _ := os.ReadFile(path)
	if got := strings.Count(string(body2), "# >>> praimate ollama config"); got != 1 {
		t.Errorf("expected exactly one managed block after re-apply, got %d:\n%s", got, body2)
	}
}

func TestApplyDeepSeek_PreservesSurroundingConfig(t *testing.T) {
	tmp := t.TempDir()
	redirectHome(t, tmp)
	dsDir := filepath.Join(tmp, ".deepseek")
	_ = os.MkdirAll(dsDir, 0o755)
	preexisting := `# user notes
theme = "dark"

[providers.openai]
api_key_env = "OPENAI_API_KEY"
`
	cfgPath := filepath.Join(dsDir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(preexisting), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyDeepSeek(Settings{Endpoint: "host:11434", Model: "qwen"}); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(cfgPath)
	for _, want := range []string{
		`theme = "dark"`,
		"[providers.openai]",
		`api_key_env = "OPENAI_API_KEY"`,
		"# >>> praimate ollama config",
		`[providers.ollama]`,
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("config missing %q after apply:\n%s", want, body)
		}
	}
}

func TestDeepSeekConfigured_ReflectsDiskState(t *testing.T) {
	tmp := t.TempDir()
	redirectHome(t, tmp)

	if DeepSeekConfigured() {
		t.Error("DeepSeekConfigured should be false before Apply")
	}
	if _, err := ApplyDeepSeek(Settings{Endpoint: "h:1", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	if !DeepSeekConfigured() {
		t.Error("DeepSeekConfigured should be true after Apply")
	}
	if _, err := DisableDeepSeek(); err != nil {
		t.Fatal(err)
	}
	if DeepSeekConfigured() {
		t.Error("DeepSeekConfigured should be false after Disable")
	}
}

func TestDisableDeepSeek_RemovesOnlyManagedBlock(t *testing.T) {
	tmp := t.TempDir()
	redirectHome(t, tmp)
	dsDir := filepath.Join(tmp, ".deepseek")
	_ = os.MkdirAll(dsDir, 0o755)
	other := "theme = \"dark\"\n[providers.openai]\nkey = \"x\"\n"
	cfgPath := filepath.Join(dsDir, "config.toml")
	_ = os.WriteFile(cfgPath, []byte(other), 0o644)

	if _, err := ApplyDeepSeek(Settings{Endpoint: "h:1", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	if _, err := DisableDeepSeek(); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(cfgPath)
	if strings.Contains(string(body), "# >>> clade ollama config") {
		t.Error("Disable should remove the managed block")
	}
	for _, keep := range []string{`theme = "dark"`, "[providers.openai]"} {
		if !strings.Contains(string(body), keep) {
			t.Errorf("Disable removed unrelated config %q:\n%s", keep, body)
		}
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

// A config carrying the PRE-REBRAND managed block must be replaced (not
// duplicated) when re-applying — legacy markers are still stripped.
func TestApplyDeepSeek_ReplacesLegacyCladeBlock(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	path, err := DeepSeekConfigPath()
	if err != nil {
		t.Skipf("no deepseek config path on this platform: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := "keep_me = true\n# >>> clade ollama config\nold = \"block\"\n# <<< clade ollama config\n"
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyDeepSeek(Settings{Endpoint: "127.0.0.1:11434", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if strings.Contains(string(body), ">>> clade ollama config") {
		t.Errorf("legacy block survived re-apply:\n%s", body)
	}
	if !strings.Contains(string(body), ">>> praimate ollama config") {
		t.Errorf("new managed block missing:\n%s", body)
	}
	if !strings.Contains(string(body), "keep_me = true") {
		t.Errorf("surrounding config lost:\n%s", body)
	}
}
