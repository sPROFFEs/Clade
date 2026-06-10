package core

import (
	"context"
	"math"
	"testing"
	"time"
)

func newMemCore(t *testing.T) *Core {
	t.Helper()
	c, _ := New(Options{Store: openTempStore(t)})
	return c
}

// --- Identity ----------------------------------------------------------

func TestIdentity_SetGetListDelete(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()

	if err := c.SetIdentity(ctx, "name", "Julio", ""); err != nil {
		t.Fatalf("SetIdentity: %v", err)
	}
	got, err := c.GetIdentity(ctx, "name")
	if err != nil || got == nil {
		t.Fatalf("GetIdentity: %v / %v", got, err)
	}
	if got.Value != "Julio" || got.Source != "manual" {
		t.Fatalf("unexpected identity: %+v", got)
	}

	// Update — same key replaces value.
	if err := c.SetIdentity(ctx, "name", "Julio D.", "manual"); err != nil {
		t.Fatalf("SetIdentity update: %v", err)
	}
	got, _ = c.GetIdentity(ctx, "name")
	if got.Value != "Julio D." {
		t.Fatalf("update didn't take: %+v", got)
	}

	// Second key, then list.
	_ = c.SetIdentity(ctx, "role", "engineer", "")
	rows, _ := c.ListIdentity(ctx)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].Key != "name" || rows[1].Key != "role" {
		t.Fatalf("list order is not alphabetical: %+v", rows)
	}

	if err := c.DeleteIdentity(ctx, "name"); err != nil {
		t.Fatalf("DeleteIdentity: %v", err)
	}
	got, _ = c.GetIdentity(ctx, "name")
	if got != nil {
		t.Fatal("expected name to be deleted")
	}
}

func TestIdentity_MissingKey_ReturnsNilNotError(t *testing.T) {
	c := newMemCore(t)
	got, err := c.GetIdentity(context.Background(), "ghost")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil identity, got %+v", got)
	}
}

func TestIdentity_RejectsEmptyKey(t *testing.T) {
	c := newMemCore(t)
	if err := c.SetIdentity(context.Background(), "", "x", ""); err == nil {
		t.Fatal("expected empty-key error")
	}
}

// --- Pinned facts ------------------------------------------------------

func TestPinned_CRUD(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()

	id, err := c.PinFact(ctx, "user prefers spaces over tabs", 0.8)
	if err != nil {
		t.Fatalf("PinFact: %v", err)
	}
	got, err := c.GetPinned(ctx, id)
	if err != nil || got == nil {
		t.Fatalf("GetPinned: %v / %v", got, err)
	}
	if got.Salience != 0.8 || got.SourceCount != 1 || got.UseCount != 0 {
		t.Fatalf("unexpected fact: %+v", got)
	}

	// Bump usage.
	if err := c.BumpPinnedUsage(ctx, id); err != nil {
		t.Fatalf("BumpPinnedUsage: %v", err)
	}
	got, _ = c.GetPinned(ctx, id)
	if got.UseCount != 1 || got.LastUsed == nil {
		t.Fatalf("usage bump didn't take: %+v", got)
	}

	// Delete.
	if err := c.DeletePinned(ctx, id); err != nil {
		t.Fatalf("DeletePinned: %v", err)
	}
	got, _ = c.GetPinned(ctx, id)
	if got != nil {
		t.Fatal("expected fact deleted")
	}
}

func TestPinned_ListOrdersBySalienceDesc(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	_, _ = c.PinFact(ctx, "low", 0.2)
	_, _ = c.PinFact(ctx, "high", 0.9)
	_, _ = c.PinFact(ctx, "mid", 0.5)
	rows, _ := c.ListPinned(ctx, 0)
	if len(rows) != 3 || rows[0].Text != "high" || rows[1].Text != "mid" || rows[2].Text != "low" {
		t.Fatalf("unexpected order: %+v", rows)
	}
	rows, _ = c.ListPinned(ctx, 2)
	if len(rows) != 2 {
		t.Fatalf("limit=2 returned %d rows", len(rows))
	}
}

