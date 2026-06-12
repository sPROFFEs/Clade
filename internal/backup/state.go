package backup

// State syncer hook — bridges the git-level backup to the SQLite DB +
// user config that live OUTSIDE the workspaces root (~/.praimate /
// %APPDATA%). The backup package stays free of database imports: the
// binaries (TUI + GUI) register a syncer at startup, and the git
// operations call back into it at the two integration points:
//
//   Export — before every stage+commit, so the snapshot the repo
//            commits reflects the live DB and config at sync time.
//   Import — after every operation that brings remote content into the
//            working tree (clone, pull, merge, rebase, reset), so the
//            remote's snapshot is row-merged into the live DB. This is
//            what makes multiple hosts share chats/agents/settings: git
//            moves the snapshot, the syncer merges it.
//
// Both directions are best-effort: a missing or failing syncer never
// blocks the git flow (better to sync the sandboxes than nothing).

import (
	"context"
	"sync"
)

// StateSyncer moves app state (DB rows, shareable config) in and out of
// the backup repo's state subdir. Implemented at the cmd layer where
// both internal/core and internal/launcher are reachable.
type StateSyncer interface {
	// Export writes the current state snapshot into repoDir's state
	// subdir, ready to be committed.
	Export(ctx context.Context, repoDir string) error
	// Import merges the state snapshot found in repoDir (typically just
	// pulled from the remote) into the live local state.
	Import(ctx context.Context, repoDir string) error
}

var (
	stateMu     sync.RWMutex
	stateSyncer StateSyncer
)

// SetStateSyncer registers the process-wide state syncer. Call once at
// startup, before any backup operation runs. Passing nil disables the
// hooks (the pre-1.1 behavior: only chats/ and templates/ sync).
func SetStateSyncer(s StateSyncer) {
	stateMu.Lock()
	stateSyncer = s
	stateMu.Unlock()
}

func currentStateSyncer() StateSyncer {
	stateMu.RLock()
	defer stateMu.RUnlock()
	return stateSyncer
}

// exportState runs the registered syncer's Export. Returns the error
// for callers that want to log it; never fatal to the git flow.
func exportState(ctx context.Context, repoDir string) error {
	s := currentStateSyncer()
	if s == nil {
		return nil
	}
	return s.Export(ctx, repoDir)
}

// importState runs the registered syncer's Import after remote content
// landed in the working tree.
func importState(ctx context.Context, repoDir string) error {
	s := currentStateSyncer()
	if s == nil {
		return nil
	}
	return s.Import(ctx, repoDir)
}
