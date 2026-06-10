package core

// Scheduled workflow runs — DB-backed configuration for time-driven
// dispatch. Supports two trigger types:
//
//   - At:    one-shot ISO timestamp. Fires once when now >= at AND
//            last_run_at IS NULL.
//   - Cron:  recurring cron expression. NOT IMPLEMENTED in 1.0a —
//            schema accepts it but TickSchedules ignores cron rows.
//            Add via a cron parser in Phase 4b.
//
// Like watchers, the actual daemon goroutine that calls TickSchedules
// on a wall-clock interval lives in Phase 4b. This file is the
// storage + due-row selection layer.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrScheduleNotFound is returned by Delete/Enable when no row matches.
var ErrScheduleNotFound = errors.New("schedule not found")

// Schedule is one configured time-driven dispatch.
type Schedule struct {
	ID        int64
	ChatID    string
	AgentID   string
	Cron      string     // standard cron expr; reserved for 1.0b
	At        *time.Time // one-shot trigger
	Workflow  string
	Inputs    map[string]string
	OnMiss    string // "skip" | "run_once" | "run_catchup"
	Priority  string // "normal" | "low"
	LastRunAt *time.Time
	NextRunAt *time.Time
	Enabled   bool
}

// AddScheduleRequest groups the fields needed to create one row.
//
// Exactly one of Cron and At must be set. Cron rows are accepted but
// TickSchedules will not fire them until Phase 4b adds a parser.
type AddScheduleRequest struct {
	AgentID  string
	ChatID   string
	Cron     string
	At       *time.Time
	Workflow string
	Inputs   map[string]string
	OnMiss   string
	Priority string
}

// AddSchedule persists a new schedule row.
func (c *Core) AddSchedule(ctx context.Context, req AddScheduleRequest) (int64, error) {
	if c.store == nil {
		return 0, errors.New("AddSchedule: no store configured")
	}
	if (req.Cron == "") == (req.At == nil) {
		return 0, errors.New("AddSchedule: exactly one of Cron or At must be set")
	}
	if req.AgentID == "" && req.ChatID == "" {
		return 0, errors.New("AddSchedule: AgentID or ChatID required")
	}
	if req.OnMiss == "" {
		req.OnMiss = "skip"
	}
	if req.Priority == "" {
		req.Priority = "normal"
	}
	inputs, _ := json.Marshal(orEmptyStringMap(req.Inputs))

	var cronStr sql.NullString
	if req.Cron != "" {
		cronStr = sql.NullString{String: req.Cron, Valid: true}
	}
	var atStr, nextStr sql.NullString
	if req.At != nil {
		s := req.At.UTC().Format(time.RFC3339Nano)
		atStr = sql.NullString{String: s, Valid: true}
		// Pre-populate next_run_at so daemons can WHERE next_run_at <=
		// now efficiently. For cron rows the parser will fill this in.
		nextStr = atStr
	}
	res, err := c.store.DB().ExecContext(ctx, `
		INSERT INTO schedules (chat_id, agent_id, cron, at, workflow,
		                       inputs_json, on_miss, priority, next_run_at, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
	`, nullableText(req.ChatID), nullableText(req.AgentID),
		cronStr, atStr, req.Workflow, string(inputs),
		req.OnMiss, req.Priority, nextStr)
	if err != nil {
		return 0, fmt.Errorf("insert schedule: %w", err)
	}
	return res.LastInsertId()
}

