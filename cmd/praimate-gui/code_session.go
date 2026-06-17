package main

// Code sessions are live PTY terminals (StartTerminal) — the real CLI,
// with no PrAImate-captured transcript. To make them listable like chats
// and studio sessions, we persist a lightweight chat record tagged
// surface="code" (folder + cli + model + optional local route). The
// Chats tab lists these under "Code sessions" and reopens them by
// relaunching a terminal in the folder. RecordCodeSession is called once
// at launch (not on reopen), so the list grows with new sessions only.

import (
	"fmt"
	"strings"

	"github.com/sPROFFEs/PrAImate/internal/core"
)

// RecordCodeSession persists a surface="code" chat pointer for a freshly
// launched terminal session and returns its id. localEndpoint, when set,
// is stored so reopening restores the local route.
func (a *App) RecordCodeSession(cli, model, cwd, localEndpoint, localAPIKey, localModel string) (string, error) {
	c, err := a.requireCore()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(cwd) == "" {
		return "", fmt.Errorf("a project folder is required")
	}
	if cli == "" {
		cli = "claude"
	}
	startModel := model
	if localEndpoint != "" {
		startModel = "" // the local route carries the model
	}
	chat, err := c.StartCleanChat(a.ctx, cli, startModel, cwd)
	if err != nil {
		return "", err
	}
	_ = c.UpdateChatSettings(a.ctx, chat.ID, func(s *core.ChatSettings) {
		s.Surface = "code"
		if localEndpoint != "" {
			s.Local = &core.ChatLocalEndpoint{Endpoint: localEndpoint, APIKey: localAPIKey, Model: localModel}
		} else if model != "" {
			s.Model = model
		}
	})
	return chat.ID, nil
}

// BindChatToTerminal pairs a live PTY with its chat row so the Sessions
// panel can resume the running terminal instead of starting a fresh
// duplicate. Called by Code.svelte right after RecordCodeSession.
func (a *App) BindChatToTerminal(termID, chatID string) {
	if a.terms == nil || termID == "" || chatID == "" {
		return
	}
	a.terms.bindChat(termID, chatID)
}
