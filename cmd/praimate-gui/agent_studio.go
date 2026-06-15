package main

// Agent authoring studio — the right-pane AI assistant is an ephemeral
// "agent-helper" chat: it helps the user write/refine an agent but is NOT
// part of the agent and is deleted when the studio closes. Its CLI/model
// are freely switchable (UpdateChatConfig) and never persisted onto the
// agent. Tagged surface="agent-helper" so it stays out of the Chats list.

import (
	"os"

	"github.com/sPROFFEs/PrAImate/internal/core"
)

// StartAgentHelperChat opens a clean, throwaway chat for the agent studio's
// assistant pane on the given CLI/model. If cwd is empty, it attempts to
// fall back to the editor folder or the system working directory.
func (a *App) StartAgentHelperChat(cli, model, cwd string) (*core.Chat, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	if cli == "" {
		cli = "claude"
	}
	if cwd == "" {
		if editorFolder != "" {
			cwd = editorFolder
		} else {
			cwd, _ = os.Getwd()
		}
	}
	chat, err := c.StartCleanChat(a.ctx, cli, model, cwd)
	if err != nil {
		return nil, err
	}
	_ = c.UpdateChatSettings(a.ctx, chat.ID, func(s *core.ChatSettings) {
		s.Surface = "agent-helper"
	})
	return chat, nil
}
