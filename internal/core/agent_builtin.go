package core

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

//go:embed builtin/*.yaml
var builtinFS embed.FS

// BuiltinAgents returns every agent shipped in the binary, parsed and
// validated. Used both for the GUI's "create from built-in" affordance
// and for SeedBuiltins() which installs them into the user's DB on
// first launch.
func BuiltinAgents() ([]*Agent, error) {
	entries, err := fs.ReadDir(builtinFS, "builtin")
	if err != nil {
		return nil, fmt.Errorf("read builtin agents: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	out := make([]*Agent, 0, len(names))
	for _, name := range names {
		body, err := fs.ReadFile(builtinFS, path.Join("builtin", name))
		if err != nil {
			return nil, fmt.Errorf("read builtin %s: %w", name, err)
		}
		a, err := ParseAgentYAML(strings.NewReader(string(body)))
		if err != nil {
			return nil, fmt.Errorf("parse builtin %s: %w", name, err)
		}
		a.SourcePath = "builtin:" + name
		out = append(out, a)
	}
	return out, nil
}

// SeedBuiltins upserts every built-in agent into the user's DB and
// removes stale builtins (rows whose source_path is "builtin:..." but
// which no longer ship in the binary).
//
// Ownership rule: a builtin row is refreshed on startup ONLY while its
// source_path still says "builtin:<id>". The moment the user edits it
// (the YAML editor / import save with an empty source path), the row
// is theirs — re-seeding must NOT clobber their tweaks on the next
// launch. Deleting the row brings the pristine builtin back.
//
// Returns the number of agents seeded.
func (c *Core) SeedBuiltins(ctx context.Context) (int, error) {
	if c.store == nil {
		return 0, nil
	}
	agents, err := BuiltinAgents()
	if err != nil {
		return 0, err
	}
	current := make(map[string]bool, len(agents))
	for _, a := range agents {
		current[a.ID] = true
		var src sql.NullString
		err := c.store.DB().QueryRowContext(ctx,
			`SELECT source_path FROM agents WHERE id = ?`, a.ID).Scan(&src)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			// absent — seed it
		case err != nil:
			return 0, fmt.Errorf("probe builtin %s: %w", a.ID, err)
		case !src.Valid || !strings.HasPrefix(src.String, "builtin:"):
			continue // user-owned now — hands off
		}
		if _, err := c.upsertAgent(ctx, a); err != nil {
			return 0, fmt.Errorf("seed %s: %w", a.ID, err)
		}
	}

	// Evict builtins that shipped in an earlier release but were
	// dropped (e.g. the original freeform/tdd-coder/release-engineer
	// set). Only rows tagged builtin:* are eligible — user imports are
	// safe.
	rows, err := c.store.DB().QueryContext(ctx,
		`SELECT id FROM agents WHERE source_path LIKE 'builtin:%'`)
	if err != nil {
		return 0, fmt.Errorf("scan builtins for eviction: %w", err)
	}
	var stale []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		if !current[id] {
			stale = append(stale, id)
		}
	}
	rows.Close()
	for _, id := range stale {
		if _, err := c.store.DB().ExecContext(ctx, `DELETE FROM agents WHERE id = ?`, id); err != nil {
			return 0, fmt.Errorf("evict stale builtin %s: %w", id, err)
		}
	}

	return len(agents), nil
}
