package core

// Custom MCP servers — add a locally-run or self-hosted MCP that isn't
// in the built-in catalogue (e.g. hexstrike-ai). The user supplies the
// command (stdio) or URL (http/sse), plus optional args and env. This
// is a thin convenience over ConnectMCP that parses the simple text
// inputs a TUI/GUI form collects.

import (
	"context"
	"fmt"
	"strings"
)

// AddCustomMCPRequest is the form-friendly shape for a user-defined MCP.
// Transport drives which of Command/URL is required.
type AddCustomMCPRequest struct {
	Name      string // required; also derives the id when ID is empty
	ID        string // optional; slugified from Name if blank
	Transport string // "stdio" | "http" | "sse"
	Command   string // stdio: the executable (may include args inline)
	Args      []string
	URL       string            // http/sse endpoint
	Env       map[string]string // KEY=VALUE pairs (e.g. API tokens)
}

// AddCustomMCP validates and persists a user-defined MCP server. Returns
// the stored server.
func (c *Core) AddCustomMCP(ctx context.Context, req AddCustomMCPRequest) (*MCPServer, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("AddCustomMCP: Name required")
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = mcpSlug(name)
	}
	if id == "" {
		return nil, fmt.Errorf("AddCustomMCP: could not derive an id from name %q", name)
	}
	transport := strings.TrimSpace(req.Transport)
	if transport == "" {
		transport = string(MCPTransportStdio)
	}

	// Allow the command field to carry inline args ("python server.py
	// --port 9000"); split them out when explicit Args weren't given.
	command := strings.TrimSpace(req.Command)
	args := req.Args
	if transport == string(MCPTransportStdio) && len(args) == 0 && command != "" {
		fields := strings.Fields(command)
		if len(fields) > 1 {
			command = fields[0]
			args = fields[1:]
		}
	}

	return c.ConnectMCP(ctx, ConnectMCPRequest{
		ID:        id,
		Name:      name,
		Transport: MCPTransport(transport),
		Command:   command,
		Args:      args,
		URL:       strings.TrimSpace(req.URL),
		Env:       req.Env,
	})
}

// ParseEnvLines turns "KEY=VALUE" lines (newline- or comma-separated)
// into a map. Blank lines and lines without "=" are skipped. Shared by
// the TUI and GUI custom-MCP forms.
func ParseEnvLines(s string) map[string]string {
	out := map[string]string{}
	if strings.TrimSpace(s) == "" {
		return out
	}
	repl := strings.NewReplacer("\r", "\n", ",", "\n")
	for _, line := range strings.Split(repl.Replace(s), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		k := strings.TrimSpace(line[:eq])
		v := strings.TrimSpace(line[eq+1:])
		if k != "" {
			out[k] = v
		}
	}
	return out
}

// mcpSlug normalises a display name to a stable id: lowercase, spaces
// and runs of punctuation to single hyphens, [a-z0-9-] only.
func mcpSlug(s string) string {
	b := make([]byte, 0, len(s))
	prevDash := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			b = append(b, c+('a'-'A'))
			prevDash = false
		case (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9'):
			b = append(b, c)
			prevDash = false
		default:
			if !prevDash {
				b = append(b, '-')
				prevDash = true
			}
		}
	}
	for len(b) > 0 && b[len(b)-1] == '-' {
		b = b[:len(b)-1]
	}
	return string(b)
}
