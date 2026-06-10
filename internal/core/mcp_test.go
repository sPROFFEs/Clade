package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListMCPCatalogue_HasExpectedProviders(t *testing.T) {
	got := ListMCPCatalogue()
	if len(got) < 25 {
		t.Fatalf("catalogue has %d providers, want at least 25", len(got))
	}
	want := map[string]bool{
		"github": false, "linear": false, "notion": false,
		"vercel": false, "supabase": false, "google-drive": false,
	}
	for _, entry := range got {
		if entry.Key == "" || entry.Name == "" {
			t.Fatalf("entry missing identity: %+v", entry)
		}
		if !knownMCPTransport(entry.Transport) {
			t.Fatalf("%s has bad transport %q", entry.Key, entry.Transport)
		}
		if _, ok := want[entry.Key]; ok {
			want[entry.Key] = true
		}
	}
	for key, seen := range want {
		if !seen {
			t.Fatalf("catalogue missing %q", key)
		}
	}
}

func TestConnectMCP_CatalogueAPIKeyPersistsEnv(t *testing.T) {
	s := openTempStore(t)
	defer s.Close()
	c, _ := New(Options{Store: s})

	got, err := c.ConnectMCP(context.Background(), ConnectMCPRequest{
		CatalogueKey: "github",
		APIKey:       "ghp_test",
	})
	if err != nil {
		t.Fatalf("ConnectMCP: %v", err)
	}
	if got.ID != "github" || got.Name != "GitHub" || got.Command != "npx" {
		t.Fatalf("catalogue defaults not applied: %+v", got)
	}
	if got.Env["GITHUB_PERSONAL_ACCESS_TOKEN"] != "ghp_test" {
		t.Fatalf("API key not mapped to env: %+v", got.Env)
	}
	if got.Auth["type"] != string(MCPAuthAPIKey) {
		t.Fatalf("auth type not persisted: %+v", got.Auth)
	}
}

func TestConnectMCP_RequiresAPIKeyForAPIKeyProvider(t *testing.T) {
	s := openTempStore(t)
	defer s.Close()
	c, _ := New(Options{Store: s})

	_, err := c.ConnectMCP(context.Background(), ConnectMCPRequest{CatalogueKey: "linear"})
	if err == nil || !strings.Contains(err.Error(), "APIKey required") {
		t.Fatalf("expected APIKey required error, got %v", err)
	}
}

func TestConnectMCP_OAuthCataloguePersistsOAuthMetadata(t *testing.T) {
	s := openTempStore(t)
	defer s.Close()
	c, _ := New(Options{Store: s})

	got, err := c.ConnectMCP(context.Background(), ConnectMCPRequest{CatalogueKey: "google-drive"})
	if err != nil {
		t.Fatalf("ConnectMCP: %v", err)
	}
	if got.Auth["type"] != string(MCPAuthOAuth) {
		t.Fatalf("expected oauth auth, got %+v", got.Auth)
	}
	if got.Auth["issuer"] != "https://accounts.google.com" {
		t.Fatalf("oauth issuer not persisted: %+v", got.Auth)
	}
	if !strings.Contains(got.Auth["scopes"], "drive.readonly") {
		t.Fatalf("oauth scopes not persisted: %+v", got.Auth)
	}
}

