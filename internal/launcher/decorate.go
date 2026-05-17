package launcher

// Post-compile decoration. After wpc has written the workpath into the
// sandbox, the launcher layers per-workspace concerns on top:
//
//   - language directive (a one-line "Respond in X" preamble)
//   - persistent MEMORY.md (copied workspace → sandbox before launch,
//     synced back after the agent exits)
//   - online skills (cloned into the right per-target skills dir)
//   - a new chats/<timestamp>-<agent>/ session manifest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sdksdk/code-launcher/internal/skills"
)

// applyDecorations runs every per-workspace post-compile concern. Called
// from PrepareSandbox after wpc has run. Errors are aggregated but
// non-fatal — we don't want a broken online-skill clone to block the
// launch.
func applyDecorations(ws Workspace, agent Agent) []string {
	var notes []string

	if lang := strings.TrimSpace(ws.Settings.Language); lang != "" {
		if err := prependLanguage(ws, agent, lang); err != nil {
			notes = append(notes, "language: "+err.Error())
		}
	}

	if ws.Settings.MemoryEnabled {
		if err := stageMemory(ws); err != nil {
			notes = append(notes, "memory: "+err.Error())
		}
	}

	if len(ws.Settings.OnlineSkills) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		dir := onlineSkillsDir(ws, agent)
		for _, r := range skills.FetchAll(ctx, ws.Settings.OnlineSkills, dir) {
			if r.Err != nil {
				notes = append(notes, fmt.Sprintf("online-skill %s: %v", r.URL, r.Err))
			}
		}
	}

	if err := recordChatSession(ws, agent); err != nil {
		notes = append(notes, "chat-log: "+err.Error())
	}

	return notes
}

func prependLanguage(ws Workspace, agent Agent, lang string) error {
	// Prepend to the file the agent actually reads.
	var path string
	switch agent.WpcTarget {
	case "claude":
		path = filepath.Join(ws.SandboxDir, ".claude", "skills", ws.Name, "SKILL.md")
	case "codex":
		path = filepath.Join(ws.SandboxDir, "AGENTS.md")
	default:
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	body := string(raw)
	directive := fmt.Sprintf("\n> **Language directive:** respond in %s.\n", lang)

	// For Claude SKILL.md we must keep YAML frontmatter intact, so insert
	// directive AFTER the second `---` line.
	if strings.HasPrefix(body, "---\n") {
		if end := strings.Index(body[4:], "\n---\n"); end >= 0 {
			cut := 4 + end + len("\n---\n")
			body = body[:cut] + directive + body[cut:]
		}
	} else {
		body = directive + "\n" + body
	}
	return os.WriteFile(path, []byte(body), 0o644)
}

// stageMemory copies workspace/MEMORY.md → sandbox/MEMORY.md before
// launch. The workspace-level file is the canonical source of truth;
// sync-back happens via SyncMemoryBack after the agent exits.
func stageMemory(ws Workspace) error {
	src := filepath.Join(ws.Root, "MEMORY.md")
	if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) {
		// Initialize a starter memory file on first use.
		header := "# Memory\n\n" +
			"This file persists across agent sessions for this workspace.\n" +
			"The agent can read it on launch and append notes you want to keep.\n"
		if err := os.WriteFile(src, []byte(header), 0o644); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	dst := filepath.Join(ws.SandboxDir, "MEMORY.md")
	return copyFileLib(src, dst)
}

// SyncMemoryBack copies sandbox/MEMORY.md → workspace/MEMORY.md if the
// sandbox version is newer. Called from main() after the agent exits.
func SyncMemoryBack(ws Workspace) error {
	if !ws.Settings.MemoryEnabled {
		return nil
	}
	sandboxPath := filepath.Join(ws.SandboxDir, "MEMORY.md")
	wsPath := filepath.Join(ws.Root, "MEMORY.md")
	sInfo, err := os.Stat(sandboxPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil // agent never touched it
	}
	if err != nil {
		return err
	}
	wInfo, wErr := os.Stat(wsPath)
	if wErr == nil && !sInfo.ModTime().After(wInfo.ModTime()) {
		return nil // no changes
	}
	return copyFileLib(sandboxPath, wsPath)
}

func onlineSkillsDir(ws Workspace, agent Agent) string {
	switch agent.WpcTarget {
	case "claude":
		// Claude Code auto-loads everything under .claude/skills/.
		return filepath.Join(ws.SandboxDir, ".claude", "skills")
	default:
		// For codex/opencode we drop them next to AGENTS.md and surface
		// the path in the orientation file. The agent can still cat them
		// on demand.
		return filepath.Join(ws.SandboxDir, "online-skills")
	}
}

// SessionRecord is the metadata blob written into
// <ws>/chats/<timestamp>-<agent>/session.json so the user can browse
// past sessions and see what each one was about. Actual transcript
// capture is a separate, agent-specific concern (Phase 3).
type SessionRecord struct {
	StartedAt   time.Time `json:"startedAt"`
	Agent       string    `json:"agent"`
	Binary      string    `json:"binary"`
	WpcTarget   string    `json:"wpcTarget"`
	WorkspaceID string    `json:"workspace"`
	SandboxDir  string    `json:"sandboxDir"`
}

func recordChatSession(ws Workspace, agent Agent) error {
	stamp := time.Now().UTC().Format("20060102-150405")
	dir := filepath.Join(ws.ChatsDir, stamp+"-"+string(agent.ID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	rec := SessionRecord{
		StartedAt:   time.Now().UTC(),
		Agent:       string(agent.ID),
		Binary:      agent.Binary,
		WpcTarget:   agent.WpcTarget,
		WorkspaceID: ws.Name,
		SandboxDir:  ws.SandboxDir,
	}
	raw, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(filepath.Join(dir, "session.json"), raw, 0o644)
}

// copyFileLib avoids name clash with the existing copyFile in workspaces.go.
func copyFileLib(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, raw, 0o644)
}
