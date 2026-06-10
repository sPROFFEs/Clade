package core

// Retrieval planner — assembles the block of context that gets
// prepended to a new chat's first user message.
//
// Composition, in order:
//   1. Identity rows (always — they're cheap and load-bearing for tone)
//   2. Best-matching episode summary (keyword overlap)
//   3. Top-N pinned facts by salience
//
// Total stays within DefaultInjectionBudgetTokens (800 tokens ≈ 3200
// chars at 4 chars/token). The planner returns empty when:
//   - memory.enabled is off (and no per-chat override flips it on)
//   - there's nothing relevant (no identity rows, no matching episode,
//     no pinned facts above salience floor)
//
// Token counting is intentionally a cheap heuristic (chars/4). A real
// tokenizer would tie us to one model family; the heuristic is correct
// to within ±20% across all current English models and good enough for
// a budget that's already an order-of-magnitude looser than necessary.

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// DefaultInjectionBudgetTokens caps how many tokens BuildMemoryInjection
// will return. Matches Osaurus's 800-token default.
const DefaultInjectionBudgetTokens = 800

// charsPerTokenHeuristic is the conversion used for budgeting.
const charsPerTokenHeuristic = 4

// InjectionOptions tunes the planner per call. Zero values use sensible
// defaults; callers normally only set Query.
type InjectionOptions struct {
	// Query is the user's first message; used to score episodes. If
	// empty, the planner picks the most recent episode (degenerate but
	// non-empty signal).
	Query string

	// BudgetTokens caps the total injection size. Default 800.
	BudgetTokens int

	// MaxPinned caps how many pinned facts to include even if budget
	// allows more. Default 5.
	MaxPinned int

	// MinSalience filters out low-confidence pinned facts. Default 0.3.
	MinSalience float64

	// ChatOverride lets the caller pass per-chat memory overrides so
	// the planner respects per-chat opt-in/opt-out without re-loading
	// the row. Nil = follow global memory.enabled.
	ChatOverride *bool
}

func (o InjectionOptions) budget() int {
	if o.BudgetTokens > 0 {
		return o.BudgetTokens
	}
	return DefaultInjectionBudgetTokens
}
func (o InjectionOptions) maxPinned() int {
	if o.MaxPinned > 0 {
		return o.MaxPinned
	}
	return 5
}
func (o InjectionOptions) minSalience() float64 {
	if o.MinSalience > 0 {
		return o.MinSalience
	}
	return 0.3
}

// BuildMemoryInjection produces the prepend block for the next chat's
// first user message. Returns "" when memory is off or nothing
// relevant exists.
func (c *Core) BuildMemoryInjection(ctx context.Context, opts InjectionOptions) (string, error) {
	enabled, err := c.IsMemoryEnabled(ctx)
	if err != nil {
		return "", err
	}
	if opts.ChatOverride != nil {
		enabled = *opts.ChatOverride
	}
	if !enabled {
		return "", nil
	}

	budget := opts.budget() * charsPerTokenHeuristic

	var parts []string

	// 1. Identity.
	identityBlock, err := c.renderIdentityBlock(ctx, budget)
	if err != nil {
		return "", err
	}
	if identityBlock != "" {
		parts = append(parts, identityBlock)
		budget -= len(identityBlock) + 2 // separators
	}

	// 2. Best-matching episode.
	episodeBlock, err := c.renderEpisodeBlock(ctx, opts.Query, budget)
	if err != nil {
		return "", err
	}
	if episodeBlock != "" {
		parts = append(parts, episodeBlock)
		budget -= len(episodeBlock) + 2
	}

	// 3. Pinned facts (top N by salience above floor).
	pinnedBlock, err := c.renderPinnedBlock(ctx, opts.maxPinned(), opts.minSalience(), budget)
	if err != nil {
		return "", err
	}
	if pinnedBlock != "" {
		parts = append(parts, pinnedBlock)
	}

	if len(parts) == 0 {
		return "", nil
	}
	return "[Context from prior sessions]\n" + strings.Join(parts, "\n\n"), nil
}

