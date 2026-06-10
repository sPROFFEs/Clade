package core

import (
	"context"
	"strings"
	"testing"
)

func TestStartAndContinueChat_ResumesSession(t *testing.T) {
	mock := &mockAdapter{name: "claude", resumable: true, replies: []string{"hi there", "still here"}}
	withMockAdapter(t, mock)

	c, _ := New(Options{Store: openTempStore(t)})
	ctx := context.Background()
	if _, err := c.upsertAgent(ctx, &Agent{
		ID: "dev", Name: "Dev", Instructions: "be helpful", Supports: []string{"claude"},
		Workflows: []Workflow{{Name: "w", Steps: []WorkflowStep{{Kind: StepUserMessage, Template: "x"}}}},
	}); err != nil {
		t.Fatal(err)
	}

	chat, err := c.StartInteractiveChat(ctx, "dev", "claude", "/tmp")
	if err != nil {
		t.Fatalf("StartInteractiveChat: %v", err)
	}

	// First turn → SingleShot (no session yet), system prompt forwarded.
	t1, err := c.ContinueChat(ctx, chat.ID, "hello", "/tmp", "be helpful")
	if err != nil {
		t.Fatalf("ContinueChat 1: %v", err)
	}
	if t1.Reply != "hi there" {
		t.Fatalf("turn1 reply = %q", t1.Reply)
	}
	if len(mock.shots) != 1 || mock.shots[0].SystemPrompt != "be helpful" {
		t.Fatalf("first turn should be SingleShot with system prompt: shots=%d", len(mock.shots))
	}

	// Session id should now be stored on the chat.
	reloaded, _ := c.GetChat(ctx, chat.ID)
	if reloaded.SessionID == "" {
		t.Fatal("session id not persisted after first turn")
	}

	// Second turn → Resume.
	t2, err := c.ContinueChat(ctx, chat.ID, "again", "/tmp", "be helpful")
	if err != nil {
		t.Fatalf("ContinueChat 2: %v", err)
	}
	if t2.Reply != "still here" {
		t.Fatalf("turn2 reply = %q", t2.Reply)
	}
	if len(mock.resumes) != 1 {
		t.Fatalf("second turn should Resume, got %d resumes", len(mock.resumes))
	}

	// Four messages persisted: user/assistant × 2.
	msgs, _ := c.ListMessages(ctx, chat.ID, 0)
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
}

func TestContinueChat_RedactsOutboundRevealsReply(t *testing.T) {
	const secret = "sk-abcdefghijklmnopqrstuvwxyzz"
	// The mock echoes whatever placeholder it received back in the reply.
	mock := &mockAdapter{name: "claude", replies: []string{"you sent [OPENAI_KEY_1]"}}
	withMockAdapter(t, mock)

	c, _ := New(Options{Store: openTempStore(t)})
	ctx := context.Background()
	_, _ = c.upsertAgent(ctx, &Agent{
		ID: "dev", Name: "Dev", Instructions: "x", Supports: []string{"claude"},
		Workflows: []Workflow{{Name: "w", Steps: []WorkflowStep{{Kind: StepUserMessage, Template: "x"}}}},
	})
	chat, _ := c.StartInteractiveChat(ctx, "dev", "claude", "/tmp")

	turn, err := c.ContinueChat(ctx, chat.ID, "key is "+secret, "/tmp", "")
	if err != nil {
		t.Fatalf("ContinueChat: %v", err)
	}
	// Adapter saw the redacted placeholder, not the secret.
	if strings.Contains(mock.shots[0].Message, secret) {
		t.Fatalf("secret leaked to adapter: %q", mock.shots[0].Message)
	}
	if !strings.Contains(mock.shots[0].Message, "[OPENAI_KEY_1]") {
		t.Fatalf("expected placeholder in adapter message: %q", mock.shots[0].Message)
	}
	// Reply revealed back to the user's original secret.
	if !strings.Contains(turn.Reply, secret) {
		t.Fatalf("reply should reveal the secret: %q", turn.Reply)
	}
}
