package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAddSchedule_RequiresAtOrCron(t *testing.T) {
	c := newMemCore(t)
	// Neither.
	if _, err := c.AddSchedule(context.Background(), AddScheduleRequest{AgentID: "x"}); err == nil {
		t.Fatal("expected error when neither At nor Cron set")
	}
	// Both.
	now := time.Now()
	if _, err := c.AddSchedule(context.Background(), AddScheduleRequest{
		AgentID: "x", At: &now, Cron: "*/5 * * * *",
	}); err == nil {
		t.Fatal("expected error when both At and Cron set")
	}
}

func TestAddSchedule_RequiresAgentOrChat(t *testing.T) {
	c := newMemCore(t)
	now := time.Now()
	if _, err := c.AddSchedule(context.Background(), AddScheduleRequest{At: &now}); err == nil {
		t.Fatal("expected AgentID-or-ChatID-required error")
	}
}

func TestAddSchedule_AtStoresAndDefaults(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	seedAgent(t, c, "x")
	at := time.Now().Add(5 * time.Minute)
	id, err := c.AddSchedule(ctx, AddScheduleRequest{
		AgentID: "x", At: &at, Workflow: "go",
	})
	if err != nil {
		t.Fatalf("AddSchedule: %v", err)
	}
	rows, _ := c.ListSchedules(ctx, false)
	if len(rows) != 1 || rows[0].ID != id {
		t.Fatalf("expected 1 schedule, got %+v", rows)
	}
	s := rows[0]
	if s.OnMiss != "skip" || s.Priority != "normal" {
		t.Fatalf("defaults not applied: %+v", s)
	}
	if s.At == nil || !s.At.Truncate(time.Second).Equal(at.Truncate(time.Second).UTC()) {
		t.Fatalf("at not round-tripped: stored=%v want=%v", s.At, at)
	}
	if s.NextRunAt == nil {
		t.Fatal("next_run_at should be pre-populated from At")
	}
}

func TestTickSchedules_FiresDueAtRows(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	seedAgent(t, c, "p")
	seedAgent(t, c, "f")
	now := time.Now()

	pastAt := now.Add(-1 * time.Minute)
	futureAt := now.Add(10 * time.Minute)

	_, _ = c.AddSchedule(ctx, AddScheduleRequest{AgentID: "p", At: &pastAt, Workflow: "go"})
	_, _ = c.AddSchedule(ctx, AddScheduleRequest{AgentID: "f", At: &futureAt, Workflow: "go"})

	fires, err := c.TickSchedules(ctx, now)
	if err != nil {
		t.Fatalf("TickSchedules: %v", err)
	}
	if len(fires) != 1 {
		t.Fatalf("expected 1 fire (past only), got %d", len(fires))
	}
	if fires[0].Schedule.AgentID != "p" {
		t.Fatalf("wrong fire: %+v", fires[0])
	}
}

func TestTickSchedules_RespectsOneShotSemantics(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	seedAgent(t, c, "p")
	now := time.Now()

	past := now.Add(-1 * time.Minute)
	id, _ := c.AddSchedule(ctx, AddScheduleRequest{AgentID: "p", At: &past, Workflow: "go"})

	// First tick fires.
	fires, _ := c.TickSchedules(ctx, now)
	if len(fires) != 1 {
		t.Fatalf("first tick: expected 1 fire, got %d", len(fires))
	}
	// Daemon marks it fired.
	if err := c.MarkScheduleFired(ctx, id, now); err != nil {
		t.Fatalf("MarkScheduleFired: %v", err)
	}
	// Second tick — same row should NOT fire again.
	fires, _ = c.TickSchedules(ctx, now.Add(time.Second))
	if len(fires) != 0 {
		t.Fatalf("second tick after MarkScheduleFired should skip, got %+v", fires)
	}
}

func TestTickSchedules_SkipsDisabled(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	seedAgent(t, c, "p")
	now := time.Now()
	past := now.Add(-1 * time.Minute)
	id, _ := c.AddSchedule(ctx, AddScheduleRequest{AgentID: "p", At: &past, Workflow: "go"})
	_ = c.SetScheduleEnabled(ctx, id, false)

	fires, _ := c.TickSchedules(ctx, now)
	if len(fires) != 0 {
		t.Fatalf("disabled schedule should not fire, got %+v", fires)
	}
}

func TestTickSchedules_SkipsCronRowsIn1_0a(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	seedAgent(t, c, "c")
	// Cron not yet implemented — row stores but ticks don't fire.
	_, err := c.AddSchedule(ctx, AddScheduleRequest{AgentID: "c", Cron: "* * * * *", Workflow: "go"})
	if err != nil {
		t.Fatalf("AddSchedule cron: %v", err)
	}
	fires, _ := c.TickSchedules(ctx, time.Now())
	if len(fires) != 0 {
		t.Fatalf("cron rows should not fire in 1.0a, got %+v", fires)
	}
}

func TestDeleteSchedule_NotFound(t *testing.T) {
	c := newMemCore(t)
	err := c.DeleteSchedule(context.Background(), 9999)
	if !errors.Is(err, ErrScheduleNotFound) {
		t.Fatalf("expected ErrScheduleNotFound, got %v", err)
	}
}
