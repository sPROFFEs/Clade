package store

import (
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
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
	st := openTestStore(t, path)
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
	keyEnvelope, err := os.ReadFile(KeyPath(path))
	if err != nil {
		t.Fatalf("read key envelope: %v", err)
	}
	if len(keyEnvelope) == encryptionKeyBytes {
		t.Fatal("database key is still stored as raw bytes")
	}
	if info, err := os.Stat(KeyPath(path)); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("key permissions = %o, want user-only", info.Mode().Perm())
	}

	reopened, err := OpenWithPassword(path, testDatabasePassword)
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

	st, err := initializeWithPassword(path, testDatabasePassword, kdfParams{
		time: 1, memoryKiB: 8 * 1024, threads: 1,
	})
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

func TestSnapshotIsEncryptedAndPortableWithEnvelope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	st := openTestStore(t, path)
	defer st.Close()
	dest := filepath.Join(t.TempDir(), "snapshot.sqlite")
	if err := st.Snapshot(t.Context(), dest); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	raw, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) >= len(sqliteHeader) && string(raw[:len(sqliteHeader)]) == string(sqliteHeader) {
		t.Fatal("portable snapshot exposes a plaintext SQLite header")
	}
}

func TestPasswordEnvelopeRejectsWrongPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	st := openTestStore(t, path)
	_ = st.Close()

	_, err := OpenWithPassword(path, "this password is definitely wrong")
	if !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("wrong password error = %v, want ErrInvalidPassword", err)
	}
}

func TestInitializeWrapsLegacyRawKeyWithoutReencryptingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	key := make([]byte, encryptionKeyBytes)
	for i := range key {
		key[i] = byte(i + 1)
	}
	if err := os.WriteFile(KeyPath(path), key, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := openSQLDatabase(path, key, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE legacy_secret (value TEXT); INSERT INTO legacy_secret VALUES ('kept')`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	st, err := initializeWithPassword(path, testDatabasePassword, kdfParams{
		time: 1, memoryKiB: 8 * 1024, threads: 1,
	})
	if err != nil {
		t.Fatalf("initialize legacy DB: %v", err)
	}
	defer st.Close()
	var got string
	if err := st.DB().QueryRow(`SELECT value FROM legacy_secret`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "kept" {
		t.Fatalf("legacy value = %q", got)
	}
	raw, err := os.ReadFile(KeyPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == encryptionKeyBytes {
		t.Fatal("legacy raw key was not replaced by a password envelope")
	}
}

func TestOpenUsesRememberedPasswordOnlyAfterExplicitOptIn(t *testing.T) {
	keyring.MockInit()
	path := filepath.Join(t.TempDir(), "db.sqlite")
	st := openTestStore(t, path)
	_ = st.Close()

	if _, err := Open(path); !errors.Is(err, ErrPasswordRequired) {
		t.Fatalf("Open without opt-in = %v, want ErrPasswordRequired", err)
	}
	if err := RememberPassword(path, testDatabasePassword); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open remembered: %v", err)
	}
	_ = reopened.Close()
	if err := ForgetRememberedPassword(path); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); !errors.Is(err, ErrPasswordRequired) {
		t.Fatalf("Open after forget = %v, want ErrPasswordRequired", err)
	}
}