func TestMCPServerLifecycle_CustomHTTP(t *testing.T) {
	s := openTempStore(t)
	defer s.Close()
	c, _ := New(Options{Store: s})
	ctx := context.Background()

	disabled := false
	got, err := c.ConnectMCP(ctx, ConnectMCPRequest{
		ID:        "custom-http",
		Name:      "Custom HTTP",
		Transport: MCPTransportHTTP,
		URL:       "https://mcp.example.test",
		Auth:      map[string]string{"type": "bearer", "token": "secret"},
		Enabled:   &disabled,
	})
	if err != nil {
		t.Fatalf("ConnectMCP custom: %v", err)
	}
	if got.Enabled {
		t.Fatal("expected custom server to start disabled")
	}

	enabled, err := c.ListMCPServers(ctx, true)
	if err != nil {
		t.Fatalf("ListMCPServers enabled: %v", err)
	}
	if len(enabled) != 0 {
		t.Fatalf("disabled server should not appear in enabled list: %+v", enabled)
	}

	if err := c.SetMCPEnabled(ctx, "custom-http", true); err != nil {
		t.Fatalf("SetMCPEnabled: %v", err)
	}
	enabled, _ = c.ListMCPServers(ctx, true)
	if len(enabled) != 1 || enabled[0].ID != "custom-http" {
		t.Fatalf("enabled list mismatch: %+v", enabled)
	}

	if err := c.DeleteMCPServer(ctx, "custom-http"); err != nil {
		t.Fatalf("DeleteMCPServer: %v", err)
	}
	_, err = c.GetMCPServer(ctx, "custom-http")
	if !errors.Is(err, ErrMCPServerNotFound) {
		t.Fatalf("expected ErrMCPServerNotFound, got %v", err)
	}
}

func TestConnectMCP_RejectsInvalidCustomServer(t *testing.T) {
	s := openTempStore(t)
	defer s.Close()
	c, _ := New(Options{Store: s})

	_, err := c.ConnectMCP(context.Background(), ConnectMCPRequest{
		ID:        "bad-http",
		Name:      "Bad HTTP",
		Transport: MCPTransportHTTP,
	})
	if err == nil || !strings.Contains(err.Error(), "URL required") {
		t.Fatalf("expected URL required error, got %v", err)
	}

	_, err = c.ConnectMCP(context.Background(), ConnectMCPRequest{
		ID:        "bad-stdio",
		Name:      "Bad Stdio",
		Transport: MCPTransportStdio,
	})
	if err == nil || !strings.Contains(err.Error(), "Command required") {
		t.Fatalf("expected Command required error, got %v", err)
	}
}

