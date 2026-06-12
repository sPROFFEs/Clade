package backup

// Managed .gitignore + .gitattributes writers. Clade owns these two
// files when it manages the workspaces root as a backup repo.
//
// .gitignore semantics — per user instruction:
//   - Everything at the workspaces root is excluded by default (a
//     stray scratch.txt at the root won't accidentally propagate).
//   - chats/ and templates/ are explicitly UN-ignored, and EVERYTHING
//     inside them is tracked (sandbox/, sessions/native/, etc.). This
//     trades repo size for full-fidelity cross-machine sync.
//
// .gitattributes semantics:
//   - Register a custom merge driver for MEMORY.md so concurrent edits
//     across machines concatenate instead of producing conflict
//     markers. The driver itself is `clade --merge-memory` and is
//     wired into .git/config by Init/Clone (see repo.go).
//
// Both files start with a magic comment identifying them as Clade-
// managed. If the user has hand-edited them (comment missing), we
// leave them alone — never silently clobber a customised file.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const managedGitignoreContent = `# Managed by Clade — do not edit by hand.
# Tracks the chats/ and templates/ subtrees. Stray files at the root
# of the workspaces directory are ignored so they don't accidentally
# propagate across machines.

# Ignore everything at the root.
/*

# Un-ignore the tracked dirs and these managed metadata files.
# .praimate-state/ carries the DB snapshot + shareable config so other
# machines can import the same chats/agents/settings.
!/chats/
!/templates/
!/.praimate-state/
!/.gitignore
!/.gitattributes
`

const managedGitattributesContent = `# Managed by Clade — do not edit by hand.
# Custom merge driver for MEMORY.md so concurrent edits across
# machines concatenate instead of producing conflict markers.

chats/*/MEMORY.md merge=clade-memory
templates/*/workpath/MEMORY.md merge=clade-memory

# State snapshots: take the remote side on merge conflicts (binary
# db.sqlite can't textual-merge); the importer row-merges the remote
# snapshot into the live DB so local rows are never lost.
.praimate-state/** merge=praimate-theirs
`

const cladeManagedMarker = "# Managed by Clade — do not edit by hand."

// WriteManagedGitignore writes the canonical .gitignore to
// <repoRoot>/.gitignore. Refuses to overwrite when the file exists
// AND its first line isn't our magic marker (user-edited). Returns
// an error in that case so the caller can surface a "found
// user-edited .gitignore, leaving alone" note instead of silently
// clobbering.
func WriteManagedGitignore(repoRoot string) error {
	return writeManagedFile(filepath.Join(repoRoot, ".gitignore"), managedGitignoreContent)
}

// WriteManagedGitattributes is the .gitattributes sibling.
func WriteManagedGitattributes(repoRoot string) error {
	return writeManagedFile(filepath.Join(repoRoot, ".gitattributes"), managedGitattributesContent)
}

// ErrUserEdited is returned when WriteManaged* refuses to clobber a
// file that doesn't carry the Clade-managed marker.
var ErrUserEdited = errors.New("file is user-edited (no Clade-managed marker); refusing to overwrite")

func writeManagedFile(path, content string) error {
	if existing, err := os.ReadFile(path); err == nil {
		if !strings.HasPrefix(strings.TrimSpace(string(existing)), cladeManagedMarker) {
			return ErrUserEdited
		}
		// Cheap idempotence: skip the write when contents already match.
		if string(existing) == content {
			return nil
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	tmp := path + ".clade-tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ManagedFileHash returns the SHA-256 of the canonical .gitignore
// content. Exposed so tests / diagnostics can prove the bundled
// content matches a known value.
func ManagedFileHash() string {
	sum := sha256.Sum256([]byte(managedGitignoreContent))
	return hex.EncodeToString(sum[:])
}
