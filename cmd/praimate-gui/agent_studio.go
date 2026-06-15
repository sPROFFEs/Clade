package main

// Agent authoring studio — the right-pane AI assistant is an ephemeral
// "agent-helper" chat: it helps the user write/refine an agent but is NOT
// part of the agent and is deleted when the studio closes. Its CLI/model
// are freely switchable (UpdateChatConfig) and never persisted onto the
// agent. Tagged surface="agent-helper" so it stays out of the Chats list.

import (
	"os"
	"time"

	"github.com/sPROFFEs/PrAImate/internal/core"
)

// helperAgentID is the built-in agent whose instructions seed the studio's
// authoring assistant — same knowledge of the PrAImate agent format the
// user is editing. If the agent is missing or doesn't support the chosen
// CLI, we fall back to a plain clean chat.
const helperAgentID = "agent-builder"

// StartAgentHelperChat opens the studio's assistant pane as a throwaway
// chat preloaded with the agent-builder system prompt, so the assistant
// already knows PrAImate's agent schema, surfaces, knowledge modes, and
// workflow rules. Tagged surface="agent-helper" so it stays out of the
// regular Chats list.
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

	agentID := ""
	title := cli
	if agent, err := c.GetAgent(a.ctx, helperAgentID); err == nil && contains(agent.Supports, cli) {
		agentID = agent.ID
		title = agent.Name
	}
	if model != "" {
		title += " · " + model
	}
	title += " · " + time.Now().Format("Jan 2 15:04")

	chat, err := c.CreateChat(a.ctx, core.CreateChatRequest{
		Title:         title,
		AgentID:       agentID,
		CLIAgent:      cli,
		WorkspacePath: cwd,
		Settings:      core.ChatSettings{Model: model, Surface: "agent-helper"},
	})
	if err != nil {
		return nil, err
	}
	return chat, nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