// ListSchedules returns every configured schedule. If enabledOnly is
// true, disabled rows are filtered.
func (c *Core) ListSchedules(ctx context.Context, enabledOnly bool) ([]Schedule, error) {
	if c.store == nil {
		return nil, errors.New("ListSchedules: no store configured")
	}
	q := `SELECT id, chat_id, agent_id, cron, at, workflow,
	             inputs_json, on_miss, priority, last_run_at, next_run_at, enabled
	      FROM schedules`
	if enabledOnly {
		q += ` WHERE enabled = 1`
	}
	q += ` ORDER BY next_run_at, id`
	rows, err := c.store.DB().QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Schedule
	for rows.Next() {
		s, err := scanSchedule(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// DeleteSchedule removes one schedule by id.
func (c *Core) DeleteSchedule(ctx context.Context, id int64) error {
	if c.store == nil {
		return errors.New("DeleteSchedule: no store configured")
	}
	res, err := c.store.DB().ExecContext(ctx, `DELETE FROM schedules WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: %d", ErrScheduleNotFound, id)
	}
	return nil
}

// SetScheduleEnabled toggles the enabled flag.
func (c *Core) SetScheduleEnabled(ctx context.Context, id int64, enabled bool) error {
	if c.store == nil {
		return errors.New("SetScheduleEnabled: no store configured")
	}
	flag := 0
	if enabled {
		flag = 1
	}
	res, err := c.store.DB().ExecContext(ctx, `UPDATE schedules SET enabled = ? WHERE id = ?`, flag, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: %d", ErrScheduleNotFound, id)
	}
	return nil
}

// ScheduleFire is one schedule the tick chose to dispatch. The daemon
// converts each into a RunWorkflow call and then calls MarkScheduleFired
// to advance state.
type ScheduleFire struct {
	Schedule Schedule
	FiredAt  time.Time
}

// TickSchedules returns every enabled schedule whose next_run_at is at
// or before now AND which has not yet fired (last_run_at IS NULL for
// at-rows). Cron rows are silently skipped in 1.0a until Phase 4b
// ships the parser.
//
// This function does NOT mutate the DB — callers iterate the returned
// fires, dispatch each workflow, and call MarkScheduleFired on
// success. That lets the caller decide what to do on dispatch failure.
func (c *Core) TickSchedules(ctx context.Context, now time.Time) ([]ScheduleFire, error) {
	rows, err := c.ListSchedules(ctx, true)
	if err != nil {
		return nil, err
	}
	var out []ScheduleFire
	for _, s := range rows {
		// At-style rows fire once.
		if s.At != nil && s.LastRunAt == nil && !s.At.After(now) {
			out = append(out, ScheduleFire{Schedule: s, FiredAt: now})
			continue
		}
		// Cron parsing — to be added in 4b.
	}
	return out, nil
}

// MarkScheduleFired stamps last_run_at on a schedule, signaling the
// daemon completed its dispatch. For at-style rows this prevents a
// re-fire on the next tick (one-shot semantics).
func (c *Core) MarkScheduleFired(ctx context.Context, id int64, firedAt time.Time) error {
	if c.store == nil {
		return errors.New("MarkScheduleFired: no store configured")
	}
	stamp := firedAt.UTC().Format(time.RFC3339Nano)
	res, err := c.store.DB().ExecContext(ctx, `
		UPDATE schedules SET last_run_at = ? WHERE id = ?
	`, stamp, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: %d", ErrScheduleNotFound, id)
	}
	return nil
}

func scanSchedule(scan func(...any) error) (*Schedule, error) {
	var (
		s                                                       Schedule
		chatID, agentID, cronStr                                sql.NullString
		atStr, lastRunStr, nextRunStr                           sql.NullString
		inputsJSON                                              string
		enabledInt                                              int
	)
	err := scan(&s.ID, &chatID, &agentID, &cronStr, &atStr, &s.Workflow,
		&inputsJSON, &s.OnMiss, &s.Priority, &lastRunStr, &nextRunStr, &enabledInt)
	if err != nil {
		return nil, err
	}
	if chatID.Valid {
		s.ChatID = chatID.String
	}
	if agentID.Valid {
		s.AgentID = agentID.String
	}
	if cronStr.Valid {
		s.Cron = cronStr.String
	}
	if atStr.Valid {
		t, _ := time.Parse(time.RFC3339, atStr.String)
		s.At = &t
	}
	if lastRunStr.Valid {
		t, _ := time.Parse(time.RFC3339, lastRunStr.String)
		s.LastRunAt = &t
	}
	if nextRunStr.Valid {
		t, _ := time.Parse(time.RFC3339, nextRunStr.String)
		s.NextRunAt = &t
	}
	if err := json.Unmarshal([]byte(inputsJSON), &s.Inputs); err != nil {
		return nil, fmt.Errorf("decode inputs_json: %w", err)
	}
	s.Enabled = enabledInt != 0
	return &s, nil
}
