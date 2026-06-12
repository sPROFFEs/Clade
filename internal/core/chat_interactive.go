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
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

// UpdateChatSettings mutates a chat's persisted settings through fn and
// writes them back. Used by the GUI's per-chat model/tools controls.
func (c *Core) UpdateChatSettings(ctx context.Context, chatID string, fn func(*ChatSettings)) error {
	if c.store == nil {
		return errors.New("UpdateChatSettings: no store configured")
	}
	chat, err := c.GetChat(ctx, chatID)
	if err != nil {
		return err
	}
	fn(&chat.Settings)
	raw, err := json.Marshal(chat.Settings)
	if err != nil {
		return fmt.Errorf("UpdateChatSettings: marshal: %w", err)
	}
	_, err = c.store.DB().ExecContext(ctx,
		`UPDATE chats SET settings_json = ?, updated_at = ? WHERE id = ?`,
		string(raw), time.Now().UTC().Format(time.RFC3339Nano), chatID)
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
	return c.ContinueChatWithAttachments(ctx, chatID, userMessage, cwd, systemPrompt, nil)
}

// ContinueChatWithAttachments is ContinueChat plus file ingestion:
// attachments is a list of absolute paths (images, PDFs, docs, …) the
// user attached to this turn. The paths are appended to the outbound
// message so file-tool-capable CLIs (claude, codex, gemini, opencode)
// read them from disk; the stored user message records them in Meta so
// the GUI can render previews.
func (c *Core) ContinueChatWithAttachments(ctx context.Context, chatID, userMessage, cwd, systemPrompt string, attachments []string) (*ChatTurn, error) {
	return c.ContinueChatStream(ctx, chatID, userMessage, cwd, systemPrompt, attachments, nil)
}

