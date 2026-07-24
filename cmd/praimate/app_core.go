package main

// app_core hosts the process-wide handle to internal/core. The TUI is a
// single bubbletea program with one shared backend, so a package-level
// pointer is the simplest way to make Core reachable from every pane
// without threading it through Pane factories.
//
// Init flow: main() calls initAppCore() after loading the legacy
// launcher.Config; if it succeeds, panes can reach the live Core via
// getAppCore(). Init failure does NOT prevent the TUI from starting —
// the existing PrAImate-style flows keep working — but the new Recipes
// pane will surface the error and refuse to operate.

import (
	"context"
	"sync"

	"git.jtsec.local/lab/PrAImate/internal/backup"
	"git.jtsec.local/lab/PrAImate/internal/core"
	"git.jtsec.local/lab/PrAImate/internal/store"
)

var (
	appCoreMu    sync.RWMutex
	appCore      *core.Core
	appWatchers  *core.WatcherDaemon
	appSchedules *core.ScheduleDaemon
	appCoreErr   error
	appCoreOnce  sync.Once
)

// initAppCore opens the default PrAImate DB, builds a Core, seeds the
// built-in agents, and registers production CLI adapters. Safe to call
// multiple times — only the first invocation does real work.
//
// Errors are recorded and surfaced via getAppCoreErr(); they are NOT
// returned because the TUI must continue starting up regardless.
func initAppCore() {
	appCoreOnce.Do(func() {
		dbPath, err := store.DefaultDBPath()
		if err != nil {
			appCoreErr = err
			return
		}
		st, err := store.Open(dbPath)
		if err != nil {
			appCoreErr = err
			return
		}
		c, err := core.New(core.Options{Store: st})
		if err != nil {
			_ = st.Close()
			appCoreErr = err
			return
		}
		if _, err := c.SeedBuiltins(context.Background()); err != nil {
			_ = st.Close()
			appCoreErr = err
			return
		}
		core.RegisterAllCLIAdapters()
		// From here on, every backup commit snapshots the DB + shareable
		// config, and every pull/merge/reset row-merges the remote's
		// snapshot back in. Must happen before runStartupAutoSync.
		backup.SetStateSyncer(coreStateSyncer{core: c})
		watchers, _ := c.StartWatcherDaemon(context.Background(), core.WatcherDaemonOptions{
			WatcherDispatchOptions: core.WatcherDispatchOptions{CLI: "claude"},
		})
		schedules, _ := c.StartScheduleDaemon(context.Background(), core.ScheduleDaemonOptions{
			ScheduleDispatchOptions: core.ScheduleDispatchOptions{CLI: "claude"},
		})

		appCoreMu.Lock()
		appCore = c
		appWatchers = watchers
		appSchedules = schedules
		appCoreMu.Unlock()
	})
}

// getAppCore returns the process-wide Core, or nil if initAppCore
// failed. Callers must handle nil — the Recipes pane treats nil as a
// "Core unavailable" rendering state.
func getAppCore() *core.Core {
	appCoreMu.RLock()
	defer appCoreMu.RUnlock()
	return appCore
}

// getAppCoreErr returns the error captured during initAppCore, or nil
// if init succeeded (or hasn't run yet). Useful for showing the user
// what went wrong when getAppCore() returns nil.
func getAppCoreErr() error {
	appCoreMu.RLock()
	defer appCoreMu.RUnlock()
	return appCoreErr
}

func restartAppWatchers() {
	c := getAppCore()
	if c == nil {
		return
	}
	appCoreMu.Lock()
	old := appWatchers
	appWatchers = nil
	appCoreMu.Unlock()
	if old != nil {
		old.Stop()
	}
	watchers, _ := c.StartWatcherDaemon(context.Background(), core.WatcherDaemonOptions{
		WatcherDispatchOptions: core.WatcherDispatchOptions{CLI: "claude"},
	})
	appCoreMu.Lock()
	appWatchers = watchers
	appCoreMu.Unlock()
}
