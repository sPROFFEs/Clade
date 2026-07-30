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

func TestListMCPCatalogue_OnlyOffersLocalProcesses(t *testing.T) {
	got := ListMCPCatalogue()
	if len(got) != 5 {
		t.Fatalf("catalogue has %d providers, want 5 local utilities", len(got))
	}
	want := map[string]bool{
		"browser": false, "fetch": false, "filesystem": false,
		"sequential-thinking": false, "sqlite": false,
	}
	for _, entry := range got {
		if entry.Key == "" || entry.Name == "" {
			t.Fatalf("entry missing identity: %+v", entry)
		}
		if !knownMCPTransport(entry.Transport) {
			t.Fatalf("%s has bad transport %q", entry.Key, entry.Transport)
		}
		if entry.Transport != MCPTransportStdio || entry.Command == "" {
			t.Fatalf("%s is not a local process: %+v", entry.Key, entry)
		}
		if _, ok := want[entry.Key]; ok {
			want[entry.Key] = true
		} else {
			t.Fatalf("third-party service remains in catalogue: %q", entry.Key)
		}
	}
	for key, seen := range want {
		if !seen {
			t.Fatalf("catalogue missing %q", key)
		}
	}
}

func TestConnectMCP_RejectsRemovedServiceCatalogueEntry(t *testing.T) {
	s := openTempStore(t)
	defer s.Close()
	c, _ := New(Options{Store: s})

	_, err := c.ConnectMCP(context.Background(), ConnectMCPRequest{CatalogueKey: "github"})
	if err == nil || !strings.Contains(err.Error(), "unknown catalogue key") {
		t.Fatalf("expected removed catalogue key error, got %v", err)
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
		ID:        "github",
		Name:      "GitHub",
		Transport: MCPTransportStdio,
		Command:   "npx",
		Args:      []string{"-y", "@modelcontextprotocol/server-github"},
		Env:       map[string]string{"GITHUB_PERSONAL_ACCESS_TOKEN": "ghp_test"},
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

	praimateCodeDir := t.TempDir()
	praimateCodeEnv, err := c.prepareMCPForRun(ctx, a, "praimate-code", praimateCodeDir)
	if err != nil {
		t.Fatalf("prepare praimate-code: %v", err)
	}
	if praimateCodeEnv["PRAIMATE_MCP_REMOTE_AUTHORIZATION"] != "secret" {
		t.Fatalf("praimate-code env mismatch: %+v", praimateCodeEnv)
	}
	praimateCodeRaw, err := os.ReadFile(filepath.Join(praimateCodeDir, "opencode.json"))
	if err != nil {
		t.Fatalf("read praimate-code opencode config: %v", err)
	}
	praimateCodeText := string(praimateCodeRaw)
	if !strings.Contains(praimateCodeText, `"type": "remote"`) ||
		!strings.Contains(praimateCodeText, `"Authorization": "Bearer {env:PRAIMATE_MCP_REMOTE_AUTHORIZATION}"`) {
		t.Fatalf("unexpected praimate-code config:\n%s", praimateCodeText)
	}
	if strings.Contains(praimateCodeText, "Bearer secret") || strings.Contains(praimateCodeText, `"secret"`) {
		t.Fatalf("praimate-code config leaked secret:\n%s", praimateCodeText)
	}
}

func TestPrepareMCPForRun_OAuthClientSecretStaysInLaunchEnv(t *testing.T) {
	s := openTempStore(t)
	defer s.Close()
	c, _ := New(Options{Store: s})
	ctx := context.Background()
	_, err := c.ConnectMCP(ctx, ConnectMCPRequest{
		ID:        "oauth-remote",
		Name:      "OAuth Remote",
		Transport: MCPTransportHTTP,
		URL:       "https://mcp.example.test/mcp",
		Auth: map[string]string{
			"type": "oauth", "client_id": "public-client",
			"client_secret": "private-client-secret",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{ID: "a", Name: "A", Instructions: "x", MCPServers: []string{"oauth-remote"}}

	for _, cli := range []string{"claude", "opencode"} {
		dir := t.TempDir()
		env, err := c.prepareMCPForRun(ctx, a, cli, dir)
		if err != nil {
			t.Fatalf("%s prepare: %v", cli, err)
		}
		const envName = "PRAIMATE_MCP_OAUTH_REMOTE_OAUTH_CLIENT_SECRET"
		if env[envName] != "private-client-secret" {
			t.Fatalf("%s OAuth secret env = %q", cli, env[envName])
		}
		path := filepath.Join(dir, ".mcp.json")
		if cli == "opencode" {
			path = filepath.Join(dir, "opencode.json")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "private-client-secret") {
			t.Fatalf("%s config leaked OAuth client secret:\n%s", cli, raw)
		}
		if !strings.Contains(string(raw), envName) {
			t.Fatalf("%s config does not reference OAuth secret env:\n%s", cli, raw)
		}
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

func TestPrepareEnabledMCPForRun_WritesPraimateCodeConfig(t *testing.T) {
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
		t.Fatalf("ConnectMCP enabled: %v", err)
	}
	disabled := false
	_, err = c.ConnectMCP(ctx, ConnectMCPRequest{
		ID:        "disabled",
		Name:      "Disabled",
		Transport: MCPTransportHTTP,
		URL:       "https://disabled.example.test/mcp",
		Enabled:   &disabled,
	})
	if err != nil {
		t.Fatalf("ConnectMCP disabled: %v", err)
	}

	dir := t.TempDir()
	env, err := c.PrepareEnabledMCPForRun(ctx, "praimate-code", dir)
	if err != nil {
		t.Fatalf("PrepareEnabledMCPForRun: %v", err)
	}
	if env["PRAIMATE_MCP_REMOTE_AUTHORIZATION"] != "secret" {
		t.Fatalf("praimate-code env mismatch: %+v", env)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "opencode.json"))
	if err != nil {
		t.Fatalf("read opencode config: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, `"remote"`) || strings.Contains(text, `"disabled"`) {
		t.Fatalf("enabled MCP config mismatch:\n%s", text)
	}
}

func TestPrepareMCPForRun_ExpandsStoredStdioCommandPaths(t *testing.T) {
	t.Setenv("PRAIMATE_MCP_ROOT", "/tmp/ghidra root")

	s := openTempStore(t)
	defer s.Close()
	c, _ := New(Options{Store: s})
	ctx := context.Background()

	_, err := c.ConnectMCP(ctx, ConnectMCPRequest{
		ID:        "ghidra1132",
		Name:      "Ghidra1132",
		Transport: MCPTransportStdio,
		Command:   "$PRAIMATE_MCP_ROOT/.venv/bin/python",
		Args:      []string{"$PRAIMATE_MCP_ROOT/bridge_mcp_ghidra.py", "--ghidra-server", "http://127.0.0.1:8080/"},
	})
	if err != nil {
		t.Fatalf("ConnectMCP: %v", err)
	}

	dir := t.TempDir()
	a := &Agent{ID: "a", Name: "A", Instructions: "x", Supports: []string{"opencode"}, MCPServers: []string{"ghidra1132"}}
	if _, err := c.prepareMCPForRun(ctx, a, "praimate-code", dir); err != nil {
		t.Fatalf("prepare praimate-code: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "opencode.json"))
	if err != nil {
		t.Fatalf("read opencode config: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, `"/tmp/ghidra root/.venv/bin/python"`) ||
		!strings.Contains(text, `"/tmp/ghidra root/bridge_mcp_ghidra.py"`) ||
		strings.Contains(text, "$PRAIMATE_MCP_ROOT") {
		t.Fatalf("stdio command was not expanded:\n%s", text)
	}
}

func TestAddCustomMCP_Stdio(t *testing.T) {
	c, _ := New(Options{Store: openTempStore(t)})
	srv, err := c.AddCustomMCP(context.Background(), AddCustomMCPRequest{
		Name:      "HexStrike AI",
		Transport: "stdio",
		Command:   "hexstrike-mcp --port 9000",
		Env:       map[string]string{"HEXSTRIKE_TOKEN": "secret"},
	})
	if err != nil {
		t.Fatalf("AddCustomMCP: %v", err)
	}
	if srv.ID != "hexstrike-ai" {
		t.Fatalf("id = %q, want hexstrike-ai", srv.ID)
	}
	if srv.Command != "hexstrike-mcp" || len(srv.Args) != 2 || srv.Args[0] != "--port" {
		t.Fatalf("command/args parse wrong: %q %v", srv.Command, srv.Args)
	}
	if srv.Env["HEXSTRIKE_TOKEN"] != "secret" {
		t.Fatalf("env not stored: %v", srv.Env)
	}
	if srv.CatalogueKey != "" {
		t.Fatalf("custom server should have no catalogue key, got %q", srv.CatalogueKey)
	}
}

func TestAddCustomMCP_StdioQuotedAndExpanded(t *testing.T) {
	t.Setenv("GHIDRA_MCP_HOME", "/opt/Ghidra MCP")

	c, _ := New(Options{Store: openTempStore(t)})
	srv, err := c.AddCustomMCP(context.Background(), AddCustomMCPRequest{
		Name:      "Ghidra1132",
		Transport: "stdio",
		Command:   `"$GHIDRA_MCP_HOME/.venv/bin/python" "$GHIDRA_MCP_HOME/bridge mcp ghidra.py" --ghidra-server http://127.0.0.1:8080/`,
	})
	if err != nil {
		t.Fatalf("AddCustomMCP: %v", err)
	}
	if srv.Command != "/opt/Ghidra MCP/.venv/bin/python" {
		t.Fatalf("command = %q", srv.Command)
	}
	wantArgs := []string{"/opt/Ghidra MCP/bridge mcp ghidra.py", "--ghidra-server", "http://127.0.0.1:8080/"}
	if len(srv.Args) != len(wantArgs) {
		t.Fatalf("args len = %d, want %d (%v)", len(srv.Args), len(wantArgs), srv.Args)
	}
	for i := range wantArgs {
		if srv.Args[i] != wantArgs[i] {
			t.Fatalf("arg[%d] = %q, want %q (all: %v)", i, srv.Args[i], wantArgs[i], srv.Args)
		}
	}
}

func TestAddCustomMCP_StdioRejectsUnterminatedQuote(t *testing.T) {
	c, _ := New(Options{Store: openTempStore(t)})
	_, err := c.AddCustomMCP(context.Background(), AddCustomMCPRequest{
		Name:      "bad",
		Transport: "stdio",
		Command:   `"python server.py`,
	})
	if err == nil || !strings.Contains(err.Error(), "unterminated quote") {
		t.Fatalf("expected unterminated quote error, got %v", err)
	}
}

func TestAddCustomMCP_HTTPRequiresURL(t *testing.T) {
	c, _ := New(Options{Store: openTempStore(t)})
	_, err := c.AddCustomMCP(context.Background(), AddCustomMCPRequest{
		Name: "remote", Transport: "http",
	})
	if err == nil {
		t.Fatal("expected URL-required error for http transport")
	}
}

func TestUpdateMCPServer_PreservesIdentityAndEnabledState(t *testing.T) {
	c, _ := New(Options{Store: openTempStore(t)})
	ctx := context.Background()
	disabled := false
	original, err := c.AddCustomMCP(ctx, AddCustomMCPRequest{
		Name:      "Local tools",
		Transport: "stdio",
		Command:   "old-server",
		Enabled:   &disabled,
	})
	if err != nil {
		t.Fatalf("AddCustomMCP: %v", err)
	}

	updated, err := c.UpdateMCPServer(ctx, original.ID, AddCustomMCPRequest{
		Name:      "Local tools renamed",
		Transport: "http",
		URL:       "http://127.0.0.1:9000/mcp",
		Env:       map[string]string{"TOKEN": "new"},
	})
	if err != nil {
		t.Fatalf("UpdateMCPServer: %v", err)
	}
	if updated.ID != original.ID {
		t.Fatalf("ID changed from %q to %q", original.ID, updated.ID)
	}
	if updated.Enabled {
		t.Fatal("edit unexpectedly enabled a disabled server")
	}
	if updated.Name != "Local tools renamed" || updated.Transport != MCPTransportHTTP ||
		updated.URL != "http://127.0.0.1:9000/mcp" || updated.Command != "" {
		t.Fatalf("edited fields not persisted: %+v", updated)
	}
	if updated.Env["TOKEN"] != "new" {
		t.Fatalf("edited environment not persisted: %+v", updated.Env)
	}
}

func TestParseEnvLines(t *testing.T) {
	got := ParseEnvLines("A=1\nB = 2 , C=3\n\nbad")
	if got["A"] != "1" || got["B"] != "2" || got["C"] != "3" {
		t.Fatalf("parse wrong: %v", got)
	}
	if _, ok := got["bad"]; ok {
		t.Fatalf("line without = should be skipped: %v", got)
	}
}
