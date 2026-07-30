package store

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
)

const encryptionKeyBytes = 64 // two 256-bit AES-XTS keys

var sqliteHeader = []byte("SQLite format 3\x00")

func migratePlainDatabase(path string, key []byte) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	header := make([]byte, len(sqliteHeader))
	n, readErr := io.ReadFull(f, header)
	_ = f.Close()
	if readErr != nil && readErr != io.ErrUnexpectedEOF {
		return readErr
	}
	if n == 0 || !bytes.Equal(header, sqliteHeader) {
		return nil // already encrypted (or invalid; opening will diagnose it)
	}

	tmp := path + ".encrypting"
	_ = os.Remove(tmp)
	plain, err := sql.Open("sqlite3", plainDSN(path, false))
	if err != nil {
		return err
	}
	if err := plain.Ping(); err != nil {
		_ = plain.Close()
		return fmt.Errorf("open plaintext source: %w", err)
	}
	// Fold any committed WAL pages into the main file before copying, then
	// truncate the plaintext sidecar so migration does not leave chat data
	// behind outside the encrypted database.
	if _, err := plain.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		_ = plain.Close()
		return fmt.Errorf("checkpoint plaintext source: %w", err)
	}

	dest := encryptedVacuumURI(tmp, key)
	query := "VACUUM INTO '" + strings.ReplaceAll(dest, "'", "''") + "'"
	if _, err := plain.Exec(query); err != nil {
		_ = plain.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("vacuum into encrypted database: %w", err)
	}
	if err := plain.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close plaintext source: %w", err)
	}
	verify, err := sql.Open("sqlite3", encryptedDSN(tmp, key, true))
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	var count int
	err = verify.QueryRow(`SELECT COUNT(*) FROM sqlite_master`).Scan(&count)
	_ = verify.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("verify encrypted database: %w", err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// Windows cannot atomically rename over an existing file. Move the
	// plaintext source aside first, install the verified encrypted file, and
	// restore the source if that second rename fails.
	plainBackup := path + ".plaintext-migrating"
	_ = os.Remove(plainBackup)
	if err := os.Rename(path, plainBackup); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("stage plaintext source: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Rename(plainBackup, path)
		_ = os.Remove(tmp)
		return fmt.Errorf("install encrypted database: %w", err)
	}
	if err := os.Remove(plainBackup); err != nil {
		return fmt.Errorf("remove plaintext source after migration: %w", err)
	}
	for _, sidecar := range []string{path + "-wal", path + "-shm"} {
		if err := os.Remove(sidecar); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove plaintext SQLite sidecar %s: %w", sidecar, err)
		}
	}
	return nil
}

func plainDSN(path string, readOnly bool) string {
	q := make(url.Values)
	if readOnly {
		q.Set("mode", "ro")
	}
	return sqliteFileURI(path, q)
}

func encryptedVacuumURI(path string, key []byte) string {
	q := url.Values{
		"hexkey": {fmt.Sprintf("%x", key)},
		"vfs":    {"xts"},
	}
	return sqliteFileURI(path, q)
}
