package core

// MCP catalogue and connected-server storage.
//
// The catalogue is intentionally static in 1.0: it gives both UIs the
// same known-provider list without network calls. Connecting a provider
// persists a row in mcp_servers; launch-time per-CLI config emission is
// a separate layer that reads these rows and the agent's MCPServers list.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// MCP transport names mirror the MCP config vocabulary.
type MCPTransport string

const (
	MCPTransportStdio MCPTransport = "stdio"
	MCPTransportHTTP  MCPTransport = "http"
	MCPTransportSSE   MCPTransport = "sse"
)

// MCPAuthType describes how a catalogue provider is connected.
type MCPAuthType string

const (
	MCPAuthNone   MCPAuthType = "none"
	MCPAuthAPIKey MCPAuthType = "api_key"
	MCPAuthOAuth  MCPAuthType = "oauth"
)

// MCPAuthSpec is provider metadata for the connection UI. OAuth flow
// execution is intentionally not here yet; this tells the UI what to
// ask for and lets Core persist pasted tokens/API keys.
type MCPAuthSpec struct {
	Type        MCPAuthType `json:"type"`
	Label       string      `json:"label,omitempty"`
	EnvVar      string      `json:"env_var,omitempty"`
	Header      string      `json:"header,omitempty"`
	Scopes      []string    `json:"scopes,omitempty"`
	OAuthIssuer string      `json:"oauth_issuer,omitempty"`
}

// MCPCatalogueEntry is one built-in provider users can connect.
type MCPCatalogueEntry struct {
	Key         string       `json:"key"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Transport   MCPTransport `json:"transport"`
	Command     string       `json:"command,omitempty"`
	URL         string       `json:"url,omitempty"`
	Args        []string     `json:"args,omitempty"`
	Auth        MCPAuthSpec  `json:"auth"`
	DocsURL     string       `json:"docs_url,omitempty"`
}

// MCPServer is one connected server row.
type MCPServer struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Transport    MCPTransport      `json:"transport"`
	Command      string            `json:"command,omitempty"`
	URL          string            `json:"url,omitempty"`
	Args         []string          `json:"args"`
	Env          map[string]string `json:"env"`
	Auth         map[string]string `json:"auth"`
	Enabled      bool              `json:"enabled"`
	CatalogueKey string            `json:"catalogue_key,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
}

// ConnectMCPRequest persists either a built-in catalogue provider or a
// fully custom MCP server. For catalogue entries, Command/URL/Args
// default from the catalogue and APIKey is mapped into the declared
// EnvVar/Header metadata.
type ConnectMCPRequest struct {
	CatalogueKey string
	ID           string
	Name         string
	Transport    MCPTransport
	Command      string
	URL          string
	Args         []string
	Env          map[string]string
	Auth         map[string]string
	APIKey       string
	Enabled      *bool
}

// ErrMCPServerNotFound is returned when a requested server row is absent.
var ErrMCPServerNotFound = errors.New("mcp server not found")

// ListMCPCatalogue returns the static provider list in display order.
func ListMCPCatalogue() []MCPCatalogueEntry {
	out := make([]MCPCatalogueEntry, len(mcpCatalogue))
	copy(out, mcpCatalogue)
	return out
}

// GetMCPCatalogueEntry returns one static provider by key.
func GetMCPCatalogueEntry(key string) (*MCPCatalogueEntry, bool) {
	for i := range mcpCatalogue {
		if mcpCatalogue[i].Key == key {
			entry := mcpCatalogue[i]
			return &entry, true
		}
	}
	return nil, false
}

// ConnectMCP inserts or updates one connected MCP server.
func (c *Core) ConnectMCP(ctx context.Context, req ConnectMCPRequest) (*MCPServer, error) {
	if c.store == nil {
		return nil, errors.New("ConnectMCP: no store configured")
	}
	server, err := buildMCPServer(req)
	if err != nil {
		return nil, err
	}
	args, _ := json.Marshal(orEmpty(server.Args))
	env, _ := json.Marshal(orEmptyStringMap(server.Env))
	auth, _ := json.Marshal(orEmptyStringMap(server.Auth))
	enabled := 0
	if server.Enabled {
		enabled = 1
	}
	_, err = c.store.DB().ExecContext(ctx, `
		INSERT INTO mcp_servers (
			id, name, transport, command, url, args_json, env_json,
			auth_json, enabled, catalogue_key, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			transport = excluded.transport,
			command = excluded.command,
			url = excluded.url,
			args_json = excluded.args_json,
			env_json = excluded.env_json,
			auth_json = excluded.auth_json,
			enabled = excluded.enabled,
			catalogue_key = excluded.catalogue_key
	`, server.ID, server.Name, string(server.Transport), nullableText(server.Command),
		nullableText(server.URL), string(args), string(env), string(auth),
		enabled, nullableText(server.CatalogueKey), server.CreatedAt.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("upsert mcp server %s: %w", server.ID, err)
	}
	return c.GetMCPServer(ctx, server.ID)
}

