package main

// Skills page Wails bindings — browse the built-in catalogue, manage
// the "default skills for new chats" list, and flip per-chat
// enablement on existing chats. Skills are CLI-tagged in the catalogue
// but injected into the system prompt regardless of CLI — the page
// warns the user when a skill is enabled on a chat whose CLI it
// wasn't designed for.

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/sPROFFEs/PrAImate/internal/core"
)

// SkillsList returns the built-in catalogue.
func (a *App) SkillsList() []core.Skill {
	return core.SkillCatalogue()
}

// SkillsForCLI returns the catalogue filtered to entries that target
// the given CLI. The Skills page uses this for the per-CLI tabs.
func (a *App) SkillsForCLI(cli string) []core.Skill {
	return core.SkillsForCLI(cli)
}

// --- default skills (global) ---

// defaultsFile is where the "default skills for new chats" list lives —
// a plain JSON file alongside the praimate config. Kept separate from
// the SQLite store so the file is greppable / hand-editable.
func defaultsFile() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "praimate")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "default-skills.json"), nil
}

type defaultsBody struct {
	Skills []string `json:"skills"`
}

// SkillsDefaults returns the IDs of skills that get auto-enabled on
// every newly-created chat. Returns an empty slice when the file
// hasn't been written yet.
func (a *App) SkillsDefaults() []string {
	path, err := defaultsFile()
	if err != nil {
		return []string{}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return []string{}
	}
	var body defaultsBody
	if err := json.Unmarshal(b, &body); err != nil {
		return []string{}
	}
	if body.Skills == nil {
		return []string{}
	}
	return body.Skills
}

// SetSkillsDefaults overwrites the default-skills list.
func (a *App) SetSkillsDefaults(ids []string) error {
	path, err := defaultsFile()
	if err != nil {
		return err
	}
	if ids == nil {
		ids = []string{}
	}
	body, err := json.MarshalIndent(defaultsBody{Skills: ids}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

// --- per-chat enablement ---

// ChatSkills returns the IDs of skills currently enabled on a chat.
func (a *App) ChatSkills(chatID string) []string {
	c, err := a.requireCore()
	if err != nil {
		return []string{}
	}
	chat, err := c.GetChat(a.ctx, chatID)
	if err != nil {
		return []string{}
	}
	if chat.Settings.Skills == nil {
		return []string{}
	}
	return chat.Settings.Skills
}

// applyDefaultSkills writes the user's "default skills" list into a
// freshly-created chat's settings. Best-effort — failure to apply
// defaults never blocks chat creation.
func (a *App) applyDefaultSkills(chatID string) {
	ids := a.SkillsDefaults()
	if len(ids) == 0 {
		return
	}
	c, err := a.requireCore()
	if err != nil {
		return
	}
	_ = c.UpdateChatSettings(a.ctx, chatID, func(s *core.ChatSettings) {
		s.Skills = append([]string(nil), ids...)
	})
}

// SetChatSkills overwrites the skills list on a chat.
func (a *App) SetChatSkills(chatID string, ids []string) error {
	c, err := a.requireCore()
	if err != nil {
		return err
	}
	return c.UpdateChatSettings(a.ctx, chatID, func(s *core.ChatSettings) {
		if ids == nil {
			s.Skills = nil
			return
		}
		s.Skills = append([]string(nil), ids...)
	})
}
