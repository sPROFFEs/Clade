package core

// Memory maintenance jobs — pure functions over the SQLite tables.
// Scheduling is the caller's responsibility:
//
//   - DecayPinned runs on every PrAImate startup if `now -
//     last_decayed_at > 7d`. The check is per-row so a stale subset
//     decays even when most facts are fresh.
//   - PromotePinned runs on a daily timer (or manually from the future
//     Memory TUI). It scans recent episodes and bumps the salience of
//     facts that appear in many of them.
//   - EvictPinned runs after DecayPinned; removes facts whose post-
//     decay salience fell below the floor AND that haven't been used
//     for `idleDays`.
//
// Default tunables match Osaurus's settings (plan §1 reference): salience
// half-life 30d, eviction floor 0.2, idle window 30d, promotion
// threshold 3 episodes. Phase 4 may expose these via settings_cli /
// settings_gui rows.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"
)

// MaintenanceConfig groups the tunables for memory maintenance.
// Zero values are fine — the defaults at the top of each method apply
// when a field is zero.
type MaintenanceConfig struct {
	// DecayHalfLifeDays controls how fast salience falls. The decay
	// multiplier per day is exp(-1/HalfLifeDays). Default: 30.
	DecayHalfLifeDays float64

	// EvictionFloor is the salience below which a fact becomes
	// eligible for eviction. Default: 0.2.
	EvictionFloor float64

	// EvictionIdleDays is how long since LastUsed (or CreatedAt if
	// never used) a fact must be quiet before eviction. Default: 30.
	EvictionIdleDays float64

	// PromotionThreshold is how many recent episodes a fact must
	// appear in to be promoted (salience set to 1.0). Default: 3.
	PromotionThreshold int

	// PromotionWindowDays bounds the "recent" window for the
	// promotion search. Default: 14.
	PromotionWindowDays float64
}

func (m MaintenanceConfig) decayHalfLife() float64 {
	if m.DecayHalfLifeDays > 0 {
		return m.DecayHalfLifeDays
	}
	return 30
}
func (m MaintenanceConfig) floor() float64 {
	if m.EvictionFloor > 0 {
		return m.EvictionFloor
	}
	return 0.2
}
func (m MaintenanceConfig) idle() float64 {
	if m.EvictionIdleDays > 0 {
		return m.EvictionIdleDays
	}
	return 30
}
func (m MaintenanceConfig) promotionThreshold() int {
	if m.PromotionThreshold > 0 {
		return m.PromotionThreshold
	}
	return 3
}
func (m MaintenanceConfig) promotionWindow() float64 {
	if m.PromotionWindowDays > 0 {
		return m.PromotionWindowDays
	}
	return 14
}

// DecayResult reports what DecayPinned did. Useful for logging and
// for the Memory TUI's "last maintenance" status line.
type DecayResult struct {
	Scanned int     // rows examined
	Decayed int     // rows whose salience changed
	NowUTC  string  // ISO timestamp written into last_decayed_at
}

