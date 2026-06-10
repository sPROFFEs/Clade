package core

// Memory is the cross-chat persistent layer that survives individual
// chat lifetimes. Modeled on Osaurus's four-layer scheme but compressed
// to three first-class types — identity, pinned facts, episodes — plus
// the transcript on disk (chat slices) as the fallback for "exact
// words" queries.
//
// Storage: all three live in the same SQLite DB as everything else.
// Schema is defined in internal/store/migrations/0001_init.sql.
//
// Phase 3a (this file): typed CRUD only. No LLM calls, no retrieval
// planner, no compaction watermark — those are Phase 3b which needs a
// design decision on which endpoint distills episodes.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// --- Identity ----------------------------------------------------------

// Identity is one fact about who the user is — e.g. ("name","Julio").
// Either user-authored ("manual") or auto-derived from accumulated
// signals ("derived"). Phase 3a only exposes manual; derived rows are
// reserved for Phase 3b's distiller.
type Identity struct {
	Key       string
	Value     string
	Source    string // "manual" | "derived"
	UpdatedAt time.Time
}

// SetIdentity inserts or updates one identity key. Source defaults to
// "manual" when empty.
func (c *Core) SetIdentity(ctx context.Context, key, value, source string) error {
	if c.store == nil {
		return errors.New("SetIdentity: no store configured")
	}
	if key == "" {
		return errors.New("SetIdentity: empty key")
	}
	if source == "" {
		source = "manual"
	}
	_, err := c.store.DB().ExecContext(ctx, `
		INSERT INTO memory_identity (key, value, source, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			source = excluded.source,
			updated_at = excluded.updated_at
	`, key, value, source, time.Now().UTC().Format(time.RFC3339))
	return err
}

