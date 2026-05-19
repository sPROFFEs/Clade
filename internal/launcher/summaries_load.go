package launcher

// Read-side helpers for stored session summaries. WriteSummary in
// summary.go writes a JSON sibling to each summary.md so this loader
// stays cheap and offline — no markdown parsing required.

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// LoadRecentSummaries returns up to limit prior session summaries
// under sessionsDir, newest first by StartedAt. Sessions without a
// summary.json (because the agent exited before transcript capture
// completed, or the format wasn't supported) are silently skipped.
// Returns an empty slice when the directory doesn't exist yet.
func LoadRecentSummaries(sessionsDir string, limit int) ([]SessionSummary, error) {
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []SessionSummary
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(sessionsDir, e.Name(), "summary.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var s SessionSummary
		if err := json.Unmarshal(raw, &s); err != nil {
			continue
		}
		if s.SessionDir == "" {
			s.SessionDir = filepath.Join(sessionsDir, e.Name())
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		ti := out[i].StartedAt
		if ti.IsZero() {
			ti = out[i].GeneratedAt
		}
		tj := out[j].StartedAt
		if tj.IsZero() {
			tj = out[j].GeneratedAt
		}
		return ti.After(tj)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
