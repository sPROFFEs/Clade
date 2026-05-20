// Package ollama configures the supported agent CLIs to talk to an
// OpenAI-compatible Ollama endpoint. Ports the ollama-local-ai-toggle.*
// scripts to Go, with one notable difference: Claude is configured per
// launch (env injected by the launcher when it spawns the agent) instead
// of by mutating the user's shell rc — keeps the launcher's effects
// contained.
package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Settings is what the TUI hands to Apply / ClaudeEnv. Stored as a JSON
// blob on the workspace (workspace.json's ollama field) so the choice
// follows the workspace, not the user's shell.
//
// APIKey is empty for vanilla Ollama (no auth) and non-empty for any
// OpenAI-compatible provider that requires Bearer auth (GPUStack,
// vLLM-with-key, LiteLLM gateways, etc.). When set, it's sent as
// `Authorization: Bearer <key>` on probe requests, injected into the
// agent's env as OPENAI_API_KEY at launch, and written into the
// per-agent config files where the provider expects it inline.
type Settings struct {
	Endpoint string `json:"endpoint"`          // e.g. http://192.168.1.50:11434
	Model    string `json:"model"`             // e.g. qwen3-coder
	WireAPI  string `json:"wireApi,omitempty"` // codex: "chat" or "responses"
	APIKey   string `json:"apiKey,omitempty"`  // Bearer token; empty for Ollama
}

// NormalizeEndpoint trims, ensures the URL starts with http:// (we do
// not assume TLS — local Ollama deployments are almost always plaintext),
// and repairs common typos like "http:host:port" with no slashes —
// which would otherwise lead to "http://http:host:port" downstream when
// the value is used by Claude Code's URL parser.
func NormalizeEndpoint(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimRight(s, "/")
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)

	// Already well-formed.
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return s
	}
	// Typo case: "http:host:port" or "https:host" — scheme present but
	// missing the //. Strip the stray prefix; we'll re-add it cleanly.
	for _, scheme := range []string{"https:", "http:"} {
		if strings.HasPrefix(lower, scheme) {
			s = s[len(scheme):]
			break
		}
	}
	// Trim any stray slashes left over.
	s = strings.TrimLeft(s, "/")
	return "http://" + s
}

// ListModels asks the endpoint what's loaded. Tries /api/tags first
// (Ollama-native), then /v1/models (OpenAI-compat). apiKey, if non-empty,
// is sent as `Authorization: Bearer <key>` — required by providers like
// GPUStack that gate /v1/models on auth. Vanilla Ollama ignores the
// header, so it's safe to always include when set.
func ListModels(ctx context.Context, endpoint, apiKey string) ([]string, error) {
	endpoint = NormalizeEndpoint(endpoint)
	if endpoint == "" {
		return nil, errors.New("empty endpoint")
	}
	cli := &http.Client{Timeout: 8 * time.Second}

	if models, err := tryOllamaTags(ctx, cli, endpoint, apiKey); err == nil && len(models) > 0 {
		return models, nil
	}
	return tryOpenAIModels(ctx, cli, endpoint, apiKey)
}

// addBearer attaches Authorization: Bearer <key> when key is non-empty.
// Centralised so every probe path stays consistent.
func addBearer(req *http.Request, apiKey string) {
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}

func tryOllamaTags(ctx context.Context, cli *http.Client, endpoint, apiKey string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	addBearer(req, apiKey)
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var body struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(body.Models))
	for _, m := range body.Models {
		if m.Name != "" {
			out = append(out, m.Name)
		}
	}
	sort.Strings(out)
	return out, nil
}

func tryOpenAIModels(ctx context.Context, cli *http.Client, endpoint, apiKey string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	addBearer(req, apiKey)
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(body.Data))
	for _, m := range body.Data {
		if m.ID != "" {
			out = append(out, m.ID)
		}
	}
	sort.Strings(out)
	return out, nil
}