// GetIdentity returns one identity row, or (nil, nil) if absent.
func (c *Core) GetIdentity(ctx context.Context, key string) (*Identity, error) {
	if c.store == nil {
		return nil, errors.New("GetIdentity: no store configured")
	}
	row := c.store.DB().QueryRowContext(ctx, `
		SELECT key, value, source, updated_at FROM memory_identity WHERE key = ?
	`, key)
	var id Identity
	var ts string
	err := row.Scan(&id.Key, &id.Value, &id.Source, &ts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	id.UpdatedAt, _ = time.Parse(time.RFC3339, ts)
	return &id, nil
}

// ListIdentity returns every row in deterministic key order.
func (c *Core) ListIdentity(ctx context.Context) ([]Identity, error) {
	if c.store == nil {
		return nil, errors.New("ListIdentity: no store configured")
	}
	rows, err := c.store.DB().QueryContext(ctx, `
		SELECT key, value, source, updated_at FROM memory_identity ORDER BY key
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Identity
	for rows.Next() {
		var id Identity
		var ts string
		if err := rows.Scan(&id.Key, &id.Value, &id.Source, &ts); err != nil {
			return nil, err
		}
		id.UpdatedAt, _ = time.Parse(time.RFC3339, ts)
		out = append(out, id)
	}
	return out, rows.Err()
}

// DeleteIdentity removes a single identity row. Returns nil even if no
// row matched — identity is a flat key/value store with no ownership
// semantics, so "delete what isn't there" is not an error condition.
func (c *Core) DeleteIdentity(ctx context.Context, key string) error {
	if c.store == nil {
		return errors.New("DeleteIdentity: no store configured")
	}
	_, err := c.store.DB().ExecContext(ctx, `DELETE FROM memory_identity WHERE key = ?`, key)
	return err
}

// --- Pinned facts ------------------------------------------------------

// PinnedFact is one promotable fact with a salience score in [0,1].
// Decayed weekly; evicted below the floor + idle window.
type PinnedFact struct {
	ID            int64
	Text          string
	Salience      float64
	SourceCount   int
	UseCount      int
	LastUsed      *time.Time
	CreatedAt     time.Time
	LastDecayedAt time.Time
}

// PinFact inserts a new fact at the supplied salience (clamped to
// [0,1]). Returns the new row's id.
//
// Phase 3a callers (user manually pinning via the future TUI) pass
// salience=1.0 to mean "definitely keep this." Phase 3b's distiller
// will use lower values.
func (c *Core) PinFact(ctx context.Context, text string, salience float64) (int64, error) {
	if c.store == nil {
		return 0, errors.New("PinFact: no store configured")
	}
	if text == "" {
		return 0, errors.New("PinFact: empty text")
	}
	salience = clampUnit(salience)
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := c.store.DB().ExecContext(ctx, `
		INSERT INTO memory_pinned (text, salience, source_count, use_count, created_at, last_decayed_at)
		VALUES (?, ?, 1, 0, ?, ?)
	`, text, salience, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListPinned returns up to `limit` facts ordered by descending salience.
// Pass limit=0 to disable the cap.
func (c *Core) ListPinned(ctx context.Context, limit int) ([]PinnedFact, error) {
	if c.store == nil {
		return nil, errors.New("ListPinned: no store configured")
	}
	q := `SELECT id, text, salience, source_count, use_count, last_used, created_at, last_decayed_at
		  FROM memory_pinned ORDER BY salience DESC, id DESC`
	args := []any{}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := c.store.DB().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PinnedFact
	for rows.Next() {
		f, err := scanPinned(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

// GetPinned fetches one fact by id.
func (c *Core) GetPinned(ctx context.Context, id int64) (*PinnedFact, error) {
	if c.store == nil {
		return nil, errors.New("GetPinned: no store configured")
	}
	row := c.store.DB().QueryRowContext(ctx, `
		SELECT id, text, salience, source_count, use_count, last_used, created_at, last_decayed_at
		FROM memory_pinned WHERE id = ?`, id)
	f, err := scanPinned(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return f, err
}

// DeletePinned removes a fact. No-op if id doesn't match.
func (c *Core) DeletePinned(ctx context.Context, id int64) error {
	if c.store == nil {
		return errors.New("DeletePinned: no store configured")
	}
	_, err := c.store.DB().ExecContext(ctx, `DELETE FROM memory_pinned WHERE id = ?`, id)
	return err
}

// BumpPinnedUsage records that a fact was injected into a chat. Updates
// use_count and last_used. The retrieval planner (Phase 3b) calls this
// for every fact it surfaces.
func (c *Core) BumpPinnedUsage(ctx context.Context, id int64) error {
	if c.store == nil {
		return errors.New("BumpPinnedUsage: no store configured")
	}
	_, err := c.store.DB().ExecContext(ctx, `
		UPDATE memory_pinned
		SET use_count = use_count + 1, last_used = ?
		WHERE id = ?
	`, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func scanPinned(scan func(...any) error) (*PinnedFact, error) {
	var f PinnedFact
	var lastUsed sql.NullString
	var created, decayed string
	if err := scan(&f.ID, &f.Text, &f.Salience, &f.SourceCount, &f.UseCount,
		&lastUsed, &created, &decayed); err != nil {
		return nil, err
	}
	if lastUsed.Valid {
		t, _ := time.Parse(time.RFC3339, lastUsed.String)
		f.LastUsed = &t
	}
	f.CreatedAt, _ = time.Parse(time.RFC3339, created)
	f.LastDecayedAt, _ = time.Parse(time.RFC3339, decayed)
	return &f, nil
}

// --- Episodes ----------------------------------------------------------

// Episode is one chat's distilled record: summary, topics, named
// entities, decisions, action items. Inserted by Phase 3b's distiller
// at chat end (or on a 60s debounce).
type Episode struct {
	ID        int64
	ChatID    string // empty if the source chat was deleted
	Summary   string
	Topics    []string
	Entities  []string
	Decisions []string
	Actions   []string
	Salience  float64
	CreatedAt time.Time
}

// AddEpisode persists an episode. Phase 3a callers may use this for
// hand-authored test fixtures; the distiller in 3b is the production
// caller.
func (c *Core) AddEpisode(ctx context.Context, e *Episode) (int64, error) {
	if c.store == nil {
		return 0, errors.New("AddEpisode: no store configured")
	}
	if e == nil || e.Summary == "" {
		return 0, errors.New("AddEpisode: nil or empty summary")
	}
	topics, _ := json.Marshal(orEmpty(e.Topics))
	entities, _ := json.Marshal(orEmpty(e.Entities))
	decisions, _ := json.Marshal(orEmpty(e.Decisions))
	actions, _ := json.Marshal(orEmpty(e.Actions))
	chatID := nullableText(e.ChatID)
	salience := clampUnit(e.Salience)
	if e.Salience == 0 {
		salience = 0.5
	}
	res, err := c.store.DB().ExecContext(ctx, `
		INSERT INTO memory_episodes (chat_id, summary, topics_json, entities_json,
		    decisions_json, actions_json, salience, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, chatID, e.Summary, string(topics), string(entities), string(decisions), string(actions),
		salience, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListEpisodes returns the most recent episodes, newest first, up to
// limit. limit=0 returns everything.
func (c *Core) ListEpisodes(ctx context.Context, limit int) ([]Episode, error) {
	if c.store == nil {
		return nil, errors.New("ListEpisodes: no store configured")
	}
	q := `SELECT id, chat_id, summary, topics_json, entities_json,
		         decisions_json, actions_json, salience, created_at
		  FROM memory_episodes ORDER BY created_at DESC, id DESC`
	args := []any{}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := c.store.DB().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Episode
	for rows.Next() {
		e, err := scanEpisode(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// DeleteEpisode removes one episode by id.
func (c *Core) DeleteEpisode(ctx context.Context, id int64) error {
	if c.store == nil {
		return errors.New("DeleteEpisode: no store configured")
	}
	_, err := c.store.DB().ExecContext(ctx, `DELETE FROM memory_episodes WHERE id = ?`, id)
	return err
}

func scanEpisode(scan func(...any) error) (*Episode, error) {
	var e Episode
	var chatID sql.NullString
	var topics, entities, decisions, actions string
	var created string
	if err := scan(&e.ID, &chatID, &e.Summary, &topics, &entities, &decisions, &actions,
		&e.Salience, &created); err != nil {
		return nil, err
	}
	if chatID.Valid {
		e.ChatID = chatID.String
	}
	if err := json.Unmarshal([]byte(topics), &e.Topics); err != nil {
		return nil, fmt.Errorf("decode topics_json: %w", err)
	}
	if err := json.Unmarshal([]byte(entities), &e.Entities); err != nil {
		return nil, fmt.Errorf("decode entities_json: %w", err)
	}
	if err := json.Unmarshal([]byte(decisions), &e.Decisions); err != nil {
		return nil, fmt.Errorf("decode decisions_json: %w", err)
	}
	if err := json.Unmarshal([]byte(actions), &e.Actions); err != nil {
		return nil, fmt.Errorf("decode actions_json: %w", err)
	}
	e.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return &e, nil
}

// --- helpers -----------------------------------------------------------

func clampUnit(x float64) float64 {
	switch {
	case x < 0:
		return 0
	case x > 1:
		return 1
	default:
		return x
	}
}
