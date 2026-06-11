package core

// Backup state export — the 1.1 structure moved the source of truth for
// chats, agents, memory, MCP, schedules and watchers into the SQLite DB
// under ~/.praimate, which the workspaces-root backup repo doesn't see.
// ExportBackupState writes a consistent snapshot of that state into a
// subdir of the backup repo so the existing git-based backup commits it
// alongside the on-disk chat sandboxes.
//
// Layout under <repoDir>/.praimate-state/:
//   db.sqlite        consistent VACUUM INTO snapshot of the live DB
//   agents/<id>.yaml exported agent definitions (portable, human-readable)
//
// Importing this back (after a restore/clone) is a follow-up; the
// snapshot alone makes the backup complete and recoverable.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// BackupStateDir is the conventional subdir name inside the backup repo.
const BackupStateDir = ".praimate-state"

// ExportBackupState snapshots the DB and exports all agents into
// <repoDir>/.praimate-state/. No-op (nil) when Core has no store.
func (c *Core) ExportBackupState(ctx context.Context, repoDir string) error {
	if c.store == nil {
		return nil
	}
	stateDir := filepath.Join(repoDir, BackupStateDir)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fmt.Errorf("export backup state: mkdir: %w", err)
	}

	// 1. Consistent DB snapshot (WAL-safe via VACUUM INTO).
	if err := c.store.Snapshot(ctx, filepath.Join(stateDir, "db.sqlite")); err != nil {
		return fmt.Errorf("export backup state: db snapshot: %w", err)
	}

	// 2. Agents as portable YAML. Rewritten each time; stale files for
	// deleted agents are pruned so the export mirrors the DB exactly.
	agentsDir := filepath.Join(stateDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return fmt.Errorf("export backup state: mkdir agents: %w", err)
	}
	agents, err := c.ListAgents(ctx)
	if err != nil {
		return fmt.Errorf("export backup state: list agents: %w", err)
	}
	keep := map[string]bool{}
	for i := range agents {
		a := &agents[i]
		keep[a.ID+".yaml"] = true
		body, err := MarshalAgentYAML(a)
		if err != nil {
			return fmt.Errorf("export backup state: marshal %s: %w", a.ID, err)
		}
		if err := os.WriteFile(filepath.Join(agentsDir, a.ID+".yaml"), body, 0o644); err != nil {
			return fmt.Errorf("export backup state: write %s: %w", a.ID, err)
		}
	}
	if entries, err := os.ReadDir(agentsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && !keep[e.Name()] {
				_ = os.Remove(filepath.Join(agentsDir, e.Name()))
			}
		}
	}
	return nil
}
