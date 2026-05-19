package launcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudeProjectSlug_Stable(t *testing.T) {
	got := claudeProjectSlug("/home/user/projects/foo.bar")
	want := "-home-user-projects-foo-bar"
	if got != want {
		t.Fatalf("slug = %q, want %q", got, want)
	}
}

func TestParseClaudeJSONL_BasicShape(t *testing.T) {
	raw := []byte(`{"type":"user","timestamp":"2025-05-19T10:00:00Z","message":{"content":[{"type":"text","text":"Hi"}]}}
{"type":"assistant","timestamp":"2025-05-19T10:00:05Z","message":{"content":[{"type":"text","text":"Hello back"},{"type":"tool_use","name":"Read"}]}}
{"type":"summary","summary":"Greeted user"}
not-json-just-noise
`)
	entries := parseClaudeJSONL(raw)
	if len(entries) < 4 {
		t.Fatalf("want >=4 entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].Kind != "user" || entries[0].Text != "Hi" {
		t.Errorf("first entry wrong: %+v", entries[0])
	}
	gotTool := false
	for _, e := range entries {
		if e.Kind == "tool_call" && e.Tool == "Read" {
			gotTool = true
		}
	}
	if !gotTool {
		t.Errorf("expected tool_call entry for Read, got %+v", entries)
	}
}

func TestCaptureTranscript_EmptyStoreReturnsNote(t *testing.T) {
	tmp := t.TempDir()
	dest := filepath.Join(tmp, "session")
	cap, err := CaptureTranscript(Agent{ID: AgentClaude}, tmp, time.Now(), dest)
	if err != nil {
		t.Fatalf("CaptureTranscript err: %v", err)
	}
	if cap.Note == "" {
		t.Error("expected a Note explaining nothing was captured")
	}
	if cap.DestPath != "" {
		t.Errorf("DestPath should be empty when nothing captured, got %q", cap.DestPath)
	}
}

func TestWriteSummary_WritesMarkdownAndJSON(t *testing.T) {
	tmp := t.TempDir()
	cap := CapturedTranscript{
		Agent: AgentClaude,
		Entries: []TranscriptEntry{
			{Kind: "user", Timestamp: time.Now(), Text: "Refactor the loader"},
			{Kind: "assistant", Timestamp: time.Now().Add(time.Minute), Text: "Done — three files touched."},
			{Kind: "tool_call", Tool: "Edit", Text: "Edit"},
		},
	}
	start := time.Now().Add(-5 * time.Minute)
	end := time.Now()
	s, err := WriteSummary(cap, tmp, start, end)
	if err != nil {
		t.Fatalf("WriteSummary err: %v", err)
	}
	if s.Headline == "" {
		t.Error("expected Headline")
	}
	if s.UserTurns != 1 || s.AssistantTurns != 1 || s.ToolCalls != 1 {
		t.Errorf("turn counts wrong: %+v", s)
	}
	for _, name := range []string{"summary.md", "summary.json"} {
		if _, err := os.Stat(filepath.Join(tmp, name)); err != nil {
			t.Errorf("expected %s to exist: %v", name, err)
		}
	}
}

func TestLoadRecentSummaries_NewestFirst(t *testing.T) {
	tmp := t.TempDir()
	dirs := []string{"20250101-100000-claude", "20250102-110000-claude", "20250103-090000-codex"}
	for i, d := range dirs {
		full := filepath.Join(tmp, d)
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatal(err)
		}
		s := SessionSummary{
			Agent:      []string{"claude", "claude", "codex"}[i],
			SessionDir: full,
			StartedAt:  time.Date(2025, 1, i+1, 10, 0, 0, 0, time.UTC),
			Headline:   d,
		}
		raw, _ := jsonMarshalIndent(s)
		if err := os.WriteFile(filepath.Join(full, "summary.json"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := LoadRecentSummaries(tmp, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 summaries, got %d", len(got))
	}
	if got[0].Headline != "20250103-090000-codex" {
		t.Errorf("expected newest first, got %q", got[0].Headline)
	}
}

func TestSearch_FindsMatchesAcrossFiles(t *testing.T) {
	root := t.TempDir()
	// Build a chat tree by hand: chats/<id>/{chat.json,MEMORY.md,sessions/<sid>/{summary.md,transcript.jsonl}}
	chatID := "20250519-100000-search-fixture"
	cRoot := filepath.Join(root, "chats", chatID)
	sessionDir := filepath.Join(cRoot, "sessions", "20250519-100000-claude")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// minimal chat.json so ListChats picks it up
	manifest := `{"label":"search fixture","agent":"claude","createdAt":"2025-05-19T10:00:00Z","lastUsed":"2025-05-19T10:00:00Z"}`
	if err := os.WriteFile(filepath.Join(cRoot, "chat.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cRoot, "MEMORY.md"),
		[]byte("# Memory\n\n## 2025-05-19 — Session opened\n\n### Refactor notes\nuser wants the loader refactored carefully\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "summary.md"),
		[]byte("# session\n\nDiscussed the LOADER refactor in depth\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hits, err := Search(root, "loader", SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) < 2 {
		t.Fatalf("expected at least 2 hits (MEMORY.md + summary.md), got %d: %+v", len(hits), hits)
	}
	gotMem, gotSum := false, false
	for _, h := range hits {
		if strings.Contains(h.File, "MEMORY.md") {
			gotMem = true
		}
		if strings.Contains(h.File, "summary.md") {
			gotSum = true
		}
	}
	if !gotMem || !gotSum {
		t.Errorf("expected hits in MEMORY.md and summary.md; gotMem=%v gotSum=%v", gotMem, gotSum)
	}
}

func TestMemoryHasContent(t *testing.T) {
	if memoryHasContent("# Memory\n\nThis file persists across\n\n## 2025-05-19 — Session opened\n_(workspace: foo)_\n") {
		t.Error("headers-only memory should report no content")
	}
	if !memoryHasContent("# Memory\n\n## 2025-05-19 — Session opened\n\n### a note\nremember to check the loader\n") {
		t.Error("memory with a note should report content")
	}
}
