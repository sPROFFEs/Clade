package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"git.jtsec.local/lab/PrAImate/internal/core"
	"git.jtsec.local/lab/PrAImate/internal/launcher"
	"git.jtsec.local/lab/PrAImate/internal/store"
)

type agentPromptTestAdapter struct {
	t             *testing.T
	expectedModel string
	expectedURL   string
	expectedToken string
}

func (agentPromptTestAdapter) Name() string                    { return "claude" }
func (agentPromptTestAdapter) Available(context.Context) error { return nil }
func (agentPromptTestAdapter) SupportsResume() bool            { return false }
func (agentPromptTestAdapter) Resume(context.Context, string, core.ResumeOpts) (*core.Reply, error) {
	return nil, errors.New("resume unsupported")
}
func (a agentPromptTestAdapter) SingleShot(_ context.Context, opts core.SingleShotOpts) (*core.Reply, error) {
	if opts.Tools != "" {
		a.t.Fatalf("headless Safe inherited wider tools policy %q", opts.Tools)
	}
	if opts.Model != a.expectedModel {
		a.t.Fatalf("model = %q, want %q", opts.Model, a.expectedModel)
	}
	if opts.Env["ANTHROPIC_BASE_URL"] != a.expectedURL {
		a.t.Fatalf("endpoint = %q, want %q", opts.Env["ANTHROPIC_BASE_URL"], a.expectedURL)
	}
	if opts.Env["ANTHROPIC_AUTH_TOKEN"] != a.expectedToken {
		a.t.Fatalf("endpoint token = %q, want encrypted setting", opts.Env["ANTHROPIC_AUTH_TOKEN"])
	}
	return &core.Reply{Text: "reviewed: " + opts.Message, ExitCode: 0}, nil
}

func TestRunAgentPromptProducesStableJSONAndRemovesTemporaryChat(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PRAIMATE_HOME", root)
	st, err := store.InitializeWithPassword(filepath.Join(root, "db.sqlite"), "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if err := launcher.SaveConfig(&launcher.Config{DefaultLocalEndpoint: "https://llm.example/v1"}); err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	c, err := core.New(core.Options{Store: st})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetSetting(context.Background(), core.ScopeCLI, "local_llm.api_key", []byte(`"db-secret"`)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ImportAgentYAML(context.Background(), []byte(`
schema: praimate.agent/v1
id: dev-team
name: Dev Team
description: Reviews code
instructions: Review carefully.
supports: [codex, claude]
tools: []
mcp_servers: []
workflows: []
surfaces: [chat]
`), ""); err != nil {
		t.Fatal(err)
	}
	if err := core.SaveAgentRuntime("dev-team", &core.AgentRuntimeManifest{
		Schema: core.AgentRuntimeSchema, Mode: core.RuntimeNative,
		Permissions: core.AgentRuntimePermissions{DefaultTools: "edits"},
	}); err != nil {
		t.Fatal(err)
	}
	core.RegisterCLIAdapter(agentPromptTestAdapter{
		t: t, expectedModel: "qwen3-coder", expectedURL: "https://llm.example/v1", expectedToken: "db-secret",
	})
	t.Cleanup(func() { core.RegisterCLIAdapter(core.NewClaudeAdapter()) })

	oldOpen, oldInput, oldOutput, oldError := openAgentRunCore, agentRunInput, agentRunOutput, agentRunError
	oldReadPassword, oldTerminalAvailable := readAgentRunTerminalPassword, agentRunTerminalAvailable
	t.Cleanup(func() {
		openAgentRunCore, agentRunInput, agentRunOutput, agentRunError = oldOpen, oldInput, oldOutput, oldError
		readAgentRunTerminalPassword, agentRunTerminalAvailable = oldReadPassword, oldTerminalAvailable
	})
	var openPasswords []string
	openAgentRunCore = func(password string) (*core.Core, func(), error) {
		openPasswords = append(openPasswords, password)
		if password == "" {
			return nil, nil, store.ErrPasswordRequired
		}
		if password != "correct horse battery staple" {
			return nil, nil, errors.New("unexpected password")
		}
		return c, func() {}, nil
	}
	agentRunTerminalAvailable = func() bool { return true }
	readAgentRunTerminalPassword = func() (string, error) {
		return "correct horse battery staple", nil
	}
	var stdout, stderr bytes.Buffer
	agentRunInput, agentRunOutput, agentRunError = bytes.NewBuffer(nil), &stdout, &stderr

	code := runAgentPrompt(agentPromptOptions{
		AgentID: "dev-team", Folder: root, Prompt: "review this code",
		CLI: "claude", Model: "qwen3-coder", Endpoint: "saved",
		Output: "json", Tools: "safe",
	})
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	var got agentRunResult
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if !got.OK || got.Schema != agentRunSchema || got.AgentID != "dev-team" || got.CLI != "claude" || got.Reply != "reviewed: review this code" {
		t.Fatalf("unexpected result: %#v", got)
	}
	if got.ChatID != "" {
		t.Fatalf("temporary run exposed chat id %q", got.ChatID)
	}
	chats, err := c.ListChats(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 0 {
		t.Fatalf("temporary chats retained: %#v", chats)
	}
	if len(openPasswords) != 2 || openPasswords[0] != "" || openPasswords[1] != "correct horse battery staple" {
		t.Fatalf("database open passwords = %#v, want remembered lookup then hidden terminal password", openPasswords)
	}
}

func TestReadAgentPromptRejectsDualSources(t *testing.T) {
	if _, err := readAgentPrompt(agentPromptOptions{Prompt: "inline", PromptFile: "prompt.txt"}); err == nil {
		t.Fatal("expected dual prompt sources to fail")
	}
}

func TestResolveAgentEndpointRejectsCredentialRedirect(t *testing.T) {
	t.Setenv("PRAIMATE_HOME", t.TempDir())
	if err := launcher.SaveConfig(&launcher.Config{DefaultLocalEndpoint: "https://trusted.example/v1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveAgentEndpoint("https://attacker.example/v1"); err == nil {
		t.Fatal("expected an endpoint different from the saved endpoint to be rejected")
	}
	if got, err := resolveAgentEndpoint("saved"); err != nil || got != "https://trusted.example/v1" {
		t.Fatalf("resolve saved endpoint = %q, %v", got, err)
	}
}

func TestWriteAgentFailureUsesResultEnvelopeForJSONL(t *testing.T) {
	oldOutput := agentRunOutput
	t.Cleanup(func() { agentRunOutput = oldOutput })
	var stdout bytes.Buffer
	agentRunOutput = &stdout

	if code := writeAgentFailure(agentPromptOptions{AgentID: "dev-team", Output: "jsonl"}, 2, "bad input"); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	var got struct {
		Type  string `json:"type"`
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSONL result: %v", err)
	}
	if got.Type != "result" || got.OK || got.Error != "bad input" {
		t.Fatalf("failure envelope = %#v", got)
	}
}
