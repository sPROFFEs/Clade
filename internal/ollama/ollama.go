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
type Settings struct {
	Endpoint string `json:"endpoint"`        // e.g. http://192.168.1.50:11434
	Model    string `json:"model"`           // e.g. qwen3-coder
	WireAPI  string `json:"wireApi,omitempty"` // codex: "chat" or "responses"
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

// ListModels asks the Ollama server what's loaded. Tries /api/tags first
// (Ollama-native), then /v1/models (OpenAI-compat). Caller decides what
// to do with the empty-list / error case.
func ListModels(ctx context.Context, endpoint string) ([]string, error) {
	endpoint = NormalizeEndpoint(endpoint)
	if endpoint == "" {
		return nil, errors.New("empty endpoint")
	}
	cli := &http.Client{Timeout: 8 * time.Second}

	if models, err := tryOllamaTags(ctx, cli, endpoint); err == nil && len(models) > 0 {
		return models, nil
	}
	return tryOpenAIModels(ctx, cli, endpoint)
}

func tryOllamaTags(ctx context.Context, cli *http.Client, endpoint string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
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

func tryOpenAIModels(ctx context.Context, cli *http.Client, endpoint string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
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
// Claude Code so it routes to Ollama instead of Anthropic. Empty Settings
// returns an empty map (no env applied).
func ClaudeEnv(s Settings) map[string]string {
	if s.Endpoint == "" || s.Model == "" {
		return nil
	}
	ep := NormalizeEndpoint(s.Endpoint)
	return map[string]string{
		"ANTHROPIC_AUTH_TOKEN": "ollama",
		"ANTHROPIC_API_KEY":    "",
		"ANTHROPIC_BASE_URL":   ep,
		"OPENAI_API_KEY":       "ollama",
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
	providers[openCodeProviderName] = map[string]any{
		"npm":  "@ai-sdk/openai-compatible",
		"name": "Ollama Remote",
		"options": map[string]any{
			"baseURL": NormalizeEndpoint(s.Endpoint) + "/v1",
		},
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
