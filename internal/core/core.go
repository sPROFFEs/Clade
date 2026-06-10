// Package core is the single API surface that both the TUI
// (cmd/praimate) and the GUI (cmd/praimate-gui, future) call into. No
// other package may import internal/launcher, internal/ollama, etc.
// directly; everything flows through Core.
//
// Today this package is a thin facade that delegates to
// internal/launcher. Over Phase 2+ the facade hardens and the
// launcher's exported surface shrinks until it is only consumed
// through Core.
//
// Core owns no concurrency primitives of its own. Callers are expected
// to be either bubbletea Cmd callbacks (TUI) or Wails-binding goroutines
// (GUI), both of which are already single-threaded per call.
package core

import (
	"context"
	"errors"

	"github.com/sPROFFEs/PrAImate/internal/launcher"
	"github.com/sPROFFEs/PrAImate/internal/store"
)

// Core is the facade. Constructed once at process start; passed by
// pointer to every screen / page that needs to read or mutate state.
type Core struct {
	store *store.Store

	privacy *PrivacyScanner

	// workspacesRoot is the legacy <root>/chats/... layout used by
	// internal/launcher today. Phase 1 keeps using this so the existing
	// TUI keeps working unchanged; Phase 2+ will route writes through
	// the DB and leave only transcript slices on disk.
	workspacesRoot string
}

// Options bundles the inputs Core needs at construction time. Each is
// optional; leaving them zero gives a no-DB, launcher-only Core that
// behaves the same as today's TUI.
type Options struct {
	// Store, if non-nil, becomes the DB-backed source of truth for
	// agents, memory, MCP, schedules, and watchers.
	Store *store.Store
	// WorkspacesRoot is the legacy on-disk workspace directory. Phase 1
	// continues to honour it so chats keep launching.
	WorkspacesRoot string
}

// New builds a Core. Either Store or WorkspacesRoot (or both) may be
// set. Passing neither returns an error because a Core that cannot
// read anything is useless.
func New(opts Options) (*Core, error) {
	if opts.Store == nil && opts.WorkspacesRoot == "" {
		return nil, errors.New("core.New: need Store or WorkspacesRoot")
	}
	c := &Core{
		store:          opts.Store,
		privacy:        NewPrivacyScanner(),
		workspacesRoot: opts.WorkspacesRoot,
	}
	c.loadPrivacyPatterns(context.Background())
	return c, nil
}

// Close releases any resources Core owns. The store is NOT closed here
// — callers own its lifetime since they supplied it.
func (c *Core) Close() error { return nil }

// Store exposes the underlying store, if any. Used by tests and by
// migrations that haven't grown a typed Core method yet.
func (c *Core) Store() *store.Store { return c.store }

// WorkspacesRoot returns the on-disk workspaces directory the legacy
// launcher uses. Empty if Core was built without one.
func (c *Core) WorkspacesRoot() string { return c.workspacesRoot }

// PrivacyScanner exposes the process-wide scanner so settings screens
// can add custom patterns before workflow runs.
func (c *Core) PrivacyScanner() *PrivacyScanner {
	if c.privacy == nil {
		c.privacy = NewPrivacyScanner()
	}
	return c.privacy
}

// --- Legacy chat list ---------------------------------------------------

// ListLegacyChats returns every chat in the legacy on-disk workspaces
// layout (the workspaces-root scheme that predates the DB-backed chats
// added in Phase 3c). Used by the existing TUI's chat list.
//
// New code should use ListChats() instead, which reads from the
// chats table and is what Recipes / workflow runs persist to.
func (c *Core) ListLegacyChats(ctx context.Context) ([]launcher.Chat, error) {
	if c.workspacesRoot == "" {
		return nil, errors.New("core.ListLegacyChats: workspaces root not configured")
	}
	return launcher.ListChats(c.workspacesRoot)
}

// --- Agents (Phase 2 placeholder) ---------------------------------------

// ListAgents returns the user's agent catalogue from the DB. Phase 1
// returns an empty slice; Phase 2 wires it up.
func (c *Core) ListAgents(ctx context.Context) ([]Agent, error) {
	if c.store == nil {
		return nil, nil
	}
	return c.listAgentsFromDB(ctx)
}
