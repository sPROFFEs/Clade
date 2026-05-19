package launcher

// Per-session summaries. After the agent exits and transcript.go has
// captured the JSONL, this file turns the parsed entries into a small
// markdown digest the launcher can both (a) show in resume diagnostics
// and (b) inject into the next launch's compiled instructions.
//
// Summaries are rule-based, not LLM-generated. The agent itself can
// produce a better summary by re-reading the transcript during the
// next session if it wants — what we need here is a deterministic,
// offline, free-of-side-effects digest the user can rely on.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SessionSummary is the structured shape we write to disk + render in
// the TUI. All fields optional except Agent, SessionDir, GeneratedAt.
type SessionSummary struct {
	Agent       string    `json:"agent"`
	SessionDir  string    `json:"sessionDir"`
	GeneratedAt time.Time `json:"generatedAt"`

	StartedAt time.Time `json:"startedAt,omitempty"`
	EndedAt   time.Time `json:"endedAt,omitempty"`
	Duration  string    `json:"duration,omitempty"`

	UserTurns      int      `json:"userTurns"`
	AssistantTurns int      `json:"assistantTurns"`
	ToolCalls      int      `json:"toolCalls"`
	ToolNames      []string `json:"toolNames,omitempty"`

	FirstUserExcerpt    string `json:"firstUserExcerpt,omitempty"`
	LastAssistantExcerpt string `json:"lastAssistantExcerpt,omitempty"`

	Headline string `json:"headline,omitempty"`
	Note     string `json:"note,omitempty"`
}

// WriteSummary renders the markdown digest and writes it to
// <sessionDir>/summary.md. It also drops a machine-readable
// summary.json next to it so future code (search, diagnostics) can
// load without re-parsing markdown.
//
// startedAt is the launch time the caller observed; endedAt is the
// process-exit time. Either may be zero — we'll fall back to the
// transcript's own timestamps when available.
func WriteSummary(cap CapturedTranscript, sessionDir string, startedAt, endedAt time.Time) (SessionSummary, error) {
	s := SessionSummary{
		Agent:       string(cap.Agent),
		SessionDir:  sessionDir,
		GeneratedAt: time.Now().UTC(),
		StartedAt:   startedAt,
		EndedAt:     endedAt,
		Note:        cap.Note,
	}
	if !cap.StartedAt.IsZero() {
		s.StartedAt = cap.StartedAt
	}
	if !cap.EndedAt.IsZero() {
		s.EndedAt = cap.EndedAt
	}
	if !s.StartedAt.IsZero() && !s.EndedAt.IsZero() && s.EndedAt.After(s.StartedAt) {
		s.Duration = humanDuration(s.EndedAt.Sub(s.StartedAt))
	}

	toolCounts := map[string]int{}
	for _, e := range cap.Entries {
		switch e.Kind {
		case "user":
			s.UserTurns++
			if s.FirstUserExcerpt == "" {
				s.FirstUserExcerpt = excerpt(e.Text, 240)
			}
		case "assistant":
			s.AssistantTurns++
			s.LastAssistantExcerpt = excerpt(e.Text, 240)
		case "tool_call":
			s.ToolCalls++
			if e.Tool != "" {
				toolCounts[e.Tool]++
			}
		}
	}
	for name := range toolCounts {
		s.ToolNames = append(s.ToolNames, name)
	}
	sort.Strings(s.ToolNames)

	// Headline: best one-line label for resume-diagnostics + injected
	// directive. Priority: first user excerpt → last assistant
	// excerpt → note → date stamp.
	switch {
	case s.FirstUserExcerpt != "":
		s.Headline = excerpt(s.FirstUserExcerpt, 90)
	case s.LastAssistantExcerpt != "":
		s.Headline = excerpt(s.LastAssistantExcerpt, 90)
	case cap.Note != "":
		s.Headline = cap.Note
	default:
		s.Headline = "session — no captured turns"
	}

	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return s, err
	}
	md := renderSummaryMarkdown(s, cap)
	if err := os.WriteFile(filepath.Join(sessionDir, "summary.md"), []byte(md), 0o644); err != nil {
		return s, err
	}
	if raw, err := jsonMarshalIndent(s); err == nil {
		_ = os.WriteFile(filepath.Join(sessionDir, "summary.json"), raw, 0o644)
	}
	return s, nil
}

func renderSummaryMarkdown(s SessionSummary, cap CapturedTranscript) string {
	var b strings.Builder
	title := s.Headline
	if title == "" {
		title = "session"
	}
	stamp := s.StartedAt
	if stamp.IsZero() {
		stamp = s.GeneratedAt
	}
	fmt.Fprintf(&b, "# %s — %s\n\n", stamp.Local().Format("2006-01-02 15:04"), title)

	fmt.Fprintf(&b, "- Agent: `%s`\n", s.Agent)
	if s.Duration != "" {
		fmt.Fprintf(&b, "- Duration: %s\n", s.Duration)
	}
	if s.UserTurns > 0 || s.AssistantTurns > 0 || s.ToolCalls > 0 {
		fmt.Fprintf(&b, "- Turns: %d user · %d assistant · %d tool calls\n",
			s.UserTurns, s.AssistantTurns, s.ToolCalls)
	}
	if len(s.ToolNames) > 0 {
		fmt.Fprintf(&b, "- Tools used: %s\n", strings.Join(s.ToolNames, ", "))
	}
	if cap.SourcePath != "" {
		fmt.Fprintf(&b, "- Source transcript: `%s`\n", cap.SourcePath)
	}
	if cap.DestPath != "" {
		rel := cap.DestPath
		if abs, err := filepath.Abs(cap.DestPath); err == nil {
			rel = abs
		}
		fmt.Fprintf(&b, "- Captured to: `%s`\n", rel)
	}
	b.WriteString("\n")

	if s.FirstUserExcerpt != "" {
		b.WriteString("## First user message\n\n")
		b.WriteString("> " + quoteWrap(s.FirstUserExcerpt) + "\n\n")
	}
	if s.LastAssistantExcerpt != "" {
		b.WriteString("## Last assistant message\n\n")
		b.WriteString("> " + quoteWrap(s.LastAssistantExcerpt) + "\n\n")
	}
	if s.Note != "" {
		b.WriteString("## Note\n\n")
		b.WriteString(s.Note + "\n")
	}
	return b.String()
}

// excerpt cuts text to maxRunes runes on a word boundary, collapsing
// internal whitespace. Returns "" for empty input.
func excerpt(text string, max int) string {
	t := strings.TrimSpace(text)
	if t == "" {
		return ""
	}
	// Collapse newlines + runs of whitespace to single spaces.
	fields := strings.Fields(t)
	joined := strings.Join(fields, " ")
	runes := []rune(joined)
	if len(runes) <= max {
		return joined
	}
	// Trim to last space within the limit so we don't cut mid-word.
	cut := max
	for cut > 0 && runes[cut] != ' ' {
		cut--
	}
	if cut <= max/2 {
		cut = max
	}
	return string(runes[:cut]) + "…"
}

// quoteWrap turns a single-line excerpt into a multi-line markdown
// blockquote by injecting "> " after newlines.
func quoteWrap(s string) string {
	s = strings.ReplaceAll(s, "\n", "\n> ")
	return s
}

// humanDuration formats a duration as "1h 23m" / "47m" / "2s" — kept
// short so it fits in the diagnostics panel.
func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) - h*60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// jsonMarshalIndent wraps json.MarshalIndent so we can produce a
// trailing newline for tidy diffs without leaking the import into the
// rest of the file.
func jsonMarshalIndent(v interface{}) ([]byte, error) {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}
