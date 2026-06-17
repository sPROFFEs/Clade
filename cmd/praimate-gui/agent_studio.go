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
// chat preloaded with the agent-builder system prompt. When `targetAgentID`
// is non-empty, the helper's cwd is the agent's on-disk root
// (<config>/praimate/agents/<id>/) and the current DB-state YAML is
// mirrored to `agent.yaml` in that folder — so the wrapped CLI can read
// and edit the agent and its knowledge files as real files. Without
// `targetAgentID` the helper falls back to the GUI cwd (the legacy
// behaviour, kept so the New-Agent flow keeps working before the agent
// has a row in the DB).
func (a *App) StartAgentHelperChat(cli, model, cwd, targetAgentID string) (*core.Chat, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	if cli == "" {
		cli = "claude"
	}

	// When we know which agent is open in the studio, pin the helper
	// to that agent's on-disk folder and mirror the current YAML so the
	// CLI sees a real file at `./agent.yaml`. This is the user-visible
	// fix for "the CLI keeps running in /home/user with nothing to edit".
	var yamlPath string
	if targetAgentID != "" {
		if dir, derr := core.AgentDir(targetAgentID); derr == nil {
			if agent, gerr := c.GetAgent(a.ctx, targetAgentID); gerr == nil && agent != nil {
				if p, werr := core.WriteAgentYAMLToDisk(agent); werr == nil {
					yamlPath = p
				}
			}
			cwd = dir
		}
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
	_ = yamlPath // reserved for future telemetry — the file is now on disk at cwd/agent.yaml
	return chat, nil
}

// SyncAgentYAMLToDisk re-renders the current DB YAML for `id` into
// <AgentDir>/agent.yaml. Called by the frontend after the user clicks
// "Save agent" in the studio so the helper CLI continues to see the
// authoritative copy on the next turn.
func (a *App) SyncAgentYAMLToDisk(id string) (string, error) {
	c, err := a.requireCore()
	if err != nil {
		return "", err
	}
	agent, err := c.GetAgent(a.ctx, id)
	if err != nil {
		return "", err
	}
	return core.WriteAgentYAMLToDisk(agent)
}

// ReadAgentYAMLFromDisk returns the contents of <AgentDir>/agent.yaml.
// Used by the studio's "Reload from disk" button so the user can pull
// the helper's edits back into the editor pane in one click.
func (a *App) ReadAgentYAMLFromDisk(id string) (string, error) {
	dir, err := core.AgentDir(id)
	if err != nil {
		return "", err
	}
	body, err := os.ReadFile(dir + string(os.PathSeparator) + "agent.yaml")
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
