package core

import (
	"context"
	"errors"
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
