package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExportBackupState_SnapshotsDBAndAgents(t *testing.T) {
	c, _ := New(Options{Store: openTempStore(t)})
	ctx := context.Background()
	if _, err := c.SeedBuiltins(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	repo := t.TempDir()
	if err := c.ExportBackupState(ctx, repo); err != nil {
		t.Fatalf("ExportBackupState: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, BackupStateDir, "db.sqlite")); err != nil {
		t.Fatalf("db snapshot missing: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(repo, BackupStateDir, "agents"))
	if len(entries) < 3 {
		t.Fatalf("expected ≥3 agent yaml exports, got %d", len(entries))
	}
}
