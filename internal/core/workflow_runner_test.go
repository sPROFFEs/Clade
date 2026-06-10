package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// mockAdapter records every call and replies with canned text. Used
// here and in any future tests that need a programmable CLI.
type mockAdapter struct {
	name      string
	replies   []string // queue; consumed in order
	resumable bool
	avail     error

	// Recorded calls — inspect in assertions.
	shots   []SingleShotOpts
	resumes []struct {
		SessionID string
		Message   string
	}
	failOnTurn int // 1-based; 0 = never fail
	turnIdx    int
}

func (m *mockAdapter) Name() string                              { return m.name }
func (m *mockAdapter) Available(_ context.Context) error         { return m.avail }
func (m *mockAdapter) SupportsResume() bool                      { return m.resumable }

func (m *mockAdapter) SingleShot(_ context.Context, opts SingleShotOpts) (*Reply, error) {
	m.shots = append(m.shots, opts)
	return m.nextReply()
}

func (m *mockAdapter) Resume(_ context.Context, sid, msg string) (*Reply, error) {
	m.resumes = append(m.resumes, struct {
		SessionID string
		Message   string
	}{sid, msg})
	return m.nextReply()
}

func (m *mockAdapter) nextReply() (*Reply, error) {
	m.turnIdx++
	if m.failOnTurn > 0 && m.turnIdx == m.failOnTurn {
		return nil, errors.New("mock: synthetic failure")
	}
	if len(m.replies) == 0 {
		return &Reply{Text: "(empty)", SessionID: fmt.Sprintf("sess-%d", m.turnIdx)}, nil
	}
	r := m.replies[0]
	m.replies = m.replies[1:]
	return &Reply{Text: r, SessionID: fmt.Sprintf("sess-%d", m.turnIdx)}, nil
}

func withMockAdapter(t *testing.T, m *mockAdapter) {
	t.Helper()
	RegisterCLIAdapter(m)
	t.Cleanup(func() { UnregisterCLIAdapter(m.name) })
}

func TestRunWorkflow_SingleStep_UsesSingleShot(t *testing.T) {
	mock := &mockAdapter{name: "mockcli", replies: []string{"hello"}}
	withMockAdapter(t, mock)

	c, _ := New(Options{Store: openTempStore(t)})
	a := &Agent{
		ID: "a", Name: "A", Instructions: "be terse", Supports: []string{"mockcli"},
		Workflows: []Workflow{{
			Name: "go",
			Inputs: []WorkflowInput{{Name: "x", Required: true}},
			Steps: []WorkflowStep{{Kind: StepUserMessage, Template: "x={{ .x }}"}},
		}},
	}
	res := c.RunWorkflow(context.Background(), RunOptions{
		Agent: a, WorkflowName: "go", Inputs: map[string]string{"x": "1"},
		CLI: "mockcli", Cwd: "/tmp",
	})
	if res.Outcome != OutcomeCompleted {
		t.Fatalf("outcome=%s err=%v", res.Outcome, res.Err)
	}
	if len(mock.shots) != 1 || len(mock.resumes) != 0 {
		t.Fatalf("expected 1 shot 0 resumes, got %d shots / %d resumes", len(mock.shots), len(mock.resumes))
	}
	if mock.shots[0].Message != "x=1" {
		t.Fatalf("unexpected rendered body: %q", mock.shots[0].Message)
	}
	if mock.shots[0].SystemPrompt != "be terse" {
		t.Fatalf("system prompt not forwarded: %q", mock.shots[0].SystemPrompt)
	}
}

func TestRunWorkflow_MultiStep_PrefersResumeWhenSupported(t *testing.T) {
	mock := &mockAdapter{name: "mockcli", resumable: true, replies: []string{"r1", "r2", "r3"}}
	withMockAdapter(t, mock)

	c, _ := New(Options{Store: openTempStore(t)})
	a := &Agent{
		ID: "a", Name: "A", Instructions: "x", Supports: []string{"mockcli"},
		Workflows: []Workflow{{
			Name: "multi",
			Steps: []WorkflowStep{
				{Kind: StepUserMessage, Template: "one"},
				{Kind: StepWaitForAssistant}, // observed but no-op
				{Kind: StepUserMessage, Template: "two"},
				{Kind: StepUserMessage, Template: "three"},
			},
		}},
	}
	res := c.RunWorkflow(context.Background(), RunOptions{
		Agent: a, WorkflowName: "multi", CLI: "mockcli", Cwd: "/tmp",
	})
	if res.Outcome != OutcomeCompleted {
		t.Fatalf("outcome=%s err=%v", res.Outcome, res.Err)
	}
	if len(mock.shots) != 1 {
		t.Fatalf("expected 1 SingleShot (turn 0), got %d", len(mock.shots))
	}
	if len(mock.resumes) != 2 {
		t.Fatalf("expected 2 Resumes (turns 1+2), got %d", len(mock.resumes))
	}
	if mock.resumes[0].Message != "two" || mock.resumes[1].Message != "three" {
		t.Fatalf("resume messages out of order: %+v", mock.resumes)
	}
}