func (c *Core) renderIdentityBlock(ctx context.Context, budget int) (string, error) {
	rows, err := c.ListIdentity(ctx)
	if err != nil || len(rows) == 0 {
		return "", err
	}
	var b strings.Builder
	b.WriteString("About the user:")
	for _, r := range rows {
		line := fmt.Sprintf("\n  %s: %s", r.Key, r.Value)
		if b.Len()+len(line) > budget {
			break
		}
		b.WriteString(line)
	}
	if b.Len() <= len("About the user:") {
		return "", nil
	}
	return b.String(), nil
}

func (c *Core) renderEpisodeBlock(ctx context.Context, query string, budget int) (string, error) {
	if budget < 100 {
		return "", nil
	}
	episodes, err := c.ListEpisodes(ctx, 50) // top-50 candidate pool
	if err != nil || len(episodes) == 0 {
		return "", err
	}
	best := pickBestEpisode(episodes, query)
	if best == nil {
		return "", nil
	}
	header := "Recent relevant session:"
	body := best.Summary
	max := budget - len(header) - 4
	if len(body) > max && max > 50 {
		body = body[:max-3] + "..."
	}
	if len(header)+len(body) > budget {
		return "", nil
	}
	return header + "\n  " + body, nil
}

func (c *Core) renderPinnedBlock(ctx context.Context, maxN int, minSal float64, budget int) (string, error) {
	if budget < 50 {
		return "", nil
	}
	facts, err := c.ListPinned(ctx, maxN*2) // overshoot then filter
	if err != nil || len(facts) == 0 {
		return "", err
	}
	var b strings.Builder
	b.WriteString("Things to remember:")
	count := 0
	for _, f := range facts {
		if f.Salience < minSal {
			continue
		}
		line := "\n  - " + f.Text
		if b.Len()+len(line) > budget {
			break
		}
		b.WriteString(line)
		count++
		if count >= maxN {
			break
		}
	}
	if count == 0 {
		return "", nil
	}
	return b.String(), nil
}

// pickBestEpisode scores each episode by keyword overlap with query
// (topics + entities + summary). Returns the highest-scoring one,
// or nil if no episode shares any keyword.
//
// When query is empty, returns the most recent episode (already
// sorted DESC by ListEpisodes).
func pickBestEpisode(episodes []Episode, query string) *Episode {
	if strings.TrimSpace(query) == "" {
		e := episodes[0]
		return &e
	}
	tokens := tokenise(query)
	if len(tokens) == 0 {
		e := episodes[0]
		return &e
	}
	type scored struct {
		ep    Episode
		score int
	}
	var ranked []scored
	for _, ep := range episodes {
		s := scoreEpisode(ep, tokens)
		if s > 0 {
			ranked = append(ranked, scored{ep, s})
		}
	}
	if len(ranked) == 0 {
		return nil
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	return &ranked[0].ep
}

func scoreEpisode(e Episode, tokens map[string]bool) int {
	hay := strings.ToLower(e.Summary)
	for _, t := range e.Topics {
		hay += " " + strings.ToLower(t)
	}
	for _, ent := range e.Entities {
		hay += " " + strings.ToLower(ent)
	}
	score := 0
	for tok := range tokens {
		if containsString(hay, tok) {
			score++
		}
	}
	return score
}

// tokenise lowercases the string and returns the unique set of tokens
// length ≥ 3 — short tokens are noisy ("is", "a") and inflate scores
// on episodes about anything.
func tokenise(s string) map[string]bool {
	out := map[string]bool{}
	cur := make([]byte, 0, 32)
	flush := func() {
		if len(cur) >= 3 {
			out[string(cur)] = true
		}
		cur = cur[:0]
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			cur = append(cur, c)
		case c >= 'A' && c <= 'Z':
			cur = append(cur, c+('a'-'A'))
		default:
			flush()
		}
	}
	flush()
	return out
}