func TestPinned_SalienceClamped(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	idLow, _ := c.PinFact(ctx, "neg", -3)
	idHigh, _ := c.PinFact(ctx, "huge", 99)
	low, _ := c.GetPinned(ctx, idLow)
	hi, _ := c.GetPinned(ctx, idHigh)
	if low.Salience != 0 || hi.Salience != 1 {
		t.Fatalf("clamp failed: low=%v high=%v", low.Salience, hi.Salience)
	}
}

// --- Episodes ----------------------------------------------------------

func TestEpisode_AddListDelete(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	// ChatID intentionally empty (NULL) — chats FK requires the row to
	// exist, and we want to test the orphan-chat path the distiller
	// hits when its source chat has been deleted before distillation
	// runs. A round-trip test that exercises the FK side lives in
	// chats_test.go (Phase 3b).
	id, err := c.AddEpisode(ctx, &Episode{
		Summary:   "Refactored launcher.",
		Topics:    []string{"refactor", "launcher"},
		Entities:  []string{"slice.go"},
		Decisions: []string{"keep transcript on disk"},
		Actions:   []string{"write release notes"},
		Salience:  0.7,
	})
	if err != nil {
		t.Fatalf("AddEpisode: %v", err)
	}
	if id <= 0 {
		t.Fatalf("bad id %d", id)
	}

	eps, _ := c.ListEpisodes(ctx, 0)
	if len(eps) != 1 {
		t.Fatalf("expected 1 episode, got %d", len(eps))
	}
	e := eps[0]
	if e.Summary != "Refactored launcher." || len(e.Topics) != 2 || e.Salience != 0.7 {
		t.Fatalf("episode lost data: %+v", e)
	}

	if err := c.DeleteEpisode(ctx, id); err != nil {
		t.Fatalf("DeleteEpisode: %v", err)
	}
	eps, _ = c.ListEpisodes(ctx, 0)
	if len(eps) != 0 {
		t.Fatalf("expected delete, got %d episodes", len(eps))
	}
}

func TestEpisode_RejectsEmptySummary(t *testing.T) {
	c := newMemCore(t)
	if _, err := c.AddEpisode(context.Background(), &Episode{Summary: ""}); err == nil {
		t.Fatal("expected empty-summary error")
	}
}

func TestEpisode_ListNewestFirst(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_, _ = c.AddEpisode(ctx, &Episode{Summary: "ep " + string(rune('A'+i))})
		time.Sleep(2 * time.Millisecond)
	}
	eps, _ := c.ListEpisodes(ctx, 0)
	if len(eps) != 3 {
		t.Fatalf("expected 3, got %d", len(eps))
	}
	if eps[0].Summary != "ep C" || eps[2].Summary != "ep A" {
		t.Fatalf("not newest-first: %+v", eps)
	}
}

// --- Decay -------------------------------------------------------------

func TestDecayPinned_AppliesHalfLifeFormula(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()

	// Pin a fact whose last_decayed_at is "now". Decay should be a no-op.
	id, _ := c.PinFact(ctx, "fresh", 1.0)
	res, err := c.DecayPinned(ctx, time.Now(), MaintenanceConfig{})
	if err != nil {
		t.Fatalf("DecayPinned: %v", err)
	}
	if res.Decayed != 0 {
		t.Fatalf("fresh fact decayed: %+v", res)
	}
	got, _ := c.GetPinned(ctx, id)
	if got.Salience < 0.999 {
		t.Fatalf("salience changed unexpectedly: %v", got.Salience)
	}

	// Push last_decayed_at back 30 days and re-run.
	thirty := time.Now().UTC().Add(-30 * 24 * time.Hour).Format(time.RFC3339)
	_, _ = c.store.DB().ExecContext(ctx, `UPDATE memory_pinned SET last_decayed_at = ? WHERE id = ?`, thirty, id)

	res, err = c.DecayPinned(ctx, time.Now(), MaintenanceConfig{})
	if err != nil {
		t.Fatalf("DecayPinned: %v", err)
	}
	if res.Decayed != 1 {
		t.Fatalf("expected 1 decayed, got %+v", res)
	}
	got, _ = c.GetPinned(ctx, id)
	// 30 days at half-life 30 = exp(-1) ≈ 0.3679.
	want := math.Exp(-1)
	if math.Abs(got.Salience-want) > 0.01 {
		t.Fatalf("salience after decay = %v, want ~%v", got.Salience, want)
	}
}