func TestRunWorkflow_MultiStep_FallsBackToSingleShotWhenNoResume(t *testing.T) {
	mock := &mockAdapter{name: "mockcli", resumable: false, replies: []string{"r1", "r2"}}
	withMockAdapter(t, mock)

	c, _ := New(Options{Store: openTempStore(t)})
	a := &Agent{
		ID: "a", Name: "A", Instructions: "x", Supports: []string{"mockcli"},
		Workflows: []Workflow{{
			Name: "multi",
			Steps: []WorkflowStep{
				{Kind: StepUserMessage, Template: "one"},
				{Kind: StepUserMessage, Template: "two"},
			},
		}},
	}
	res := c.RunWorkflow(context.Background(), RunOptions{
		Agent: a, WorkflowName: "multi", CLI: "mockcli", Cwd: "/tmp",
	})
	if res.Outcome != OutcomeCompleted {
		t.Fatalf("outcome=%s err=%v", res.Outcome, res.Err)
	}
	if len(mock.shots) != 2 || len(mock.resumes) != 0 {
		t.Fatalf("expected 2 SingleShots / 0 Resumes, got %d/%d", len(mock.shots), len(mock.resumes))
	}
}

func TestRunWorkflow_RejectsUnsupportedCLI(t *testing.T) {
	c, _ := New(Options{Store: openTempStore(t)})
	a := &Agent{ID: "a", Name: "A", Instructions: "x", Supports: []string{"claude"},
		Workflows: []Workflow{{Name: "w", Steps: []WorkflowStep{{Kind: StepUserMessage, Template: "x"}}}}}
	res := c.RunWorkflow(context.Background(), RunOptions{
		Agent: a, WorkflowName: "w", CLI: "codex", Cwd: "/tmp",
	})
	if res.Err == nil || !strings.Contains(res.Err.Error(), "does not support") {
		t.Fatalf("expected unsupported-CLI error, got: %v", res.Err)
	}
}

func TestRunWorkflow_PropagatesAdapterError(t *testing.T) {
	mock := &mockAdapter{name: "mockcli", failOnTurn: 1}
	withMockAdapter(t, mock)

	c, _ := New(Options{Store: openTempStore(t)})
	a := &Agent{ID: "a", Name: "A", Instructions: "x", Supports: []string{"mockcli"},
		Workflows: []Workflow{{Name: "w", Steps: []WorkflowStep{{Kind: StepUserMessage, Template: "x"}}}}}
	res := c.RunWorkflow(context.Background(), RunOptions{
		Agent: a, WorkflowName: "w", CLI: "mockcli", Cwd: "/tmp",
	})
	if res.Outcome != OutcomeAdapterErr {
		t.Fatalf("expected OutcomeAdapterErr, got %s", res.Outcome)
	}
}

func TestRunWorkflow_OnTurnCallback(t *testing.T) {
	mock := &mockAdapter{name: "mockcli", resumable: true, replies: []string{"r1", "r2"}}
	withMockAdapter(t, mock)

	c, _ := New(Options{Store: openTempStore(t)})
	a := &Agent{ID: "a", Name: "A", Instructions: "x", Supports: []string{"mockcli"},
		Workflows: []Workflow{{Name: "w", Steps: []WorkflowStep{
			{Kind: StepUserMessage, Template: "one"},
			{Kind: StepUserMessage, Template: "two"},
		}}}}
	var seen []string
	c.RunWorkflow(context.Background(), RunOptions{
		Agent: a, WorkflowName: "w", CLI: "mockcli", Cwd: "/tmp",
		OnTurn: func(tr TurnResult) { seen = append(seen, tr.Reply.Text) },
	})
	if len(seen) != 2 || seen[0] != "r1" || seen[1] != "r2" {
		t.Fatalf("OnTurn order/content wrong: %v", seen)
	}
}

func TestParseClaudeJSON_HandlesSingleEnvelope(t *testing.T) {
	body := []byte(`{"type":"result","session_id":"sess-xyz","result":"hello\n","is_error":false}`)
	r, err := parseClaudeJSON(body)
	if err != nil {
		t.Fatalf("parseClaudeJSON: %v", err)
	}
	if r.SessionID != "sess-xyz" {
		t.Fatalf("session id: %q", r.SessionID)
	}
	if r.Text != "hello" {
		t.Fatalf("text: %q", r.Text)
	}
}

func TestParseClaudeJSON_HandlesMultilineNDJSON(t *testing.T) {
	body := []byte(`{"type":"system","subtype":"init"}
{"type":"result","session_id":"sess-multi","result":"final","is_error":false}
`)
	r, err := parseClaudeJSON(body)
	if err != nil {
		t.Fatalf("parseClaudeJSON: %v", err)
	}
	if r.SessionID != "sess-multi" || r.Text != "final" {
		t.Fatalf("wrong envelope chosen: %+v", r)
	}
}
