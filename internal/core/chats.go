package core

// DB-backed chat persistence. New flows (Recipes pane, workflow runner)
// write here; the legacy on-disk workspace layout is unaffected and
// still surfaced via ListLegacyChats.
//
// One Chat row owns N Message rows (FK ON DELETE CASCADE) and
// optionally references an Agent. settings_json carries the per-chat
// memory / distill-endpoint preferences set in Phase 3c.
//
// Persistence is opt-in per workflow run — RunOptions.Persist toggles
// it. The runner creates a row, appends messages turn-by-turn, and
// stamps ended_at + exit_kind on the way out.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrChatNotFound is returned by GetChat / DeleteChat when no row
// matches.
var ErrChatNotFound = errors.New("chat not found")

// Chat is one DB-backed conversation. Mirrors the chats table row by
// row; settings_json deserialises into Settings.
type Chat struct {
	ID            string
	Title         string
	AgentID       string // empty if the chat had no PrAImate agent (rare; e.g. direct CLI launch)
	CLIAgent      string // "claude" | "codex" | ...
	WorkspacePath string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	EndedAt       *time.Time // nil while the chat is live
	ExitKind      string     // populated by EndChat; mirrors RunOutcome
	SessionID     string     // CLI adapter session id, for interactive resume
	Settings      ChatSettings
}

// ChatSettings persists into chats.settings_json. Per-chat overrides
// for the global memory toggle and the distillation endpoint go here.
// Add fields with `omitempty` so older rows round-trip without
// schema migration.
type ChatSettings struct {
	// DistillEndpoint, if set, overrides the global default for this
	// chat. Nil means "use global default."
	DistillEndpoint *DistillEndpoint `json:"distill_endpoint,omitempty"`

	// MemoryOverride lets a chat opt in to memory even when the
	// global toggle is off (positive) or opt out when global is on
	// (negative). Nil means "follow global."
	MemoryOverride *bool `json:"memory_override,omitempty"`

	// Model, if set, pins the CLI's model for every turn of this chat
	// (claude/openclaude --model, codex -m, opencode/praimate-code
	// --model provider/model, gemini -m). Empty means the CLI's own
	// default. CLIs without a model flag (deepseek) ignore it.
	Model string `json:"model,omitempty"`
}

// Message is one stored turn. Role is "user" | "assistant" | "tool" |
// "system". Meta carries adapter-specific metadata that doesn't fit
// the role/content shape (tool name, exit codes, etc.) — opaque to
// the rest of the system.
type Message struct {
	ID      int64
	ChatID  string
	TS      time.Time
	Role    string
	Content string
	Tokens  int64 // 0 if not measured
	Meta    map[string]any
}

// CreateChatRequest groups the fields needed to start one chat row.
type CreateChatRequest struct {
	ID            string // optional; auto-generated if empty
	Title         string
	AgentID       string
	CLIAgent      string
	WorkspacePath string
	Settings      ChatSettings
}

// CreateChat inserts a new chat row and returns it. ID is generated
// from the title + a UTC timestamp if not supplied.
func (c *Core) CreateChat(ctx context.Context, req CreateChatRequest) (*Chat, error) {
	if c.store == nil {
		return nil, errors.New("CreateChat: no store configured")
	}
	if req.CLIAgent == "" {
		return nil, errors.New("CreateChat: CLIAgent required")
	}
	if req.Title == "" {
		req.Title = "untitled"
	}
	now := time.Now().UTC()
	id := req.ID
	if id == "" {
		id = chatID(now, req.Title)
	}
	settings, err := json.Marshal(req.Settings)
	if err != nil {
		return nil, fmt.Errorf("marshal chat settings: %w", err)
	}
	_, err = c.store.DB().ExecContext(ctx, `
		INSERT INTO chats (id, title, agent_id, cli_agent, workspace_path,
		                   created_at, updated_at, settings_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, id, req.Title,
		nullableText(req.AgentID), req.CLIAgent,
		nullableText(req.WorkspacePath),
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		string(settings))
	if err != nil {
		return nil, fmt.Errorf("insert chat: %w", err)
	}
	return c.GetChat(ctx, id)
}

// GetChat fetches one chat row by id.
func (c *Core) GetChat(ctx context.Context, id string) (*Chat, error) {
	if c.store == nil {
		return nil, errors.New("GetChat: no store configured")
	}
	row := c.store.DB().QueryRowContext(ctx, chatSelectByID, id)
	ch, err := scanChat(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrChatNotFound, id)
	}
	return ch, err
}

// ListChats returns DB-backed chats newest-first, up to limit. limit=0
// returns everything.
func (c *Core) ListChats(ctx context.Context, limit int) ([]Chat, error) {
	if c.store == nil {
		return nil, errors.New("ListChats: no store configured")
	}
	q := chatSelectAll
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
	var out []Chat
	for rows.Next() {
		ch, err := scanChat(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *ch)
	}
	return out, rows.Err()
}

// EndChat stamps ended_at and exit_kind. Idempotent — re-ending a chat
// is allowed and just rewrites the stamps.
func (c *Core) EndChat(ctx context.Context, id, exitKind string) error {
	if c.store == nil {
		return errors.New("EndChat: no store configured")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := c.store.DB().ExecContext(ctx, `
		UPDATE chats SET ended_at = ?, exit_kind = ?, updated_at = ? WHERE id = ?
	`, now, exitKind, now, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrChatNotFound, id)
	}
	return nil
}

// DeleteChat removes one chat and all its messages (FK cascade).
func (c *Core) DeleteChat(ctx context.Context, id string) error {
	if c.store == nil {
		return errors.New("DeleteChat: no store configured")
	}
	res, err := c.store.DB().ExecContext(ctx, `DELETE FROM chats WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrChatNotFound, id)
	}
	return nil
}

