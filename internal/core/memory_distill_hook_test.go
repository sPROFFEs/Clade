package core

import (
	"context"
	"testing"
)

// mockDistiller is a programmable Distiller for hook tests.
type mockDistiller struct {
	name   string
	avail  error
	result *DistillResult
	err    error

	calls int
}

func (m *mockDistiller) Name() string                       { return m.name }
func (m *mockDistiller) Available(_ context.Context) error  { return m.avail }
func (m *mockDistiller) Distill(_ context.Context, _ []DistillMessage) (*DistillResult, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

// withMockCLI registers a mock CLIAdapter, runs fn, then unregisters.
func withMockCLI(t *testing.T, name string, replies []string) *mockAdapter {
	t.Helper()
	m := &mockAdapter{name: name, replies: replies}
	withMockAdapter(t, m)
	return m
}

// distillViaCLI builds a CLI-kind endpoint pointing at a registered
// mock adapter. The mock returns one canned JSON reply.
func distillViaCLI(t *testing.T, cliName string, reply string) DistillEndpoint {
	t.Helper()
	withMockCLI(t, cliName, []string{reply})
	return DistillEndpoint{Kind: DistillKindCLI, CLIName: cliName}
}

func TestDistillChat_NoEndpointReturnsZero(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	_ = c.SetMemoryEnabled(ctx, true)

	ch, _ := c.CreateChat(ctx, CreateChatRequest{Title: "x", CLIAgent: "claude"})
	_, _ = c.AddMessage(ctx, ch.ID, "user", "hello", nil)
	_, _ = c.AddMessage(ctx, ch.ID, "assistant", "hi there", nil)

	id, err := c.DistillChat(ctx, ch.ID, nil)
	if err != nil {
		t.Fatalf("DistillChat: %v", err)
	}
	if id != 0 {
		t.Fatalf("expected 0 (no endpoint resolved), got %d", id)
	}
}

func TestDistillChat_MemoryOffSkipsDistillation(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	ch, _ := c.CreateChat(ctx, CreateChatRequest{Title: "x", CLIAgent: "claude"})
	_, _ = c.AddMessage(ctx, ch.ID, "user", "hello", nil)
	_, _ = c.AddMessage(ctx, ch.ID, "assistant", "hi there", nil)

	id, err := c.DistillChat(ctx, ch.ID, nil)
	if err != nil {
		t.Fatalf("DistillChat: %v", err)
	}
	if id != 0 {
		t.Fatalf("expected 0 (memory disabled), got %d", id)
	}
}

func TestDistillChat_PersistsEpisodeAndPinned(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	_ = c.SetMemoryEnabled(ctx, true)

	ch, _ := c.CreateChat(ctx, CreateChatRequest{Title: "real chat", CLIAgent: "claude"})
	_, _ = c.AddMessage(ctx, ch.ID, "user", "Help me ship the release", nil)
	_, _ = c.AddMessage(ctx, ch.ID, "assistant", "We bumped the version, ran the build, produced checksums. Done.", nil)

	reply := `{"summary":"Cut 1.0.1 release","topics":["release","build"],` +
		`"entities":["dist/SHA256SUMS"],"decisions":["use gh release upload --clobber"],` +
		`"actions":["write release notes"],"pinned_candidates":["user is meticulous about checksums"]}`
	ep := distillViaCLI(t, "mockcli", reply)

	id, err := c.DistillChat(ctx, ch.ID, &ep)
	if err != nil {
		t.Fatalf("DistillChat: %v", err)
	}
	if id == 0 {
		t.Fatal("expected episode id, got 0")
	}

	eps, _ := c.ListEpisodes(ctx, 0)
	if len(eps) != 1 || eps[0].Summary != "Cut 1.0.1 release" {
		t.Fatalf("episode not persisted: %+v", eps)
	}

	facts, _ := c.ListPinned(ctx, 0)
	if len(facts) != 1 || facts[0].Text != "user is meticulous about checksums" {
		t.Fatalf("pinned candidate not persisted: %+v", facts)
	}
	if facts[0].Salience != 0.4 {
		t.Fatalf("pinned candidate salience should start at 0.4, got %v", facts[0].Salience)
	}
}

func TestDistillChat_NoMaterialSkips(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	_ = c.SetMemoryEnabled(ctx, true)

	ch, _ := c.CreateChat(ctx, CreateChatRequest{Title: "empty", CLIAgent: "claude"})
	// Only a user turn, no assistant — should skip.
	_, _ = c.AddMessage(ctx, ch.ID, "user", "hi", nil)

	ep := DistillEndpoint{Kind: DistillKindCLI, CLIName: "any"}
	id, err := c.DistillChat(ctx, ch.ID, &ep)
	if err != nil {
		t.Fatalf("DistillChat: %v", err)
	}
	if id != 0 {
		t.Fatalf("expected skip (no assistant turn), got id %d", id)
	}
}

func TestDistillChat_ChatOverrideEnables(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	// Memory off globally.

	ch, _ := c.CreateChat(ctx, CreateChatRequest{Title: "force-on", CLIAgent: "claude"})
	on := true
	ch.Settings.MemoryOverride = &on
	ch.Settings.DistillEndpoint = nil // still nil = no endpoint resolves
	// Manually re-save the override since CreateChat just used defaults.
	settingsJSON := `{"memory_override":true}`
	_, _ = c.store.DB().ExecContext(ctx,
		`UPDATE chats SET settings_json = ? WHERE id = ?`, settingsJSON, ch.ID)
	_, _ = c.AddMessage(ctx, ch.ID, "user", "hi", nil)
	_, _ = c.AddMessage(ctx, ch.ID, "assistant", "hi back this is content of meaningful length", nil)

	reply := `{"summary":"chat happened","topics":[],"entities":[],"decisions":[],"actions":[],"pinned_candidates":[]}`
	ep := distillViaCLI(t, "mockcli", reply)
	id, err := c.DistillChat(ctx, ch.ID, &ep)
	if err != nil {
		t.Fatalf("DistillChat: %v", err)
	}
	if id == 0 {
		t.Fatal("expected episode despite memory disabled globally (chat override should win)")
	}
}

func TestHasMaterial(t *testing.T) {
	cases := []struct {
		name string
		ms   []DistillMessage
		want bool
	}{
		{"empty", nil, false},
		{"user only", []DistillMessage{{Role: "user", Content: "hello"}}, false},
		{"assistant short", []DistillMessage{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "ok"},
		}, false},
		{"assistant long enough", []DistillMessage{
			{Role: "user", Content: "explain the deploy flow please i need details"},
			{Role: "assistant", Content: "here is the deploy flow"},
		}, true},
	}
	for _, tc := range cases {
		if got := hasMaterial(tc.ms); got != tc.want {
			t.Errorf("%s: hasMaterial = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRunWorkflow_PersistOptionSavesChat(t *testing.T) {
	mock := &mockAdapter{name: "mockcli", replies: []string{"reply text content"}}
	withMockAdapter(t, mock)

	c, _ := New(Options{Store: openTempStore(t)})
	a := &Agent{
		ID: "p", Name: "P", Instructions: "x", Supports: []string{"mockcli"},
		Workflows: []Workflow{{
			Name: "go",
			Steps: []WorkflowStep{
				{Kind: StepUserMessage, Template: "hello workflow"},
			},
		}},
	}
	res := c.RunWorkflow(context.Background(), RunOptions{
		Agent: a, WorkflowName: "go", CLI: "mockcli", Cwd: "/tmp", Persist: true,
	})
	if res.Outcome != OutcomeCompleted {
		t.Fatalf("outcome %s err %v", res.Outcome, res.Err)
	}
	if res.ChatID == "" {
		t.Fatal("expected ChatID populated when Persist=true")
	}

	chat, err := c.GetChat(context.Background(), res.ChatID)
	if err != nil {
		t.Fatalf("GetChat: %v", err)
	}
	if chat.ExitKind != string(OutcomeCompleted) {
		t.Fatalf("EndChat didn't run: %+v", chat)
	}

	msgs, _ := c.ListMessages(context.Background(), res.ChatID, 0)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (user+assistant), got %d", len(msgs))
	}
}

func TestRunWorkflow_MemoryInjectionRidesFirstTurn(t *testing.T) {
	mock := &mockAdapter{name: "mockcli", resumable: true, replies: []string{"r1", "r2"}}
	withMockAdapter(t, mock)

	c, _ := New(Options{Store: openTempStore(t)})
	a := &Agent{
		ID: "p", Name: "P", Instructions: "x", Supports: []string{"mockcli"},
		Workflows: []Workflow{{
			Name: "go",
			Steps: []WorkflowStep{
				{Kind: StepUserMessage, Template: "one"},
				{Kind: StepUserMessage, Template: "two"},
			},
		}},
	}
	_ = c.RunWorkflow(context.Background(), RunOptions{
		Agent: a, WorkflowName: "go", CLI: "mockcli", Cwd: "/tmp",
		MemoryInjection: "[Context block]",
	})
	if len(mock.shots) != 1 {
		t.Fatalf("expected 1 SingleShot, got %d", len(mock.shots))
	}
	if mock.shots[0].Message == "one" {
		t.Fatal("injection didn't ride first turn")
	}
	if len(mock.resumes) != 1 || mock.resumes[0].Message != "two" {
		t.Fatalf("second turn should NOT include injection: %+v", mock.resumes)
	}
}
