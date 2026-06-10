# References — OpenCode (sst/opencode)

PrAImate does **not** fork OpenCode. We treat it as an MIT-licensed
reference implementation and study specific subsystems for inspiration
when designing equivalent Go components.

The full rationale for not forking is in `knowledge/1.0-plan.md` § 10.

## Upstream

- Repo: <https://github.com/sst/opencode>
- License: MIT
- Stack: TypeScript (~69%) + Bun runtime, with a Go bubbletea TUI in
  `packages/tui/`. Server-side agent loop in TypeScript.

## What we study (read-only)

| OpenCode area | Relevance to PrAImate | Where we apply it |
|---|---|---|
| `packages/tui/` bubbletea patterns | Chat-stream rendering, multi-pane layouts, keybinding affordances | `cmd/praimate/` TUI (Phase 2+) |
| Web UI source (visual layout) | Component hierarchy, colour choices, sidebar shell | `cmd/praimate-gui` SvelteKit frontend (Phase 5) |
| `opencode.json` agent schema | Field naming, defaults, what to require vs. infer | `agent` YAML schema (Phase 2) |
| MCP integration code | OAuth 2.1 + DCR flow patterns | MCP catalogue (Phase 4a) |
| Provider abstraction | How a single agent loop fans out to multiple LLM providers | Background research only — PrAImate routes through CLI agents instead |

## What we do NOT take

- Source code (we reimplement in Go).
- Brand assets (logo, colours, fonts).
- Telemetry / analytics hooks.
- TypeScript runtime / Bun toolchain.
- Their agent loop — PrAImate is a harness, not its own coding agent.

## Attribution

This file is the paper trail. When a PrAImate component is materially
inspired by an OpenCode file, add an entry here:

```
- internal/core/mcp.go — OAuth flow modeled on opencode/packages/server/src/mcp/oauth.ts
```

This satisfies the spirit of MIT attribution without copying code.

## Re-evaluation trigger

If PrAImate 1.0 ships and the harness model proves to have a ceiling we
cannot break through (e.g. third-party CLIs lack a feature we need that
forking would unblock), revisit the fork question in a 2.0 conversation.
The relevant constraint then is the cost-vs-benefit math in
`knowledge/1.0-plan.md` § 10.3 (divergence tax).