// ClaudeEnv returns the env vars the launcher should set when spawning
// Claude Code so it routes to the local endpoint instead of Anthropic.
// Empty Settings returns an empty map (no env applied).
//
// When s.APIKey is set, it's used as the Bearer token for both Anthropic-
// and OpenAI-shaped requests; that covers GPUStack and any other gated
// OpenAI-compatible backend. When empty, we fall back to the literal
// "ollama" — vanilla Ollama accepts (and ignores) any token, so this
// keeps the no-auth path unchanged.
func ClaudeEnv(s Settings) map[string]string {
	if s.Endpoint == "" || s.Model == "" {
		return nil
	}
	ep := NormalizeEndpoint(s.Endpoint)
	token := s.APIKey
	if token == "" {
		token = "ollama"
	}
	return map[string]string{
		"ANTHROPIC_AUTH_TOKEN": token,
		"ANTHROPIC_API_KEY":    "",
		"ANTHROPIC_BASE_URL":   ep,
		"OPENAI_API_KEY":       token,
	}
}

// CodexConfigPath is ~/.codex/config.toml.
func CodexConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "config.toml"), nil
}

// OpenCodeConfigPath is $XDG_CONFIG_HOME/opencode/opencode.json (or
// ~/.config/opencode/opencode.json on systems without XDG set).
func OpenCodeConfigPath() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "opencode", "opencode.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "opencode", "opencode.json"), nil
}

const (
	codexProviderName = "ollama_remote"
	codexProfileName  = "ollama_remote"
)

// ProbeCodexCompat issues a stub POST to <endpoint>/v1/responses to
// verify that the configured backend speaks codex 0.130+'s wire_api
// ("responses"). The wizard calls this BEFORE ApplyCodex; the returned
// (warning, error) pair has these semantics:
//
//   - error != nil → refuse the codex apply; show the message
//   - error == nil && warning != "" → apply but surface the warning
//   - both empty   → apply cleanly
//
// We split the result this way because "does the endpoint IMPLEMENT
// /v1/responses?" and "is the endpoint HEALTHY right now?" are
// independent questions, and confusing them produces false refusals
// (a transient upstream 503 from GPUStack would block a config that
// IS valid — the worker just isn't reachable). The probe distinguishes:
//
//   2xx                          → pass clean
//   401 / 403                    → pass clean (route exists; auth's the
//                                  only complaint, real launch handles
//                                  via env_key)
//   502 / 503 / 504              → pass + warn ("route exists but
//                                  upstream returned X — backend may
//                                  need to come up before codex works")
//   404 / 405 / 501              → REFUSE: route doesn't exist
//   400 + chat-completions hint  → REFUSE: server is chat-completions-only
//   400 (other)                  → REFUSE: surface body
//   500 / other 5xx              → REFUSE: surface body
//   Network error                → REFUSE: endpoint unreachable
//
// Probe timeout is short (12s) — slow servers degrade UX but the
// probe must not block the wizard indefinitely.
func ProbeCodexCompat(ctx context.Context, endpoint, apiKey, model string) (warning string, err error) {
	endpoint = NormalizeEndpoint(endpoint)
	if endpoint == "" {
		return "", errors.New("empty endpoint")
	}
	if model == "" {
		// Codex won't omit `model` in a real request, but if we don't
		// have one yet (wizard's probe-before-model-pick flow), pass
		// a placeholder. Any /v1/responses server should still reject
		// with a model-not-found error AFTER routing, telling us the
		// route exists.
		model = "probe-model"
	}
	bodyJSON := `{"model":"` + jsonEscape(model) + `","input":[{"role":"user","content":[{"type":"input_text","text":"ping"}]}],"max_output_tokens":1,"stream":false}`
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint+"/v1/responses", strings.NewReader(bodyJSON))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	addBearer(req, apiKey)
	cli := &http.Client{Timeout: 12 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return "", fmt.Errorf("couldn't reach %s/v1/responses: %w", endpoint, err)
	}
	defer resp.Body.Close()

	// Read a slice of body up front so every branch can include it
	// in its message without duplicating the read logic.
	buf := make([]byte, 2048)
	n, _ := resp.Body.Read(buf)
	bodyText := strings.TrimSpace(string(buf[:n]))

	switch {
	case resp.StatusCode/100 == 2:
		return "", nil
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		// Route exists; auth's the only complaint. Real codex launch
		// will set the proper OPENAI_API_KEY via env_key and should
		// succeed.
		return "", nil
	case resp.StatusCode == 502 || resp.StatusCode == 503 || resp.StatusCode == 504:
		// Route exists, upstream isn't reachable / overloaded. Common
		// case: GPUStack's API gateway returns 503 when its internal
		// worker is down. The config IS valid — the user just has a
		// backend health issue. Pass with a warning so the user sees
		// it and can act, but don't block the apply.
		hint := ""
		if bodyText != "" {
			hint = ": " + truncForMsg(bodyText, 200)
		}
		return fmt.Sprintf(
			"endpoint speaks /v1/responses but currently returns HTTP %d (upstream unreachable%s). "+
				"Config saved; check your backend worker before launching codex.",
			resp.StatusCode, hint), nil
	case resp.StatusCode == 404 || resp.StatusCode == 405 || resp.StatusCode == 501:
		return "", fmt.Errorf(
			"endpoint %s doesn't implement /v1/responses (HTTP %d). "+
				"codex 0.130+ requires this endpoint. "+
				"Use claude / opencode / deepseek (all work via /v1/chat/completions), "+
				"or proxy this endpoint through LiteLLM",
			endpoint, resp.StatusCode)
	default:
		body := bodyText
		if body == "" {
			body = "(empty body)"
		}
		// Some chat-completions-only servers return 400 with a hint
		// pointing at the wrong endpoint. Detect a couple of common
		// shapes so we can give a better message.
		lower := strings.ToLower(body)
		if strings.Contains(lower, "chat/completions") ||
			strings.Contains(lower, "unknown endpoint") ||
			strings.Contains(lower, "method not allowed") {
			return "", fmt.Errorf(
				"endpoint %s appears to only implement /v1/chat/completions (HTTP %d: %s). "+
					"codex 0.130+ requires /v1/responses. "+
					"Use claude / opencode / deepseek, or proxy through LiteLLM",
				endpoint, resp.StatusCode, truncForMsg(body, 200))
		}
		return "", fmt.Errorf("endpoint %s returned HTTP %d for /v1/responses: %s",
			endpoint, resp.StatusCode, truncForMsg(body, 400))
	}
}

