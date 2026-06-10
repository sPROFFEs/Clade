package core

// Memory distillation — converts a chat's raw turns into one Episode
// row through a single LLM call.
//
// Decision 3b: distillation endpoint is configurable per chat (the
// chat's own CLI, an explicit Ollama endpoint, or any cloud endpoint).
// The user can disable distillation entirely via IsMemoryEnabled().
//
// This file defines the small interface + helpers. Concrete distillers
// live in memory_distill_ollama.go and memory_distill_cli.go. The
// chat-lifecycle integration (60s debounce, end-of-chat trigger) is
// Phase 3c — this file is just the pure machinery.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// DistillEndpointKind enumerates the supported distillation backends.
type DistillEndpointKind string

const (
	// DistillKindNone disables distillation for this chat regardless
	// of the global memory toggle. Same as "off."
	DistillKindNone DistillEndpointKind = ""

	// DistillKindOllama hits an Ollama-compatible HTTP server.
	DistillKindOllama DistillEndpointKind = "ollama"

	// DistillKindCLI re-uses one of the registered CLIAdapter
	// implementations (claude, codex, …) in single-shot mode. Useful
	// when the user wants to summarise using the same model that
	// powered the chat.
	DistillKindCLI DistillEndpointKind = "cli"
)

// DistillEndpoint configures one distillation call. Fields apply
// per-Kind:
//
//   - Ollama: BaseURL + Model required; APIKey optional.
//   - CLI:    CLIName required (must match a registered CLIAdapter
//             Name()); other fields ignored.
type DistillEndpoint struct {
	Kind    DistillEndpointKind
	BaseURL string
	Model   string
	APIKey  string
	CLIName string
}

// DistillMessage is one turn from the source chat. Role is "user" or
// "assistant"; Tool messages should be coalesced into the matching
// assistant turn before being passed in (Phase 3c does that).
type DistillMessage struct {
	Role    string
	Content string
}

// DistillResult is the structured envelope every distiller must return.
// Mirrors the Episode struct minus the IDs; the caller fills those in
// when persisting via AddEpisode.
//
// PinnedCandidates are free-form facts the distiller thinks are worth
// promoting to long-term memory (e.g. "user uses fish shell"). Phase
// 3c's pinning logic decides whether to PinFact() each candidate.
type DistillResult struct {
	Summary          string   `json:"summary"`
	Topics           []string `json:"topics"`
	Entities         []string `json:"entities"`
	Decisions        []string `json:"decisions"`
	Actions          []string `json:"actions"`
	PinnedCandidates []string `json:"pinned_candidates"`
}

// Distiller turns chat turns into a DistillResult. Implementations
// must be safe for concurrent use across goroutines — the lifecycle
// goroutine may fire multiple distills if chats end faster than they
// can be summarised.
type Distiller interface {
	Name() string
	Available(ctx context.Context) error
	Distill(ctx context.Context, messages []DistillMessage) (*DistillResult, error)
}

// NewDistiller builds the distiller for ep. Returns (nil, nil) if
// ep.Kind == DistillKindNone — callers treat that as "skip this chat."
func NewDistiller(ep DistillEndpoint) (Distiller, error) {
	switch ep.Kind {
	case DistillKindNone:
		return nil, nil
	case DistillKindOllama:
		return newOllamaDistiller(ep)
	case DistillKindCLI:
		return newCLIDistiller(ep)
	default:
		return nil, fmt.Errorf("unknown distill kind %q", ep.Kind)
	}
}

// DistillPrompt is the system prompt every distiller sends. Worded to
// produce the DistillResult JSON shape exactly; subtle changes here
// can break parsing in every backend at once, so the prompt + the
// JSON schema are versioned together.
const DistillPrompt = `You are a memory distiller for a coding assistant.

Read the conversation that follows and return a compact JSON object
summarising what matters for future sessions. Use this exact schema —
no commentary, no markdown, just the JSON object:

{
  "summary":   string (3-5 sentences, neutral tone, what the user did/decided),
  "topics":    string[] (3-8 keywords/phrases, lowercase),
  "entities":  string[] (file paths, library names, people, proper nouns mentioned),
  "decisions": string[] (concrete choices the user made — "use Postgres", "skip the rewrite"),
  "actions":   string[] (open follow-ups the user committed to but did not finish),
  "pinned_candidates": string[] (durable facts about the user worth remembering across all future chats — "prefers spaces over tabs", "works on the auth team")
}

Rules:
- Every field is REQUIRED. Use [] for empty arrays. Use "" for an empty summary only if the transcript was empty.
- Do not invent facts not present in the transcript.
- pinned_candidates should be USER traits, not chat-specific decisions.
- Keep summaries factual and ~50 words.

Conversation:
`

// RenderDistillInput builds the prompt body the distiller sends to the
// model: DistillPrompt followed by the conversation formatted as
//
//	<role>: <content>
//	<role>: <content>
//
// Truncates each message at 4 KiB to bound the request size; the
// distiller already targets short conversations (one chat session).
func RenderDistillInput(messages []DistillMessage) string {
	var b strings.Builder
	b.WriteString(DistillPrompt)
	for _, m := range messages {
		role := m.Role
		if role == "" {
			role = "user"
		}
		content := m.Content
		if len(content) > 4096 {
			content = content[:4093] + "..."
		}
		b.WriteString(role)
		b.WriteString(": ")
		b.WriteString(content)
		b.WriteString("\n")
	}
	return b.String()
}

// ParseDistillJSON extracts a DistillResult from a model's reply. The
// reply may have leading prose despite our prompt; we scan for the
// first '{' and parse from there.
func ParseDistillJSON(body string) (*DistillResult, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, errors.New("empty distiller reply")
	}
	// Strip common code-fence wrappers if the model added them.
	body = strings.TrimPrefix(body, "```json")
	body = strings.TrimPrefix(body, "```")
	body = strings.TrimSuffix(body, "```")
	body = strings.TrimSpace(body)

	// Find the first '{' and the matching final '}'. Use a simple
	// depth counter — sufficient because the schema has no escaped
	// braces.
	start := strings.IndexByte(body, '{')
	if start < 0 {
		return nil, fmt.Errorf("no JSON object found in distiller reply (got %q)", truncDist(body))
	}
	depth, end := 0, -1
	for i := start; i < len(body); i++ {
		switch body[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
				break
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return nil, errors.New("unbalanced braces in distiller reply")
	}

	var out DistillResult
	if err := json.Unmarshal([]byte(body[start:end+1]), &out); err != nil {
		return nil, fmt.Errorf("decode distiller JSON: %w", err)
	}
	// Normalise nil slices to empty for predictable downstream code.
	if out.Topics == nil {
		out.Topics = []string{}
	}
	if out.Entities == nil {
		out.Entities = []string{}
	}
	if out.Decisions == nil {
		out.Decisions = []string{}
	}
	if out.Actions == nil {
		out.Actions = []string{}
	}
	if out.PinnedCandidates == nil {
		out.PinnedCandidates = []string{}
	}
	return &out, nil
}

func truncDist(s string) string {
	if len(s) <= 120 {
		return s
	}
	return s[:117] + "..."
}
