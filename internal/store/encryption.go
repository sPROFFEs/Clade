package store

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const encryptionKeyBytes = 64 // two 256-bit AES-XTS keys

var sqliteHeader = []byte("SQLite format 3\x00")

func loadOrCreateKey(path string) ([]byte, error) {
	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) != encryptionKeyBytes {
			return nil, fmt.Errorf("%s has %d bytes; expected %d", path, len(key), encryptionKeyBytes)
		}
		_ = os.Chmod(path, 0o600)
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	key = make([]byte, encryptionKeyBytes)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return loadOrCreateKey(path)
	}
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := f.Write(key); err != nil {
		return nil, err
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	ok = true
	return key, nil
}

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
	mode := ""
	if readOnly {
		mode = "?mode=ro"
	}
	return "file:" + filepath.ToSlash(path) + mode
}

func encryptedVacuumURI(path string, key []byte) string {
	return "file:" + filepath.ToSlash(path) + "?vfs=xts&hexkey=" + fmt.Sprintf("%x", key)
}
