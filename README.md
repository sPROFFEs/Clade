# code-launcher

A terminal launcher for agent CLIs (**Claude Code**, **Codex CLI**,
**OpenCode**) that pairs each session with a self-contained *workpath* —
a versioned bundle of mission, playbook, rules, shell tools, and
subagents — and runs the agent in an isolated sandbox.

```
┌─────────────────────────┐    ┌──────────────────────────┐    ┌────────────────────┐
│  pick a workspace       │ →  │  pick an agent CLI       │ →  │  agent runs in a   │
│  (or scaffold a new one)│    │  (install if missing)    │    │  sandbox with the  │
│                         │    │  (route to local Ollama) │    │  workpath compiled │
└─────────────────────────┘    └──────────────────────────┘    └────────────────────┘
```

Single static Go binary per OS. No runtime deps.

## What it does for you

- **First run wizard** — picks a workspaces root, seeds two sample
  workpaths (`reversing`, `code-review`) so you have something to run
  immediately.
- **Workspaces** — each one bundles a `workpath/` (knowledge base —
  mission, playbook, rules, tools, subagents) and a `sandbox/` (the
  agent's working directory, gitignored). The launcher never lets the
  agent's cwd be the knowledge base.
- **Rich create wizard** — name, description, default response language,
  persistent `MEMORY.md` toggle, and a list of online skill repos to
  clone on every launch.
- **Agent CLI detection + install** — probes `claude` / `codex` /
  `opencode` on PATH. For missing ones, the **built-in installer**
  ships per-OS install commands (Homebrew / winget / curl / pnpm) with
  prereq checks (Node + pnpm, with auto-`corepack enable`) and shows
  the exact command before running it.
- **Local-model routing** — the **Ollama screen** probes a remote
  endpoint, lists models, and configures any combination of:
  - Claude (per-workspace `ANTHROPIC_*` env injection — no shell rc mutation)
  - Codex (idempotent writes to `~/.codex/config.toml`)
  - OpenCode (idempotent writes to `~/.config/opencode/opencode.json`)
- **Workpath compilation** — the wpc compiler (bundled, importable as
  a Go library) turns one workpath source into the right format for
  whichever agent you picked. The compiled tree is written into the
  sandbox.
- **Per-workspace decoration** — language directive prepended to the
  compiled `SKILL.md` / `AGENTS.md`; `MEMORY.md` staged and
  synced-back; online skills cloned into the agent's expected skills
  dir; chat session manifest dropped under `chats/`.
- **Clean TTY hand-off** — Bubble Tea releases the terminal, the agent
  inherits stdio uniformly on every OS.

## Install

### From a release archive

Each release ships pre-built binaries plus `samples/` and docs:

```sh
# Linux / macOS
tar -xzf code-launcher-0.1.0-linux-amd64.tar.gz
cd linux-amd64
./code-launcher
```

```powershell
# Windows
Expand-Archive code-launcher-0.1.0-windows-amd64.zip
.\windows-amd64\code-launcher.exe
```

### From source

Requires Go ≥ 1.21.

```sh
git clone http://192.168.100.86:3000/sdksdk/code-launcher.git
cd code-launcher
go build -o code-launcher ./cmd/code-launcher   # code-launcher.exe on Windows
./code-launcher
```

The optional `wpc` CLI (direct access to the compiler — useful for
authoring workpaths outside the launcher) is a separate binary in the
same module:

```sh
go build -o wpc ./cmd/wpc
wpc --help
```

## Quick start

```sh
./code-launcher
```

First run:

1. Choose where workspaces live (default `~/code-launcher-workspaces`).
2. The sample workpaths get seeded into it.
3. Pick one or scaffold a new one with the wizard.
4. Pick an agent. If it's not installed, press `enter` (or `i`) and the
   launcher walks you through installing it.
5. The agent launches in the workspace's sandbox with the workpath
   compiled in.

## Keys

### Workspaces screen
| Key       | Effect                              |
|-----------|-------------------------------------|
| `↑/↓ k/j` | Move selection                      |
| `enter`   | Open workspace (or `+ new`)         |
| `s`       | Settings for highlighted workspace  |
| `r`       | Refresh list                        |

### Agents screen
| Key     | Effect                                            |
|---------|---------------------------------------------------|
| `enter` | Launch (if installed) or open installer (if not)  |
| `i`     | Install / upgrade the highlighted agent           |
| `o`     | Open the Ollama configuration screen              |
| `esc`   | Back                                              |

### Anywhere
| Key      | Effect                  |
|----------|-------------------------|
| `ctrl-c` | Quit immediately        |
| `esc`    | Step back one screen    |

## Files on disk

| Path                                          | Holds                                                   |
|-----------------------------------------------|---------------------------------------------------------|
| `~/.config/code-launcher/config.json` (Linux/XDG) | `workspacesRoot`, `lastAgent`                         |
| `~/Library/Application Support/code-launcher/...` (macOS) | same                                          |
| `%AppData%\code-launcher\config.json` (Windows) | same                                                  |
| `<workspaces-root>/<name>/workpath/`          | wpc source — `mission.md`, `playbook.md`, `rules.md`, `tools/`, `agents/` |
| `<workspaces-root>/<name>/sandbox/`           | Agent cwd; compiled artifacts; gitignored               |
| `<workspaces-root>/<name>/workspace.json`     | `language`, `memoryEnabled`, `onlineSkills[]`, `ollama` |
| `<workspaces-root>/<name>/MEMORY.md`          | Persistent agent memory (synced from sandbox after exit)|
| `<workspaces-root>/<name>/chats/`             | One subdir per launch with `session.json`               |

## Launch sequence

```
User picks workspace "reversing" → picks Codex CLI
                  │
                  ▼
   wpc compiles <ws>/workpath → <ws>/sandbox (target=codex)
   language directive prepended · MEMORY.md staged · skills cloned
                  │
                  ▼
        <ws>/sandbox/AGENTS.md           ← Codex reads this on every prompt
        <ws>/sandbox/AGENTS.assets/...   ← Tools + subagent prompts
        <ws>/sandbox/MEMORY.md           ← agent reads / writes
        <ws>/sandbox/SANDBOX.md          ← Orientation file for the user
                  │
                  ▼
   Bubble Tea releases the TTY; `codex` spawned with cwd=<ws>/sandbox
                  │
                  ▼
   Agent runs interactively · waiting for the user
                  │ (agent exits)
                  ▼
   MEMORY.md synced back to <ws>/MEMORY.md
   Exit code propagated to the shell
```

## Workpath source format

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

The launcher compiles this into the right format for the chosen agent:

| Agent      | Target  | Output in sandbox                                          |
|------------|---------|------------------------------------------------------------|
| Claude     | `claude`| `.claude/skills/<name>/SKILL.md` + `scripts/` + `.claude/agents/<name>__<agent>.md` |
| Codex      | `codex` | `AGENTS.md` + `AGENTS.assets/`                              |
| OpenCode   | `codex` | `AGENTS.md` + `AGENTS.assets/` (reuses the codex target)    |

The compiler also supports two formats not used by the launcher itself
but useful when authoring workpaths for other tools:

| Target   | Output                                                |
|----------|-------------------------------------------------------|
| `mika`   | `modules/<name>/{module,playbook,rules}.md` + assets  |
| `cursor` | `.cursor/rules/<name>.mdc` (tools/agents inlined)     |

See [`docs/SCHEMA.md`](docs/SCHEMA.md) for the full schema and
[`docs/TARGETS.md`](docs/TARGETS.md) / [`docs/ACTIVATION.md`](docs/ACTIVATION.md)
for the per-target reference.

## Repository layout

```
code-launcher/
├── cmd/
│   ├── code-launcher/      The Bubble Tea TUI (main user-facing binary)
│   └── wpc/                The compiler CLI (optional, for direct use)
├── internal/
│   ├── launcher/           Config, workspaces, agents, launch, decorate
│   ├── installer/          Per-OS install methods for claude/codex/opencode
│   ├── ollama/             Endpoint probe + per-agent config writers
│   └── skills/             Online-skill git fetcher
├── pkg/
│   ├── workpath/           Loader + validator (importable)
│   └── targets/            One file per output target (claude.go, codex.go, ...)
├── samples/workpaths/      Bundled samples used by first-run seeding
├── scripts/                Cross-compile (build.sh / build.ps1)
├── testdata/               Fixtures for the unit tests
└── docs/
    ├── LAUNCHER.md         Full launcher reference
    ├── QUICKSTART.md       Workpath authoring quickstart
    ├── SCHEMA.md           Workpath schema reference
    ├── TARGETS.md          Per-target output documentation
    └── ACTIVATION.md       How each host CLI loads compiled output
```

## Cross-compile + release

```sh
# macOS / Linux / Git Bash
scripts/build.sh                 # all 5 targets → dist/
scripts/build.sh linux-amd64     # one target
scripts/build.sh --no-archive    # skip tar.gz/zip
```

```powershell
# Windows PowerShell
.\scripts\build.ps1
.\scripts\build.ps1 -Targets linux-amd64
.\scripts\build.ps1 -NoArchive
```

Produces statically linked binaries under `dist/<os>-<arch>/` for:
`windows-amd64`, `linux-amd64`, `linux-arm64`, `darwin-amd64`,
`darwin-arm64`. Each bundle ships both binaries (`code-launcher`,
`wpc`), the `samples/` tree, the docs, and a tar.gz/zip archive.

## Tests

```sh
go test ./...
```

75 tests across 7 packages — launcher logic, TUI screen transitions
(driven with synthetic Bubble Tea messages, no TTY needed), installer
catalog per OS/agent, Ollama config round-trips with an `httptest`
server, online-skill git clones, and the wpc compiler targets.

## Phase 3 — not started

- Actual transcript capture (each agent stores transcripts differently
  — needs per-agent adapters)
- Zip-archive online-skills (today: git-only)
- Per-workspace agent-CLI version pin
- Dedicated `opencode` wpc target with native subagent support
  (currently OpenCode reuses the `codex` target since both read `AGENTS.md`)

## Why?

CLI coding agents have converged on similar primitives — instruction
files (skill / rule / `AGENTS.md`), shell tools, named subagents — but
each names them differently and stores them in a different place.
**code-launcher** lets you author the instructions once, route to
whichever agent you have on PATH today, and route a single agent
through whichever model endpoint you have today, without forking
`.claude/`, `.cursor/`, or `modules/` trees by hand and without
modifying your shell rc files.
