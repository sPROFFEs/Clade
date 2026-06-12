package main

// MCP approval shim — the bridge that gives claude/openclaude REAL
// mid-turn approvals in the GUI chat ("ask" Tools level):
//
//   claude --print … --mcp-config <tmp> --permission-prompt-tool
//          mcp__praimate__approve
//
// claude spawns THIS SAME BINARY (praimate-gui -mcp-approve <url>
// -mcp-token <tok>) as a stdio MCP server and calls its "approve" tool
// whenever a tool use needs permission. The shim forwards each request
// to the GUI's loopback approval broker (approval_broker.go), which
// shows the Allow/Deny card and blocks until the user answers. The
// shim's tool result is the JSON contract claude expects:
//
//   {"behavior":"allow","updatedInput":<original input>}   or
//   {"behavior":"deny","message":"…"}
//
// FAIL-CLOSED: any transport error, bad token, or broker timeout
// produces a deny — never a silent allow.
//
// Transport: MCP stdio = newline-delimited JSON-RPC 2.0, one message
// per line.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// runApprovalShim is the -mcp-approve entry point. Returns the process
// exit code.
func runApprovalShim(stdin io.Reader, stdout io.Writer, endpoint, token string) int {
	client := &http.Client{Timeout: 10 * time.Minute} // user think-time
	decide := func(tool string, input map[string]any) (bool, string) {
		body, _ := json.Marshal(map[string]any{"tool_name": tool, "input": input})
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return false, "approval broker unreachable: " + err.Error()
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Praimate-Token", token)
		resp, err := client.Do(req)
		if err != nil {
			return false, "approval broker unreachable: " + err.Error()
		}
		defer resp.Body.Close()
		var out struct {
			Behavior string `json:"behavior"`
			Message  string `json:"message"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || resp.StatusCode != http.StatusOK {
			return false, "approval broker error"
		}
		return out.Behavior == "allow", out.Message
	}
	return serveMCPApproval(stdin, stdout, decide)
}

// mcpRequest is the tolerant JSON-RPC 2.0 request superset we handle.
type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"params"`
}

// serveMCPApproval runs the MCP message loop. decide is called for each
// approve-tool invocation; its verdict becomes the permission result.
// Split from runApprovalShim so tests can drive it with buffers.
func serveMCPApproval(in io.Reader, out io.Writer, decide func(tool string, input map[string]any) (bool, string)) int {
	enc := json.NewEncoder(out)
	respond := func(id json.RawMessage, result any) {
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	respondErr := func(id json.RawMessage, code int, msg string) {
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}})
	}

	br := bufio.NewReaderSize(in, 1024*1024)
	for {
		raw, err := br.ReadBytes('\n')
		line := bytes.TrimSpace(raw)
		if len(line) > 0 {
			var req mcpRequest
			if jerr := json.Unmarshal(line, &req); jerr == nil {
				switch {
				case req.Method == "initialize":
					respond(req.ID, map[string]any{
						"protocolVersion": "2024-11-05",
						"capabilities":    map[string]any{"tools": map[string]any{}},
						"serverInfo":      map[string]any{"name": "praimate-approve", "version": "1.0"},
					})
				case req.Method == "ping":
					respond(req.ID, map[string]any{})
				case req.Method == "tools/list":
					respond(req.ID, map[string]any{
						"tools": []any{map[string]any{
							"name":        "approve",
							"description": "Forwards a permission request to the PrAImate GUI and returns the user's decision.",
							"inputSchema": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"tool_name": map[string]any{"type": "string"},
									"input":     map[string]any{"type": "object"},
								},
							},
						}},
					})
				case req.Method == "tools/call":
					if req.Params.Name != "approve" {
						respondErr(req.ID, -32602, "unknown tool "+req.Params.Name)
						break
					}
					tool, _ := req.Params.Arguments["tool_name"].(string)
					input, _ := req.Params.Arguments["input"].(map[string]any)
					allow, msg := decide(tool, input)
					var verdict map[string]any
					if allow {
						if input == nil {
							input = map[string]any{}
						}
						verdict = map[string]any{"behavior": "allow", "updatedInput": input}
					} else {
						if msg == "" {
							msg = "denied by user in PrAImate GUI"
						}
						verdict = map[string]any{"behavior": "deny", "message": msg}
					}
					vraw, _ := json.Marshal(verdict)
					respond(req.ID, map[string]any{
						"content": []any{map[string]any{"type": "text", "text": string(vraw)}},
					})
				case strings.HasPrefix(req.Method, "notifications/"):
					// fire-and-forget; no response
				default:
					if len(req.ID) > 0 && string(req.ID) != "null" {
						respondErr(req.ID, -32601, "method not found: "+req.Method)
					}
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				return 0
			}
			fmt.Fprintln(out) // best-effort flush separator
			return 1
		}
	}
}
