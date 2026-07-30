# Activating a compiled workpath

`wpc compile` only writes files — it doesn't register anything. Each host
CLI picks the files up on its own terms. This page documents the **exact
out-dir, scope (project vs user), and reload behavior** for each supported
target.

This is standalone compiler documentation. A compiled target is not
automatically added to the PrAImate GUI's CLI list.

## Quick reference

| Target    | Out-dir to pass `--out`        | Scope                    | Reload         |
|-----------|--------------------------------|--------------------------|----------------|
| `claude`  | `<project>` or `~`             | project or user          | next session   |
| `cursor`  | `<project>`                    | project only             | next prompt    |
| `mika`    | `<project>` or `~/.mika`       | legacy project or user   | next session   |
| `codex`   | `<project>` or `~/.codex`      | project or user          | next prompt    |
| `generic` | anywhere                       | depends on host          | manual         |

"Project" means the out-dir is your repo root; the host CLI picks it up
when launched from that repo. "User" means the out-dir is your home dir
or a per-user config dir; every project sees it.

---

## Claude Code (`claude`)

```sh
# Per-project (recommended): only this repo gets the skill
wpc compile my-workpath --target claude --out .

# Per-user (global): every Claude Code session sees it
wpc compile my-workpath --target claude --out ~
```

This writes:

```
<out>/.claude/skills/<name>/SKILL.md      ← the skill itself
<out>/.claude/skills/<name>/scripts/*     ← tool scripts the skill can call
<out>/.claude/agents/<name>__<agent>.md   ← subagents, namespaced
```

Claude Code discovers skills by scanning `.claude/skills/` in the current
working directory and in `~/.claude/`. Subagents are picked up from
`.claude/agents/` the same way.

**Activation:** start (or restart) Claude Code from the project root.
`/skills` lists what was loaded; `/agents` lists subagents. If the skill
doesn't appear, check that `SKILL.md` is at `<out>/.claude/skills/<name>/`
(not nested deeper) and that the YAML frontmatter parsed (one-line
`description`).

**Gotcha:** the workpath name is kebab-cased on emit (Claude rejects
underscores in skill names). `my_workpath` becomes `my-workpath`.

---

## Cursor (`cursor`)

```sh
wpc compile my-workpath --target cursor --out .
```

This writes a single file:

```
<out>/.cursor/rules/<name>.mdc
```

Cursor auto-loads every `.mdc` under `.cursor/rules/` for the current
workspace. The frontmatter `alwaysApply: false` means the rule is offered
as a suggestion; flip it in the emitted file if you want the rule injected
on every prompt.

**Activation:** open the project in Cursor; the rule is available
immediately on the next prompt — no restart.

**Gotcha:** Cursor has no native concept of agent-callable tools or
subagents. The compiler inlines them into the rule body as a markdown
checklist so the agent *knows* they exist, but it cannot call them.
Scripts are **not** copied to disk. If you need executable tools, also
compile to `generic` or `mika` alongside.

---

## Legacy mika-code (`mika`)

```sh
# Per-project
wpc compile my-workpath --target mika --out .

# Per-user
wpc compile my-workpath --target mika --out ~/.mika
```

Output:

```
<out>/modules/<name>/module.md     ← header + mission body
<out>/modules/<name>/playbook.md   ← if non-empty
<out>/modules/<name>/rules.md      ← if non-empty
<out>/modules/<name>/tools/*       ← scripts
<out>/modules/<name>/agents/*      ← subagents (frontmatter preserved)
```

This is the exact layout mika's `internal/module` loader expects.

**Activation:** mika loads `modules/` on session start. Restart mika after
compiling. List modules from inside the session to confirm.

---

## Codex CLI (`codex`)

```sh
# Per-project: AGENTS.md at the repo root
wpc compile my-workpath --target codex --out .

# Per-user: ~/.codex/AGENTS.md applies to every Codex session
wpc compile my-workpath --target codex --out ~/.codex
```

Output:

```
<out>/AGENTS.md                ← mission + playbook + rules + tools/agents index
<out>/AGENTS.assets/tools/*    ← scripts, callable by relative path
<out>/AGENTS.assets/agents/*   ← subagent prompts
```

Codex CLI reads `AGENTS.md` from the project root and from `~/.codex/`.
Both are concatenated.

**Activation:** next `codex` invocation from the project root picks it up.
No restart of any daemon — Codex re-reads `AGENTS.md` each prompt.

**Gotcha:** Codex has no native subagent primitive. Subagents are listed
in `AGENTS.md` as "named personas the model can adopt"; their prompt
files in `AGENTS.assets/agents/` are referenced by path so the model can
read them on demand. Tool scripts are real files — the model can shell
out to them with the Bash tool.

**If you already have an `AGENTS.md`:** `--target codex` will overwrite
it. Compile to a scratch dir first (`--out /tmp/x`) and merge by hand, or
compile to `--target generic --out .` and append `<name>.md` to your
existing `AGENTS.md` manually.

---

## OpenCode and PrAImate Code

For project-scoped instructions, the `codex` target emits an `AGENTS.md`
layout that OpenCode and PrAImate Code can consume:

```sh
wpc compile my-workpath --target codex --out .
```

Activation: launch the CLI from that project after compiling. User-global
instruction paths are host-specific, so do not compile to `~/.codex` for
OpenCode unless that host is explicitly configured to read it.

---

## Any other CLI agent (`generic`)

```sh
wpc compile my-workpath --target generic --out .
```

Output:

```
<out>/<name>.md                ← self-contained markdown
<out>/<name>.assets/tools/*
<out>/<name>.assets/agents/*
```

Use this when the host CLI expects a single instruction file under a
specific name (e.g. `CLAUDE.md`, `GEMINI.md`, `WARP.md`). Rename
`<name>.md` to whatever the host expects, or symlink it.

---

## Compiling for everything at once

```sh
wpc compile my-workpath --target all --out .
```

Writes all five targets side by side. Safe to run in any project — each
target uses its own subtree (`.claude/`, `.cursor/`, `modules/`,
`AGENTS.md`, `<name>.md`).

## Re-compiling

Every target is **idempotent**: re-running with the same `--out` produces
the same tree (modulo file mtimes). It is safe to wire `wpc compile` into
a pre-commit hook, a `Makefile`, or `package.json` script so the compiled
output never drifts from the workpath source.
