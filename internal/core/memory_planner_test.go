package core

import (
	"context"
	"strings"
	"testing"
)

func TestBuildMemoryInjection_DisabledByDefault(t *testing.T) {
	c := newMemCore(t)
	_ = c.SetIdentity(context.Background(), "name", "Julio", "")
	got, err := c.BuildMemoryInjection(context.Background(), InjectionOptions{Query: "hi"})
	if err != nil {
		t.Fatalf("BuildMemoryInjection: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty when memory disabled, got: %q", got)
	}
}

func TestBuildMemoryInjection_EnabledIncludesIdentity(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	_ = c.SetMemoryEnabled(ctx, true)
	_ = c.SetIdentity(ctx, "name", "Julio", "")
	_ = c.SetIdentity(ctx, "role", "engineer", "")

	got, err := c.BuildMemoryInjection(ctx, InjectionOptions{Query: "refactor"})
	if err != nil {
		t.Fatalf("BuildMemoryInjection: %v", err)
	}
	if !strings.Contains(got, "About the user") || !strings.Contains(got, "Julio") {
		t.Fatalf("identity not in injection: %q", got)
	}
}

func TestBuildMemoryInjection_PicksMatchingEpisode(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	_ = c.SetMemoryEnabled(ctx, true)
	_, _ = c.AddEpisode(ctx, &Episode{
		Summary: "Fixed the auth bug in middleware.",
		Topics:  []string{"auth", "middleware"},
	})
	_, _ = c.AddEpisode(ctx, &Episode{
		Summary: "Refactored launcher to use channels.",
		Topics:  []string{"launcher", "concurrency"},
	})

	got, _ := c.BuildMemoryInjection(ctx, InjectionOptions{Query: "checking launcher behaviour"})
	if !strings.Contains(got, "launcher") {
		t.Fatalf("expected launcher episode chosen, got: %q", got)
	}
	if strings.Contains(got, "auth bug") {
		t.Fatalf("wrong episode chosen, got: %q", got)
	}
}

func TestBuildMemoryInjection_NoMatchSkipsEpisode(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	_ = c.SetMemoryEnabled(ctx, true)
	_, _ = c.AddEpisode(ctx, &Episode{
		Summary: "Fixed the auth bug in middleware.",
		Topics:  []string{"auth", "middleware"},
	})
	got, _ := c.BuildMemoryInjection(ctx, InjectionOptions{Query: "wholly different territory"})
	if strings.Contains(got, "Recent relevant session") {
		t.Fatalf("episode block surfaced despite no overlap: %q", got)
	}
}

func TestBuildMemoryInjection_PinnedSortedBySalience(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	_ = c.SetMemoryEnabled(ctx, true)
	_, _ = c.PinFact(ctx, "uses fish shell", 0.9)
	_, _ = c.PinFact(ctx, "prefers terse responses", 0.5)
	_, _ = c.PinFact(ctx, "old fact", 0.1) // below default floor 0.3

	got, _ := c.BuildMemoryInjection(ctx, InjectionOptions{Query: "x"})
	if !strings.Contains(got, "uses fish shell") {
		t.Fatalf("high-salience fact missing: %q", got)
	}
	if strings.Contains(got, "old fact") {
		t.Fatalf("below-floor fact leaked: %q", got)
	}
	// High-salience fact should appear before lower one.
	hi := strings.Index(got, "uses fish shell")
	lo := strings.Index(got, "prefers terse responses")
	if hi < 0 || lo < 0 || hi > lo {
		t.Fatalf("pinned ordering wrong: hi=%d lo=%d body=%q", hi, lo, got)
	}
}

func TestBuildMemoryInjection_ChatOverrideEnables(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	// Memory is OFF globally.
	_ = c.SetIdentity(ctx, "name", "Julio", "")
	on := true
	got, _ := c.BuildMemoryInjection(ctx, InjectionOptions{
		Query: "x", ChatOverride: &on,
	})
	if !strings.Contains(got, "Julio") {
		t.Fatalf("chat override didn't enable injection: %q", got)
	}
}

func TestBuildMemoryInjection_ChatOverrideDisables(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	_ = c.SetMemoryEnabled(ctx, true)
	_ = c.SetIdentity(ctx, "name", "Julio", "")

	off := false
	got, _ := c.BuildMemoryInjection(ctx, InjectionOptions{
		Query: "x", ChatOverride: &off,
	})
	if got != "" {
		t.Fatalf("chat override didn't disable injection: %q", got)
	}
}

func TestBuildMemoryInjection_EmptyWhenNothingRelevant(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	_ = c.SetMemoryEnabled(ctx, true)
	got, _ := c.BuildMemoryInjection(ctx, InjectionOptions{Query: "x"})
	if got != "" {
		t.Fatalf("expected empty injection on empty memory: %q", got)
	}
}

func TestTokenise_FiltersShortTokens(t *testing.T) {
	got := tokenise("the QUICK fix is in")
	for _, want := range []string{"quick", "fix"} {
		if !got[want] {
			t.Errorf("missing token %q", want)
		}
	}
	// Filter is len >= 3, so "is" and "in" are filtered but "the" is not
	// — that's deliberate; 3-char keywords like "tab", "fix", "url" are
	// load-bearing for episode matching.
	for _, badge := range []string{"is", "in"} {
		if got[badge] {
			t.Errorf("2-char token %q leaked through filter", badge)
		}
	}
}
