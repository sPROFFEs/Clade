package launcher

// One-shot migration: previous launcher versions put workspaces directly
// under <root>/<name>/. The new model puts patterns under <root>/templates/
// and runtime instances under <root>/chats/. This file detects the legacy
// layout and moves every <root>/<name>/ that contains a workpath/ subdir
// into <root>/templates/<name>/.

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// MigrationResult is what MigrateLegacyLayout returns so the TUI can
// surface a note like "promoted 3 workspaces to templates".
type MigrationResult struct {
	Promoted []string // workspace names that were moved to templates/
}

// MigrateLegacyLayout scans root for top-level dirs that look like the
// old workspace layout (<root>/<name>/workpath/) and moves them to
// <root>/templates/<name>/. Existing templates/ and chats/ subdirs are
// left alone, as are hidden dirs.
//
// Idempotent: re-running is a no-op once everything is migrated.
func MigrateLegacyLayout(root string) (MigrationResult, error) {
	var res MigrationResult
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return res, nil
		}
		return res, err
	}
	templatesRoot := filepath.Join(root, TemplatesDir)

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == TemplatesDir || name == ChatsDir || strings.HasPrefix(name, ".") {
			continue
		}
		legacyWp := filepath.Join(root, name, "workpath")
		if st, err := os.Stat(legacyWp); err != nil || !st.IsDir() {
			continue
		}
		if err := os.MkdirAll(templatesRoot, 0o755); err != nil {
			return res, err
		}
		dst := filepath.Join(templatesRoot, name)
		if _, err := os.Stat(dst); err == nil {
			// Same-named template already exists — don't clobber. Skip
			// (the user can resolve by hand).
			continue
		}
		if err := os.Rename(filepath.Join(root, name), dst); err != nil {
			return res, err
		}
		res.Promoted = append(res.Promoted, name)
	}
	return res, nil
}
