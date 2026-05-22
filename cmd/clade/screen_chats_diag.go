package main

// Resume diagnostics rendering for the chat list. Inline under the
// selected chat: shows MEMORY size, captured session count + last
// summary headline, and (where we can probe it) how many native
// agent sessions are resumable.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/sPROFFEs/Clade/internal/launcher"
)

func renderResumeDiagnostics(c launcher.Chat) string {
	d := launcher.ComputeResumeDiagnostics(c)
	if !diagnosticsHasAny(d) {
		return descStyle.Render("    ↩ resume: nothing captured yet · this will start cold")
	}

	var bits []string
	if d.MemoryBytes > 0 {
		size := humanBytes(d.MemoryBytes)
		if d.MemoryHasContent {
			bits = append(bits, fmt.Sprintf("memory %s", size))
		} else {
			bits = append(bits, fmt.Sprintf("memory %s (empty)", size))
		}
	}
	if d.CapturedSessions > 0 {
		bits = append(bits, fmt.Sprintf("%d captured", d.CapturedSessions))
	}
	switch {
	case d.AgentNativeSessions > 0:
		bits = append(bits, fmt.Sprintf("%d native resumable", d.AgentNativeSessions))
	case d.AgentNativeSessions == 0 && d.AgentStorePath != "":
		bits = append(bits, "native store empty")
	}

	var b strings.Builder
	b.WriteString(diagStyle().Render("    ↩ resume · " + strings.Join(bits, " · ")))
	if d.LastHeadline != "" {
		b.WriteString("\n")
		hl := d.LastHeadline
		if len(hl) > 90 {
			hl = hl[:90] + "…"
		}
		b.WriteString(diagStyle().Render("      last: " + hl))
	} else if d.LastNote != "" {
		b.WriteString("\n")
		note := d.LastNote
		if len(note) > 90 {
			note = note[:90] + "…"
		}
		b.WriteString(diagStyle().Render("      note: " + note))
	}
	return b.String()
}

func diagnosticsHasAny(d launcher.ResumeDiagnostics) bool {
	return d.MemoryBytes > 0 ||
		d.CapturedSessions > 0 ||
		d.AgentNativeSessions > 0
}

func diagStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Subtitle)
}

// humanBytes formats n as "1.2 KB" / "342 B" / "5.6 MB" for compact
// display in the diagnostics line.
func humanBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}