// DecayPinned scales each fact's salience by exp(-Δdays / halfLife)
// where Δdays is the time since its last_decayed_at. Updates the row
// in place. Caller passes `now` so tests can control the clock.
func (c *Core) DecayPinned(ctx context.Context, now time.Time, cfg MaintenanceConfig) (*DecayResult, error) {
	if c.store == nil {
		return nil, errors.New("DecayPinned: no store configured")
	}
	halfLife := cfg.decayHalfLife()

	rows, err := c.store.DB().QueryContext(ctx, `
		SELECT id, salience, last_decayed_at FROM memory_pinned
	`)
	if err != nil {
		return nil, err
	}
	type bucket struct {
		id          int64
		newSalience float64
	}
	var changed []bucket
	scanned := 0
	for rows.Next() {
		var id int64
		var sal float64
		var lastDecayedStr string
		if err := rows.Scan(&id, &sal, &lastDecayedStr); err != nil {
			rows.Close()
			return nil, err
		}
		scanned++
		lastDecayed, _ := time.Parse(time.RFC3339, lastDecayedStr)
		days := now.Sub(lastDecayed).Hours() / 24
		if days <= 0 {
			continue
		}
		factor := math.Exp(-days / halfLife)
		newSal := clampUnit(sal * factor)
		if !approx(newSal, sal) {
			changed = append(changed, bucket{id: id, newSalience: newSal})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	nowStr := now.UTC().Format(time.RFC3339)
	for _, b := range changed {
		if _, err := c.store.DB().ExecContext(ctx, `
			UPDATE memory_pinned SET salience = ?, last_decayed_at = ? WHERE id = ?
		`, b.newSalience, nowStr, b.id); err != nil {
			return nil, fmt.Errorf("update pinned %d: %w", b.id, err)
		}
	}
	// Also stamp last_decayed_at on every untouched row so the next
	// decay run computes Δ from this moment, not the original create
	// timestamp. Cheap single UPDATE.
	if _, err := c.store.DB().ExecContext(ctx, `
		UPDATE memory_pinned SET last_decayed_at = ?
		WHERE last_decayed_at <> ?
	`, nowStr, nowStr); err != nil {
		return nil, err
	}

	return &DecayResult{Scanned: scanned, Decayed: len(changed), NowUTC: nowStr}, nil
}

// EvictResult reports what EvictPinned removed.
type EvictResult struct {
	Removed int
}

// EvictPinned deletes facts whose salience is below cfg.EvictionFloor
// AND whose last_used (or created_at if never used) is older than
// cfg.EvictionIdleDays.
func (c *Core) EvictPinned(ctx context.Context, now time.Time, cfg MaintenanceConfig) (*EvictResult, error) {
	if c.store == nil {
		return nil, errors.New("EvictPinned: no store configured")
	}
	cutoff := now.Add(-time.Duration(cfg.idle()*24) * time.Hour).UTC().Format(time.RFC3339)
	res, err := c.store.DB().ExecContext(ctx, `
		DELETE FROM memory_pinned
		WHERE salience < ?
		  AND COALESCE(last_used, created_at) < ?
	`, cfg.floor(), cutoff)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	return &EvictResult{Removed: int(n)}, nil
}

// PromoteResult reports what PromotePinned did.
type PromoteResult struct {
	Scanned  int // episodes examined
	Promoted int // facts whose salience was set to 1.0
}

// PromotePinned looks at every fact and checks whether its `text` appears
// (as a substring, case-insensitive) in cfg.PromotionThreshold or more
// recent episodes (within cfg.PromotionWindowDays). Each promoted fact
// has salience set to 1.0 and source_count bumped to match the matching
// episode count.
//
// This is a simple O(facts × episodes_in_window) scan. Cheap for the
// scales we expect (≤ thousands of facts, ≤ thousands of episodes);
// if it grows past that we add an FTS index in a follow-up migration.
func (c *Core) PromotePinned(ctx context.Context, now time.Time, cfg MaintenanceConfig) (*PromoteResult, error) {
	if c.store == nil {
		return nil, errors.New("PromotePinned: no store configured")
	}
	cutoff := now.Add(-time.Duration(cfg.promotionWindow()*24) * time.Hour).UTC().Format(time.RFC3339)

	eps, err := c.store.DB().QueryContext(ctx, `
		SELECT summary || ' ' || topics_json || ' ' || entities_json
		FROM memory_episodes WHERE created_at >= ?
	`, cutoff)
	if err != nil {
		return nil, err
	}
	var corpus []string
	for eps.Next() {
		var blob string
		if err := eps.Scan(&blob); err != nil {
			eps.Close()
			return nil, err
		}
		corpus = append(corpus, lower(blob))
	}
	if err := eps.Err(); err != nil {
		eps.Close()
		return nil, err
	}
	eps.Close()

	facts, err := c.ListPinned(ctx, 0)
	if err != nil {
		return nil, err
	}
	threshold := cfg.promotionThreshold()
	res := &PromoteResult{Scanned: len(corpus)}
	for _, f := range facts {
		needle := lower(f.Text)
		if needle == "" {
			continue
		}
		hits := 0
		for _, blob := range corpus {
			if containsString(blob, needle) {
				hits++
			}
		}
		if hits >= threshold {
			_, err := c.store.DB().ExecContext(ctx, `
				UPDATE memory_pinned
				SET salience = 1.0, source_count = ?
				WHERE id = ?
			`, hits, f.ID)
			if err != nil {
				return nil, fmt.Errorf("promote %d: %w", f.ID, err)
			}
			res.Promoted++
		}
	}
	return res, nil
}

// approx returns true if a and b are within 1e-6. Salience values are
// stored as REAL; tiny rounding differences shouldn't count as
// "changed."
func approx(a, b float64) bool {
	return math.Abs(a-b) < 1e-6
}

// lower / containsString — package-local copies so this file doesn't
// pull in strings just to call ToLower + Contains. Cheap and explicit.

func lower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func containsString(haystack, needle string) bool {
	hl, nl := len(haystack), len(needle)
	if nl == 0 {
		return true
	}
	if nl > hl {
		return false
	}
	for i := 0; i <= hl-nl; i++ {
		match := true
		for j := 0; j < nl; j++ {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