// ContinueChatStream is the live variant: when onEvent is non-nil and
// the chat's CLI supports streaming (claude/openclaude stream-json,
// codex exec --json), text deltas and tool-call activity are emitted as
// they happen and the turn can be interrupted by cancelling ctx — an
// interrupted turn persists whatever text already streamed (flagged
// interrupted in the message meta) instead of erroring. CLIs without a
// stream fall back to the buffered path; onEvent then just never fires
// before the final reply.
//
// Note on privacy: streamed deltas show the REDACTED outbound form;
// placeholder reveal happens only on the persisted final message (same
// trade Claude/Codex desktop make — you watch the wire format live).
func (c *Core) ContinueChatStream(ctx context.Context, chatID, userMessage, cwd, systemPrompt string, attachments []string, onEvent StreamHandler) (*ChatTurn, error) {
	if c.store == nil {
		return nil, errors.New("ContinueChat: no store configured")
	}
	if userMessage == "" && len(attachments) == 0 {
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
	if len(attachments) > 0 {
		var b strings.Builder
		b.WriteString(outbound)
		b.WriteString("\n\nThe user attached these files — read them from disk with your file tools:\n")
		for _, p := range attachments {
			b.WriteString("- " + p + "\n")
		}
		outbound = b.String()
	}

	var meta map[string]any
	if len(attachments) > 0 {
		meta = map[string]any{"attachments": attachments}
	}
	if _, err := c.AddMessage(ctx, chatID, "user", userMessage, meta); err != nil {
		return nil, fmt.Errorf("persist user message: %w", err)
	}

	// Wrap the caller's handler to also collect a compact activity log
	// that persists on the assistant message, so reopened chats still
	// show what the agent did.
	var activity []map[string]any
	collect := onEvent
	if onEvent != nil {
		collect = func(ev StreamEvent) {
			switch ev.Type {
			case "tool_start":
				if len(activity) < 100 {
					activity = append(activity, map[string]any{"tool": ev.Tool, "detail": ev.Detail, "ok": true})
				}
			case "tool_end":
				if !ev.OK {
					for i := len(activity) - 1; i >= 0; i-- {
						if activity[i]["ok"] == true {
							activity[i]["ok"] = false
							break
						}
					}
				}
			}
			onEvent(ev)
		}
	}

	// Ask level: wire the approval shim when a provider is registered
	// (GUI). Without one — TUI, tests — "ask" degrades to the safe
	// default inside the adapters.
	var approval *ApprovalConfig
	if chat.Settings.Tools == "ask" && c.approvalProvider != nil {
		approval = c.approvalProvider(chatID)
	}
	resumeOpts := ResumeOpts{Message: outbound, Model: chat.Settings.Model, Tools: chat.Settings.Tools, Approval: approval}
	shotOpts := SingleShotOpts{
		Cwd:          cwd,
		Message:      outbound,
		SystemPrompt: privacyRedactPlain(privacy, systemPrompt),
		Model:        chat.Settings.Model,
		Tools:        chat.Settings.Tools,
		Approval:     approval,
	}
	resuming := chat.SessionID != "" && adapter.SupportsResume()

	start := time.Now()
	var reply *Reply
	streamed := false
	if sc, ok := adapter.(streamingAdapter); ok && collect != nil {
		if resuming {
			reply, err = sc.ResumeStream(ctx, chat.SessionID, resumeOpts, collect)
		} else {
			reply, err = sc.SingleShotStream(ctx, shotOpts, collect)
		}
		if errors.Is(err, ErrStreamUnsupported) {
			reply, err = nil, nil
		} else {
			streamed = true
		}
	}
	if !streamed {
		if resuming {
			reply, err = adapter.Resume(ctx, chat.SessionID, resumeOpts)
		} else {
			reply, err = adapter.SingleShot(ctx, shotOpts)
		}
	}

	var assistantMeta map[string]any
	if len(activity) > 0 {
		assistantMeta = map[string]any{"activity": activity}
	}

	// Interrupted turn: keep whatever streamed instead of erroring —
	// that's the desktop-app stop-button contract.
	if err != nil && errors.Is(err, context.Canceled) && reply != nil && reply.Text != "" {
		if assistantMeta == nil {
			assistantMeta = map[string]any{}
		}
		assistantMeta["interrupted"] = true
		revealed := privacy.Reveal(reply.Text, matches)
		// ctx is dead — persist with a fresh background context.
		pctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, perr := c.AddMessage(pctx, chatID, "assistant", revealed, assistantMeta); perr != nil {
			return nil, fmt.Errorf("persist interrupted reply: %w", perr)
		}
		return &ChatTurn{
			UserMessage: userMessage,
			Reply:       revealed,
			SessionID:   reply.SessionID,
			DurationMs:  time.Since(start).Milliseconds(),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", adapter.Name(), err)
	}
	if reply.ExitCode != 0 {
		return nil, fmt.Errorf("%s exited with code %d", adapter.Name(), reply.ExitCode)
	}

	revealed := privacy.Reveal(reply.Text, matches)
	if _, err := c.AddMessage(ctx, chatID, "assistant", revealed, assistantMeta); err != nil {
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

// StartCleanChat creates a DB-backed chat bound to a CLI only — no
// PrAImate agent, no system prompt. model, if non-empty, pins the CLI's
// model for every turn (see ChatSettings.Model for per-CLI semantics).
// Use this from the GUI "new chat" affordance when the user wants a
// plain conversation with the CLI's model rather than an agent persona.
func (c *Core) StartCleanChat(ctx context.Context, cli, model, cwd string) (*Chat, error) {
	if c.store == nil {
		return nil, errors.New("StartCleanChat: no store configured")
	}
	if cli == "" {
		return nil, errors.New("StartCleanChat: cli required")
	}
	if _, err := GetCLIAdapter(cli); err != nil {
		return nil, err
	}
	title := cli
	if model != "" {
		title += " · " + model
	}
	title += " · " + time.Now().Format("Jan 2 15:04")
	return c.CreateChat(ctx, CreateChatRequest{
		Title:         title,
		CLIAgent:      cli,
		WorkspacePath: cwd,
		Settings:      ChatSettings{Model: model},
	})
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