// truncForMsg cuts a string at max chars and appends "…" if cut, so
// long server error bodies don't blow out the UI line.
func truncForMsg(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// jsonEscape escapes a string for safe inclusion as a JSON value. We
// don't want to pull in encoding/json just for the probe's body, so
// this minimal version covers the cases that can appear in model
// names (backslash, quotes, control chars).
func jsonEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ApplyCodex writes the [model_providers.ollama_remote] + [profiles.ollama_remote]
// blocks to ~/.codex/config.toml. Existing matching blocks are replaced
// (idempotent). Existing unrelated config is preserved.
//
// Use codex with: `codex -p ollama_remote`.
func ApplyCodex(s Settings) (configPath string, err error) {
	configPath, err = CodexConfigPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return configPath, err
	}
	existing, _ := os.ReadFile(configPath)
	stripped := stripCodexBlocks(string(existing))
	wireAPI := s.WireAPI
	if wireAPI == "" {
		// Codex CLI 0.40+ deprecated "chat" and requires "responses".
		// Older Codex builds still want "chat" — pass s.WireAPI="chat"
		// explicitly in those cases. We default to "responses" because
		// new installs hit the deprecation error otherwise.
		wireAPI = "responses"
	}
	block := fmt.Sprintf(`
[model_providers.%s]
name = "Ollama Remote"
base_url = "%s/v1"
env_key = "OPENAI_API_KEY"
wire_api = "%s"

[profiles.%s]
model_provider = "%s"
model = "%s"
`, codexProviderName, NormalizeEndpoint(s.Endpoint), wireAPI, codexProfileName, codexProviderName, s.Model)
	final := strings.TrimRight(stripped, "\n") + "\n" + block
	return configPath, atomicWrite(configPath, []byte(final))
}

// DisableCodex strips the ollama_remote blocks from ~/.codex/config.toml.
// No-op if the file doesn't exist.
func DisableCodex() (string, error) {
	configPath, err := CodexConfigPath()
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(configPath)
	if errors.Is(err, fs.ErrNotExist) {
		return configPath, nil
	}
	if err != nil {
		return configPath, err
	}
	stripped := stripCodexBlocks(string(raw))
	return configPath, atomicWrite(configPath, []byte(stripped))
}

