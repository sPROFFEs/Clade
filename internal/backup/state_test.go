package backup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSyncer records hook invocations and drops a marker file so the
// commit-path tests can assert the state dir actually gets tracked.
type fakeSyncer struct {
	exports, imports int
}

func (f *fakeSyncer) Export(_ context.Context, repoDir string) error {
	f.exports++
	dir := filepath.Join(repoDir, ".praimate-state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "db.sqlite"), []byte("snapshot\n"), 0o644)
}

func (f *fakeSyncer) Import(context.Context, string) error {
	f.imports++
	return nil
}

// A registered syncer's snapshot must be exported and committed by the
// normal stage+commit path — this is the "DB travels with the backup"
// contract.
func TestCommitLocalChanges_CommitsStateSnapshot(t *testing.T) {
	skipIfNoGit(t)
	ctx := context.Background()
	fs := &fakeSyncer{}
	SetStateSyncer(fs)
	defer SetStateSyncer(nil)

	dir := t.TempDir()
	if _, err := Init(ctx, dir); err != nil {
		t.Fatal(err)
	}
	_ = Run(ctx, dir, "config", "user.email", "test@example.com")
	_ = Run(ctx, dir, "config", "user.name", "Clade Test")

	if err := CommitLocalChanges(ctx, dir, ""); err != nil {
		t.Fatal(err)
	}
	if fs.exports == 0 {
		t.Fatal("Export was never invoked by the commit path")
	}
	r := Run(ctx, dir, "ls-files")
	if r.Failed() {
		t.Fatal(UserError(r))
	}
	if !strings.Contains(r.Stdout, ".praimate-state/db.sqlite") {
		t.Errorf(".praimate-state/db.sqlite should be tracked; tracked files:\n%s", r.Stdout)
	}
}

// A pull that brings remote content in must trigger Import so the
// remote snapshot lands in the live DB.
func TestPull_TriggersStateImport(t *testing.T) {
	skipIfNoGit(t)
	ctx := context.Background()

	remote := t.TempDir()
	if r := Run(ctx, remote, "init", "--bare", "-b", "main"); r.Failed() {
		t.Skipf("git init --bare -b main failed (likely old git): %s", UserError(r))
	}

	// Seed the remote from a writer clone.
	writer := t.TempDir()
	if _, err := Init(ctx, writer); err != nil {
		t.Fatal(err)
	}
	_ = Run(ctx, writer, "config", "user.email", "test@example.com")
	_ = Run(ctx, writer, "config", "user.name", "Clade Test")
	_ = os.MkdirAll(filepath.Join(writer, "chats", "x"), 0o755)
	_ = os.WriteFile(filepath.Join(writer, "chats", "x", "chat.json"), []byte("{}"), 0o644)
	if err := CommitLocalChanges(ctx, writer, ""); err != nil {
		t.Fatal(err)
	}
	if err := AddRemote(ctx, writer, remote); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Sync(ctx, writer); err != nil {
		t.Fatal(err)
	}

	// Reader clone pulls — Import must fire.
	reader := filepath.Join(t.TempDir(), "clone")
	if err := Clone(ctx, remote, reader); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSyncer{}
	SetStateSyncer(fs)
	defer SetStateSyncer(nil)
	if err := Pull(ctx, reader, true); err != nil {
		t.Fatal(err)
	}
	if fs.imports == 0 {
		t.Fatal("Import was never invoked by the pull path")
	}
}