// --- Evict -------------------------------------------------------------

func TestEvictPinned_OnlyBelowFloorAndIdle(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()

	// Three facts: one below floor + idle (evict), one below floor + recent
	// (keep), one above floor + idle (keep).
	idEvict, _ := c.PinFact(ctx, "evict me", 0.1)
	idKeepRecent, _ := c.PinFact(ctx, "low but used", 0.05)
	idKeepHigh, _ := c.PinFact(ctx, "high salience", 0.9)

	old := time.Now().UTC().Add(-60 * 24 * time.Hour).Format(time.RFC3339)
	recent := time.Now().UTC().Format(time.RFC3339)

	_, _ = c.store.DB().ExecContext(ctx, `UPDATE memory_pinned SET created_at = ?, last_used = NULL WHERE id = ?`, old, idEvict)
	_, _ = c.store.DB().ExecContext(ctx, `UPDATE memory_pinned SET last_used = ? WHERE id = ?`, recent, idKeepRecent)
	_, _ = c.store.DB().ExecContext(ctx, `UPDATE memory_pinned SET created_at = ?, last_used = NULL WHERE id = ?`, old, idKeepHigh)

	res, err := c.EvictPinned(ctx, time.Now(), MaintenanceConfig{})
	if err != nil {
		t.Fatalf("EvictPinned: %v", err)
	}
	if res.Removed != 1 {
		t.Fatalf("expected 1 removed, got %d", res.Removed)
	}
	if got, _ := c.GetPinned(ctx, idEvict); got != nil {
		t.Fatal("evict-target still present")
	}
	if got, _ := c.GetPinned(ctx, idKeepRecent); got == nil {
		t.Fatal("low-but-recent fact was removed")
	}
	if got, _ := c.GetPinned(ctx, idKeepHigh); got == nil {
		t.Fatal("high-salience fact was removed")
	}
}

// --- Promote -----------------------------------------------------------

func TestPromotePinned_BumpsFactsAppearingInEnoughEpisodes(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()

	id, _ := c.PinFact(ctx, "spaces over tabs", 0.3)

	// Three episodes mentioning the needle, one not.
	_, _ = c.AddEpisode(ctx, &Episode{Summary: "User insists on spaces over tabs for consistency."})
	_, _ = c.AddEpisode(ctx, &Episode{Summary: "Refactor uses spaces over tabs throughout."})
	_, _ = c.AddEpisode(ctx, &Episode{Summary: "Spaces over tabs is non-negotiable."})
	_, _ = c.AddEpisode(ctx, &Episode{Summary: "Unrelated work on the build script."})

	res, err := c.PromotePinned(ctx, time.Now(), MaintenanceConfig{})
	if err != nil {
		t.Fatalf("PromotePinned: %v", err)
	}
	if res.Promoted != 1 {
		t.Fatalf("expected 1 promoted, got %+v", res)
	}
	got, _ := c.GetPinned(ctx, id)
	if got.Salience != 1.0 || got.SourceCount != 3 {
		t.Fatalf("promote did not bump: %+v", got)
	}
}

func TestPromotePinned_RequiresThreshold(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	id, _ := c.PinFact(ctx, "rare topic", 0.3)
	_, _ = c.AddEpisode(ctx, &Episode{Summary: "Mentions rare topic once."})
	_, _ = c.AddEpisode(ctx, &Episode{Summary: "Different subject."})

	res, err := c.PromotePinned(ctx, time.Now(), MaintenanceConfig{})
	if err != nil {
		t.Fatalf("PromotePinned: %v", err)
	}
	if res.Promoted != 0 {
		t.Fatalf("expected 0 promotions (1 hit < threshold 3), got %+v", res)
	}
	got, _ := c.GetPinned(ctx, id)
	if got.Salience != 0.3 {
		t.Fatalf("salience unexpectedly changed: %v", got.Salience)
	}
}
