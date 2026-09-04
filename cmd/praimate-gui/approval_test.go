package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestApprovalBrokerDenyScopeReleasesDetachedRequest(t *testing.T) {
	emitted := make(chan ApprovalRequest, 1)
	b := &approvalBroker{
		pending: map[string]chan approvalDecision{},
		scopes:  map[string]string{},
		always:  map[string]map[string]bool{},
		emit:    func(req ApprovalRequest) { emitted <- req },
	}
	result := make(chan bool, 1)
	go func() {
		allowed, _ := b.request(context.Background(), "chat-detached", "Bash", nil)
		result <- allowed
	}()
	req := <-emitted
	if !b.hasPending("chat-detached") {
		t.Fatal("pending approval was not tracked by chat scope")
	}
	if b.resolveScoped("another-chat", req.ID, true, false) {
		t.Fatal("a detached chat resolved another chat's approval")
	}
	b.denyScope("chat-detached")
	select {
	case allowed := <-result:
		if allowed {
			t.Fatal("closing a detached window allowed its pending request")
		}
	case <-time.After(time.Second):
		t.Fatal("scoped denial did not release the pending request")
	}
}

// Drives the MCP shim loop end-to-end over buffers: initialize →
// tools/list → tools/call(approve), with a decide func standing in for
// the broker. Pins the JSON-RPC envelope and the permission verdict
// contract claude expects.
func TestServeMCPApproval_Protocol(t *testing.T) {
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"approve","arguments":{"tool_name":"Bash","input":{"command":"rm -rf node_modules"}}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"approve","arguments":{"tool_name":"Edit","input":{"file_path":"x.go"}}}}`,
	}, "\n") + "\n"

	var got []struct {
		tool  string
		input map[string]any
	}
	decide := func(tool string, input map[string]any) (bool, string) {
		got = append(got, struct {
			tool  string
			input map[string]any
		}{tool, input})
		return tool == "Bash", "user said no" // allow Bash, deny Edit
	}

	var out bytes.Buffer
	if code := serveMCPApproval(strings.NewReader(in), &out, decide); code != 0 {
		t.Fatalf("exit code %d", code)
	}

	var responses []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("non-JSON response line %q: %v", line, err)
		}
		responses = append(responses, m)
	}
	// initialize + tools/list + 2 tool calls = 4 responses (the
	// notification gets none).
	if len(responses) != 4 {
		t.Fatalf("want 4 responses, got %d: %v", len(responses), responses)
	}
	if len(got) != 2 || got[0].tool != "Bash" || got[1].tool != "Edit" {
		t.Fatalf("decide calls = %+v", got)
	}
	if got[0].input["command"] != "rm -rf node_modules" {
		t.Errorf("input not forwarded: %+v", got[0].input)
	}

	verdict := func(resp map[string]any) map[string]any {
		result := resp["result"].(map[string]any)
		content := result["content"].([]any)[0].(map[string]any)
		var v map[string]any
		if err := json.Unmarshal([]byte(content["text"].(string)), &v); err != nil {
			t.Fatalf("verdict not JSON: %v", err)
		}
		return v
	}
	allow := verdict(responses[2])
	if allow["behavior"] != "allow" || allow["updatedInput"] == nil {
		t.Errorf("Bash verdict = %v", allow)
	}
	deny := verdict(responses[3])
	if deny["behavior"] != "deny" || deny["message"] != "user said no" {
		t.Errorf("Edit verdict = %v", deny)
	}
}

// The broker must emit the request, block until resolved, honour
// "always allow", and reject bad tokens.
func TestApprovalBroker_ResolveAndAlways(t *testing.T) {
	var emitted []ApprovalRequest
	b, err := newApprovalBroker(func(req ApprovalRequest) {
		emitted = append(emitted, req)
		// Simulate the user clicking "Always allow" shortly after.
		go func() { time.Sleep(20 * time.Millisecond); b2resolve(req.ID) }()
	})
	if err != nil {
		t.Fatal(err)
	}
	resolveTarget = b
	url := "http://" + b.addr + "/approve/chat-1"
	post := func(token string) map[string]string {
		body := bytes.NewReader([]byte(`{"tool_name":"Bash","input":{"command":"go test"}}`))
		req, _ := http.NewRequest(http.MethodPost, url, body)
		req.Header.Set("X-Praimate-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return map[string]string{"status": resp.Status}
		}
		var out map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return out
	}

	// Wrong token → forbidden.
	if out := post("nope"); out["status"] == "" {
		t.Fatalf("bad token must be rejected, got %v", out)
	}
	// First request blocks until resolved with always=true.
	if out := post(b.token); out["behavior"] != "allow" {
		t.Fatalf("first request: %v", out)
	}
	if len(emitted) != 1 || emitted[0].ChatID != "chat-1" || emitted[0].Tool != "Bash" || emitted[0].Detail != "go test" {
		t.Fatalf("emitted = %+v", emitted)
	}
	// Second identical request auto-allows with NO new emission.
	if out := post(b.token); out["behavior"] != "allow" {
		t.Fatalf("always-allow replay: %v", out)
	}
	if len(emitted) != 1 {
		t.Fatalf("always-allowed request must not emit again; emitted=%d", len(emitted))
	}
}

// test plumbing: the emit callback fires before `b` is assigned, so the
// goroutine resolves through this indirection.
var resolveTarget *approvalBroker

func b2resolve(id string) {
	if resolveTarget != nil {
		resolveTarget.resolve(id, true, true)
	}
}
