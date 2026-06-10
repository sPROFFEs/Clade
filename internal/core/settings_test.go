package core

import (
	"context"
	"testing"
)

func TestSettings_GetMissingReturnsNil(t *testing.T) {
	c := newMemCore(t)
	got, err := c.GetSetting(context.Background(), ScopeCLI, "nope")
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for missing key, got %q", got)
	}
}

func TestSettings_SetGetDeleteRoundTrip(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	if err := c.SetSetting(ctx, ScopeCLI, "k1", []byte(`"hello"`)); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	got, _ := c.GetSetting(ctx, ScopeCLI, "k1")
	if string(got) != `"hello"` {
		t.Fatalf("round-trip mismatch: %q", got)
	}
	// Update same key.
	_ = c.SetSetting(ctx, ScopeCLI, "k1", []byte(`"world"`))
	got, _ = c.GetSetting(ctx, ScopeCLI, "k1")
	if string(got) != `"world"` {
		t.Fatalf("update failed: %q", got)
	}
	// Delete.
	_ = c.DeleteSetting(ctx, ScopeCLI, "k1")
	got, _ = c.GetSetting(ctx, ScopeCLI, "k1")
	if got != nil {
		t.Fatalf("expected deleted, got %q", got)
	}
}

func TestSettings_CLIAndGUIAreSeparate(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	_ = c.SetSetting(ctx, ScopeCLI, "theme", []byte(`"dark"`))
	_ = c.SetSetting(ctx, ScopeGUI, "theme", []byte(`"light"`))

	cli, _ := c.GetSetting(ctx, ScopeCLI, "theme")
	gui, _ := c.GetSetting(ctx, ScopeGUI, "theme")
	if string(cli) != `"dark"` || string(gui) != `"light"` {
		t.Fatalf("CLI/GUI scopes leaked: cli=%q gui=%q", cli, gui)
	}
}

func TestSettings_InvalidScopeRejected(t *testing.T) {
	c := newMemCore(t)
	if _, err := c.GetSetting(context.Background(), SettingsScope("bogus"), "k"); err == nil {
		t.Fatal("expected invalid-scope error")
	}
}

func TestMemoryToggle_DefaultsToFalse(t *testing.T) {
	c := newMemCore(t)
	on, err := c.IsMemoryEnabled(context.Background())
	if err != nil {
		t.Fatalf("IsMemoryEnabled: %v", err)
	}
	if on {
		t.Fatal("memory should default to off in 1.0")
	}
}

func TestMemoryToggle_RoundTrips(t *testing.T) {
	c := newMemCore(t)
	ctx := context.Background()
	if err := c.SetMemoryEnabled(ctx, true); err != nil {
		t.Fatalf("SetMemoryEnabled: %v", err)
	}
	on, _ := c.IsMemoryEnabled(ctx)
	if !on {
		t.Fatal("toggle did not stick")
	}
	_ = c.SetMemoryEnabled(ctx, false)
	on, _ = c.IsMemoryEnabled(ctx)
	if on {
		t.Fatal("toggle should be off after reset")
	}
}
