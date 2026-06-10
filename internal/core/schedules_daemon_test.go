package core

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestDispatchDueSchedules_AgentScopedRunsWorkflow(t *testing.T) {
	mock := &mockAdapter{name: "claude", replies: []string{"done"}}
	withMockAdapter(t, mock)

	c := newMemCore(t)
	ctx := context.Background()
	a := seedScheduleAgent(t, c, "agent-a", []string{"claude"})
	cwd := t.TempDir()
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	at := now.Add(-time.Minute)
	id, err := c.AddSchedule(ctx, AddScheduleRequest{
		AgentID:  a.ID,
		At:       &at,
		Workflow: "go",
		Inputs: map[string]string{
			"task": "daily review",
		},
	})
	if err != nil {
		t.Fatalf("AddSchedule: %v", err)
	}

	runs, err := c.DispatchDueSchedules(ctx, ScheduleDispatchOptions{
		CLI: "claude",
		Cwd: cwd,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("DispatchDueSchedules: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].Result.Outcome != OutcomeCompleted {
		t.Fatalf("run outcome=%s err=%v", runs[0].Result.Outcome, runs[0].Result.Err)
	}
	if len(mock.shots) != 1 {
		t.Fatalf("expected one CLI shot, got %d", len(mock.shots))
	}
	body := mock.shots[0].Message
	if !strings.Contains(body, "daily review") || !strings.Contains(body, now.Format(time.RFC3339Nano)) {
		t.Fatalf("unexpected rendered schedule body: %q", body)
	}
	if mock.shots[0].Cwd != cwd {
		t.Fatalf("Cwd mismatch: %q", mock.shots[0].Cwd)
	}

	fires, err := c.TickSchedules(ctx, now.Add(time.Second))
	if err != nil {
		t.Fatalf("TickSchedules: %v", err)
	}
	if len(fires) != 0 {
		t.Fatalf("schedule %d should be marked fired, got %+v", id, fires)
	}
}

func TestDispatchDueSchedules_ChatScopedUsesChatCLIAndWorkspace(t *testing.T) {
	mock := &mockAdapter{name: "claude", replies: []string{"done"}}
	withMockAdapter(t, mock)

	c := newMemCore(t)
	ctx := context.Background()
	a := seedScheduleAgent(t, c, "agent-chat", []string{"claude"})
	workspace := t.TempDir()
	ch, err := c.CreateChat(ctx, CreateChatRequest{
		Title:         "scheduled chat",
		AgentID:       a.ID,
		CLIAgent:      "claude",
		WorkspacePath: workspace,
	})
	if err != nil {
		t.Fatalf("CreateChat: %v", err)
	}
	now := time.Date(2026, 6, 10, 13, 0, 0, 0, time.UTC)
	at := now.Add(-time.Minute)
	_, err = c.AddSchedule(ctx, AddScheduleRequest{
		ChatID:   ch.ID,
		At:       &at,
		Workflow: "go",
		Inputs: map[string]string{
			"task": "chat task",
		},
	})
	if err != nil {
		t.Fatalf("AddSchedule: %v", err)
	}

	_, err = c.DispatchDueSchedules(ctx, ScheduleDispatchOptions{
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("DispatchDueSchedules: %v", err)
	}
	if len(mock.shots) != 1 {
		t.Fatalf("expected one shot, got %d", len(mock.shots))
	}
	if mock.shots[0].Cwd != workspace {
		t.Fatalf("chat workspace not used: %q", mock.shots[0].Cwd)
	}
}

func TestStartScheduleDaemon_DispatchesAndStops(t *testing.T) {
	mock := &mockAdapter{name: "claude", replies: []string{"done"}}
	withMockAdapter(t, mock)

	c := newMemCore(t)
	ctx := context.Background()
	a := seedScheduleAgent(t, c, "agent-daemon", []string{"claude"})
	now := time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC)
	at := now.Add(-time.Minute)
	if _, err := c.AddSchedule(ctx, AddScheduleRequest{AgentID: a.ID, At: &at, Workflow: "go"}); err != nil {
		t.Fatalf("AddSchedule: %v", err)
	}
	fired := make(chan ScheduleRun, 1)
	d, err := c.StartScheduleDaemon(ctx, ScheduleDaemonOptions{
		ScheduleDispatchOptions: ScheduleDispatchOptions{
			CLI: "claude",
			Cwd: t.TempDir(),
			Now: func() time.Time { return now },
			OnFire: func(run ScheduleRun) {
				fired <- run
			},
		},
		Interval: time.Hour,
	})
	if err != nil {
		t.Fatalf("StartScheduleDaemon: %v", err)
	}
	defer d.Stop()

	select {
	case run := <-fired:
		if run.Result.Outcome != OutcomeCompleted {
			t.Fatalf("run outcome=%s err=%v", run.Result.Outcome, run.Result.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("schedule daemon did not dispatch due schedule")
	}
}

func seedScheduleAgent(t *testing.T, c *Core, id string, supports []string) *Agent {
	t.Helper()
	a, err := c.upsertAgent(context.Background(), &Agent{
		ID:           id,
		Name:         id,
		Instructions: "x",
		Supports:     supports,
		Workflows: []Workflow{{
			Name: "go",
			Inputs: []WorkflowInput{
				{Name: "task"},
				{Name: "schedule_id"},
				{Name: "schedule_cron"},
				{Name: "scheduled_at"},
				{Name: "workflow"},
			},
			Steps: []WorkflowStep{{
				Kind:     StepUserMessage,
				Template: "{{ .task }} {{ .schedule_id }} {{ .schedule_cron }} {{ .scheduled_at }} {{ .workflow }}",
			}},
		}},
		DefaultWorkflow: "go",
	})
	if err != nil {
		t.Fatalf("seedScheduleAgent %q: %v", id, err)
	}
	return a
}
