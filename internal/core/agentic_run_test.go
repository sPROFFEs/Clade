package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func autonomousTestAgent(id, cli string) *Agent {
	return &Agent{
		ID: id, Name: "Autonomous Test", Instructions: "Work carefully.", Supports: []string{cli},
		Surfaces:  append([]string(nil), AllSurfaces...),
		Workflows: []Workflow{{Name: "review", Steps: []WorkflowStep{{Kind: StepUserMessage, Template: "Review {{ .target }}"}}, Inputs: []WorkflowInput{{Name: "target", Required: true}}}},
	}
}

func saveAutonomousRuntime(t *testing.T, id string) {
	t.Helper()
	if err := SaveAgentRuntime(id, &AgentRuntimeManifest{
		Schema: AgentRuntimeSchema, PresetOrigin: PresetAutonomous, Mode: RuntimeAgentic,
		Features: AgentRuntimeFeatures{ManagedTools: true, WorkingMemory: true, Artifacts: true},
		Limits:   AgentRuntimeLimits{MaxTurns: 8, MaxContextChars: 8000, MaxOutputChars: 2000},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRunManagedAgentUsesSafeCLIAndPersistsInspectableState(t *testing.T) {
	withTempConfigDir(t)
	mock := &mockAdapter{name: "managed-mock", resumable: true, replies: []string{
		`{"action":"tool","tool":"memory.note","arguments":{"content":"checked parser"}}`,
		`{"action":"tool","tool":"artifact.write","arguments":{"name":"report.md","content":"managed report"}}`,
		`{"action":"finish","message":"Done safely."}`,
	}}
	withMockAdapter(t, mock)
	c, _ := New(Options{Store: openTempStore(t)})
	agent := autonomousTestAgent("managed", mock.name)
	saveAutonomousRuntime(t, agent.ID)

	var events []ManagedRunEvent
	run, err := c.RunManagedAgent(context.Background(), ManagedRunRequest{
		Surface: SurfaceChat, Agent: agent, CLI: mock.name, Cwd: t.TempDir(), Task: "Review.",
		Env:     map[string]string{"PRAIMATE_MANAGED_TEST": "present"},
		OnEvent: func(event ManagedRunEvent) { events = append(events, event) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.State != "completed" || run.Final != "Done safely." || len(run.Memory) != 1 || len(run.Artifacts) != 1 {
		t.Fatalf("run = %#v", run)
	}
	if len(mock.shots) != 1 || mock.shots[0].Tools != "" || len(mock.resumes) != 2 {
		t.Fatalf("managed CLI calls shots=%d resumes=%d tools=%q", len(mock.shots), len(mock.resumes), mock.shots[0].Tools)
	}
	if mock.shots[0].Env["PRAIMATE_MANAGED_TEST"] != "present" {
		t.Fatalf("managed env was not propagated: %#v", mock.shots[0].Env)
	}
	listed, err := c.ListManagedRuns(agent.ID)
	if err != nil || len(listed) != 1 || listed[0].ID != run.ID {
		t.Fatalf("listed=%#v err=%v", listed, err)
	}
	loaded, err := c.GetManagedRun(run.ID)
	if err != nil || len(loaded.Memory) != 1 {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	body, err := c.ReadManagedArtifact(run.ID, "report.md")
	if err != nil || string(body) != "managed report" {
		t.Fatalf("artifact=%q err=%v", body, err)
	}
	if len(events) == 0 || events[0].Type != "run.started" {
		t.Fatalf("events = %#v", events)
	}
}

func TestRunManagedAgentRequiresCompletePhaseThreeFeatureSet(t *testing.T) {
	withTempConfigDir(t)
	mock := &mockAdapter{name: "incomplete-managed", replies: []string{`{"action":"finish","message":"bad"}`}}
	withMockAdapter(t, mock)
	c, _ := New(Options{Store: openTempStore(t)})
	agent := autonomousTestAgent("incomplete", mock.name)
	if err := SaveAgentRuntime(agent.ID, &AgentRuntimeManifest{
		Schema: AgentRuntimeSchema, Mode: RuntimeAgentic,
		Features: AgentRuntimeFeatures{WorkingMemory: true},
	}); err != nil {
		t.Fatal(err)
	}
	_, err := c.RunManagedAgent(context.Background(), ManagedRunRequest{
		Surface: SurfaceChat, Agent: agent, CLI: mock.name, Cwd: t.TempDir(), Task: "Do it.",
	})
	if err == nil || !strings.Contains(err.Error(), "managed tools") || !strings.Contains(err.Error(), "artifact store") {
		t.Fatalf("err = %v", err)
	}
	if len(mock.shots) != 0 {
		t.Fatal("incomplete runtime reached the CLI")
	}
}

func TestRunManagedAgentCallsMCPThroughApprovalBroker(t *testing.T) {
	withTempConfigDir(t)
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case "initialize":
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"protocolVersion": managedMCPProtocolVersion, "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "test", "version": "1"}}})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"tools": []map[string]any{{"name": "lookup", "description": "Look up a value", "inputSchema": map[string]any{"type": "object"}}}}})
		case "tools/call":
			called = true
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"content": []map[string]any{{"type": "text", "text": "MCP result"}}}})
		}
	}))
	defer server.Close()
	mock := &mockAdapter{name: "managed-mcp", resumable: true, replies: []string{
		`{"action":"tool","tool":"mcp.call","arguments":{"server":"local-test","tool":"lookup","arguments":{"value":"x"}}}`,
		`{"action":"finish","message":"MCP complete."}`,
	}}
	withMockAdapter(t, mock)
	c, _ := New(Options{Store: openTempStore(t)})
	agent := autonomousTestAgent("managed-mcp-agent", mock.name)
	agent.MCPServers = []string{"local-test"}
	if _, err := c.ConnectMCP(context.Background(), ConnectMCPRequest{ID: "local-test", Name: "Local test", Transport: MCPTransportHTTP, URL: server.URL}); err != nil {
		t.Fatal(err)
	}
	if err := SaveAgentRuntime(agent.ID, &AgentRuntimeManifest{
		Schema: AgentRuntimeSchema, PresetOrigin: PresetAutonomous, Mode: RuntimeAgentic,
		Capabilities: AgentCapabilities{ExternalServices: true},
		Features:     AgentRuntimeFeatures{ManagedTools: true, WorkingMemory: true, Artifacts: true, Checkpoints: true},
		Limits:       AgentRuntimeLimits{MaxTurns: 8, MaxContextChars: 8000, MaxOutputChars: 2000},
	}); err != nil {
		t.Fatal(err)
	}
	approvals := 0
	run, err := c.RunManagedAgent(context.Background(), ManagedRunRequest{
		Surface: SurfaceChat, Agent: agent, CLI: mock.name, Cwd: t.TempDir(), Task: "Do it.",
		Approval: &ApprovalConfig{Request: func(_ context.Context, tool string, _ map[string]any) (bool, error) {
			approvals++
			return strings.Contains(tool, "lookup") || strings.Contains(tool, "connect"), nil
		}},
	})
	if err != nil || run.State != "completed" || !called || approvals != 2 {
		t.Fatalf("run=%#v called=%v approvals=%d err=%v", run, called, approvals, err)
	}
}

