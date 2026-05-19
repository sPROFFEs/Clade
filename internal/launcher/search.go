package launcher

// Cross-chat search. The TUI's "/" screen calls this to find which
// past chats touched a topic — looks in MEMORY.md, summary.md, and
// transcript.jsonl files under every chat dir.
//
// Implementation is deliberately dumb: walk all chats, substring-match
// each file. Fast enough for the typical "tens of chats with kilobytes
// each" workload; if a user ever has thousands we can add an index.

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SearchHit is one matching location.
type SearchHit struct {
	ChatID    string
	ChatLabel string
	File      string // human-readable relative path within the chat
	AbsFile   string // absolute path on disk (for opening externally)
	LineNum   int    // 1-based; 0 when match was synthetic (e.g. filename only)
	Snippet   string // a window around the match, trimmed to ~200 chars
	Modified  time.Time
}

// SearchOptions tweaks the search. All optional.
type SearchOptions struct {
	// MaxHits caps total returned hits (default 200). Stops scanning
	// further files once reached.
	MaxHits int
	// PerFileCap caps hits per file (default 3) so a single transcript
	// can't dominate the result list.
	PerFileCap int
}

// Search walks every chat under root and returns substring matches
// for query (case-insensitive). An empty/whitespace-only query
// returns nil, nil — the UI uses that as "show nothing yet".
func Search(root, query string, opts SearchOptions) ([]SearchHit, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, nil
	}
	if opts.MaxHits <= 0 {
		opts.MaxHits = 200
	}
	if opts.PerFileCap <= 0 {
		opts.PerFileCap = 3
	}

	chats, err := ListChats(root)
	if err != nil {
		return nil, err
	}

	var hits []SearchHit
	for _, c := range chats {
		if len(hits) >= opts.MaxHits {
			break
		}
		hits = append(hits, searchChat(c, q, opts.MaxHits-len(hits), opts.PerFileCap)...)
	}
	// Sort by chat last-used desc, then by file path so the most-recent
	// chat surfaces first while still grouping within a chat.
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Modified.Equal(hits[j].Modified) {
			return hits[i].File < hits[j].File
		}
		return hits[i].Modified.After(hits[j].Modified)
	})
	return hits, nil
}

func searchChat(c Chat, q string, maxHits, perFileCap int) []SearchHit {
	var hits []SearchHit
	add := func(file, abs string, mod time.Time, lineNum int, snippet string) bool {
		hits = append(hits, SearchHit{
			ChatID:    c.ID,
			ChatLabel: c.Label,
			File:      file,
			AbsFile:   abs,
			LineNum:   lineNum,
			Snippet:   snippet,
			Modified:  mod,
		})
		return len(hits) < maxHits
	}

	// MEMORY.md — the canonical persistent memory.
	memPath := filepath.Join(c.Root, "MEMORY.md")
	if matches, mod := scanFile(memPath, q, perFileCap); len(matches) > 0 {
		for _, m := range matches {
			if !add("MEMORY.md", memPath, mod, m.LineNum, m.Snippet) {
				return hits
			}
		}
	}

	// Session summaries + transcripts.
	entries, err := os.ReadDir(c.SessionsDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return hits
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sessionRel := filepath.Join("sessions", e.Name())
		for _, fname := range []string{"summary.md", "transcript.jsonl"} {
			abs := filepath.Join(c.SessionsDir, e.Name(), fname)
			matches, mod := scanFile(abs, q, perFileCap)
			for _, m := range matches {
				if !add(filepath.ToSlash(filepath.Join(sessionRel, fname)), abs, mod, m.LineNum, m.Snippet) {
					return hits
				}
			}
		}
	}
	return hits
}

type fileMatch struct {
	LineNum int
	Snippet string
}

// scanFile reads a file line-by-line looking for q (already
// lowercased) as a substring. Returns up to cap matches and the
// file's modtime.
func scanFile(path, q string, cap int) ([]fileMatch, time.Time) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}
	}
	if info.Size() > 8*1024*1024 {
		// Cap scan size; transcript files can grow.
		return nil, info.ModTime()
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, info.ModTime()
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	// 1MB max line — JSONL lines can be long.
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var out []fileMatch
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if !strings.Contains(strings.ToLower(line), q) {
			continue
		}
		out = append(out, fileMatch{LineNum: lineNum, Snippet: trimSnippet(line, q, 200)})
		if len(out) >= cap {
			break
		}
	}
	return out, info.ModTime()
}

// trimSnippet centres a ~max-char window around the match so the user
// sees context, not just the start of the line.
func trimSnippet(line, q string, max int) string {
	if len(line) <= max {
		return strings.TrimSpace(line)
	}
	lower := strings.ToLower(line)
	idx := strings.Index(lower, q)
	if idx < 0 {
		idx = 0
	}
	start := idx - max/2
	if start < 0 {
		start = 0
	}
	end := start + max
	if end > len(line) {
		end = len(line)
		start = end - max
		if start < 0 {
			start = 0
		}
	}
	snip := line[start:end]
	if start > 0 {
		snip = "…" + snip
	}
	if end < len(line) {
		snip = snip + "…"
	}
	return strings.TrimSpace(snip)
}
