package core

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCreateChat_AutoSlugAndDefaults(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()

	ch, err := c.CreateChat(ctx, CreateChatRequest{
		Title:    "Refactor the Launcher!",
		CLIAgent: "claude",
	})
	if err != nil {
		t.Fatalf("CreateChat: %v", err)
	}
	if ch.ID == "" || !strings.Contains(ch.ID, "refactor-the-launcher") {
		t.Fatalf("expected slug-ish id, got %q", ch.ID)
	}
	if ch.Title != "Refactor the Launcher!" {
		t.Fatalf("title lost: %q", ch.Title)
	}
	if ch.EndedAt != nil {
		t.Fatal("EndedAt should be nil for a new chat")
	}
}

func TestCreateChat_RejectsMissingCLIAgent(t *testing.T) {
	c := newMemCore(t)
	if _, err := c.CreateChat(context.Background(), CreateChatRequest{Title: "x"}); err == nil {
		t.Fatal("expected CLIAgent-required error")
	}
}

func TestAddMessage_BumpsChatUpdatedAt(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	ch, _ := c.CreateChat(ctx, CreateChatRequest{Title: "msg test", CLIAgent: "claude"})

	_, err := c.AddMessage(ctx, ch.ID, "user", "hi", nil)
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	_, _ = c.AddMessage(ctx, ch.ID, "assistant", "hello", nil)

	msgs, _ := c.ListMessages(ctx, ch.ID, 0)
	if len(msgs) != 2 || msgs[0].Role != "user" || msgs[1].Role != "assistant" {
		t.Fatalf("messages out of order: %+v", msgs)
	}
}

func TestEndChat_StampsExitKind(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	ch, _ := c.CreateChat(ctx, CreateChatRequest{Title: "x", CLIAgent: "claude"})
	if err := c.EndChat(ctx, ch.ID, string(OutcomeCompleted)); err != nil {
		t.Fatalf("EndChat: %v", err)
	}
	got, _ := c.GetChat(ctx, ch.ID)
	if got.EndedAt == nil || got.ExitKind != string(OutcomeCompleted) {
		t.Fatalf("EndChat didn't stamp: %+v", got)
	}
}

func TestEndChat_NotFound(t *testing.T) {
	c := newMemCore(t)
	err := c.EndChat(context.Background(), "ghost", "x")
	if !errors.Is(err, ErrChatNotFound) {
		t.Fatalf("expected ErrChatNotFound, got %v", err)
	}
}

func TestDeleteChat_CascadesMessages(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	ch, _ := c.CreateChat(ctx, CreateChatRequest{Title: "del", CLIAgent: "claude"})
	_, _ = c.AddMessage(ctx, ch.ID, "user", "first", nil)
	_, _ = c.AddMessage(ctx, ch.ID, "assistant", "second", nil)

	if err := c.DeleteChat(ctx, ch.ID); err != nil {
		t.Fatalf("DeleteChat: %v", err)
	}
	msgs, _ := c.ListMessages(ctx, ch.ID, 0)
	if len(msgs) != 0 {
		t.Fatalf("messages survived chat deletion: %+v", msgs)
	}
}

func TestListChats_NewestFirst(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	a, _ := c.CreateChat(ctx, CreateChatRequest{Title: "alpha", CLIAgent: "claude"})
	_, _ = c.CreateChat(ctx, CreateChatRequest{Title: "beta", CLIAgent: "claude"})
	_, _ = c.CreateChat(ctx, CreateChatRequest{Title: "gamma", CLIAgent: "claude"})

	// Touch alpha last so it bubbles to the top via updated_at.
	_, _ = c.AddMessage(ctx, a.ID, "user", "ping", nil)

	chats, err := c.ListChats(ctx, 0)
	if err != nil {
		t.Fatalf("ListChats: %v", err)
	}
	if len(chats) != 3 {
		t.Fatalf("want 3, got %d", len(chats))
	}
	if chats[0].Title != "alpha" {
		t.Fatalf("expected alpha first (most-recent activity), got %q", chats[0].Title)
	}
}
