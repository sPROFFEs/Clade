package launcher

// Workpath-injection token estimator. Surveys the on-disk content
// that will land in the agent's system prompt / first-turn context
// (CLAUDE.md / AGENTS.md / GEMINI.md, MEMORY.md, and the Option-C
// primer when enabled) and reports a rough token count.
//
// Estimation, not measurement — bytes / 4 is the GPT/Claude rule of
// thumb. Real numbers come from each agent's transcript after the
// fact (out of scope for T1). Off by ±20% in practice; good enough
// for "this chat warms up at ~N tokens" UX.

import (
	"os"
	"path/filepath"
)

// InjectionEstimate is what the TUI renders on the launching screen.
// Total is the sum of the per-source bytes / 4. The breakdown is
// preserved so the UI can show "where the tokens are going" rather
// than just one number.
type InjectionEstimate struct {
	Total int

	// Per-source byte counts. The launching screen renders a tidy
	// breakdown so the user understands what they're paying for.
	RootMarkdownBytes int   // CLAUDE.md / AGENTS.md / GEMINI.md
	MemoryBytes       int64 // MEMORY.md staged into sandbox
	KnowledgeBytes    int64 // knowledge/ tree (rough — manifest, not full)
	PrimerBytes       int   // Option-C primer prompt
}

const bytesPerToken = 4 // rough approximation

// EstimateInjection inspects the chat's sandbox + workspace settings
// and returns a rough token-count estimate for the next launch.
// Best-effort: missing files / unreadable trees contribute zero.
func EstimateInjection(c Chat, agent Agent) InjectionEstimate {
	var est InjectionEstimate

	// Root markdown — depends on the agent's wpc target.
	// claude → CLAUDE.md
	// codex / deepseek → AGENTS.md
	// gemini → GEMINI.md
	// opencode → AGENTS.md (uses codex target's output)
	var candidates []string
	switch agent.WpcTarget {
	case "claude":
		candidates = []string{"CLAUDE.md"}
	case "codex":
		candidates = []string{"AGENTS.md"}
	case "gemini":
		candidates = []string{"GEMINI.md"}
	}
	for _, name := range candidates {
		if info, err := os.Stat(filepath.Join(c.SandboxDir, name)); err == nil {
			est.RootMarkdownBytes += int(info.Size())
		}
	}

	// MEMORY.md — the staged sandbox copy (Decorate stages it on
	// every launch; pre-launch the workspace-root copy is the
	// authoritative size).
	if info, err := os.Stat(filepath.Join(c.Root, "MEMORY.md")); err == nil {
		est.MemoryBytes = info.Size()
	}

	// Knowledge/ tree — sum sizes recursively. The agent doesn't
	// read all of these into context (it pulls on demand), so this
	// is an UPPER bound; in practice usage is much lower. We still
	// surface it because a 5 MB knowledge tree IS a warning sign.
	knowledgeDir := filepath.Join(c.WorkpathDir, "knowledge")
	_ = filepath.Walk(knowledgeDir, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		est.KnowledgeBytes += info.Size()
		return nil
	})

	// Primer — only counts when enabled AND the agent supports it.
	// contextPrimerPrompt returns "" (len 0) for unsupported agents, so
	// this naturally contributes nothing for opencode/deepseek.
	if !c.Settings.DisableContextPrimer {
		est.PrimerBytes = len(contextPrimerPrompt(agent.ID))
	}

	est.Total = (est.RootMarkdownBytes + int(est.MemoryBytes) + est.PrimerBytes) / bytesPerToken
	// Knowledge is reported separately as an "on-demand budget"
	// rather than added to Total, since the agent doesn't load it
	// unconditionally.
	return est
}
