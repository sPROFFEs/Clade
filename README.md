# Clade

```
                       ╲ │ ╱
                        ╲│╱
                         λ
                        ╱│╲
                       ╱ │ ╲

       ██████╗ ██╗      █████╗ ██████╗ ███████╗
      ██╔════╝ ██║     ██╔══██╗██╔══██╗██╔════╝
      ██║      ██║     ███████║██║  ██║█████╗
      ██║      ██║     ██╔══██║██║  ██║██╔══╝
      ╚██████╗ ███████╗██║  ██║██████╔╝███████╗
       ╚═════╝ ╚══════╝╚═╝  ╚═╝╚═════╝ ╚══════╝

           fork agent chats from one common template
```

> **clade** *(noun, biology)*: a group of organisms that all descend
> from one common ancestor — exactly what happens when you fork a
> chat from a template. The Greek lower-case **λ** at the top is
> both a nod to Half-Life's logo and the cladogram fork that the
> launcher exists to manage.

A terminal launcher for agent CLIs (**Claude Code**, **Codex CLI**,
**OpenCode**) that pairs each session with a self-contained *template* —
a versioned bundle of mission, playbook, rules, shell tools, and
subagents — and clones it into a fresh isolated **chat** every time you
start working on something new.

```
┌──────────────────┐    ┌──────────────────────┐    ┌───────────────────┐
│ pick a chat to   │ →  │ (or start a new chat │ →  │ agent runs in     │
│ resume           │    │  from a template)    │    │ that chat's       │
│                  │    │                      │    │ private sandbox   │
└──────────────────┘    └──────────────────────┘    └───────────────────┘
```

Single static Go binary per OS. No runtime deps.

## The model

- **Template** — a reusable workpath pattern (mission + playbook + rules
  + tools + subagents). Templates don't run; they're cloned. Ship with
  two samples: `reversing`, `code-review`.
- **Chat** — a cloned-and-running instance of a template. Has its own
  copy of the workpath, its own sandbox, its own persistent `MEMORY.md`,
  and is bound to a specific agent CLI at creation. Each chat is a
  separate cwd, so Claude / Codex / OpenCode treat them as distinct
  projects and offer their own per-project session resume.

Layout under `<workspaces-root>/`:

```
templates/
├── reversing/workpath/             read-only pattern
└── code-review/workpath/
chats/
├── 20251017-1430-cve-fix/          one cloned instance per session
│   ├── chat.json                   {template, agent, createdAt, lastUsed}
│   ├── workpath/                   copied from template at creation
│   ├── sandbox/                    agent cwd, gitignored
│   ├── sessions/                   one subdir per agent launch
│   └── MEMORY.md                   persistent across re-opens
└── 20251017-1500-pr-123-review/
```

## What the launcher does for you

- **Home screen = chat list** sorted by last-used. Resume any past chat
  with Enter; the agent picks up its own transcript history because the
  cwd is stable.
- **New chat wizard** — pick a template → name the chat → pick an agent.
  Template is cloned, sandbox is compiled, agent is launched.
- **Template management** (`t` from home) — list / new / edit / delete.
- **Rich template wizard** — name + description + default response
  language + persistent `MEMORY.md` toggle + list of online skill repos
  to clone on every launch. Every chat cloned from the template inherits
  those defaults (and can override per-chat).
- **Agent CLI detection + install** — probes `claude` / `codex` /
  `opencode` on PATH. Detection requires `--version` to actually run, so
  broken installs are caught before launch. For missing agents the
  built-in installer offers per-OS commands (Homebrew / winget / curl /
  pnpm) with pnpm-first dependency handling, including auto-`corepack
  enable` and auto-`pnpm setup`.
- **Local-model routing (Ollama screen)** — probes a remote endpoint,
  lists models, configures any combination of Claude (per-chat env
  injection), Codex (`~/.codex/config.toml`), and OpenCode
  (`~/.config/opencode/opencode.json`).
