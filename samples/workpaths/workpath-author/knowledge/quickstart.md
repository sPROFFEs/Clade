# Workpath compiler quickstart

This is a five-minute walkthrough for the standalone `wpc` compiler bundled
with PrAImate. Workpaths are portable instruction packages; they are separate
from the database-backed YAML agents created on the GUI's **Agents** page.

Build or install PrAImate first, then confirm that `wpc` is available:

```sh
wpc targets
```

## 1. Scaffold

```sh
wpc init code-review
```

This creates:

```
code-review/
├── workpath.json    {"description": "...", "version": "1"}
├── mission.md       placeholder
├── playbook.md      "Stage 1 — Scout / Stage 2 — Execute"
├── rules.md         placeholder
├── tools/.gitkeep
└── agents/.gitkeep
```

## 2. Fill it in

Edit `mission.md` — describe in 2-3 sentences what this workpath does, what
inputs the agent will receive, and what artifact it should produce.

Edit `playbook.md` — list the stages, in order, the agent should follow.
Use H2 headings (`## Stage 1 — Scout`).

Edit `rules.md` — hard constraints. Bullet list. "Never X" and "Always Y".

Drop a `description` into `workpath.json`. Keep it to one line — every
target uses it in a discovery surface (Claude skill description, Cursor
rule description, etc.).

## 3. Add tools (optional)

Anything in `tools/` ending in `.sh` or `.ps1` becomes an agent-callable
command. Auto-discovery uses the filename as the tool name and the first
`# comment` line as the description:

```sh
#!/usr/bin/env bash
# Run linter on the changed files
git diff --name-only main | xargs eslint
```

That becomes a tool named `lint` (if the file is `lint.sh`) with that
description.

## 4. Add subagents (optional)

Anything in `agents/` ending in `.md` becomes a named subagent. The first
H1 is the description; the rest is the subagent's system prompt.

```md
# Quick keep/reject classifier on a PR

You are a triage gatekeeper. The calling agent hands you a PR URL.
...
```

## 5. Validate

```sh
wpc validate code-review
```

Should print `ok: code-review (N tools, M agents)`. Fix any errors before
compiling.

## 6. Compile to a target

```sh
# Claude Code:
wpc compile code-review --target claude --out .

# Cursor:
wpc compile code-review --target cursor --out .

# Codex CLI or an AGENTS.md-compatible host:
wpc compile code-review --target codex --out .

# Generic AGENTS.md-style:
wpc compile code-review --target generic --out .

# Legacy mika-code module layout:
wpc compile code-review --target mika --out .
```

Or all at once for a multi-tool team:

```sh
wpc compile code-review --target all --out dist/
```

## 7. Iterate

Re-running compile is idempotent — it overwrites the same files. The
authoritative source is your workpath directory; the compiled outputs are
disposable. See [TARGETS.md](TARGETS.md) for the exact files each target emits
and [ACTIVATION.md](ACTIVATION.md) for host-specific placement.
