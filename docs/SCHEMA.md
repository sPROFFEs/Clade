# Workpath source format

A workpath is a **directory** named after the workpath itself. The directory
name is the canonical workpath name unless overridden in `workpath.json`.

## Files

### `mission.md` — required

A markdown file describing what the workpath is for. Format is free; the
content is passed verbatim to most target outputs.

For back-compatibility with mika-code, `module.md` is accepted as a synonym
for `mission.md` (used when `mission.md` is absent).

### `workpath.json` — optional

Metadata and overrides. All fields optional.

```json
{
  "name": "my-workpath",
  "description": "One-line summary of what this workpath does",
  "version": "1",
  "license": "MIT",
  "imports": ["_common/graphify"],
  "tools": [
    {
      "name": "file_summary",
      "description": "Summarize a binary's header and sections",
      "script": "tools/file_summary.sh"
    }
  ],
  "agents": [
    {
      "name": "triage",
      "description": "Quick keep/reject classifier",
      "prompt": "agents/triage.md",
      "tools": ["file_summary"]
    }
  ]
}
```

| Field         | Type      | Default                          | Notes                                          |
|---------------|-----------|----------------------------------|------------------------------------------------|
| `name`        | string    | directory name                   | Must match `^[a-z0-9][a-z0-9_-]*$`             |
| `description` | string    | first paragraph of `mission.md`  | Required for compilation; one line             |
| `version`     | string    | `"1"`                            | Free-form                                      |
| `license`     | string    | `""`                             | Free-form; passed to `generic` target          |
| `imports`     | string[]  | `[]`                             | Capability bundles to merge in (see below)     |
| `tools`       | array     | auto-discovered from `tools/`    | When set, replaces auto-discovery entirely     |
| `agents`      | array     | auto-discovered from `agents/`   | When set, replaces auto-discovery entirely     |

#### `imports` — shared capability bundles

Pull `knowledge/`, `tools/`, `agents/`, `hooks.json`,
`playbook-fragment.md`, and `rules-fragment.md` from sibling bundles
under `templates/_common/` into this workpath at compile time. Saves
duplicating wrappers across every template that needs the same
capability.

- Each entry is resolved **relative to the parent of this workpath's
  source directory** (the "templates root"), so `_common/graphify`
  resolves to `<templates-root>/_common/graphify/`.
- Nested templates (e.g. `templates/praimate-dev/workpath/`) need an
  extra `..`: use `["../_common/graphify"]`.
- On name/path/event-collision: **the consumer wins**. A template
  tool / agent / knowledge file / hook (keyed by `event+matcher`)
  with the same key as an imported one overrides the import. Fragment
  text is always appended under `## Imported capabilities: <bundle>`
  and `## Imported rules: <bundle>` headings.
- A missing import is a **hard compile error** — typos fail loud.

### `playbook.md` — optional

A staged process. Convention: one H2 per stage (e.g. `## Stage 1 — Triage`).
Included verbatim in target output.

### `rules.md` — optional

Hard constraints, written as a bullet list. Included verbatim.

### `hooks.json` — optional

Chat-lifecycle triggers the agent runs as events fire. Cross-harness:
one source schema, each wpc target compiles to its native hook format.
Today only the `claude` target has a real emitter (writes
`.claude/settings.json`); other targets append a "hooks declared but
not wired" note to the compiled instructions so authors aren't
surprised.

```json
{
  "hooks": [
    {"event": "pre_tool",      "matcher": "Bash",  "command": "scripts/audit.sh"},
    {"event": "post_tool",     "matcher": "Edit",  "command": "scripts/lint.sh"},
    {"event": "session_start",                      "command": "scripts/setup.sh"},
    {"event": "session_stop",                       "command": "scripts/teardown.sh"}
  ]
}
```

Event vocabulary (portable Clade names → per-target mapping):

| Clade event       | Claude Code      | Codex / OpenCode / Gemini / Mika |
|-------------------|------------------|----------------------------------|
| `pre_tool`        | `PreToolUse`     | not yet wired                    |
| `post_tool`       | `PostToolUse`    | not yet wired                    |
| `user_input`      | `UserPromptSubmit` | not yet wired                  |
| `session_start`   | `SessionStart`   | not yet wired                    |
| `session_stop`    | `Stop`           | not yet wired                    |
| `subagent_stop`   | `SubagentStop`   | not yet wired                    |
| `notification`    | `Notification`   | not yet wired                    |

Per-hook fields:

| Field         | Required | Notes                                                  |
|---------------|----------|--------------------------------------------------------|
| `event`       | yes      | One of the seven above; loader errors on unknown.      |
| `matcher`     | no       | For `pre_tool`/`post_tool`: tool-name pattern (claude accepts regex). Empty = match every occurrence. Ignored for non-tool events. |
| `command`     | yes      | Shell command. Runs with the sandbox as CWD, so `scripts/<name>.sh` finds the wpc-staged tool dir. Empty / whitespace-only commands fail validation. |
| `description` | no       | One-line note; surfaced in the compiled instruction body. |

When merged from imports, collisions key on `event + matcher` and
**the consumer wins**. Different matchers on the same event coexist —
the import's `pre_tool/Bash` and the consumer's `pre_tool/Edit` both
fire.

### `tools/` — optional

Each `*.sh` and `*.ps1` becomes a tool the agent can invoke. Auto-discovery:

- **Name**: filename without extension (e.g. `file_summary.sh` → `file_summary`)
- **Description**: first non-shebang `# ...` comment line in the script
- **Shell**: `bash` for `.sh`, `pwsh` for `.ps1`

To override (rename, change description, expose a subset), use the
`tools` array in `workpath.json`. Once set, auto-discovery is disabled.

### `agents/` — optional

Each `*.md` becomes a named subagent. Auto-discovery:

- **Name**: filename without extension
- **Description**: first H1 (`# ...`), or first non-empty line

Override with the `agents` array. An agent's `tools` field is a name
allowlist; an empty/missing list means *all* workpath tools are available.

## Validation rules

Run `wpc validate <dir>` to check. Fatal errors:

- `name` doesn't match `^[a-z0-9][a-z0-9_-]*$`
- `description` is empty (manifest absent AND no derivable first paragraph)
- `mission.md`/`module.md` missing or empty
- Tool / agent name doesn't match `^[a-z0-9][a-z0-9_-]*$`
- Tool / agent name is duplicated
- Tool / agent has an empty script/prompt path
- Agent references a tool name that doesn't exist