// --- Messages ----------------------------------------------------------

// AddMessage persists one turn against chat id. Also bumps the chat's
// updated_at so list ordering reflects activity.
func (c *Core) AddMessage(ctx context.Context, chatID, role, content string, meta map[string]any) (*Message, error) {
	if c.store == nil {
		return nil, errors.New("AddMessage: no store configured")
	}
	if chatID == "" {
		return nil, errors.New("AddMessage: empty chatID")
	}
	if role == "" {
		return nil, errors.New("AddMessage: empty role")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	metaJSON, _ := json.Marshal(orEmptyMeta(meta))

	tx, err := c.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx, `
		INSERT INTO messages (chat_id, ts, role, content, meta_json)
		VALUES (?, ?, ?, ?, ?)
	`, chatID, now, role, content, string(metaJSON))
	if err != nil {
		return nil, fmt.Errorf("insert message: %w", err)
	}
	id, _ := res.LastInsertId()
	if _, err := tx.ExecContext(ctx, `UPDATE chats SET updated_at = ? WHERE id = ?`, now, chatID); err != nil {
		return nil, fmt.Errorf("touch chat updated_at: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Message{
		ID: id, ChatID: chatID, TS: time.Now().UTC(),
		Role: role, Content: content, Meta: meta,
	}, nil
}

// ListMessages returns every message for chat id in chronological
// order. limit=0 means everything.
func (c *Core) ListMessages(ctx context.Context, chatID string, limit int) ([]Message, error) {
	if c.store == nil {
		return nil, errors.New("ListMessages: no store configured")
	}
	q := `SELECT id, chat_id, ts, role, content, tokens, meta_json
	      FROM messages WHERE chat_id = ? ORDER BY id`
	args := []any{chatID}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := c.store.DB().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// --- helpers -----------------------------------------------------------

const (
	chatColumns = `id, title, agent_id, cli_agent, workspace_path,
		created_at, updated_at, ended_at, exit_kind, settings_json, session_id`

	chatSelectAll  = `SELECT ` + chatColumns + ` FROM chats ORDER BY updated_at DESC, id DESC`
	chatSelectByID = `SELECT ` + chatColumns + ` FROM chats WHERE id = ?`
)

func scanChat(scan func(...any) error) (*Chat, error) {
	var (
		ch                                                     Chat
		agentID, workspacePath, exitKind, endedAtNS, sessionID sql.NullString
		createdAt, updatedAt, settingsJSON                     string
	)
	err := scan(&ch.ID, &ch.Title, &agentID, &ch.CLIAgent, &workspacePath,
		&createdAt, &updatedAt, &endedAtNS, &exitKind, &settingsJSON, &sessionID)
	if err != nil {
		return nil, err
	}
	if agentID.Valid {
		ch.AgentID = agentID.String
	}
	if workspacePath.Valid {
		ch.WorkspacePath = workspacePath.String
	}
	if exitKind.Valid {
		ch.ExitKind = exitKind.String
	}
	if sessionID.Valid {
		ch.SessionID = sessionID.String
	}
	ch.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	ch.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	if endedAtNS.Valid {
		t, _ := time.Parse(time.RFC3339, endedAtNS.String)
		ch.EndedAt = &t
	}
	if err := json.Unmarshal([]byte(settingsJSON), &ch.Settings); err != nil {
		return nil, fmt.Errorf("decode settings_json: %w", err)
	}
	return &ch, nil
}

func scanMessage(scan func(...any) error) (*Message, error) {
	var (
		m        Message
		tokens   sql.NullInt64
		metaJSON string
		ts       string
	)
	err := scan(&m.ID, &m.ChatID, &ts, &m.Role, &m.Content, &tokens, &metaJSON)
	if err != nil {
		return nil, err
	}
	if tokens.Valid {
		m.Tokens = tokens.Int64
	}
	m.TS, _ = time.Parse(time.RFC3339, ts)
	_ = json.Unmarshal([]byte(metaJSON), &m.Meta)
	return &m, nil
}

func orEmptyMeta(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// chatID builds a stable id from the title and the creation time. The
// timestamp prefix keeps natural sort order; the slug makes ls output
// scannable.
func chatID(t time.Time, title string) string {
	slug := chatSlug(title)
	if slug == "" {
		slug = "chat"
	}
	return fmt.Sprintf("%s-%s", t.Format("20060102-150405"), slug)
}

func chatSlug(s string) string {
	b := make([]byte, 0, len(s))
	prevDash := true
	for i := 0; i < len(s) && len(b) < 32; i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b = append(b, c)
			prevDash = false
		case c >= 'A' && c <= 'Z':
			b = append(b, c+('a'-'A'))
			prevDash = false
		case !prevDash:
			b = append(b, '-')
			prevDash = true
		}
	}
	// Trim trailing dash.
	for len(b) > 0 && b[len(b)-1] == '-' {
		b = b[:len(b)-1]
	}
	return string(b)
}
