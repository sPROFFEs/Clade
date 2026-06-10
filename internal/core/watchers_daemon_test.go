package core

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestDispatchWatcherEvent_AgentScopedRunsWorkflow(t *testing.T) {
	ResetDebounceLog()
	mock := &mockAdapter{name: "claude", replies: []string{"done"}}
	withMockAdapter(t, mock)

	c := newMemCore(t)
	ctx := context.Background()
	a := seedWatcherAgent(t, c, "agent-a", []string{"claude"})
	root := t.TempDir()
	_, err := c.AddWatcher(ctx, AddWatcherRequest{
		AgentID:  a.ID,
		Path:     root,
		Patterns: []string{"*.go"},
		Inputs: map[string]string{
			"task": "review change",
		},
		DebounceMs: 1,
	})
	if err != nil {
		t.Fatalf("AddWatcher: %v", err)
	}

	file := filepath.Join(root, "main.go")
	runs, err := c.DispatchWatcherEvent(ctx, WatcherEvent{Path: file, Op: "write"}, WatcherDispatchOptions{
		CLI: "claude",
		Cwd: root,
		Now: func() time.Time { return time.Unix(100, 0) },
	})
	if err != nil {
		t.Fatalf("DispatchWatcherEvent: %v", err)
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
	if body != "review change write "+file {
		t.Fatalf("unexpected rendered watcher body: %q", body)
	}
	if mock.shots[0].Cwd != root {
		t.Fatalf("Cwd mismatch: %q", mock.shots[0].Cwd)
	}
}

func TestDispatchWatcherEvent_ChatScopedUsesChatCLIAndWorkspace(t *testing.T) {
	ResetDebounceLog()
	mock := &mockAdapter{name: "claude", replies: []string{"done"}}
	withMockAdapter(t, mock)

	c := newMemCore(t)
	ctx := context.Background()
	a := seedWatcherAgent(t, c, "agent-chat", []string{"claude"})
	workspace := t.TempDir()
	ch, err := c.CreateChat(ctx, CreateChatRequest{
		Title:         "watched chat",
		AgentID:       a.ID,
		CLIAgent:      "claude",
		WorkspacePath: workspace,
	})
	if err != nil {
		t.Fatalf("CreateChat: %v", err)
	}
	_, err = c.AddWatcher(ctx, AddWatcherRequest{
		ChatID: ch.ID,
		Path:   workspace,
		Inputs: map[string]string{
			"task": "chat task",
		},
		DebounceMs: 1,
	})
	if err != nil {
		t.Fatalf("AddWatcher: %v", err)
	}

	_, err = c.DispatchWatcherEvent(ctx, WatcherEvent{Path: filepath.Join(workspace, "x.txt"), Op: "create"}, WatcherDispatchOptions{
		Now: func() time.Time { return time.Unix(200, 0) },
	})
	if err != nil {
		t.Fatalf("DispatchWatcherEvent: %v", err)
	}
	if len(mock.shots) != 1 {
		t.Fatalf("expected one shot, got %d", len(mock.shots))
	}
	if mock.shots[0].Cwd != workspace {
		t.Fatalf("chat workspace not used: %q", mock.shots[0].Cwd)
	}
}

func TestMatchWatchers_DoesNotPrefixMatchSiblingDirectory(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	ResetDebounceLog()
	seedAgent(t, c, "a")
	root := filepath.Join(t.TempDir(), "project-a")
	_, _ = c.AddWatcher(ctx, AddWatcherRequest{
		AgentID: "a",
		Path:    root,
	})

	hits, err := c.MatchWatchers(ctx, WatcherEvent{Path: root + "-sibling/main.go"}, time.Now())
	if err != nil {
		t.Fatalf("MatchWatchers: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("sibling path should not match watcher root, got %+v", hits)
	}
}

func TestWatcherEventFromFSNotify(t *testing.T) {
	cases := []struct {
		name string
		op   fsnotify.Op
		want string
		ok   bool
	}{
		{"create", fsnotify.Create, "create", true},
		{"write", fsnotify.Write, "write", true},
		{"remove", fsnotify.Remove, "remove", true},
		{"rename", fsnotify.Rename, "rename", true},
		{"chmod", fsnotify.Chmod, "chmod", true},
		{"empty", 0, "", false},
	}
	for _, tc := range cases {
		got, ok := watcherEventFromFSNotify(fsnotify.Event{Name: "/tmp/x", Op: tc.op})
		if ok != tc.ok {
			t.Fatalf("%s: ok=%v want %v", tc.name, ok, tc.ok)
		}
		if ok && got.Op != tc.want {
			t.Fatalf("%s: op=%q want %q", tc.name, got.Op, tc.want)
		}
	}
}

func TestEnabledWatcherPaths_DedupesAndSorts(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	seedAgent(t, c, "a")
	a := filepath.Join(t.TempDir(), "a")
	b := filepath.Join(t.TempDir(), "b")
	_, _ = c.AddWatcher(ctx, AddWatcherRequest{AgentID: "a", Path: b})
	_, _ = c.AddWatcher(ctx, AddWatcherRequest{AgentID: "a", Path: a})
	_, _ = c.AddWatcher(ctx, AddWatcherRequest{AgentID: "a", Path: b})

	paths, err := c.enabledWatcherPaths(ctx)
	if err != nil {
		t.Fatalf("enabledWatcherPaths: %v", err)
	}
	if len(paths) != 2 || paths[0] != a || paths[1] != b {
		t.Fatalf("paths not deduped/sorted: %+v", paths)
	}
}

func seedWatcherAgent(t *testing.T, c *Core, id string, supports []string) *Agent {
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
				{Name: "event_op"},
				{Name: "event_path"},
			},
			Steps: []WorkflowStep{{
				Kind:     StepUserMessage,
				Template: "{{ .task }} {{ .event_op }} {{ .event_path }}",
			}},
		}},
		DefaultWorkflow: "go",
	})
	if err != nil {
		t.Fatalf("seedWatcherAgent %q: %v", id, err)
	}
	return a
}
