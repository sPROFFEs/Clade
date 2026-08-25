package core

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestExternalAgentRunClaimReplayConflictAndRetry(t *testing.T) {
	c, _ := New(Options{Store: openTempStore(t)})
	ctx := context.Background()
	req := ClaimExternalAgentRunRequest{
		ID: "controller-job-42", RequestHash: "hash-a", AgentID: "dev-team",
		CLI: "praimate-code", Runtime: "native", Kind: "prompt",
	}
	run, execute, err := c.ClaimExternalAgentRun(ctx, req)
	if err != nil || !execute || run.Attempt != 1 || run.State != "running" {
		t.Fatalf("first claim = %#v execute=%v err=%v", run, execute, err)
	}
	if err := c.FinishExternalAgentRun(ctx, run.ID, run.Attempt, "completed", `{"ok":true}`, ""); err != nil {
		t.Fatal(err)
	}
	replayed, execute, err := c.ClaimExternalAgentRun(ctx, req)
	if err != nil || execute || replayed.State != "completed" || replayed.ResultJSON == "" {
		t.Fatalf("replay = %#v execute=%v err=%v", replayed, execute, err)
	}
	conflict := req
	conflict.RequestHash = "hash-b"
	if _, _, err := c.ClaimExternalAgentRun(ctx, conflict); !errors.Is(err, ErrExternalAgentRunConflict) {
		t.Fatalf("conflict error = %v", err)
	}
	req.Retry = true
	retried, execute, err := c.ClaimExternalAgentRun(ctx, req)
	if err != nil || !execute || retried.Attempt != 2 || retried.State != "running" || retried.ResultJSON != "" {
		t.Fatalf("retry = %#v execute=%v err=%v", retried, execute, err)
	}
	if err := c.FinishExternalAgentRun(ctx, retried.ID, 1, "completed", `{"stale":true}`, ""); !errors.Is(err, ErrExternalAgentRunStale) {
		t.Fatalf("stale completion error = %v", err)
	}
	if err := c.FinishExternalAgentRun(ctx, retried.ID, retried.Attempt, "completed", `{"ok":true}`, ""); err != nil {
		t.Fatal(err)
	}
}

func TestExternalAgentRunRejectsUnsafeID(t *testing.T) {
	c, _ := New(Options{Store: openTempStore(t)})
	_, _, err := c.ClaimExternalAgentRun(context.Background(), ClaimExternalAgentRunRequest{
		ID: "../escape", RequestHash: "h", AgentID: "a", CLI: "c", Kind: "prompt",
	})
	if err == nil {
		t.Fatal("expected unsafe run ID to fail")
	}
}

func TestExternalAgentRunConcurrentInitialClaimExecutesOnce(t *testing.T) {
	c, _ := New(Options{Store: openTempStore(t)})
	req := ClaimExternalAgentRunRequest{
		ID: "concurrent-job-42", RequestHash: "hash-a", AgentID: "dev-team",
		CLI: "praimate-code", Runtime: "native", Kind: "prompt",
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	var executions atomic.Int32
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, execute, err := c.ClaimExternalAgentRun(context.Background(), req)
			if err != nil {
				errs <- err
				return
			}
			if execute {
				executions.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("claim failed: %v", err)
	}
	if got := executions.Load(); got != 1 {
		t.Fatalf("executions=%d, want 1", got)
	}
}
