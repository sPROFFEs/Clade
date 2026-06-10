package core

// Interactive chat — send a follow-up message into an existing chat and
// get the assistant's reply, resuming the underlying CLI session so the
// conversation has continuity.
//
// This is the conversational counterpart to RunWorkflow's scripted
// turns. Each call is one round-trip: persist the user message, drive
// the CLI (Resume if we have a session id, else SingleShot), persist
// the reply, and remember the new session id on the chat row.
//
// Privacy redaction applies on the way out and is revealed on the way
// back, matching RunWorkflow.

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ChatTurn is one completed interactive exchange.
type ChatTurn struct {
	UserMessage string
	Reply       string
	SessionID   string
	DurationMs  int64
}

// SetChatSessionID persists the adapter session id on a chat row so the
// next ContinueChat / interactive resume picks up where this one left
// off. Exposed so the workflow runner can stamp it after a persisted
// run, making any persisted chat continuable.
func (c *Core) SetChatSessionID(ctx context.Context, chatID, sessionID string) error {
	if c.store == nil {
		return errors.New("SetChatSessionID: no store configured")
	}
	_, err := c.store.DB().ExecContext(ctx,
		`UPDATE chats SET session_id = ? WHERE id = ?`, sessionID, chatID)
	return err
}

// ContinueChat sends userMessage to the chat's CLI and returns the
// reply. cwd is the working directory the CLI runs in (use the chat's
// workspace_path, or the app cwd). The first call on a fresh chat
// starts a new session; subsequent calls resume it.
//
// systemPrompt is sent only on the FIRST turn of a brand-new session
// (when the chat has no session id yet) so the agent's instructions
// frame the conversation without being repeated every turn.
func (c *Core) ContinueChat(ctx context.Context, chatID, userMessage, cwd, systemPrompt string) (*ChatTurn, error) {
	if c.store == nil {
		return nil, errors.New("ContinueChat: no store configured")
	}
	if userMessage == "" {
		return nil, errors.New("ContinueChat: empty message")
	}
	chat, err := c.GetChat(ctx, chatID)
	if err != nil {
		return nil, err
	}
	adapter, err := GetCLIAdapter(chat.CLIAgent)
	if err != nil {
		return nil, err
	}
	if cwd == "" {
		cwd = chat.WorkspacePath
	}
	if cwd == "" {
		cwd = "."
	}

	// Redact outbound; the stored user message keeps the original text.
	privacy := c.PrivacyScanner()
	outbound, matches := privacy.Redact(userMessage)

	if _, err := c.AddMessage(ctx, chatID, "user", userMessage, nil); err != nil {
		return nil, fmt.Errorf("persist user message: %w", err)
	}

	start := time.Now()
	var reply *Reply
	if chat.SessionID != "" && adapter.SupportsResume() {
		reply, err = adapter.Resume(ctx, chat.SessionID, outbound)
	} else {
		reply, err = adapter.SingleShot(ctx, SingleShotOpts{
			Cwd:          cwd,
			Message:      outbound,
			SystemPrompt: privacyRedactPlain(privacy, systemPrompt),
		})
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", adapter.Name(), err)
	}
	if reply.ExitCode != 0 {
		return nil, fmt.Errorf("%s exited with code %d", adapter.Name(), reply.ExitCode)
	}

	revealed := privacy.Reveal(reply.Text, matches)
	if _, err := c.AddMessage(ctx, chatID, "assistant", revealed, nil); err != nil {
		return nil, fmt.Errorf("persist reply: %w", err)
	}
	if reply.SessionID != "" && reply.SessionID != chat.SessionID {
		_ = c.SetChatSessionID(ctx, chatID, reply.SessionID)
	}

	return &ChatTurn{
		UserMessage: userMessage,
		Reply:       revealed,
		SessionID:   reply.SessionID,
		DurationMs:  time.Since(start).Milliseconds(),
	}, nil
}

// privacyRedactPlain redacts text and discards the match set — used for
// the system prompt, which we never need to reveal.
func privacyRedactPlain(p *PrivacyScanner, text string) string {
	if text == "" {
		return ""
	}
	out, _ := p.Redact(text)
	return out
}

// StartInteractiveChat creates a DB-backed chat for an agent and returns
// it ready for ContinueChat. It does NOT send a first message — the
// caller drives the conversation. Use this from the GUI "new chat with
// agent" affordance.
func (c *Core) StartInteractiveChat(ctx context.Context, agentID, cli, cwd string) (*Chat, error) {
	if c.store == nil {
		return nil, errors.New("StartInteractiveChat: no store configured")
	}
	agent, err := c.GetAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if cli == "" {
		if len(agent.Supports) == 0 {
			return nil, fmt.Errorf("agent %q supports no CLI", agentID)
		}
		cli = agent.Supports[0]
	}
	if !contains(agent.Supports, cli) {
		return nil, fmt.Errorf("agent %q does not support CLI %q", agentID, cli)
	}
	title := agent.Name + " · " + time.Now().Format("Jan 2 15:04")
	return c.CreateChat(ctx, CreateChatRequest{
		Title:         title,
		AgentID:       agentID,
		CLIAgent:      cli,
		WorkspacePath: cwd,
	})
}
