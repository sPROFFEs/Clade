package core

// Distillation hook — reads a chat's stored messages, runs them through
// a Distiller, and writes the result into memory_episodes (plus pinned
// candidates into memory_pinned).
//
// The caller decides when to invoke this. Common patterns:
//
//   - The Recipes screen fires DistillChat in a fire-and-forget
//     goroutine when transitioning away from a completed-run view.
//
//   - The future "memory daemon" iterates over chats whose ended_at >
//     last_distilled and pending_distill = true.
//
// This file is the workhorse; lifecycle wiring is the caller's job.

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// DistillChat reads chat id's messages, distills them through ep, and
// persists the resulting Episode + any pinned candidates.
//
// Returns the new Episode id (or 0 if distillation was skipped because
// the chat had no substantive content).
//
// The endpoint is resolved by priority:
//
//   1. ep argument (caller-supplied)
//   2. chat.Settings.DistillEndpoint
//   3. global default (Phase 3c stores nothing global yet; future
//      Settings TUI work writes settings.memory.distill_endpoint)
//
// If no endpoint resolves OR memory is disabled (and no chat override
// flips it on), DistillChat returns (0, nil) — not an error.
func (c *Core) DistillChat(ctx context.Context, chatID string, ep *DistillEndpoint) (int64, error) {
	if c.store == nil {
		return 0, errors.New("DistillChat: no store configured")
	}
	if chatID == "" {
		return 0, errors.New("DistillChat: empty chatID")
	}

	chat, err := c.GetChat(ctx, chatID)
	if err != nil {
		return 0, err
	}

	// Cheap content check FIRST so the empty-chat case never probes
	// adapter registration or hits the network. Distilling a one-line
	// chat would burn tokens for a meaningless episode.
	messages, err := c.loadDistillMessages(ctx, chatID)
	if err != nil {
		return 0, err
	}
	if !hasMaterial(messages) {
		return 0, nil
	}

	endpoint, err := c.resolveDistillEndpoint(ctx, chat, ep)
	if err != nil {
		return 0, err
	}
	if endpoint == nil {
		return 0, nil
	}

	d, err := NewDistiller(*endpoint)
	if err != nil {
		return 0, err
	}
	if d == nil {
		return 0, nil
	}

	result, err := d.Distill(ctx, messages)
	if err != nil {
		return 0, fmt.Errorf("distill: %w", err)
	}

	epID, err := c.AddEpisode(ctx, &Episode{
		ChatID:    chatID,
		Summary:   result.Summary,
		Topics:    result.Topics,
		Entities:  result.Entities,
		Decisions: result.Decisions,
		Actions:   result.Actions,
		Salience:  0.5,
	})
	if err != nil {
		return 0, fmt.Errorf("save episode: %w", err)
	}

	// Pinned candidates start at moderate salience. Phase 3a's
	// PromotePinned will bump them when they recur; otherwise they
	// decay out.
	for _, cand := range dedup(result.PinnedCandidates) {
		if strings.TrimSpace(cand) == "" {
			continue
		}
		_, _ = c.PinFact(ctx, cand, 0.4)
	}
	return epID, nil
}

func (c *Core) resolveDistillEndpoint(ctx context.Context, chat *Chat, override *DistillEndpoint) (*DistillEndpoint, error) {
	if override != nil && override.Kind != DistillKindNone {
		return override, nil
	}
	if chat.Settings.DistillEndpoint != nil && chat.Settings.DistillEndpoint.Kind != DistillKindNone {
		return chat.Settings.DistillEndpoint, nil
	}

	enabled, err := c.IsMemoryEnabled(ctx)
	if err != nil {
		return nil, err
	}
	if chat.Settings.MemoryOverride != nil {
		enabled = *chat.Settings.MemoryOverride
	}
	if !enabled {
		return nil, nil
	}
	// Memory is on but no endpoint was set — caller wanted distillation
	// but didn't tell us where. Return nil rather than guessing; future
	// Settings TUI work will plug a default endpoint here.
	return nil, nil
}

func (c *Core) loadDistillMessages(ctx context.Context, chatID string) ([]DistillMessage, error) {
	msgs, err := c.ListMessages(ctx, chatID, 0)
	if err != nil {
		return nil, err
	}
	out := make([]DistillMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, DistillMessage{Role: m.Role, Content: m.Content})
	}
	return out, nil
}

// hasMaterial returns true if the message list has enough content to
// warrant distillation. Threshold: ≥1 assistant turn AND ≥40 total
// characters of content across all turns.
func hasMaterial(messages []DistillMessage) bool {
	if len(messages) == 0 {
		return false
	}
	totalChars := 0
	sawAssistant := false
	for _, m := range messages {
		totalChars += len(m.Content)
		if m.Role == "assistant" {
			sawAssistant = true
		}
	}
	return sawAssistant && totalChars >= 40
}

func dedup(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		k := strings.ToLower(strings.TrimSpace(s))
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	return out
}
