package launcher

// Bundle discovery for the chat-settings "Bundles" toggle. Scans
// <workspacesRoot>/templates/_common/<bundle>/ for available shared
// capability bundles and reports each one's name + description so the
// settings screen can render a one-line label.

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Bundle describes one capability bundle under templates/_common/.
// Mirrors the inventory the user sees in templates/_common/README.md
// — but read fresh from disk so the launcher reflects new bundles
// the user drops in without recompiling.
type Bundle struct {
	// Name is the bundle dir's basename (e.g. "graphify"). This is
	// what goes into a workpath.json `imports:` entry as the
	// `_common/<Name>` suffix.
	Name string
	// SourceDir is the absolute path to the bundle dir.
	SourceDir string
	// Title is a one-line label for display. Pulled from the H1 of
	// playbook-fragment.md, falling back to the rules-fragment.md H1,
	// then to the bundle name kebab-cased.
	Title string
	// Description is the first non-heading paragraph from
	// playbook-fragment.md, capped at ~120 chars.
	Description string
}

// DiscoverBundles returns the registered bundles under
// <workspacesRoot>/templates/_common/. Hidden dirs and README.md are
// skipped; an empty templates root or missing _common/ dir returns an
// empty slice (not an error — fresh installs have no bundles yet).
func DiscoverBundles(workspacesRoot string) ([]Bundle, error) {
	base := filepath.Join(workspacesRoot, "templates", "_common")
	entries, err := os.ReadDir(base)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Bundle
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		dir := filepath.Join(base, name)
		b := Bundle{
			Name:      name,
			SourceDir: dir,
			Title:     name,
		}
		// Try playbook-fragment.md first; rules-fragment.md as backup.
		for _, candidate := range []string{"playbook-fragment.md", "rules-fragment.md"} {
			if title, desc := summariseFragment(filepath.Join(dir, candidate)); title != "" || desc != "" {
				if title != "" {
					b.Title = title
				}
				if desc != "" {
					b.Description = desc
				}
				break
			}
		}
		out = append(out, b)
	}
	return out, nil
}

// summariseFragment returns (title, firstParagraph) from a markdown
// fragment file. Title is the first H3 (the fragments use ### as their
// top-level since they're INSIDE a larger doc) or H2/H1 fallback.
// Description is the first non-blank, non-heading line, capped at
// ~120 chars on a word boundary.
func summariseFragment(path string) (title, description string) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	if len(body) > 4096 {
		body = body[:4096]
	}
	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		switch {
		case strings.HasPrefix(t, "### "):
			if title == "" {
				title = strings.TrimSpace(strings.TrimPrefix(t, "###"))
			}
		case strings.HasPrefix(t, "## "):
			if title == "" {
				title = strings.TrimSpace(strings.TrimPrefix(t, "##"))
			}
		case strings.HasPrefix(t, "# "):
			if title == "" {
				title = strings.TrimSpace(strings.TrimPrefix(t, "#"))
			}
		default:
			if description == "" {
				description = t
				if len(description) > 120 {
					cut := 117
					if sp := strings.LastIndexByte(description[:cut], ' '); sp > 60 {
						cut = sp
					}
					description = description[:cut] + "…"
				}
				return title, description
			}
		}
	}
	return title, description
}

// RelativeImportPath computes the path string a workpath.json should
// store in its `imports:` array to reach the bundle at
// <workspacesRoot>/templates/_common/<bundleName>.
//
// The result honors the loader's resolution rule (paths in imports:
// resolve relative to the PARENT of the workpath's source dir), so:
//
//   workpathDir = .../templates/<name>/           → "_common/<bundleName>"
//   workpathDir = .../templates/<name>/workpath/  → "../_common/<bundleName>"
//   workpathDir = .../chats/<chat>/workpath/      → "../../templates/_common/<bundleName>"
//
// Always returns forward-slash form so the JSON written stays
// platform-portable.
func RelativeImportPath(workpathDir, workspacesRoot, bundleName string) (string, error) {
	absWp, err := filepath.Abs(workpathDir)
	if err != nil {
		return "", err
	}
	absRoot, err := filepath.Abs(workspacesRoot)
	if err != nil {
		return "", err
	}
	target := filepath.Join(absRoot, "templates", "_common", bundleName)
	// Imports resolve relative to filepath.Dir(wp.SourceDir).
	parent := filepath.Dir(absWp)
	rel, err := filepath.Rel(parent, target)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}
