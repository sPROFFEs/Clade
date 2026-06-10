package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const privacyPatternsKey = "privacy.custom_patterns"

// ListPrivacyPatterns returns the persisted custom privacy regexes.
func (c *Core) ListPrivacyPatterns(ctx context.Context) ([]string, error) {
	if c.store == nil {
		return nil, errors.New("ListPrivacyPatterns: no store configured")
	}
	var raw string
	err := c.store.DB().QueryRowContext(ctx, `SELECT value_json FROM settings_cli WHERE key = ?`, privacyPatternsKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var patterns []string
	if err := json.Unmarshal([]byte(raw), &patterns); err != nil {
		return nil, fmt.Errorf("decode privacy patterns: %w", err)
	}
	return patterns, nil
}

// AddPrivacyPattern validates, persists, and activates one custom
// regex. Duplicate patterns are ignored.
func (c *Core) AddPrivacyPattern(ctx context.Context, pattern string) error {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return errors.New("AddPrivacyPattern: empty pattern")
	}
	current, err := c.ListPrivacyPatterns(ctx)
	if err != nil {
		return err
	}
	for _, existing := range current {
		if existing == pattern {
			return nil
		}
	}
	next := append(append([]string{}, current...), pattern)
	if err := c.PrivacyScanner().SetCustomPatterns(next); err != nil {
		return err
	}
	return c.savePrivacyPatterns(ctx, next)
}

// DeletePrivacyPattern removes one custom regex by index.
func (c *Core) DeletePrivacyPattern(ctx context.Context, index int) error {
	current, err := c.ListPrivacyPatterns(ctx)
	if err != nil {
		return err
	}
	if index < 0 || index >= len(current) {
		return fmt.Errorf("DeletePrivacyPattern: index %d out of range", index)
	}
	next := append([]string{}, current[:index]...)
	next = append(next, current[index+1:]...)
	if err := c.PrivacyScanner().SetCustomPatterns(next); err != nil {
		return err
	}
	return c.savePrivacyPatterns(ctx, next)
}

func (c *Core) loadPrivacyPatterns(ctx context.Context) {
	if c.store == nil {
		return
	}
	patterns, err := c.ListPrivacyPatterns(ctx)
	if err != nil {
		return
	}
	_ = c.PrivacyScanner().SetCustomPatterns(patterns)
}

func (c *Core) savePrivacyPatterns(ctx context.Context, patterns []string) error {
	if c.store == nil {
		return errors.New("savePrivacyPatterns: no store configured")
	}
	raw, err := json.Marshal(patterns)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = c.store.DB().ExecContext(ctx, `
		INSERT INTO settings_cli (key, value_json, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json,
		                                updated_at = excluded.updated_at
	`, privacyPatternsKey, string(raw), now)
	return err
}