- **Per-chat decoration on every launch** — language directive
  prepended to the compiled `SKILL.md` / `AGENTS.md`; `MEMORY.md` staged
  in the sandbox and synced back after the agent exits; online skills
  cloned via git into the agent's expected location.
- **Clean TTY hand-off** — Bubble Tea releases the terminal, the agent
  inherits stdio uniformly on every OS.

## Screens

> The launcher renders with [Bubble Tea](https://github.com/charmbracelet/bubbletea)
> + [Lip Gloss](https://github.com/charmbracelet/lipgloss) — k9s /
> lazygit-style chrome (rounded outer frame, title bar, help bar). The
> snippets below are the actual rendered output (colour stripped) — what
> you'd see in a real terminal session.

### Home — chat list

The home screen is your chat list, sorted by last-used. Resume any
past chat with Enter; persistent rows at the bottom take you into
the new-chat wizard or the template manager.

```
╭──────────────────────────────────────────────────────────────────────╮
│ clade │ Chats                                          ctrl-c quit   │
│ ──────────────────────────────────────────────────────────────────── │
│ › security-audit                (reversing · claude · 2h ago)        │
│     red-team audit of the auth flow                                  │
│   pr-123-review                 (code-review · codex · 1d ago)       │
│   cve-fix                       (code-review · claude · 4d ago)      │
│                                                                      │
│   + new chat…                                                        │
│   Manage templates →                                                 │
│ ──────────────────────────────────────────────────────────────────── │
│ ↑↓ select · enter open · n new · d delete · t templates · r refresh  │
╰──────────────────────────────────────────────────────────────────────╯
```

### New chat — template picker

Choose which template to fork from. Bundled samples (`reversing`,
`code-review`) appear here on first run.

```
╭──────────────────────────────────────────────────────────────────────╮
│ clade │ New chat · pick template                       ctrl-c quit   │
│ ──────────────────────────────────────────────────────────────────── │
│ › reversing       (binary analysis · IDA / Ghidra workflow)          │
│   code-review     (PR audit · diff + rules + memory)                 │
│                                                                      │
│   + new template…                                                    │
│ ──────────────────────────────────────────────────────────────────── │
│ ↑↓ select · enter pick · esc back                                    │
╰──────────────────────────────────────────────────────────────────────╯
```

### Agent picker

After naming the chat you pick an agent. Detection runs `--version`
on each candidate so broken installs are caught early; missing
agents reveal an `i` shortcut into the per-OS installer.

```
╭──────────────────────────────────────────────────────────────────────╮
│ clade │ Pick agent for "cve-fix"                       ctrl-c quit   │
│ ──────────────────────────────────────────────────────────────────── │
│ › claude       ✓ installed   1.0.92  (Anthropic Claude Code)         │
│   codex        ✓ installed   0.42.1  (OpenAI Codex CLI)              │
│   opencode     ✗ missing             [press i to install]            │
│   gemini       ✓ installed   0.42.0  (Google Gemini CLI)             │
│ ──────────────────────────────────────────────────────────────────── │
│ enter launch · i install · o ollama · esc back                       │
╰──────────────────────────────────────────────────────────────────────╯
```

### Ollama — local-model routing

Reachable from any chat via `o`. Probes a remote endpoint, lists the
installed models, and writes per-agent config (Claude env vars,
Codex `config.toml`, OpenCode `opencode.json`).

```
╭──────────────────────────────────────────────────────────────────────╮
│ clade │ Ollama config for "cve-fix"                    ctrl-c quit   │
│ ──────────────────────────────────────────────────────────────────── │
│ endpoint:   http://192.168.1.50:11434                                │
│ status:     ✓ reachable · 5 models                                   │
│                                                                      │
│ model:    › qwen3-coder:14b                                          │
│             llama3.1:8b                                              │
│             phi3:mini                                                │
│             gemma3:4b                                                │
│             gemini-nano (via bridge)                                 │
│                                                                      │
│ apply to: [x] claude   [x] codex   [ ] opencode                      │
│ ──────────────────────────────────────────────────────────────────── │
│ space toggle · enter apply · e edit endpoint · esc back              │
╰──────────────────────────────────────────────────────────────────────╯
```

### Launching — decoration + hand-off

When you confirm, the launcher compiles the chat's workpath into its
sandbox, stages `MEMORY.md`, clones any online skills, then releases
the TTY so the agent inherits stdio uniformly across OSes.

```
╭──────────────────────────────────────────────────────────────────────╮
│ clade │ Launching cve-fix                              ctrl-c quit   │
│ ──────────────────────────────────────────────────────────────────── │
│ ✓ compiled workpath → sandbox (target=claude)                        │
│ ✓ prepended personality.md → SKILL.md                                │
│ ✓ prepended language directive (en)                                  │
│ ✓ staged MEMORY.md in sandbox/                                       │
│ ✓ cloned online skill 'security-skills' (3 files)                    │
│ ✓ session marker appended to MEMORY.md                               │
│                                                                      │
│   spawning: claude  --model qwen3-coder:14b                          │
│   cwd:      /home/uwu/clade-workspaces/chats/cve-fix/sandbox         │
│                                                                      │
│   handing off TTY to the agent now...                                │
╰──────────────────────────────────────────────────────────────────────╯
```

## Past chatlog review

The launcher doesn't capture agent transcripts itself (each agent stores
them differently). Instead, **each chat has a stable cwd**, so when you
open it again:

- **Claude Code** sees the same project hash and lets you pick from past
  sessions via its own `claude /sessions` view (or `claude -c` to
  continue the most recent).
- **Codex CLI** stores sessions per project too; resume with `codex
  resume` from inside the chat.
- **OpenCode** offers `opencode --continue` from the chat dir.

In other words: the launcher hands the agent a consistent project; the
agent handles its own transcript browser. The MEMORY.md the launcher
syncs back gives you a portable, agent-agnostic place to keep notes you
want to survive even if the agent's session store is wiped.

## Install

### One-liner (recommended)

**Linux / macOS:**

```sh
curl -fsSL https://raw.githubusercontent.com/sPROFFEs/Clade/main/scripts/install.sh | bash
```

**Windows (PowerShell):**

```powershell
iwr -useb https://raw.githubusercontent.com/sPROFFEs/Clade/main/scripts/install.ps1 | iex
```

Both installers ask you to pick between:

1. **Download a prebuilt release** — grabs the archive attached to
   the [`release` tag](https://github.com/sPROFFEs/Clade/releases/tag/release)
   on GitHub (`clade-<os>-<arch>.{tar.gz,zip}`), extracts it, drops
   `clade` + `wpc` into the right per-OS install dir, and updates
   `PATH`. Asset names are version-less so the maintainer can swap
   the binaries on that single release without breaking the URL
   anyone runs.
2. **Build from source** — git-clones the repo to a temp dir and
   runs `go build`. If Go isn't installed the script offers to
   install it via the system package manager (`apt`, `dnf`,
   `pacman`, `zypper`, `apk`, `brew`, or `winget`) and asks for
   your confirmation before running anything.

The installer never edits your shell rc behind your back. If the
install dir isn't already on `$PATH` it prints the one line you'd
need to add — except on Windows, where it updates the persistent
User (or Machine) PATH via `[Environment]::SetEnvironmentVariable`.

Default destinations:

| OS              | Default dir                         | `--user` / no-admin equivalent      |
|-----------------|-------------------------------------|-------------------------------------|
| Linux / macOS   | `/usr/local/bin` (sudo if needed)   | `~/.local/bin`                       |
| Windows         | `%LOCALAPPDATA%\Programs\Clade`     | (same — no admin needed by default) |
| Windows `-AllUsers` | `%ProgramFiles%\Clade`           | requires elevated PowerShell        |

Common flags (POSIX form; the PS variants drop the leading `--`):

```sh
# Skip the prompt, force source build, install to ~/.local/bin:
curl -fsSL https://… | bash -s -- --source --user --yes

# Custom install dir:
curl -fsSL https://… | bash -s -- --prefix /opt/clade/bin
```

### From a release archive (no curl)

```sh
# Linux / macOS
tar -xzf clade-linux-amd64.tar.gz
cd linux-amd64
./scripts/install.sh        # auto-detects local binaries, skips download
```

```powershell
# Windows
Expand-Archive clade-windows-amd64.zip
cd windows-amd64
.\scripts\install.ps1       # same — uses the bundled binaries
```

You can also just run the binary in place from the extracted folder
(`./clade` on Linux/macOS, `.\clade.exe` on Windows) if you'd rather
not install globally yet.

### From source by hand

If you'd rather not run the installer:

```sh
git clone https://github.com/sPROFFEs/Clade.git
cd Clade
go build -o clade ./cmd/clade
go build -o wpc   ./cmd/wpc
sudo install -m 0755 clade wpc /usr/local/bin/   # or wherever you keep tools
```

### Manual PATH snippets

If `clade` is sitting somewhere not on `$PATH`, pick the row that
matches your shell / OS:

| Platform              | Add to PATH                                                                                                              |
|-----------------------|--------------------------------------------------------------------------------------------------------------------------|
| Linux / macOS (user)  | `echo 'export PATH="$PATH:$HOME/.local/bin"' >> ~/.bashrc` (or `~/.zshrc`)                                                |
| macOS Homebrew users  | Already on `$PATH` if you drop the binary into `$(brew --prefix)/bin/`                                                   |
| Windows (per-user)    | `setx PATH "%PATH%;%LOCALAPPDATA%\Programs\Clade"` (close + reopen the terminal).                                          |
| Windows (all users)   | From an elevated PowerShell: `[Environment]::SetEnvironmentVariable("PATH","$env:PATH;$env:ProgramFiles\Clade","Machine")`. |

After install, **open a new terminal** (so the new PATH propagates),
then verify:

```sh
clade -version   # prints e.g. "clade 0.1.0 linux/amd64"
```

## Quick start

```sh
./clade
```

First run is two short prompts (and only happens once — after that
the launcher jumps straight to your chat list):

1. **Workspaces root** — where everything lives. Default
   `~/clade-workspaces`.
2. **Seed bundled templates? (y/n, default yes)** — the launcher ships
   with two example templates (`reversing`, `code-review`). Pick `y`
   to copy them into `<root>/templates/` so you have something to chat
   against immediately; pick `n` to start with an empty workspace.
   You can add or remove templates later via `t` on the home screen.

Then:

3. Home screen → press `n` (or Enter on `+ new chat`) → pick a
   template → name the chat → pick an agent.
4. The chat is created, the workpath is compiled into its sandbox, the
   agent launches.

Next time:
1. Home shows your chat with "last used 2h ago".
2. Enter resumes it — Claude offers to continue the previous session
   from that cwd.

## Keys

### Home (chat list)
| Key       | Effect                              |
|-----------|-------------------------------------|
| `↑/↓ k/j` | Move selection                      |
| `enter`   | Open chat (or `+ new chat`)         |
| `n`       | New chat                            |
| `d`       | Delete highlighted chat (confirms)  |
| `t`       | Template management                 |
| `r`       | Refresh                             |
| `ctrl-c`  | Quit                                |

### Template list (`t` from home)
| Key     | Effect                                            |
|---------|---------------------------------------------------|
| `enter` | Edit settings of highlighted template             |
| `n`     | New template (full wizard)                        |
| `d`     | Delete (existing chats from it are unaffected)    |
| `esc`   | Back to chats                                     |

### Agents screen (during new-chat or open-chat)
| Key     | Effect                                            |
|---------|---------------------------------------------------|
| `enter` | Launch (if installed) or open installer (if not)  |
| `i`     | Install / upgrade the highlighted agent           |
| `o`     | Open the Ollama configuration screen              |

## Files on disk

| Path                                                | Holds                                                   |
|-----------------------------------------------------|---------------------------------------------------------|
| `~/.config/clade/config.json` (Linux/XDG)   | `workspacesRoot`, `lastAgent`                           |
| `~/Library/Application Support/clade/…` (macOS) | same                                                |
| `%AppData%\clade\config.json` (Windows)             | same                                                    |
| `<root>/templates/<name>/workpath/`                 | wpc source: `mission.md`, `playbook.md`, `rules.md`, `tools/`, `agents/` |
| `<root>/templates/<name>/template.json`             | default settings inherited by new chats                 |
| `<root>/chats/<chat-id>/workpath/`                  | cloned from template at chat creation                   |
| `<root>/chats/<chat-id>/sandbox/`                   | agent cwd; compiled artifacts; gitignored               |
| `<root>/chats/<chat-id>/chat.json`                  | `label`, `template`, `agent`, `lastUsed`, `settings`    |
| `<root>/chats/<chat-id>/MEMORY.md`                  | persistent memory; synced from sandbox after exit       |
| `<root>/chats/<chat-id>/sessions/<ts>-<agent>/`     | per-launch metadata (future: transcript)                |

## Launch sequence (open chat)

```
Home → pick chat "cve-fix" → pick agent (Codex)
                  │
                  ▼
   wpc compiles chat's workpath → chat's sandbox (target=codex)
   language directive prepended · MEMORY.md staged · online skills cloned
                  │
                  ▼
        <chat>/sandbox/AGENTS.md           ← Codex reads on every prompt
        <chat>/sandbox/AGENTS.assets/...   ← Tools + subagent prompts
        <chat>/sandbox/MEMORY.md           ← agent reads / writes
        <chat>/sandbox/SANDBOX.md          ← orientation file
                  │
                  ▼
   Bubble Tea releases the TTY; `codex` spawned with cwd=<chat>/sandbox
                  │
                  ▼
   Agent runs interactively · Codex sees the project, offers session resume
                  │ (agent exits)
                  ▼
   MEMORY.md synced back to <chat>/MEMORY.md
   chat.json's lastUsed bumped → next launcher run sorts this chat to top
   Exit code propagated to the shell
```

## Migration from v0.1

Earlier versions stored runtime instances directly at `<root>/<name>/`.
On startup the launcher auto-promotes those to `<root>/templates/<name>/`
and surfaces a one-line note: "Promoted N legacy workspace(s) to
templates." Existing chats from past launches don't exist in v0.1 — you
start fresh, with your old patterns now available as templates.

## Workpath source format

A workpath is a directory. Minimum:

```
my-workpath/
└── mission.md         required, non-empty
```

Full shape:

```
my-workpath/
├── workpath.json      optional metadata + tool/agent overrides
├── mission.md         required
├── playbook.md        optional staged process
├── rules.md           optional hard constraints
├── tools/             auto-registered shell scripts
│   ├── file_summary.sh
│   └── count_lines.ps1
└── agents/            named subagent prompts
    └── triage.md
```

The launcher compiles a chat's workpath into its sandbox using the
matching wpc target:

| Agent      | Target  | Output in sandbox                                            |
|------------|---------|--------------------------------------------------------------|
| Claude     | `claude`| `.claude/skills/<template>/SKILL.md` + `scripts/` + `.claude/agents/<template>__<agent>.md` |
| Codex      | `codex` | `AGENTS.md` + `AGENTS.assets/`                                |
| OpenCode   | `codex` | `AGENTS.md` + `AGENTS.assets/` (OpenCode reads `AGENTS.md` too) |

Two extra targets are useful when authoring templates for other tools:

| Target   | Output                                                |
|----------|-------------------------------------------------------|
| `mika`   | `modules/<name>/{module,playbook,rules}.md` + assets  |
| `cursor` | `.cursor/rules/<name>.mdc` (tools/agents inlined)     |

See [`docs/SCHEMA.md`](docs/SCHEMA.md), [`docs/TARGETS.md`](docs/TARGETS.md),
[`docs/ACTIVATION.md`](docs/ACTIVATION.md), and
[`docs/LAUNCHER.md`](docs/LAUNCHER.md) for the full reference.

## Repository layout

```
Clade/
├── cmd/
│   ├── Clade/      The Bubble Tea TUI (main binary)
│   └── wpc/                The compiler CLI (optional, for direct use)
├── internal/
│   ├── launcher/           Templates, chats, config, agents, launch, decorate, migrate
│   ├── installer/          Per-OS install methods + pnpm setup auto-resolve
│   ├── ollama/             Endpoint probe + per-agent config writers
│   └── skills/             Online-skill git fetcher
├── pkg/
│   ├── workpath/           wpc loader + validator (importable)
│   └── targets/            One file per output target (claude.go, codex.go, ...)
├── samples/workpaths/      Bundled samples seeded into templates/ on first run
├── scripts/                Cross-compile (build.sh / build.ps1)
├── testdata/               Fixtures for unit tests
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

Produces statically linked binaries under `dist/<os>-<arch>/` for
`windows-amd64`, `linux-amd64`, `linux-arm64`, `darwin-amd64`,
`darwin-arm64`. Each bundle ships both binaries (`clade`,
`wpc`), the `samples/` tree, the docs, and a tar.gz/zip archive.

## Tests

```sh
go test ./...
```

94 tests across 7 packages — template/chat storage and migration, TUI
screen transitions (driven with synthetic Bubble Tea messages, no TTY
needed), installer catalog per OS/agent plus pnpm auto-resolve, Ollama
config round-trips with `httptest`, online-skill git clones, and the
wpc compiler targets.

## Gemini + Ollama

The Ollama screen configures **Claude / Codex / OpenCode** automatically.
Gemini CLI is intentionally not in that list — here's why and how to
work around it.

**What didn't work:** the launcher tried injecting `OPENAI_API_KEY` +
`OPENAI_BASE_URL` (the env-var convention that works for Codex /
OpenCode). Gemini CLI 0.42+ ignores them and stays on its cached
Google OAuth credentials, then fails with `Model "..." was not found
or is invalid` when you try to use an Ollama model name.

**What does work** — pick one:

1. **Run a proxy (recommended).** [litellm](https://github.com/BerriAI/litellm)
   exposes Ollama as a real Gemini-API-compatible endpoint that the
   official Gemini CLI accepts:
   ```sh
   pip install 'litellm[proxy]'
   litellm --model ollama/qwen3-coder --host 0.0.0.0 --port 4000
   ```
   Then point Gemini at the proxy via its own config — depends on the
   CLI version you have installed.

2. **Hand-edit `~/.gemini/settings.json`** to flip
   `selectedAuthType` to the OpenAI provider in your Gemini CLI
   version. The exact schema has shifted between releases (the launcher
   doesn't auto-write it for this reason). Check `gemini --help`
   and your CLI's docs for the current shape.

When Gemini officially documents a stable OpenAI-compat config
mechanism, the launcher will pick it up — open an issue with the
shape and we'll add it.

## Roadmap

Not yet implemented; PRs welcome:

- First-class transcript browser (per-agent adapter for
  `~/.claude/projects/`, `~/.codex/sessions/`, etc.)
- Zip-archive online-skills (git-only today)
- Per-chat agent override (currently locked at creation)
- Rich chat search / tagging

## Why?

CLI coding agents have converged on similar primitives — instruction
files (skill / rule / `AGENTS.md`), shell tools, named subagents — but
each names them differently and stores them in a different place.
**Clade** lets you write the instructions once as a template,
clone a fresh chat from it every time you start a new task, and route
that chat through whichever agent you have on PATH today and whichever
model endpoint you have today — without forking `.claude/`, `.cursor/`,
or `modules/` trees by hand and without modifying your shell rc.
