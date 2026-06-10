package core

// Folder watchers — DB-backed config rows for filesystem-triggered
// workflow runs. The actual fsnotify goroutine that turns FS events
// into MatchWatchers() calls lives in Phase 4b (main.go integration);
// this file is the storage + matching layer the daemon will call.
//
// Per the v1 schema (watchers table):
//
//   path           — directory to watch (absolute)
//   patterns_json  — []string of glob patterns, relative to path
//   workflow       — workflow name on the agent to fire
//   inputs_json    — fixed input values to pass into the workflow
//   debounce_ms    — minimum ms between successive fires for this watcher
//   enabled        — master toggle
//
// Each watcher is scoped to an agent_id (or chat_id, mutually exclusive).
// MatchWatchers picks the agent's default workflow when watchers.workflow
// is empty.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrWatcherNotFound is returned by GetWatcher / DeleteWatcher when
// no row matches.
var ErrWatcherNotFound = errors.New("watcher not found")

// Watcher is one configured watch entry.
type Watcher struct {
	ID         int64
	ChatID     string // optional; one of ChatID / AgentID set
	AgentID    string // optional
	Path       string
	Patterns   []string
	Workflow   string
	Inputs     map[string]string
	DebounceMs int
	Enabled    bool
}

// AddWatcherRequest groups everything needed to create one row.
type AddWatcherRequest struct {
	AgentID    string
	ChatID     string
	Path       string
	Patterns   []string
	Workflow   string
	Inputs     map[string]string
	DebounceMs int
}

// AddWatcher persists a new watcher and returns its id.
func (c *Core) AddWatcher(ctx context.Context, req AddWatcherRequest) (int64, error) {
	if c.store == nil {
		return 0, errors.New("AddWatcher: no store configured")
	}
	if req.Path == "" {
		return 0, errors.New("AddWatcher: Path required")
	}
	if req.AgentID == "" && req.ChatID == "" {
		return 0, errors.New("AddWatcher: AgentID or ChatID required")
	}
	patterns, _ := json.Marshal(orEmpty(req.Patterns))
	inputs, _ := json.Marshal(orEmptyStringMap(req.Inputs))
	debounce := req.DebounceMs
	if debounce <= 0 {
		debounce = 1000
	}
	res, err := c.store.DB().ExecContext(ctx, `
		INSERT INTO watchers (chat_id, agent_id, path, patterns_json, workflow,
		                      inputs_json, debounce_ms, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, 1)
	`, nullableText(req.ChatID), nullableText(req.AgentID),
		req.Path, string(patterns), req.Workflow, string(inputs), debounce)
	if err != nil {
		return 0, fmt.Errorf("insert watcher: %w", err)
	}
	return res.LastInsertId()
}

// ListWatchers returns every configured watcher. If enabledOnly is
// true, disabled rows are filtered.
func (c *Core) ListWatchers(ctx context.Context, enabledOnly bool) ([]Watcher, error) {
	if c.store == nil {
		return nil, errors.New("ListWatchers: no store configured")
	}
	q := `SELECT id, chat_id, agent_id, path, patterns_json, workflow,
	             inputs_json, debounce_ms, enabled FROM watchers`
	if enabledOnly {
		q += ` WHERE enabled = 1`
	}
	q += ` ORDER BY id`
	rows, err := c.store.DB().QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Watcher
	for rows.Next() {
		w, err := scanWatcher(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *w)
	}
	return out, rows.Err()
}

