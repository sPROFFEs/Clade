# wpc — workpath compiler

Author a workpath once, compile it to **Claude Code skills**, **mika-code
workpaths**, **Cursor rules**, or a **generic AGENTS.md-style markdown**
for any CLI agent.

A *workpath* (or *skill-path*) is a self-contained bundle that turns a
fuzzy domain task ("reverse this binary", "review this Rails PR") into a
deterministic process: a one-line mission, a staged playbook, hard rules,
shell scripts the agent can call, and named subagents.

```
samples/workpaths/reversing/        →  wpc compile ... --target all
├── workpath.json   (optional)         │
├── mission.md                         ├─→  build/.claude/skills/reversing/SKILL.md  (+ scripts/)
├── playbook.md                        ├─→  build/.claude/agents/reversing__*.md
├── rules.md                           ├─→  build/modules/reversing/{module,playbook,rules}.md  (+ tools/, agents/)
├── tools/*.sh                         ├─→  build/.cursor/rules/reversing.mdc
└── agents/*.md                        └─→  build/reversing.md  (+ reversing.assets/)
```

## Install

```sh
go build -o wpc ./cmd/wpc
```

Single static binary, Go 1.21+. No runtime deps.

## Quick start

```sh
# Scaffold a new workpath
wpc init my-workpath

# Validate it
wpc validate my-workpath

# See all available output formats
wpc targets

# Compile to one format
wpc compile my-workpath --target claude --out build/

# Compile to every format at once
wpc compile my-workpath --target all --out build/
```

See [`docs/QUICKSTART.md`](docs/QUICKSTART.md) for a five-minute walk-through.

## Source format

A workpath is a directory. The minimum:

```
my-workpath/
└── mission.md         # required, non-empty
```

The full shape:

```
my-workpath/
├── workpath.json      # optional metadata + tool/agent overrides
├── mission.md         # required: what this workpath is for
├── playbook.md        # optional: staged process
├── rules.md           # optional: hard constraints
├── tools/             # optional: shell scripts, auto-registered
│   ├── file_summary.sh
│   └── count_lines.ps1
└── agents/            # optional: subagent prompts
    └── triage.md
```

See [`docs/SCHEMA.md`](docs/SCHEMA.md) for the complete field reference.

## Targets

| Target    | Output                                                                    |
|-----------|---------------------------------------------------------------------------|
| `claude`  | `.claude/skills/<name>/SKILL.md` + scripts + namespaced `.claude/agents/` |
| `mika`    | `modules/<name>/{module.md, playbook.md, rules.md, tools/, agents/}`      |
| `cursor`  | `.cursor/rules/<name>.mdc` (single file; tools/agents inlined)            |
| `codex`   | `AGENTS.md` + `AGENTS.assets/` (also works for OpenCode)                  |
| `generic` | `<name>.md` + `<name>.assets/` for any CLI agent                          |

See [`docs/TARGETS.md`](docs/TARGETS.md) for what each target produces and
the limitations of each, and [`docs/ACTIVATION.md`](docs/ACTIVATION.md) for
**how to actually turn the compiled output on** in each host CLI (where
files go, project vs user scope, what needs a restart).

## Layout

```
skills-project/
├── cmd/
│   ├── wpc/main.go         The compiler CLI
│   └── waifu/              The Bubble Tea TUI launcher
├── pkg/
│   ├── workpath/           Types, loader, validator
│   └── targets/            One target per file (claude.go, codex.go, ...)
├── internal/
│   └── launcher/           Launcher logic: config, workspaces, agents, launch
├── samples/workpaths/      Bundled sample workpaths (reversing, code-review)
├── scripts/                Cross-compile (build.sh / build.ps1)
├── testdata/               Fixtures for unit tests
└── docs/
    ├── QUICKSTART.md
    ├── SCHEMA.md
    ├── TARGETS.md
    └── ACTIVATION.md       How to turn compiled output on in each CLI
```

Tests:

```sh
go test ./...
```

## `waifu` — the TUI launcher

A second binary (`cmd/waifu`) in the same Go module wraps `wpc` end-to-end:

- First-run wizard → picks a workspaces root, seeds the bundled samples.
- Workspace list / select / create. Each workspace separates a `workpath/`
  (knowledge base, read-only as far as the launcher is concerned) from a
  `sandbox/` (the agent's working dir, gitignored).
- Detects which agent CLI (`claude`, `codex`, `opencode`) is installed.
- Compiles the workpath into the sandbox using the matching wpc target,
  then hands the TTY off to the agent.

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) +
Lip Gloss. Single static Go binary, no runtime deps.

```sh
go build -o waifu ./cmd/waifu      # or waifu.exe on Windows
./waifu
```

See [`docs/LAUNCHER.md`](docs/LAUNCHER.md) for the full launcher guide.

Phase 2 (deferred, called out in the UI): native install flow for missing
CLIs, Ollama local-model menu, rich creation wizard (memory/language/
online-skills), per-workspace chat-log retention.

## Cross-compile + release

```sh
# macOS / Linux / Git Bash:
scripts/build.sh                 # all 5 targets → dist/
scripts/build.sh linux-amd64     # one target
scripts/build.sh --no-archive    # skip tar.gz/zip

# Windows PowerShell:
.\scripts\build.ps1
.\scripts\build.ps1 -Targets linux-amd64
.\scripts\build.ps1 -NoArchive
```

Produces statically linked binaries under `dist/<os>-<arch>/` for:
`windows-amd64`, `linux-amd64`, `linux-arm64`, `darwin-amd64`,
`darwin-arm64`. Each bundle ships the binaries, `samples/`, the docs, and
a tar.gz/zip archive.

## Why?

CLI agents are converging on similar primitives — instruction files (skill,
rule, AGENTS.md), shell tools, and subagents — but each one names them
differently and stores them in a different place. Writing a workpath once
and recompiling beats forking your `.claude/`, `.cursor/`, and
`modules/` trees by hand.
