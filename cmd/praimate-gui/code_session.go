package main

// Code sessions are live PTY terminals (StartTerminal) — the real CLI.
// While a PTY is alive, its bounded output history is replayed when the Code
// page is reopened. We also persist a lightweight chat record tagged
// surface="code" (folder + cli + model + optional local route), so the Chats
// tab can reattach to the original PTY or launch a replacement after it has
// exited. RecordCodeSession is called once at launch, so replacements do not
// create duplicate session rows.

import (
	"fmt"
	"strings"

	"git.jtsec.local/lab/PrAImate/internal/core"
)

// RecordCodeSession persists a surface="code" chat pointer for a freshly
// launched terminal session and returns its id. localEndpoint, when set,
// is stored so reopening restores the local route.
func (a *App) RecordCodeSession(cli, model, cwd, localEndpoint, _ string, localModel string) (string, error) {
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
			s.Local = &core.ChatLocalEndpoint{Endpoint: localEndpoint, Model: localModel}
		} else if model != "" {
			s.Model = model
		}
	})
	return chat.ID, nil
}

// BindChatToTerminal pairs a live PTY with its chat row so the Sessions
// panel can resume the running terminal instead of starting a fresh
// duplicate. Called by Code.svelte right after RecordCodeSession.
func (a *App) BindChatToTerminal(termID, chatID string) error {
	if a.terms == nil || termID == "" || chatID == "" {
		return fmt.Errorf("terminal id and chat id are required")
	}
	return a.terms.bindChat(termID, chatID)
}

// GetCodeSessionSnapshot returns the bounded, in-memory output tail for a
// live Code terminal. No terminal output is persisted to disk.
func (a *App) GetCodeSessionSnapshot(chatID, termID string) (TerminalSnapshot, error) {
	if a.terms == nil {
		return TerminalSnapshot{}, fmt.Errorf("terminal manager is not available")
	}
	return a.terms.codeSnapshot(chatID, termID)
}
