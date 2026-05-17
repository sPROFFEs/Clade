# `waifu` — the TUI launcher

`waifu` is a Bubble Tea TUI that wraps `wpc` to give you an
agent-launch-pad: pick a workspace, pick an agent CLI, get dropped into
a sandbox with the workpath already compiled in.

## Status

**Phase 1 (this version) — what works**

- First-run wizard: prompts for a workspaces root, seeds the bundled
  samples (`reversing`, `code-review`).
- Workspace list / select.
- Minimal "new workspace" wizard (`name` + `description`).
- Per-workspace `sandbox/` auto-created, gitignored.
- Agent CLI detection via `exec.LookPath` + `--version` probe.
- `wpc compile --target <claude|codex>` into the sandbox before launch
  (called as a Go library — no shelling out to `wpc`).
- Clean TTY hand-off: Bubble Tea quits, the agent inherits stdio.

**Phase 2 (clearly deferred — UI shows this)**

- Install missing CLIs from inside the launcher (currently greys them
  out and points at `agent-cli-installer.sh`). Will be a TS-free port
  with pnpm-first dep handling.
- Local-model setup (currently the `ollama-local-ai-toggle.sh/.ps1`
  scripts work standalone).
- Rich creation wizard: memory file toggle, default language, online
  skill picker (e.g. [caveman](https://github.com/juliusbrussee/caveman)).
- Per-workspace chat-log retention (`chats/` dir is reserved).
- Workspace settings edit screen (`workspace.json` is read but not yet
  edited via the UI).

## Install

### From source

```sh
git clone <repo>
cd skills-project
go build -o waifu ./cmd/waifu      # waifu.exe on Windows
./waifu
```

### From a release archive

Unpack the `dist/<os>-<arch>/` bundle from `scripts/build.sh` anywhere.
Run the binary from inside the bundle — `samples/` is sitting next to it,
so first-run can seed it.

```sh
tar -xzf waifu-0.1.0-linux-amd64.tar.gz
cd linux-amd64
./waifu
```

## Configuration

| File                                          | Holds                                          |
|-----------------------------------------------|------------------------------------------------|
| `$XDG_CONFIG_HOME/waifu/config.json` (Linux)  | `workspacesRoot`, `lastAgent`, `wpcBinary?`   |
| `~/Library/Application Support/waifu/...` (macOS) | same                                       |
| `%AppData%\waifu\config.json` (Windows)       | same                                           |
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
        <ws>/sandbox/WAIFU_SANDBOX.md     ← Orientation file for the user
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

| Key      | Effect                                               |
|----------|------------------------------------------------------|
| ↑/↓ or k/j | Move selection                                     |
| enter    | Open / launch                                        |
| esc      | Back to the previous screen                          |
| r        | Refresh the workspace list                           |
| ctrl-c   | Quit immediately                                     |