func TestRunWorkflow_PreparesClaudeMCPConfigAndEnv(t *testing.T) {
	mock := &mockAdapter{name: "claude", replies: []string{"ok"}}
	withMockAdapter(t, mock)

	s := openTempStore(t)
	defer s.Close()
	c, _ := New(Options{Store: s})
	_, err := c.ConnectMCP(context.Background(), ConnectMCPRequest{
		CatalogueKey: "github",
		APIKey:       "ghp_test",
	})
	if err != nil {
		t.Fatalf("ConnectMCP: %v", err)
	}

	cwd := t.TempDir()
	a := &Agent{
		ID: "a", Name: "A", Instructions: "x", Supports: []string{"claude"},
		MCPServers: []string{"github"},
		Workflows: []Workflow{{
			Name:  "go",
			Steps: []WorkflowStep{{Kind: StepUserMessage, Template: "hello"}},
		}},
	}
	res := c.RunWorkflow(context.Background(), RunOptions{
		Agent: a, WorkflowName: "go", CLI: "claude", Cwd: cwd,
	})
	if res.Outcome != OutcomeCompleted {
		t.Fatalf("outcome=%s err=%v", res.Outcome, res.Err)
	}
	if got := mock.shots[0].Env["GITHUB_PERSONAL_ACCESS_TOKEN"]; got != "ghp_test" {
		t.Fatalf("secret not injected into launch env: %q", got)
	}

	raw, err := os.ReadFile(filepath.Join(cwd, ".mcp.json"))
	if err != nil {
		t.Fatalf("read .mcp.json: %v", err)
	}
	var body struct {
		MCPServers map[string]struct {
			Type    string            `json:"type"`
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode .mcp.json: %v\n%s", err, raw)
	}
	gh := body.MCPServers["github"]
	if gh.Type != "stdio" || gh.Command != "npx" {
		t.Fatalf("bad github config: %+v", gh)
	}
	if gh.Env["GITHUB_PERSONAL_ACCESS_TOKEN"] != "${GITHUB_PERSONAL_ACCESS_TOKEN}" {
		t.Fatalf("config should reference env, not inline secret: %+v", gh.Env)
	}
	if strings.Contains(string(raw), "ghp_test") {
		t.Fatalf(".mcp.json leaked secret:\n%s", raw)
	}
}

func TestPrepareMCPForRun_WritesCodexAndOpenCodeConfigs(t *testing.T) {
	s := openTempStore(t)
	defer s.Close()
	c, _ := New(Options{Store: s})
	ctx := context.Background()

	_, err := c.ConnectMCP(ctx, ConnectMCPRequest{
		ID:        "remote",
		Name:      "Remote",
		Transport: MCPTransportHTTP,
		URL:       "https://mcp.example.test/mcp",
		Auth:      map[string]string{"header": "Authorization", "token": "secret"},
	})
	if err != nil {
		t.Fatalf("ConnectMCP: %v", err)
	}
	a := &Agent{ID: "a", Name: "A", Instructions: "x", Supports: []string{"codex", "opencode"}, MCPServers: []string{"remote"}}

	codexDir := t.TempDir()
	codexEnv, err := c.prepareMCPForRun(ctx, a, "codex", codexDir)
	if err != nil {
		t.Fatalf("prepare codex: %v", err)
	}
	if codexEnv["PRAIMATE_MCP_REMOTE_AUTHORIZATION"] != "Bearer secret" {
		t.Fatalf("codex bearer env mismatch: %+v", codexEnv)
	}
	codexRaw, err := os.ReadFile(filepath.Join(codexDir, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("read codex config: %v", err)
	}
	codexText := string(codexRaw)
	if !strings.Contains(codexText, "[mcp_servers.remote]") ||
		!strings.Contains(codexText, `url = "https://mcp.example.test/mcp"`) ||
		!strings.Contains(codexText, `env_http_headers = { Authorization = "PRAIMATE_MCP_REMOTE_AUTHORIZATION" }`) {
		t.Fatalf("unexpected codex config:\n%s", codexText)
	}
	if strings.Contains(codexText, "Bearer secret") || strings.Contains(codexText, `"secret"`) {
		t.Fatalf("codex config leaked secret:\n%s", codexText)
	}

	openCodeDir := t.TempDir()
	openEnv, err := c.prepareMCPForRun(ctx, a, "opencode", openCodeDir)
	if err != nil {
		t.Fatalf("prepare opencode: %v", err)
	}
	if openEnv["PRAIMATE_MCP_REMOTE_AUTHORIZATION"] != "secret" {
		t.Fatalf("opencode env mismatch: %+v", openEnv)
	}
	openRaw, err := os.ReadFile(filepath.Join(openCodeDir, "opencode.json"))
	if err != nil {
		t.Fatalf("read opencode config: %v", err)
	}
	openText := string(openRaw)
	if !strings.Contains(openText, `"type": "remote"`) ||
		!strings.Contains(openText, `"Authorization": "Bearer {env:PRAIMATE_MCP_REMOTE_AUTHORIZATION}"`) {
		t.Fatalf("unexpected opencode config:\n%s", openText)
	}
	if strings.Contains(openText, "Bearer secret") || strings.Contains(openText, `"secret"`) {
		t.Fatalf("opencode config leaked secret:\n%s", openText)
	}
}

func TestPrepareMCPForRun_MissingDeclaredServerErrors(t *testing.T) {
	s := openTempStore(t)
	defer s.Close()
	c, _ := New(Options{Store: s})

	a := &Agent{ID: "a", Name: "A", Instructions: "x", Supports: []string{"claude"}, MCPServers: []string{"missing"}}
	_, err := c.prepareMCPForRun(context.Background(), a, "claude", t.TempDir())
	if !errors.Is(err, ErrMCPServerNotFound) {
		t.Fatalf("expected ErrMCPServerNotFound, got %v", err)
	}
}
