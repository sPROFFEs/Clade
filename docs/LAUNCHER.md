# `code-launcher` — the TUI launcher

`code-launcher` is a Bubble Tea TUI that wraps `wpc` to give you an
agent-launch-pad: pick a workspace, pick an agent CLI, get dropped into
a sandbox with the workpath already compiled in.

## Status

**Phase 1 + Phase 2 ship in this version.** Phase 3 is the chat-transcript
capture work that hooks into per-agent session storage; not started yet.

**What works**

- First-run wizard: prompts for a workspaces root, seeds the bundled
  samples (`reversing`, `code-review`).
- Workspace list, select, refresh, **settings edit** (`s`).
- **Rich create wizard** (5 steps): name → description → language →
  memory toggle → online-skills list.
- Per-workspace `sandbox/` auto-created, gitignored.
- Agent CLI detection via `exec.LookPath` + `--version` probe.
- `wpc compile --target <claude|codex>` into the sandbox before launch
  (called as a Go library — no shelling out to `wpc`).
- **Install missing CLIs from inside the launcher** (`i`): per-OS catalog
  with pnpm-first dependency handling, prereq checks, command shown
  before execution, live output stream.
- **Ollama config screen** (`o`): probes the endpoint, lists models,
  configures any combination of Claude (per-workspace env injection),
  Codex (writes `~/.codex/config.toml`), and OpenCode (writes
  `~/.config/opencode/opencode.json`).
- **Per-workspace language directive** prepended to compiled
  `SKILL.md` / `AGENTS.md` so the agent replies in the right language.
- **Persistent `MEMORY.md`** at the workspace root, copied into the
  sandbox before launch and synced back after the agent exits.
- **Online skills** cloned via git into `.claude/skills/<name>/` (or
  `online-skills/<name>/` for codex/opencode) on every launch.
- **Chat-log dir**: each launch records a `chats/<timestamp>-<agent>/
  session.json` blob so past sessions are browseable.
- Clean TTY hand-off: Bubble Tea quits, the agent inherits stdio.

**Phase 3 (not started)**

- Actual transcript capture (each agent stores transcripts differently;
  needs per-agent adapters).
- Zip-archive online-skills support (currently git-only).
- Per-workspace agent-CLI installation pin (use a specific version
  per workspace).

## Install

### From source

```sh
git clone <repo>
cd skills-project
go build -o code-launcher ./cmd/code-launcher      # code-launcher.exe on Windows
./code-launcher
```

### From a release archive

Unpack the `dist/<os>-<arch>/` bundle from `scripts/build.sh` anywhere.
Run the binary from inside the bundle — `samples/` is sitting next to it,
so first-run can seed it.

```sh
tar -xzf code-launcher-0.1.0-linux-amd64.tar.gz
cd linux-amd64
./code-launcher
```

## Configuration

| File                                          | Holds                                          |
|-----------------------------------------------|------------------------------------------------|
| `$XDG_CONFIG_HOME/code-launcher/config.json` (Linux)  | `workspacesRoot`, `lastAgent`, `wpcBinary?`   |
| `~/Library/Application Support/code-launcher/...` (macOS) | same                                       |
| `%AppData%de-launcher\config.json` (Windows)       | same                                           |
| `<workspaces-root>/<name>/workpath/`          | wpc source (`mission.md`, `playbook.md`, …)    |
| `<workspaces-root>/<name>/sandbox/`           | Agent cwd; compiled artifacts; gitignored      |
| `<workspaces-root>/<name>/workspace.json`     | Future: `language`, `onlineSkills[]`, `memoryEnabled` |
| `<workspaces-root>/<name>/chats/`             | Reserved for Phase 2 transcript storage        |

## End-to-end launch sequence

```
User picks workspace "reversing" → picks Codex CLI
                            │
                            ▼
   targets.Get("codex").Compile(wp, <ws>/sandbox)
                            │
                            ▼
        <ws>/sandbox/AGENTS.md            ← Codex reads this on every prompt
        <ws>/sandbox/AGENTS.assets/...    ← Tools + subagent prompts
        <ws>/sandbox/SANDBOX.md     ← Orientation file for the user
                            │
                            ▼
            Bubble Tea quits; `codex` spawned with cwd=<ws>/sandbox
                            │
                            ▼
              Agent inherits stdin/stdout/stderr; user takes over
```

For **Claude Code** the target is `claude`, which writes
`<ws>/sandbox/.claude/skills/<name>/SKILL.md` + `scripts/` plus a sibling
`.claude/agents/<name>__<agent>.md` tree. Claude Code picks them up on
session start.

For **OpenCode** the launcher uses the `codex` target (OpenCode reads
`AGENTS.md` too). A dedicated `opencode` wpc target with native subagent
support is on the roadmap.

## Keys

### Workspaces screen
| Key   | Effect                                |
|-------|---------------------------------------|
| ↑/↓ k/j | Move selection                      |
| enter | Open workspace (or `+ new`)           |
| s     | Settings for highlighted workspace    |
| r     | Refresh list                          |

### Agents screen
| Key   | Effect                                            |
|-------|---------------------------------------------------|
| enter | Launch (if installed) or open installer (if not)  |
| i     | Install / upgrade the highlighted agent           |
| o     | Open the Ollama configuration screen              |
| esc   | Back                                              |

### Anywhere
| Key   | Effect                  |
|-------|-------------------------|
| ctrl-c| Quit immediately        |
| esc   | Step back one screen    |
