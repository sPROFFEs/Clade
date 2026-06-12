package main

// Streaming chat bindings — the GUI-side half of the desktop-app feel:
// SendChatStream forwards core StreamEvents over the Wails event bus as
// the turn runs (text deltas, tool activity), and CancelChatTurn is the
// Stop button. CLIs without an event stream silently degrade to the
// buffered behavior — the promise just resolves with the full reply and
// no events fire first.

import (
	"context"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/sPROFFEs/PrAImate/internal/core"
)

// ChatStreamEvent is what the frontend receives on the
// "praimate:chat-stream" event channel.
type ChatStreamEvent struct {
	ChatID string `json:"chatId"`
	Type   string `json:"type"` // "text" | "tool_start" | "tool_end"
	Text   string `json:"text,omitempty"`
	Tool   string `json:"tool,omitempty"`
	Detail string `json:"detail,omitempty"`
	ID     string `json:"id,omitempty"`
	OK     bool   `json:"ok,omitempty"`
}

// SendChatStream sends one message (with optional staged attachment
// paths) and streams progress events while the CLI runs. The returned
// turn is the persisted final state — after the promise resolves the
// frontend reloads messages from the DB exactly like the non-streaming
// path.
func (a *App) SendChatStream(chatID, message string, attachments []string) (*core.ChatTurn, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	chat, err := c.GetChat(a.ctx, chatID)
	if err != nil {
		return nil, err
	}
	systemPrompt := ""
	if chat.AgentID != "" {
		if agent, err := c.GetAgent(a.ctx, chat.AgentID); err == nil {
			systemPrompt = agent.Instructions
		}
	}

	ctx, cancel := context.WithCancel(a.ctx)
	a.chatCancelMu.Lock()
	if old, ok := a.chatCancels[chatID]; ok {
		old() // one in-flight turn per chat; a racing second send wins
	}
	a.chatCancels[chatID] = cancel
	a.chatCancelMu.Unlock()
	defer func() {
		cancel()
		a.chatCancelMu.Lock()
		delete(a.chatCancels, chatID)
		a.chatCancelMu.Unlock()
	}()

	onEvent := func(ev core.StreamEvent) {
		wruntime.EventsEmit(a.ctx, "praimate:chat-stream", ChatStreamEvent{
			ChatID: chatID,
			Type:   ev.Type,
			Text:   ev.Text,
			Tool:   ev.Tool,
			Detail: ev.Detail,
			ID:     ev.ID,
			OK:     ev.OK,
		})
	}
	return c.ContinueChatStream(ctx, chatID, message, chat.WorkspacePath, systemPrompt, attachments, onEvent)
}

// CancelChatTurn interrupts the chat's in-flight streamed turn. No-op
// when nothing is running. The interrupted turn persists whatever text
// already streamed (flagged in the message meta).
func (a *App) CancelChatTurn(chatID string) {
	a.chatCancelMu.Lock()
	cancel, ok := a.chatCancels[chatID]
	a.chatCancelMu.Unlock()
	if ok {
		cancel()
	}
}
