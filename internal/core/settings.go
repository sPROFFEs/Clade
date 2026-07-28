package core

// Settings retain two schema-compatible tables from the 1.0 data model.
// settings_cli holds launcher-wide preferences also consumed by
// maintenance commands; settings_gui holds desktop presentation state.
// Shared values (chats, agents, MCP) have dedicated tables.
//
// Values are persisted as JSON strings so the same column can hold
// booleans, ints, strings, and small structs without schema churn.
// Raw GetSetting/SetSetting provide forward compatibility for controls
// that do not yet have typed accessors.

import (
	"context"
	"database/sql"
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
// producing valid JSON.
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
