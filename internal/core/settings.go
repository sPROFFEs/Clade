package core

// Settings live in two parallel tables — settings_cli and settings_gui —
// because the TUI and the GUI surface different controls (decision 8 in
// the 1.0 plan). Shared values (chats, agents, memory, MCP) are NOT
// stored here; they have their own dedicated tables.
//
// Values are persisted as JSON strings so the same column can hold
// booleans, ints, strings, and small structs without schema churn.
// Typed accessors at the bottom of this file (IsMemoryEnabled,
// SetMemoryEnabled, etc.) are the recommended way to read/write —
// raw GetSetting/SetSetting exist for forward-compat with not-yet-
// modeled keys.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// SettingsScope picks which of the two parallel tables to read/write.
type SettingsScope string

const (
	ScopeCLI SettingsScope = "cli"
	ScopeGUI SettingsScope = "gui"
)

func (s SettingsScope) table() (string, error) {
	switch s {
	case ScopeCLI:
		return "settings_cli", nil
	case ScopeGUI:
		return "settings_gui", nil
	default:
		return "", fmt.Errorf("invalid settings scope %q", s)
	}
}

// GetSetting fetches a raw JSON value. Returns (nil, nil) if the key
// is absent. Callers decode into the type they expect.
func (c *Core) GetSetting(ctx context.Context, scope SettingsScope, key string) ([]byte, error) {
	if c.store == nil {
		return nil, errors.New("GetSetting: no store configured")
	}
	tbl, err := scope.table()
	if err != nil {
		return nil, err
	}
	var raw string
	err = c.store.DB().QueryRowContext(ctx,
		"SELECT value_json FROM "+tbl+" WHERE key = ?", key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return []byte(raw), nil
}

// SetSetting upserts a raw JSON value. Caller is responsible for
// producing valid JSON; we don't validate here because typed callers
// (SetMemoryEnabled etc.) marshal through encoding/json which is
// already correct.
func (c *Core) SetSetting(ctx context.Context, scope SettingsScope, key string, valueJSON []byte) error {
	if c.store == nil {
		return errors.New("SetSetting: no store configured")
	}
	if key == "" {
		return errors.New("SetSetting: empty key")
	}
	tbl, err := scope.table()
	if err != nil {
		return err
	}
	_, err = c.store.DB().ExecContext(ctx, `
		INSERT INTO `+tbl+` (key, value_json, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at
	`, key, string(valueJSON), time.Now().UTC().Format(time.RFC3339))
	return err
}

// DeleteSetting removes a key. No-op if absent.
func (c *Core) DeleteSetting(ctx context.Context, scope SettingsScope, key string) error {
	if c.store == nil {
		return errors.New("DeleteSetting: no store configured")
	}
	tbl, err := scope.table()
	if err != nil {
		return err
	}
	_, err = c.store.DB().ExecContext(ctx, "DELETE FROM "+tbl+" WHERE key = ?", key)
	return err
}

// --- Typed helpers -----------------------------------------------------
//
// Each public setting has a constant key + a Get/Set pair returning a
// typed value. Add new ones as features land; never write raw keys
// inline at call sites.

const (
	// keyMemoryEnabled — bool. Master switch for memory distillation
	// and injection. Defaults to false (opt-in per plan §3 Phase 3 gate).
	keyMemoryEnabled = "memory.enabled"
)

// IsMemoryEnabled reports the user's memory toggle. Stored under the
// CLI scope; the GUI reads/writes the same scope so the toggle is
// shared across surfaces (the toggle is a user preference, not a
// surface-specific UI control). Default when unset: false.
func (c *Core) IsMemoryEnabled(ctx context.Context) (bool, error) {
	raw, err := c.GetSetting(ctx, ScopeCLI, keyMemoryEnabled)
	if err != nil || raw == nil {
		return false, err
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return false, fmt.Errorf("decode %s: %w", keyMemoryEnabled, err)
	}
	return b, nil
}

// SetMemoryEnabled writes the master memory toggle.
func (c *Core) SetMemoryEnabled(ctx context.Context, enabled bool) error {
	val, _ := json.Marshal(enabled)
	return c.SetSetting(ctx, ScopeCLI, keyMemoryEnabled, val)
}