// MigrateCodexWireAPI rewrites wire_api = "chat" → wire_api = "responses"
// inside our managed [model_providers.ollama_remote] block in
// ~/.codex/config.toml. Codex 0.40+ deprecated "chat" and hard-errors at
// startup when it sees it. Idempotent. No-op if the file or our block
// is missing.
//
// Only touches lines inside the ollama_remote block — other providers
// are left alone.
func MigrateCodexWireAPI() (changed bool, err error) {
	path, err := CodexConfigPath()
	if err != nil {
		return false, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	lines := strings.Split(string(raw), "\n")
	inOurBlock := false
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "[model_providers."+codexProviderName+"]" {
			inOurBlock = true
			continue
		}
		if inOurBlock && strings.HasPrefix(trim, "[") {
			inOurBlock = false
		}
		if inOurBlock {
			// Match wire_api = "chat" (with any spacing).
			if strings.Contains(line, `wire_api`) && strings.Contains(line, `"chat"`) {
				lines[i] = strings.Replace(line, `"chat"`, `"responses"`, 1)
				changed = true
			}
		}
	}
	if !changed {
		return false, nil
	}
	return true, atomicWrite(path, []byte(strings.Join(lines, "\n")))
}

// stripCodexBlocks removes the [model_providers.ollama_remote] and
// [profiles.ollama_remote] tables from a TOML body. Stops when it hits
// the next [section] header or EOF.
func stripCodexBlocks(body string) string {
	headers := []string{
		"[model_providers." + codexProviderName + "]",
		"[profiles." + codexProfileName + "]",
	}
	out := body
	for _, h := range headers {
		out = stripTomlTable(out, h)
	}
	return out
}

func stripTomlTable(body, header string) string {
	lines := strings.Split(body, "\n")
	var kept []string
	skip := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if !skip && trim == header {
			skip = true
			continue
		}
		if skip && strings.HasPrefix(trim, "[") {
			skip = false
		}
		if !skip {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// opencodeConfig is what we serialize back to opencode.json. We keep
// unknown fields by round-tripping through map[string]any.
type opencodeConfig map[string]any

const openCodeProviderName = "ollama_remote"

// ApplyOpenCode merges the Ollama provider into opencode.json. If the
// file exists, only the provider entry is overwritten — other config is
// preserved.
func ApplyOpenCode(s Settings, makeDefault bool) (string, error) {
	configPath, err := OpenCodeConfigPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return configPath, err
	}

	cfg := opencodeConfig{}
	if raw, err := os.ReadFile(configPath); err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &cfg)
	}

	cfg["$schema"] = "https://opencode.ai/config.json"
	providers, _ := cfg["provider"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
	}
	options := map[string]any{
		"baseURL": NormalizeEndpoint(s.Endpoint) + "/v1",
	}
	if s.APIKey != "" {
		// @ai-sdk/openai-compatible reads `apiKey` from options and sends
		// it as `Authorization: Bearer <key>`. Required by GPUStack and
		// other gated OpenAI-compatible backends.
		options["apiKey"] = s.APIKey
	}
	providers[openCodeProviderName] = map[string]any{
		"npm":     "@ai-sdk/openai-compatible",
		"name":    "Ollama Remote",
		"options": options,
		"models": map[string]any{
			s.Model: map[string]any{"name": s.Model},
		},
	}
	cfg["provider"] = providers

	if makeDefault {
		cfg["model"] = openCodeProviderName + "/" + s.Model
		cfg["small_model"] = openCodeProviderName + "/" + s.Model
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return configPath, err
	}
	out = append(out, '\n')
	return configPath, atomicWrite(configPath, out)
}

// CodexConfigured reports whether ~/.codex/config.toml has the
// [model_providers.ollama_remote] block we manage. The Ollama screen
// uses this to pre-check the codex checkbox when reopened.
func CodexConfigured() bool {
	path, err := CodexConfigPath()
	if err != nil {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), "[model_providers."+codexProviderName+"]")
}

