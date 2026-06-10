package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sPROFFEs/PrAImate/internal/core"
	"github.com/sPROFFEs/PrAImate/internal/store"
)

func TestAutomation_LoadsRowsAndPatterns(t *testing.T) {
	c, agentID := newAutomationTestCore(t)
	withAutomationAppCore(t, c)
	ctx := context.Background()
	_, _ = c.AddWatcher(ctx, core.AddWatcherRequest{AgentID: agentID, Path: t.TempDir(), Patterns: []string{"*.go"}})
	_, _ = c.AddSchedule(ctx, core.AddScheduleRequest{AgentID: agentID, Cron: "* * * * *"})
	_ = c.AddPrivacyPattern(ctx, `internal-\d+`)

	m := newAutomationModel(fakeCfg(t))
	msg := m.Init()().(automationLoadedMsg)
	mdl, _ := m.Update(msg)
	got := mdl.(automationModel)
	if len(got.watchers) != 1 || len(got.schedules) != 1 || len(got.patterns) != 1 {
		t.Fatalf("loaded rows wrong: watchers=%d schedules=%d patterns=%d", len(got.watchers), len(got.schedules), len(got.patterns))
	}
}

func TestAutomation_AddPrivacyPattern(t *testing.T) {
	c, _ := newAutomationTestCore(t)
	withAutomationAppCore(t, c)

	m := newAutomationModel(fakeCfg(t))
	m.loaded = true
	m.tab = automationTabPrivacy
	m.mode = automationModeAddPrivacy
	m.privacyInput.SetValue(`ticket-\d+`)
	_, cmd := m.submitForm()
	action := cmd().(automationActionMsg)
	if action.err != nil {
		t.Fatalf("submitForm: %v", action.err)
	}
	patterns, err := c.ListPrivacyPatterns(context.Background())
	if err != nil {
		t.Fatalf("ListPrivacyPatterns: %v", err)
	}
	if len(patterns) != 1 || patterns[0] != `ticket-\d+` {
		t.Fatalf("pattern not saved: %+v", patterns)
	}
}

func TestAutomation_ToggleWatcher(t *testing.T) {
	c, agentID := newAutomationTestCore(t)
	withAutomationAppCore(t, c)
	ctx := context.Background()
	id, _ := c.AddWatcher(ctx, core.AddWatcherRequest{AgentID: agentID, Path: t.TempDir()})
	watchers, _ := c.ListWatchers(ctx, false)

	m := newAutomationModel(fakeCfg(t))
	m.loaded = true
	m.watchers = watchers
	_, cmd := m.toggleSelected()
	action := cmd().(automationActionMsg)
	if action.err != nil {
		t.Fatalf("toggleSelected: %v", action.err)
	}
	got, _ := c.ListWatchers(ctx, false)
	if len(got) != 1 || got[0].ID != id || got[0].Enabled {
		t.Fatalf("watcher should be disabled: %+v", got)
	}
}

func TestAutomation_AddScheduleWithAt(t *testing.T) {
	c, agentID := newAutomationTestCore(t)
	withAutomationAppCore(t, c)

	at := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	m := newAutomationModel(fakeCfg(t))
	m.loaded = true
	m.mode = automationModeAddSchedule
	m.scheduleInputs[0].SetValue(agentID)
	m.scheduleInputs[2].SetValue(at.Format(time.RFC3339))
	_, cmd := m.submitForm()
	action := cmd().(automationActionMsg)
	if action.err != nil {
		t.Fatalf("submitForm: %v", action.err)
	}
	schedules, err := c.ListSchedules(context.Background(), false)
	if err != nil {
		t.Fatalf("ListSchedules: %v", err)
	}
	if len(schedules) != 1 || schedules[0].At == nil {
		t.Fatalf("schedule not saved as at row: %+v", schedules)
	}
}

func TestAutomation_PrivacyBodyDoesNotHidePattern(t *testing.T) {
	m := newAutomationModel(fakeCfg(t))
	m.loaded = true
	m.tab = automationTabPrivacy
	m.patterns = []string{`internal-\d+`}
	body := m.Body()
	if !strings.Contains(body, `internal-\d+`) {
		t.Fatalf("privacy body should show editable pattern, got %q", body)
	}
}

func newAutomationTestCore(t *testing.T) (*core.Core, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	c, err := core.New(core.Options{Store: st})
	if err != nil {
		t.Fatalf("core.New: %v", err)
	}
	if _, err := c.SeedBuiltins(context.Background()); err != nil {
		t.Fatalf("SeedBuiltins: %v", err)
	}
	agents, err := c.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) == 0 {
		t.Fatal("SeedBuiltins produced no agents")
	}
	return c, agents[0].ID
}

func withAutomationAppCore(t *testing.T, c *core.Core) {
	t.Helper()
	appCoreMu.Lock()
	oldCore := appCore
	oldErr := appCoreErr
	oldWatchers := appWatchers
	appCore = c
	appCoreErr = nil
	appWatchers = nil
	appCoreMu.Unlock()
	t.Cleanup(func() {
		appCoreMu.Lock()
		currentWatchers := appWatchers
		appWatchers = oldWatchers
		appCore = oldCore
		appCoreErr = oldErr
		appCoreMu.Unlock()
		if currentWatchers != nil && currentWatchers != oldWatchers {
			currentWatchers.Stop()
		}
	})
}
