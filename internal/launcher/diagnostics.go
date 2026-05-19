package launcher

// Resume diagnostics for the TUI. Before the user hits Enter on a
// chat we surface what state will be reloaded: how big MEMORY.md is,
// how many prior sessions we captured, the headline of the most
// recent one, whether the agent's own session store is non-empty.

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ResumeDiagnostics is the read-only snapshot rendered next to the
// selected chat. All fields optional; "no data" is a valid state and
// the UI should handle it gracefully.
type ResumeDiagnostics struct {
	// MemoryBytes is the size of <chat>/MEMORY.md. Zero when the
	// file doesn't exist or memory is disabled.
	MemoryBytes int64
	// MemoryHasContent is true when MEMORY.md has at least one
	// non-marker line — i.e. the agent actually wrote something
	// durable last session.
	MemoryHasContent bool

	// CapturedSessions is the number of summary.json files we wrote
	// under sessions/. A proxy for "how much continuity is on disk".
	CapturedSessions int
	// LastHeadline is the Headline field of the newest captured
	// summary, when one exists.
	LastHeadline string
	// LastNote carries the Note from the newest captured summary
	// when capture was partial (helps explain "0 turns" entries).
	LastNote string

	// AgentNativeSessions is the count of session files the agent's
	// own store has for this sandbox. >0 means hitting `claude
	// /sessions` / `codex resume` / `opencode --continue` after
	// launch will resume something. -1 means we don't have a
	// detector for that agent yet.
	AgentNativeSessions int
	// AgentStorePath is where we looked for native sessions; empty
	// when no detector ran. Useful for debugging "why didn't resume
	// pick anything up".
	AgentStorePath string
}

// ComputeResumeDiagnostics inspects the chat's on-disk state and
// returns a ResumeDiagnostics snapshot. Cheap (stat-only + a single
// directory read for the session count).
func ComputeResumeDiagnostics(c Chat) ResumeDiagnostics {
	var d ResumeDiagnostics

	if info, err := os.Stat(filepath.Join(c.Root, "MEMORY.md")); err == nil && !info.IsDir() {
		d.MemoryBytes = info.Size()
		if raw, err := os.ReadFile(filepath.Join(c.Root, "MEMORY.md")); err == nil {
			d.MemoryHasContent = memoryHasContent(string(raw))
		}
	}

	if summaries, err := LoadRecentSummaries(c.SessionsDir, 1); err == nil {
		// Count the raw session dirs separately because not every
		// session necessarily produced a summary.json.
		d.CapturedSessions = countSessionDirs(c.SessionsDir)
		if len(summaries) > 0 {
			d.LastHeadline = summaries[0].Headline
			d.LastNote = summaries[0].Note
		}
	}

	d.AgentNativeSessions, d.AgentStorePath = countAgentNativeSessions(c.AgentID, c.SandboxDir)

	return d
}

// memoryHasContent returns true when MEMORY.md has at least one line
// of actual notes — i.e. anything beyond the auto-generated header
// and session-start markers. We use this to colour "memory carries
// notes" vs "memory file exists but is empty boilerplate" differently
// in the UI.
func memoryHasContent(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "#") {
			continue // header / session markers
		}
		if strings.HasPrefix(t, "_(workspace:") {
			continue // session-start subtitle
		}
		if strings.HasPrefix(t, "This file persists across") ||
			strings.HasPrefix(t, "Each session opens") ||
			strings.HasPrefix(t, "below the most recent") {
			continue // header prose
		}
		return true
	}
	return false
}

func countSessionDirs(sessionsDir string) int {
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0
		}
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			n++
		}
	}
	return n
}

// countAgentNativeSessions probes the agent's own session-store for
// this sandbox. Returns (count, store-path-checked) or (-1, "") when
// we don't have a detector for the agent.
func countAgentNativeSessions(id AgentID, sandboxDir string) (int, string) {
	home := homeDir()
	if home == "" {
		return -1, ""
	}
	switch id {
	case AgentClaude:
		dir := filepath.Join(home, ".claude", "projects", claudeProjectSlug(sandboxDir))
		return countJSONLFiles(dir), dir
	case AgentCodex:
		// Codex stores sessions by date, not cwd; scanning every
		// rollout to count matches would be expensive. Surface the
		// base path so the user knows where to look, and return 0
		// as a "unknown count, but resume is supported" sentinel.
		return -1, filepath.Join(home, ".codex", "sessions")
	case AgentOpenCode:
		dir := filepath.Join(home, ".opencode", "storage", "session", "info")
		return countMatchingOpenCodeSessions(dir, sandboxDir), dir
	case AgentGemini:
		return -1, filepath.Join(home, ".gemini", "tmp")
	}
	return -1, ""
}

func countJSONLFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".jsonl") {
			n++
		}
	}
	return n
}

func countMatchingOpenCodeSessions(infoDir, sandboxDir string) int {
	entries, err := os.ReadDir(infoDir)
	if err != nil {
		return 0
	}
	want := normaliseCwd(sandboxDir)
	n := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(infoDir, e.Name()))
		if err != nil {
			continue
		}
		// Cheap substring check — full JSON parse not worth it just
		// to count.
		if strings.Contains(strings.ToLower(string(raw)), `"directory":"`+strings.ToLower(want)) {
			n++
		}
	}
	return n
}
