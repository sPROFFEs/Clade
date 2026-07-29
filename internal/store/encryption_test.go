package store

import (
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLiteURIsPreserveWindowsDrivePath(t *testing.T) {
	// filepath.ToSlash produces this form from the native backslash path when
	// the application runs on Windows.
	const path = `C:/Users/evaluator/AppData/Roaming/PrAImate/db.sqlite`
	const wantPrefix = "file:C:/Users/evaluator/AppData/Roaming/PrAImate/db.sqlite"
	key := make([]byte, encryptionKeyBytes)

	tests := map[string]string{
		"encrypted open":   encryptedDSN(path, key, false),
		"plaintext open":   plainDSN(path, false),
		"encrypted vacuum": encryptedVacuumURI(path, key),
		"plain snapshot":   sqliteFileURI(path, url.Values{"vfs": {"os"}}),
	}
	for name, got := range tests {
		t.Run(name, func(t *testing.T) {
			if !strings.HasPrefix(got, wantPrefix) {
				t.Fatalf("URI = %q, want prefix %q", got, wantPrefix)
			}
			if strings.HasPrefix(got, "file://C:/") {
				t.Fatalf("URI %q turns drive C: into a UNC authority", got)
			}
		})
	}
}

func TestSQLiteFileURIEscapesPathCharacters(t *testing.T) {
	got := sqliteFileURI("C:/Users/test user/data#1.sqlite", nil)
	const want = "file:C:/Users/test%20user/data%231.sqlite"
	if got != want {
		t.Fatalf("URI = %q, want %q", got, want)
	}
}

func TestOpenCreatesEncryptedDatabaseAndReopens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := st.DB().Exec(`CREATE TABLE secret (value TEXT); INSERT INTO secret VALUES ('classified')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) >= len(sqliteHeader) && string(raw[:len(sqliteHeader)]) == string(sqliteHeader) {
		t.Fatal("database has a plaintext SQLite header")
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("database permissions = %o, want user-only", info.Mode().Perm())
	}
	key, err := os.ReadFile(KeyPath(path))
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	if len(key) != encryptionKeyBytes {
		t.Fatalf("key length = %d, want %d", len(key), encryptionKeyBytes)
	}
	if info, err := os.Stat(KeyPath(path)); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("key permissions = %o, want user-only", info.Mode().Perm())
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	var got string
	if err := reopened.DB().QueryRow(`SELECT value FROM secret`).Scan(&got); err != nil {
		t.Fatalf("query reopened: %v", err)
	}
	if got != "classified" {
		t.Fatalf("value = %q", got)
	}
}

func TestOpenMigratesPlaintextDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.sqlite")
	plain, err := sql.Open("sqlite3", plainDSN(path, false))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plain.Exec(`CREATE TABLE legacy (value TEXT); INSERT INTO legacy VALUES ('kept')`); err != nil {
		t.Fatal(err)
	}
	if err := plain.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open migration: %v", err)
	}
	defer st.Close()
	var got string
	if err := st.DB().QueryRow(`SELECT value FROM legacy`).Scan(&got); err != nil {
		t.Fatalf("query migrated: %v", err)
	}
	if got != "kept" {
		t.Fatalf("value = %q", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) >= len(sqliteHeader) && string(raw[:len(sqliteHeader)]) == string(sqliteHeader) {
		t.Fatal("migrated database remains plaintext")
	}
	// The encrypted connection may create its own encrypted WAL/SHM files
	// immediately after migration, so only the migration staging files must
	// be absent here.
	for _, leftover := range []string{path + ".encrypting", path + ".plaintext-migrating"} {
		if _, err := os.Stat(leftover); !os.IsNotExist(err) {
			t.Errorf("migration left plaintext artifact %s", leftover)
		}
	}
}

func TestSnapshotIsPortablePlainSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dest := filepath.Join(t.TempDir(), "snapshot.sqlite")
	if err := st.Snapshot(t.Context(), dest); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	raw, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < len(sqliteHeader) || string(raw[:len(sqliteHeader)]) != string(sqliteHeader) {
		t.Fatal("portable snapshot is not plaintext SQLite")
	}
}
