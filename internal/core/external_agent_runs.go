package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"
)

var (
	ErrExternalAgentRunNotFound = errors.New("external agent run not found")
	ErrExternalAgentRunConflict = errors.New("external agent run ID belongs to a different request")
	ErrExternalAgentRunStale    = errors.New("external agent run attempt is no longer active")
)

var externalAgentRunIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type ExternalAgentRun struct {
	ID          string
	RequestHash string
	AgentID     string
	CLI         string
	Runtime     string
	Kind        string
	State       string
	Attempt     int
	ResultJSON  string
	Error       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CompletedAt *time.Time
}

type ClaimExternalAgentRunRequest struct {
	ID          string
	RequestHash string
	AgentID     string
	CLI         string
	Runtime     string
	Kind        string
	Retry       bool
}

// ClaimExternalAgentRun atomically creates or reclaims a durable external run.
// Execute is false when a matching prior result/state must be returned without
// launching another CLI process. Retry deliberately reclaims even a running
// record because a previous process may have crashed; callers must accept that
// native CLI side effects cannot be proven exactly-once across that boundary.
func (c *Core) ClaimExternalAgentRun(ctx context.Context, req ClaimExternalAgentRunRequest) (run *ExternalAgentRun, execute bool, err error) {
	if c.store == nil {
		return nil, false, errors.New("ClaimExternalAgentRun: no store configured")
	}
	if !externalAgentRunIDPattern.MatchString(req.ID) {
		return nil, false, errors.New("run ID must be 1-128 letters, numbers, dots, underscores, or dashes")
	}
	if req.RequestHash == "" || req.AgentID == "" || req.CLI == "" || (req.Kind != "prompt" && req.Kind != "workflow") {
		return nil, false, errors.New("ClaimExternalAgentRun: incomplete request")
	}
	conn, err := c.store.DB().Conn(ctx)
	if err != nil {
		return nil, false, err
	}
	defer conn.Close()
	// A reserved write lock makes the initial read+insert one atomic claim
	// across processes. The configured SQLite busy timeout lets contenders wait
	// and then observe the row created by the winner instead of failing with a
	// uniqueness or database-locked error.
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return nil, false, err
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), "ROLLBACK") }()

	run, err = scanExternalAgentRun(conn.QueryRowContext(ctx, externalAgentRunSelect+` WHERE id = ?`, req.ID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		now := time.Now().UTC()
		_, err = conn.ExecContext(ctx, `INSERT INTO external_agent_runs
			(id, request_hash, agent_id, cli, runtime, kind, state, attempt, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, 'running', 1, ?, ?)`,
			req.ID, req.RequestHash, req.AgentID, req.CLI, req.Runtime, req.Kind,
			now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
		if err != nil {
			return nil, false, err
		}
		if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
			return nil, false, err
		}
		return &ExternalAgentRun{
			ID: req.ID, RequestHash: req.RequestHash, AgentID: req.AgentID,
			CLI: req.CLI, Runtime: req.Runtime, Kind: req.Kind, State: "running",
			Attempt: 1, CreatedAt: now, UpdatedAt: now,
		}, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	if run.RequestHash != req.RequestHash {
		return nil, false, ErrExternalAgentRunConflict
	}
	if !req.Retry {
		return run, false, nil
	}
	now := time.Now().UTC()
	_, err = conn.ExecContext(ctx, `UPDATE external_agent_runs
		SET state = 'running', attempt = attempt + 1, result_json = NULL,
			error = '', completed_at = NULL, updated_at = ? WHERE id = ?`,
		now.Format(time.RFC3339Nano), req.ID)
	if err != nil {
		return nil, false, err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return nil, false, err
	}
	run.State, run.ResultJSON, run.Error = "running", "", ""
	run.Attempt++
	run.UpdatedAt, run.CompletedAt = now, nil
	return run, true, nil
}

func (c *Core) FinishExternalAgentRun(ctx context.Context, id string, attempt int, state, resultJSON, message string) error {
	if c.store == nil {
		return errors.New("FinishExternalAgentRun: no store configured")
	}
	switch state {
	case "completed", "failed", "timed_out", "interrupted":
	default:
		return fmt.Errorf("FinishExternalAgentRun: invalid state %q", state)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := c.store.DB().ExecContext(ctx, `UPDATE external_agent_runs
		SET state = ?, result_json = ?, error = ?, updated_at = ?, completed_at = ?
		WHERE id = ? AND attempt = ? AND state = 'running'`,
		state, nullableString(resultJSON), message, now, now, id, attempt)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var exists int
		if err := c.store.DB().QueryRowContext(ctx, `SELECT 1 FROM external_agent_runs WHERE id = ?`, id).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return ErrExternalAgentRunNotFound
		} else if err != nil {
			return err
		}
		return ErrExternalAgentRunStale
	}
	return nil
}

func (c *Core) GetExternalAgentRun(ctx context.Context, id string) (*ExternalAgentRun, error) {
	if c.store == nil {
		return nil, errors.New("GetExternalAgentRun: no store configured")
	}
	run, err := scanExternalAgentRun(c.store.DB().QueryRowContext(ctx, externalAgentRunSelect+` WHERE id = ?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrExternalAgentRunNotFound
	}
	return run, err
}

const externalAgentRunSelect = `SELECT id, request_hash, agent_id, cli, runtime,
	kind, state, attempt, result_json, error, created_at, updated_at, completed_at
	FROM external_agent_runs`

func scanExternalAgentRun(scan func(...any) error) (*ExternalAgentRun, error) {
	var run ExternalAgentRun
	var result, completed sql.NullString
	var created, updated string
	if err := scan(&run.ID, &run.RequestHash, &run.AgentID, &run.CLI, &run.Runtime,
		&run.Kind, &run.State, &run.Attempt, &result, &run.Error, &created, &updated, &completed); err != nil {
		return nil, err
	}
	run.ResultJSON = result.String
	run.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	run.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	if completed.Valid {
		t, _ := time.Parse(time.RFC3339Nano, completed.String)
		run.CompletedAt = &t
	}
	return &run, nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
