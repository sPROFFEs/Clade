package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

// seedAgent inserts a minimal agent so watchers/schedules can reference
// it without tripping the FK constraint. Returns the agent id.
func seedAgent(t *testing.T, c *Core, id string) string {
	t.Helper()
	_, err := c.upsertAgent(context.Background(), &Agent{
		ID:           id,
		Name:         id,
		Instructions: "x",
		Supports:     []string{"claude"},
		Workflows: []Workflow{{
			Name:  "go",
			Steps: []WorkflowStep{{Kind: StepUserMessage, Template: "x"}},
		}},
	})
	if err != nil {
		t.Fatalf("seedAgent %q: %v", id, err)
	}
	return id
}

func TestAddWatcher_RequiresPath(t *testing.T) {
	c := newMemCore(t)
	_, err := c.AddWatcher(context.Background(), AddWatcherRequest{AgentID: "x"})
	if err == nil {
		t.Fatal("expected Path-required error")
	}
}

func TestAddWatcher_RequiresAgentOrChat(t *testing.T) {
	c := newMemCore(t)
	_, err := c.AddWatcher(context.Background(), AddWatcherRequest{Path: "/tmp"})
	if err == nil {
		t.Fatal("expected AgentID-or-ChatID-required error")
	}
}

func TestAddWatcher_AppliesDefaultDebounce(t *testing.T) {
	c := newMemCore(t)
	seedAgent(t, c, "x")
	id, err := c.AddWatcher(context.Background(), AddWatcherRequest{
		AgentID: "x", Path: "/tmp",
	})
	if err != nil {
		t.Fatalf("AddWatcher: %v", err)
	}
	rows, _ := c.ListWatchers(context.Background(), false)
	if len(rows) != 1 {
		t.Fatalf("expected 1 watcher, got %d", len(rows))
	}
	if rows[0].DebounceMs != 1000 {
		t.Fatalf("expected default 1000ms debounce, got %d", rows[0].DebounceMs)
	}
	if !rows[0].Enabled {
		t.Fatal("new watcher should be enabled by default")
	}
	if rows[0].ID != id {
		t.Fatalf("id mismatch: row=%d returned=%d", rows[0].ID, id)
	}
}

func TestSetWatcherEnabled_FlipsAndFilters(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	seedAgent(t, c, "x")
	id, _ := c.AddWatcher(ctx, AddWatcherRequest{AgentID: "x", Path: "/tmp"})

	if err := c.SetWatcherEnabled(ctx, id, false); err != nil {
		t.Fatalf("SetWatcherEnabled: %v", err)
	}
	enabled, _ := c.ListWatchers(ctx, true)
	if len(enabled) != 0 {
		t.Fatalf("disabled watcher should not appear in enabled-only list: %+v", enabled)
	}
	all, _ := c.ListWatchers(ctx, false)
	if len(all) != 1 || all[0].Enabled {
		t.Fatalf("watcher row missing or wrong flag: %+v", all)
	}
}

func TestDeleteWatcher_NotFound(t *testing.T) {
	c := newMemCore(t)
	err := c.DeleteWatcher(context.Background(), 9999)
	if !errors.Is(err, ErrWatcherNotFound) {
		t.Fatalf("expected ErrWatcherNotFound, got %v", err)
	}
}

func TestMatchWatchers_PathScoping(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	ResetDebounceLog()
	seedAgent(t, c, "a")
	seedAgent(t, c, "b")

	_, _ = c.AddWatcher(ctx, AddWatcherRequest{
		AgentID: "a", Path: "/project/a", Patterns: nil, DebounceMs: 0,
	})
	_, _ = c.AddWatcher(ctx, AddWatcherRequest{
		AgentID: "b", Path: "/project/b", Patterns: nil, DebounceMs: 0,
	})

	hits, _ := c.MatchWatchers(ctx, WatcherEvent{Path: "/project/a/x.go", Op: "write"}, time.Now())
	if len(hits) != 1 || hits[0].Watcher.AgentID != "a" {
		t.Fatalf("expected only /project/a watcher to fire, got %+v", hits)
	}

	hits, _ = c.MatchWatchers(ctx, WatcherEvent{Path: "/elsewhere/c.go", Op: "write"}, time.Now())
	if len(hits) != 0 {
		t.Fatalf("expected no fires for unmatched path, got %+v", hits)
	}
}

func TestMatchWatchers_PatternGlob(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	ResetDebounceLog()
	seedAgent(t, c, "a")

	_, _ = c.AddWatcher(ctx, AddWatcherRequest{
		AgentID: "a", Path: "/p", Patterns: []string{"*.go"}, DebounceMs: 0,
	})

	if hits, _ := c.MatchWatchers(ctx, WatcherEvent{Path: "/p/main.go"}, time.Now()); len(hits) != 1 {
		t.Fatalf("expected *.go to match /p/main.go, got %+v", hits)
	}
	if hits, _ := c.MatchWatchers(ctx, WatcherEvent{Path: "/p/main.py"}, time.Now()); len(hits) != 0 {
		t.Fatalf("expected *.go NOT to match /p/main.py, got %+v", hits)
	}
}

func TestMatchWatchers_DebounceSuppressesRepeats(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	ResetDebounceLog()
	seedAgent(t, c, "a")

	_, _ = c.AddWatcher(ctx, AddWatcherRequest{
		AgentID: "a", Path: "/p", DebounceMs: 100,
	})

	now := time.Now()
	first, _ := c.MatchWatchers(ctx, WatcherEvent{Path: "/p/a.go"}, now)
	if len(first) != 1 {
		t.Fatalf("first fire should match, got %d", len(first))
	}
	// 50ms later — still within debounce window.
	second, _ := c.MatchWatchers(ctx, WatcherEvent{Path: "/p/a.go"}, now.Add(50*time.Millisecond))
	if len(second) != 0 {
		t.Fatalf("debounce should suppress, got %d", len(second))
	}
	// 200ms later — outside window.
	third, _ := c.MatchWatchers(ctx, WatcherEvent{Path: "/p/a.go"}, now.Add(200*time.Millisecond))
	if len(third) != 1 {
		t.Fatalf("debounce window passed, should fire again, got %d", len(third))
	}
}

func TestMatchWatchers_SkipsDisabled(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	ResetDebounceLog()
	seedAgent(t, c, "a")
	id, _ := c.AddWatcher(ctx, AddWatcherRequest{AgentID: "a", Path: "/p"})
	_ = c.SetWatcherEnabled(ctx, id, false)

	hits, _ := c.MatchWatchers(ctx, WatcherEvent{Path: "/p/a.go"}, time.Now())
	if len(hits) != 0 {
		t.Fatalf("disabled watcher should not fire, got %+v", hits)
	}
}