// DeleteWatcher removes one watcher by id.
func (c *Core) DeleteWatcher(ctx context.Context, id int64) error {
	if c.store == nil {
		return errors.New("DeleteWatcher: no store configured")
	}
	res, err := c.store.DB().ExecContext(ctx, `DELETE FROM watchers WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: %d", ErrWatcherNotFound, id)
	}
	return nil
}

// SetWatcherEnabled toggles the enabled flag.
func (c *Core) SetWatcherEnabled(ctx context.Context, id int64, enabled bool) error {
	if c.store == nil {
		return errors.New("SetWatcherEnabled: no store configured")
	}
	flag := 0
	if enabled {
		flag = 1
	}
	res, err := c.store.DB().ExecContext(ctx, `UPDATE watchers SET enabled = ? WHERE id = ?`, flag, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: %d", ErrWatcherNotFound, id)
	}
	return nil
}

// WatcherEvent is what the fsnotify daemon hands MatchWatchers. Path
// is the changed file (absolute); Op is informational for debouncing
// downstream consumers.
type WatcherEvent struct {
	Path string
	Op   string // "create" | "write" | "remove" | "rename"
}

// WatcherFire represents one watcher that matched an event and is
// ready to dispatch. The daemon converts this into a workflow run.
type WatcherFire struct {
	Watcher Watcher
	Event   WatcherEvent
	FiredAt time.Time
}

// debounceLog tracks the last fire time per watcher id so MatchWatchers
// can suppress over-frequent triggers without persisting state. Process-
// local — a restart resets debouncing, which is the right behavior
// (a daemon restart should re-evaluate stale state).
var (
	debounceMu  sync.Mutex
	debounceLog = map[int64]time.Time{}
)

// MatchWatchers returns every enabled watcher whose path+patterns
// match the given event. Honors per-watcher debounce windows. The
// daemon is expected to dispatch a workflow run for each returned
// fire.
func (c *Core) MatchWatchers(ctx context.Context, event WatcherEvent, now time.Time) ([]WatcherFire, error) {
	watchers, err := c.ListWatchers(ctx, true)
	if err != nil {
		return nil, err
	}
	var out []WatcherFire

	debounceMu.Lock()
	defer debounceMu.Unlock()

	for _, w := range watchers {
		if !matchesWatcher(w, event) {
			continue
		}
		if last, ok := debounceLog[w.ID]; ok {
			if now.Sub(last) < time.Duration(w.DebounceMs)*time.Millisecond {
				continue
			}
		}
		debounceLog[w.ID] = now
		out = append(out, WatcherFire{Watcher: w, Event: event, FiredAt: now})
	}
	return out, nil
}

// ResetDebounceLog clears process-local debounce state. Useful for
// tests; production code should not call this.
func ResetDebounceLog() {
	debounceMu.Lock()
	defer debounceMu.Unlock()
	debounceLog = map[int64]time.Time{}
}

func matchesWatcher(w Watcher, ev WatcherEvent) bool {
	// Path filter: event must be inside (or equal to) the watched
	// path. Normalise separators on both sides.
	wpath := filepath.Clean(w.Path)
	epath := filepath.Clean(ev.Path)
	if !strings.HasPrefix(epath, wpath) {
		return false
	}
	// Patterns: if none specified, fire on any file under path.
	if len(w.Patterns) == 0 {
		return true
	}
	rel, err := filepath.Rel(wpath, epath)
	if err != nil {
		return false
	}
	for _, pat := range w.Patterns {
		// Match both the relative path and the basename, so users can
		// write either "*.go" (basename) or "internal/*.go" (relative).
		if ok, _ := filepath.Match(pat, rel); ok {
			return true
		}
		if ok, _ := filepath.Match(pat, filepath.Base(rel)); ok {
			return true
		}
	}
	return false
}

func scanWatcher(scan func(...any) error) (*Watcher, error) {
	var (
		w                          Watcher
		chatID, agentID            sql.NullString
		patternsJSON, inputsJSON   string
		enabledInt                 int
	)
	err := scan(&w.ID, &chatID, &agentID, &w.Path, &patternsJSON, &w.Workflow,
		&inputsJSON, &w.DebounceMs, &enabledInt)
	if err != nil {
		return nil, err
	}
	if chatID.Valid {
		w.ChatID = chatID.String
	}
	if agentID.Valid {
		w.AgentID = agentID.String
	}
	if err := json.Unmarshal([]byte(patternsJSON), &w.Patterns); err != nil {
		return nil, fmt.Errorf("decode patterns_json: %w", err)
	}
	if err := json.Unmarshal([]byte(inputsJSON), &w.Inputs); err != nil {
		return nil, fmt.Errorf("decode inputs_json: %w", err)
	}
	w.Enabled = enabledInt != 0
	return &w, nil
}

func orEmptyStringMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
