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

// Two machines, one remote: machine A's chats + messages must appear on
// machine B after importing A's snapshot, without disturbing B's own
// rows. This is the multi-host sharing contract.
func TestImportBackupState_MergesChatsAcrossMachines(t *testing.T) {
	ctx := context.Background()
	a, _ := New(Options{Store: openTempStore(t)})
	b, _ := New(Options{Store: openTempStore(t)})

	chA, err := a.CreateChat(ctx, CreateChatRequest{Title: "from machine A", CLIAgent: "claude"})
	if err != nil {
		t.Fatalf("create chat A: %v", err)
	}
	if _, err := a.AddMessage(ctx, chA.ID, "user", "hello from A", nil); err != nil {
		t.Fatalf("add message A: %v", err)
	}
	chB, _ := b.CreateChat(ctx, CreateChatRequest{Title: "machine B local", CLIAgent: "codex"})

	repo := t.TempDir()
	if err := a.ExportBackupState(ctx, repo); err != nil {
		t.Fatalf("export A: %v", err)
	}
	if err := b.ImportBackupState(ctx, repo); err != nil {
		t.Fatalf("import into B: %v", err)
	}

	got, err := b.GetChat(ctx, chA.ID)
	if err != nil {
		t.Fatalf("A's chat missing on B after import: %v", err)
	}
	if got.Title != "from machine A" {
		t.Fatalf("title = %q", got.Title)
	}
	msgs, _ := b.ListMessages(ctx, chA.ID, 0)
	if len(msgs) != 1 || msgs[0].Content != "hello from A" {
		t.Fatalf("A's messages not merged: %+v", msgs)
	}
	if _, err := b.GetChat(ctx, chB.ID); err != nil {
		t.Fatalf("B's own chat lost after import: %v", err)
	}

	// Idempotence: importing the same snapshot again must not duplicate
	// messages (natural-key dedupe, not autoincrement ids).
	if err := b.ImportBackupState(ctx, repo); err != nil {
		t.Fatalf("re-import: %v", err)
	}
	msgs, _ = b.ListMessages(ctx, chA.ID, 0)
	if len(msgs) != 1 {
		t.Fatalf("re-import duplicated messages: got %d", len(msgs))
	}
}

// Keyed rows follow newer-updated_at-wins: a title change on machine A
// must overwrite B's stale copy, but a stale snapshot must never
// clobber a newer local row.
func TestImportBackupState_NewerUpdatedAtWins(t *testing.T) {
	ctx := context.Background()
	a, _ := New(Options{Store: openTempStore(t)})
	b, _ := New(Options{Store: openTempStore(t)})

	ch, _ := a.CreateChat(ctx, CreateChatRequest{Title: "v1", CLIAgent: "claude"})
	repo := t.TempDir()
	if err := a.ExportBackupState(ctx, repo); err != nil {
		t.Fatalf("export v1: %v", err)
	}
	if err := b.ImportBackupState(ctx, repo); err != nil {
		t.Fatalf("import v1: %v", err)
	}

	// Bump the row on A with a strictly newer updated_at.
	if _, err := a.store.DB().ExecContext(ctx,
		`UPDATE chats SET title = 'v2', updated_at = '2999-01-01T00:00:00Z' WHERE id = ?`, ch.ID); err != nil {
		t.Fatalf("bump A: %v", err)
	}
	if err := a.ExportBackupState(ctx, repo); err != nil {
		t.Fatalf("export v2: %v", err)
	}
	if err := b.ImportBackupState(ctx, repo); err != nil {
		t.Fatalf("import v2: %v", err)
	}
	got, _ := b.GetChat(ctx, ch.ID)
	if got.Title != "v2" {
		t.Fatalf("newer remote row should win; title = %q", got.Title)
	}

	// Now B is newer than the (stale) snapshot — re-import must keep v3.
	if _, err := b.store.DB().ExecContext(ctx,
		`UPDATE chats SET title = 'v3', updated_at = '3000-01-01T00:00:00Z' WHERE id = ?`, ch.ID); err != nil {
		t.Fatalf("bump B: %v", err)
	}
	if err := b.ImportBackupState(ctx, repo); err != nil {
		t.Fatalf("re-import stale: %v", err)
	}
	got, _ = b.GetChat(ctx, ch.ID)
	if got.Title != "v3" {
		t.Fatalf("stale snapshot clobbered newer local row; title = %q", got.Title)
	}
}