// ListMCPServers returns all connected servers. If enabledOnly is true,
// disabled rows are filtered.
func (c *Core) ListMCPServers(ctx context.Context, enabledOnly bool) ([]MCPServer, error) {
	if c.store == nil {
		return nil, errors.New("ListMCPServers: no store configured")
	}
	q := `SELECT id, name, transport, command, url, args_json, env_json,
	             auth_json, enabled, catalogue_key, created_at
	      FROM mcp_servers`
	if enabledOnly {
		q += ` WHERE enabled = 1`
	}
	q += ` ORDER BY name`
	rows, err := c.store.DB().QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MCPServer
	for rows.Next() {
		s, err := scanMCPServer(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// GetMCPServer fetches one connected server by id.
func (c *Core) GetMCPServer(ctx context.Context, id string) (*MCPServer, error) {
	if c.store == nil {
		return nil, errors.New("GetMCPServer: no store configured")
	}
	row := c.store.DB().QueryRowContext(ctx, `
		SELECT id, name, transport, command, url, args_json, env_json,
		       auth_json, enabled, catalogue_key, created_at
		FROM mcp_servers WHERE id = ?
	`, id)
	s, err := scanMCPServer(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrMCPServerNotFound, id)
	}
	return s, err
}

// SetMCPEnabled toggles one connected server.
func (c *Core) SetMCPEnabled(ctx context.Context, id string, enabled bool) error {
	if c.store == nil {
		return errors.New("SetMCPEnabled: no store configured")
	}
	flag := 0
	if enabled {
		flag = 1
	}
	res, err := c.store.DB().ExecContext(ctx,
		`UPDATE mcp_servers SET enabled = ? WHERE id = ?`, flag, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrMCPServerNotFound, id)
	}
	return nil
}

// DeleteMCPServer removes one connected server.
func (c *Core) DeleteMCPServer(ctx context.Context, id string) error {
	if c.store == nil {
		return errors.New("DeleteMCPServer: no store configured")
	}
	res, err := c.store.DB().ExecContext(ctx, `DELETE FROM mcp_servers WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrMCPServerNotFound, id)
	}
	return nil
}

func buildMCPServer(req ConnectMCPRequest) (*MCPServer, error) {
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	s := &MCPServer{
		ID:           req.ID,
		Name:         req.Name,
		Transport:    req.Transport,
		Command:      req.Command,
		URL:          req.URL,
		Args:         append([]string(nil), req.Args...),
		Env:          copyStringMap(req.Env),
		Auth:         copyStringMap(req.Auth),
		Enabled:      enabled,
		CatalogueKey: req.CatalogueKey,
		CreatedAt:    time.Now().UTC(),
	}
	if req.CatalogueKey != "" {
		entry, ok := GetMCPCatalogueEntry(req.CatalogueKey)
		if !ok {
			return nil, fmt.Errorf("ConnectMCP: unknown catalogue key %q", req.CatalogueKey)
		}
		if s.ID == "" {
			s.ID = entry.Key
		}
		if s.Name == "" {
			s.Name = entry.Name
		}
		if s.Transport == "" {
			s.Transport = entry.Transport
		}
		if s.Command == "" {
			s.Command = entry.Command
		}
		if s.URL == "" {
			s.URL = entry.URL
		}
		if len(s.Args) == 0 {
			s.Args = append([]string(nil), entry.Args...)
		}
		if s.Env == nil {
			s.Env = map[string]string{}
		}
		if s.Auth == nil {
			s.Auth = map[string]string{}
		}
		if req.APIKey != "" {
			if entry.Auth.EnvVar != "" {
				s.Env[entry.Auth.EnvVar] = req.APIKey
			}
			if entry.Auth.Header != "" {
				s.Auth["header"] = entry.Auth.Header
				s.Auth["token"] = req.APIKey
			}
			s.Auth["type"] = string(entry.Auth.Type)
		}
		if entry.Auth.Type == MCPAuthAPIKey && req.APIKey == "" && entry.Auth.EnvVar != "" && s.Env[entry.Auth.EnvVar] == "" {
			return nil, fmt.Errorf("ConnectMCP %s: APIKey required", entry.Key)
		}
		if entry.Auth.Type == MCPAuthOAuth {
			s.Auth["type"] = string(MCPAuthOAuth)
			if entry.Auth.OAuthIssuer != "" {
				s.Auth["issuer"] = entry.Auth.OAuthIssuer
			}
			if len(entry.Auth.Scopes) > 0 {
				s.Auth["scopes"] = strings.Join(entry.Auth.Scopes, " ")
			}
		}
	}
	if s.ID == "" {
		return nil, errors.New("ConnectMCP: ID required")
	}
	if s.Name == "" {
		return nil, errors.New("ConnectMCP: Name required")
	}
	if !knownMCPTransport(s.Transport) {
		return nil, fmt.Errorf("ConnectMCP %s: unknown transport %q", s.ID, s.Transport)
	}
	if s.Transport == MCPTransportStdio && s.Command == "" {
		return nil, fmt.Errorf("ConnectMCP %s: Command required for stdio transport", s.ID)
	}
	if (s.Transport == MCPTransportHTTP || s.Transport == MCPTransportSSE) && s.URL == "" {
		return nil, fmt.Errorf("ConnectMCP %s: URL required for %s transport", s.ID, s.Transport)
	}
	return s, nil
}

func scanMCPServer(scan func(...any) error) (*MCPServer, error) {
	var (
		s                                    MCPServer
		transport                            string
		command, url, catalogueKey           sql.NullString
		argsJSON, envJSON, authJSON, created string
		enabled                              int
	)
	err := scan(&s.ID, &s.Name, &transport, &command, &url, &argsJSON,
		&envJSON, &authJSON, &enabled, &catalogueKey, &created)
	if err != nil {
		return nil, err
	}
	s.Transport = MCPTransport(transport)
	if command.Valid {
		s.Command = command.String
	}
	if url.Valid {
		s.URL = url.String
	}
	if catalogueKey.Valid {
		s.CatalogueKey = catalogueKey.String
	}
	s.Enabled = enabled == 1
	if err := json.Unmarshal([]byte(argsJSON), &s.Args); err != nil {
		return nil, fmt.Errorf("decode args_json: %w", err)
	}
	if err := json.Unmarshal([]byte(envJSON), &s.Env); err != nil {
		return nil, fmt.Errorf("decode env_json: %w", err)
	}
	if err := json.Unmarshal([]byte(authJSON), &s.Auth); err != nil {
		return nil, fmt.Errorf("decode auth_json: %w", err)
	}
	if created != "" {
		t, err := time.Parse(time.RFC3339, created)
		if err != nil {
			return nil, fmt.Errorf("decode created_at: %w", err)
		}
		s.CreatedAt = t
	}
	return &s, nil
}

func knownMCPTransport(t MCPTransport) bool {
	switch t {
	case MCPTransportStdio, MCPTransportHTTP, MCPTransportSSE:
		return true
	}
	return false
}

func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

var mcpCatalogue = []MCPCatalogueEntry{
	stdioMCP("sqlite", "SQLite", "Read and inspect a local SQLite database.", "SQLITE_DB_PATH", "@modelcontextprotocol/server-sqlite"),
	stdioMCP("filesystem", "Filesystem", "Scoped local file access for selected directories.", "", "@modelcontextprotocol/server-filesystem"),
	stdioMCP("fetch", "Fetch", "HTTP fetch and web content conversion.", "", "@modelcontextprotocol/server-fetch"),
	stdioMCP("browser", "Browser", "Browser automation for web QA and scraping.", "", "@modelcontextprotocol/server-puppeteer"),
	stdioMCP("sequential-thinking", "Sequential Thinking", "Structured reasoning scratchpad for multi-step tasks.", "", "@modelcontextprotocol/server-sequential-thinking"),
}

func stdioMCP(key, name, desc, envVar, pkg string) MCPCatalogueEntry {
	auth := MCPAuthSpec{Type: MCPAuthNone}
	if envVar != "" {
		auth = MCPAuthSpec{Type: MCPAuthAPIKey, Label: name + " API key", EnvVar: envVar}
	}
	return MCPCatalogueEntry{
		Key:         key,
		Name:        name,
		Description: desc,
		Transport:   MCPTransportStdio,
		Command:     "npx",
		Args:        []string{"-y", pkg},
		Auth:        auth,
	}
}