func TestRunManagedAgentExposesRAGKnowledgeBroker(t *testing.T) {
	withTempConfigDir(t)
	mock := &mockAdapter{name: "managed-rag", replies: []string{`{"action":"finish","message":"bad"}`}}
	withMockAdapter(t, mock)
	c, _ := New(Options{Store: openTempStore(t)})
	agent := autonomousTestAgent("managed-rag-agent", mock.name)
	agent.Knowledge = "rag"
	knowledgeDir, err := AgentKnowledgeDir(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(knowledgeDir, "graphify-out"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(knowledgeDir, "graphify-out", "graph.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	saveAutonomousRuntime(t, agent.ID)
	run, err := c.RunManagedAgent(context.Background(), ManagedRunRequest{
		Surface: SurfaceChat, Agent: agent, CLI: mock.name, Cwd: t.TempDir(), Task: "Do it.",
	})
	if err != nil || run.State != "completed" {
		t.Fatalf("run=%#v err=%v", run, err)
	}
	if len(mock.shots) != 1 || !strings.Contains(mock.shots[0].SystemPrompt, "knowledge.query") {
		t.Fatalf("managed RAG prompt = %#v", mock.shots)
	}
}

func TestCoreResumesManagedRunFromStoredRequestAndCheckpoint(t *testing.T) {
	withTempConfigDir(t)
	mock := &mockAdapter{name: "claude", resumable: true, failOnTurn: 2, replies: []string{
		`{"action":"tool","tool":"memory.fact","arguments":{"content":"durable fact"}}`,
	}}
	RegisterCLIAdapter(mock)
	t.Cleanup(func() { RegisterCLIAdapter(NewClaudeAdapter()) })
	c, _ := New(Options{Store: openTempStore(t)})
	agent := autonomousTestAgent("managed-resume-agent", mock.name)
	if _, err := c.upsertAgent(context.Background(), agent); err != nil {
		t.Fatal(err)
	}
	saveAutonomousRuntime(t, agent.ID)
	failed, err := c.RunManagedAgent(context.Background(), ManagedRunRequest{
		Surface: SurfaceChat, Agent: agent, CLI: mock.name, Cwd: t.TempDir(), Task: "Recover me.",
	})
	if err == nil || failed == nil || failed.State != "failed" {
		t.Fatalf("failed=%#v err=%v", failed, err)
	}
	details, err := c.GetManagedRun(failed.ID)
	if err != nil || !details.CanResume {
		t.Fatalf("details=%#v err=%v", details, err)
	}
	root, _ := managedRunsRoot()
	spec, err := loadManagedRunSpec(root, failed.ID)
	if err != nil || spec.Local != nil {
		t.Fatalf("spec=%#v err=%v", spec, err)
	}
	mock.failOnTurn = 0
	mock.replies = []string{`{"action":"finish","message":"Recovered through Core."}`}
	resumed, err := c.ResumeManagedRun(context.Background(), failed.ID, nil)
	if err != nil || resumed.ID != failed.ID || resumed.State != "completed" || resumed.Final != "Recovered through Core." {
		t.Fatalf("resumed=%#v err=%v", resumed, err)
	}
	if len(mock.resumes) < 2 || !strings.Contains(mock.resumes[len(mock.resumes)-1].Message, "durable fact") {
		t.Fatalf("resume calls=%#v", mock.resumes)
	}
}

func TestRunManagedAgentRejectsCLIWithoutEnforcedSafeMode(t *testing.T) {
	withTempConfigDir(t)
	mock := &mockAdapter{name: "unsafe-managed", unsafe: true, replies: []string{`{"action":"finish","message":"bad"}`}}
	withMockAdapter(t, mock)
	c, _ := New(Options{Store: openTempStore(t)})
	agent := autonomousTestAgent("unsafe-managed-agent", mock.name)
	saveAutonomousRuntime(t, agent.ID)
	_, err := c.RunManagedAgent(context.Background(), ManagedRunRequest{
		Surface: SurfaceChat, Agent: agent, CLI: mock.name, Cwd: t.TempDir(), Task: "Do it.",
	})
	if err == nil || !strings.Contains(err.Error(), "safe mode") {
		t.Fatalf("err = %v", err)
	}
	if len(mock.shots) != 0 {
		t.Fatal("unsafe managed adapter reached the CLI")
	}
}

func TestRunManagedAgentRejectsUnimplementedSandboxClaim(t *testing.T) {
	withTempConfigDir(t)
	mock := &mockAdapter{name: "blocked-managed", replies: []string{`{"action":"finish","message":"bad"}`}}
	withMockAdapter(t, mock)
	c, _ := New(Options{Store: openTempStore(t)})
	agent := autonomousTestAgent("blocked", mock.name)
	if err := SaveAgentRuntime(agent.ID, &AgentRuntimeManifest{
		Schema: AgentRuntimeSchema, Mode: RuntimeAgentic,
		Features: AgentRuntimeFeatures{ManagedTools: true, Sandbox: true},
	}); err != nil {
		t.Fatal(err)
	}
	_, err := c.RunManagedAgent(context.Background(), ManagedRunRequest{
		Surface: SurfaceChat, Agent: agent, CLI: mock.name, Cwd: t.TempDir(), Task: "Do it.",
	})
	if err == nil || !strings.Contains(err.Error(), "sandbox") {
		t.Fatalf("err = %v", err)
	}
	if len(mock.shots) != 0 {
		t.Fatal("blocked runtime reached the CLI")
	}
}

func TestRunWorkflowRoutesAutonomousAgentThroughManagedRuntime(t *testing.T) {
	withTempConfigDir(t)
	mock := &mockAdapter{name: "managed-workflow", replies: []string{
		`{"action":"tool","tool":"memory.task","arguments":{"title":"Review","content":"Inspect src"}}`,
		`{"action":"finish","message":"Workflow complete."}`,
	}}
	withMockAdapter(t, mock)
	c, _ := New(Options{Store: openTempStore(t)})
	agent := autonomousTestAgent("managed-workflow-agent", mock.name)
	saveAutonomousRuntime(t, agent.ID)
	res := c.RunWorkflow(context.Background(), RunOptions{
		Agent: agent, WorkflowName: "review", Inputs: map[string]string{"target": "src"},
		CLI: mock.name, Cwd: t.TempDir(), Persist: true,
	})
	if res.Outcome != OutcomeCompleted || len(res.Turns) != 1 || res.Turns[0].Reply.Text != "Workflow complete." {
		t.Fatalf("result=%#v err=%v", res, res.Err)
	}
	msgs, err := c.ListMessages(context.Background(), res.ChatID, 0)
	if err != nil || len(msgs) != 2 || msgs[1].Meta["managed_run_id"] == nil {
		t.Fatalf("messages=%#v err=%v", msgs, err)
	}
}

func TestContinueChatRoutesAutonomousAgentThroughManagedRuntime(t *testing.T) {
	withTempConfigDir(t)
	mock := &mockAdapter{name: "claude", replies: []string{`{"action":"finish","message":"Managed answer."}`}}
	RegisterCLIAdapter(mock)
	t.Cleanup(func() { RegisterCLIAdapter(NewClaudeAdapter()) })
	c, _ := New(Options{Store: openTempStore(t)})
	ctx := context.Background()
	agent := autonomousTestAgent("managed-chat-agent", mock.name)
	if _, err := c.upsertAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	saveAutonomousRuntime(t, agent.ID)
	chat, err := c.StartInteractiveChat(ctx, agent.ID, mock.name, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	turn, err := c.ContinueChat(ctx, chat.ID, "Analyze this.", chat.WorkspacePath, agent.Instructions)
	if err != nil || turn.Reply != "Managed answer." {
		t.Fatalf("turn=%#v err=%v", turn, err)
	}
	msgs, err := c.ListMessages(ctx, chat.ID, 0)
	if err != nil || len(msgs) != 2 || msgs[1].Meta["managed_run_id"] == nil {
		t.Fatalf("messages=%#v err=%v", msgs, err)
	}
}

func TestManagedChatCarriesBoundedConversationHistoryIntoEachRun(t *testing.T) {
	withTempConfigDir(t)
	mock := &mockAdapter{name: "claude", replies: []string{
		`{"action":"finish","message":"First managed answer."}`,
		`{"action":"finish","message":"Second managed answer."}`,
	}}
	RegisterCLIAdapter(mock)
	t.Cleanup(func() { RegisterCLIAdapter(NewClaudeAdapter()) })
	c, _ := New(Options{Store: openTempStore(t)})
	ctx := context.Background()
	agent := autonomousTestAgent("managed-history-agent", mock.name)
	if _, err := c.upsertAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	saveAutonomousRuntime(t, agent.ID)
	chat, err := c.StartInteractiveChat(ctx, agent.ID, mock.name, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ContinueChat(ctx, chat.ID, "First question.", chat.WorkspacePath, agent.Instructions); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ContinueChat(ctx, chat.ID, "Follow up.", chat.WorkspacePath, agent.Instructions); err != nil {
		t.Fatal(err)
	}
	if len(mock.shots) != 2 {
		t.Fatalf("shots = %d", len(mock.shots))
	}
	second := mock.shots[1].Message
	for _, want := range []string{"First question.", "First managed answer.", "Follow up."} {
		if !strings.Contains(second, want) {
			t.Fatalf("second managed task omitted %q: %s", want, second)
		}
	}
}

func TestAgentStudioAuthoringHelperStaysOnNativeExecution(t *testing.T) {
	withTempConfigDir(t)
	mock := &mockAdapter{name: "claude", replies: []string{"Native authoring reply."}}
	RegisterCLIAdapter(mock)
	t.Cleanup(func() { RegisterCLIAdapter(NewClaudeAdapter()) })
	c, _ := New(Options{Store: openTempStore(t)})
	ctx := context.Background()
	agent := autonomousTestAgent("managed-authoring-agent", mock.name)
	if _, err := c.upsertAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	saveAutonomousRuntime(t, agent.ID)
	chat, err := c.CreateChat(ctx, CreateChatRequest{
		Title: "Authoring", AgentID: agent.ID, CLIAgent: mock.name,
		WorkspacePath: t.TempDir(), Settings: ChatSettings{Surface: "agent-helper"},
	})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := c.ContinueChat(ctx, chat.ID, "Refine the YAML.", chat.WorkspacePath, agent.Instructions)
	if err != nil || turn.Reply != "Native authoring reply." {
		t.Fatalf("turn=%#v err=%v", turn, err)
	}
	if len(mock.shots) != 1 || strings.Contains(mock.shots[0].SystemPrompt, "managed single-agent runtime") {
		t.Fatalf("authoring helper used managed protocol: %#v", mock.shots)
	}
}