// OpenCodeConfigured reports whether opencode.json has our ollama_remote
// provider entry. Same purpose as CodexConfigured.
func OpenCodeConfigured() bool {
	path, err := OpenCodeConfigPath()
	if err != nil {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	// Cheap check — full JSON parse not needed, the key string is
	// distinctive enough.
	return strings.Contains(string(raw), `"`+openCodeProviderName+`"`)
}

// DisableOpenCode removes the ollama_remote provider entry. No-op if the
// file doesn't exist.
func DisableOpenCode() (string, error) {
	configPath, err := OpenCodeConfigPath()
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(configPath)
	if errors.Is(err, fs.ErrNotExist) {
		return configPath, nil
	}
	if err != nil {
		return configPath, err
	}
	cfg := opencodeConfig{}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return configPath, err
	}
	if providers, ok := cfg["provider"].(map[string]any); ok {
		delete(providers, openCodeProviderName)
		cfg["provider"] = providers
	}
	for _, k := range []string{"model", "small_model"} {
		if v, ok := cfg[k].(string); ok && strings.HasPrefix(v, openCodeProviderName+"/") {
			delete(cfg, k)
		}
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return configPath, err
	}
	out = append(out, '\n')
	return configPath, atomicWrite(configPath, out)
}

// DeepSeekConfigPath is ~/.deepseek/config.toml (the user-level file
// DeepSeek-TUI reads on startup).
func DeepSeekConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".deepseek", "config.toml"), nil
}

const (
	// Marker comments wrap our managed block in ~/.deepseek/config.toml
	// so re-applies can find and replace the block without touching
	// any hand-edited config the user wrote around it. The wrapper
	// approach matches what the bot scripts use for ~/.bashrc edits
	// and is more forgiving than the codex stripTomlTable approach
	// when the user has multiple [providers.*] blocks.
	deepseekBlockStart = "# >>> clade ollama config"
	deepseekBlockEnd   = "# <<< clade ollama config"
)

// ApplyDeepSeek writes the [providers.ollama] block + top-level
// provider/model defaults to ~/.deepseek/config.toml. The block is
// wrapped in marker comments so re-applies are clean and the user
// can keep their own config above and below the managed section.
//
// DeepSeek-TUI accepts the OpenAI-compat API on /v1, same as
// Codex/OpenCode, so the endpoint format matches.
func ApplyDeepSeek(s Settings) (string, error) {
	configPath, err := DeepSeekConfigPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return configPath, err
	}
	existing, _ := os.ReadFile(configPath)
	stripped := stripDeepSeekBlock(string(existing))

	// When the endpoint requires Bearer auth (GPUStack et al.), write the
	// key inline. DeepSeek-TUI also honours `api_key_env`, but the
	// inline form is one less moving piece for the user.
	keyLine := ""
	if s.APIKey != "" {
		keyLine = fmt.Sprintf("api_key = %q\n", s.APIKey)
	}
	block := fmt.Sprintf(`
%s
provider = "ollama"
model = "%s"

[providers.ollama]
base_url = "%s/v1"
%s%s
`,
		deepseekBlockStart,
		s.Model,
		NormalizeEndpoint(s.Endpoint),
		keyLine,
		deepseekBlockEnd,
	)
	final := strings.TrimRight(stripped, "\n") + "\n" + block
	return configPath, atomicWrite(configPath, []byte(final))
}

// DisableDeepSeek strips the managed block from ~/.deepseek/config.toml.
// No-op when the file doesn't exist.
func DisableDeepSeek() (string, error) {
	configPath, err := DeepSeekConfigPath()
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(configPath)
	if errors.Is(err, fs.ErrNotExist) {
		return configPath, nil
	}
	if err != nil {
		return configPath, err
	}
	return configPath, atomicWrite(configPath, []byte(stripDeepSeekBlock(string(raw))))
}

// DeepSeekConfigured reports whether ~/.deepseek/config.toml has our
// managed block. Surfaces the pre-checked state on the Ollama screen
// when reopened.
func DeepSeekConfigured() bool {
	path, err := DeepSeekConfigPath()
	if err != nil {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), deepseekBlockStart)
}

// stripDeepSeekBlock removes everything between our marker comments
// (inclusive). Lines outside the markers are preserved verbatim — no
// TOML parsing needed because we control the markers.
func stripDeepSeekBlock(body string) string {
	lines := strings.Split(body, "\n")
	var kept []string
	skip := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == deepseekBlockStart {
			skip = true
			continue
		}
		if skip && trim == deepseekBlockEnd {
			skip = false
			continue
		}
		if !skip {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// CopyTo writes the given data to dst, creating parent dirs. Exposed for
// tests that want a sink other than os.WriteFile (e.g., capturing into a
// byte buffer).
func CopyTo(dst io.Writer, src io.Reader) (int64, error) { return io.Copy(dst, src) }
