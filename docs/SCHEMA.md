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
| `tools`       | array     | auto-discovered from `tools/`    | When set, replaces auto-discovery entirely     |
| `agents`      | array     | auto-discovered from `agents/`   | When set, replaces auto-discovery entirely     |

### `playbook.md` — optional

A staged process. Convention: one H2 per stage (e.g. `## Stage 1 — Triage`).
Included verbatim in target output.

### `rules.md` — optional

Hard constraints, written as a bullet list. Included verbatim.

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
