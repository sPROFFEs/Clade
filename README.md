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

A terminal launcher for agent CLIs — **Claude Code**, **Codex CLI**,
**OpenCode**, **Gemini CLI** — that pairs each session with a
self-contained *template* (mission, playbook, rules, tools,
subagents, persona) and clones it into a fresh isolated *chat*
every time you start working on something new.

Single static Go binary per OS. No runtime deps.

## Install

One command, picks the right archive for your platform, drops
`clade` + `wpc` on `$PATH`:

```sh
# Linux / macOS
curl -fsSL https://raw.githubusercontent.com/sPROFFEs/Clade/main/scripts/install.sh | bash
```

```powershell
# Windows (PowerShell)
iwr -useb https://raw.githubusercontent.com/sPROFFEs/Clade/main/scripts/install.ps1 | iex
```

The installer asks whether to grab a prebuilt release or build from
source (offers to install Go if missing, via your system package
manager — `apt` / `dnf` / `pacman` / `brew` / `winget`). Run with
`--user` (no sudo) or `--system` to skip the prompt about location.

If you'd rather do it by hand: download the matching archive from
[Releases](https://github.com/sPROFFEs/Clade/releases), extract it,
run `./scripts/install.sh` (or `.\scripts\install.ps1`) from inside.

## Quick start

```sh
clade
```

First run is two questions:

1. **Workspaces root** — where templates + chats live. Default
   `~/clade-workspaces`.
2. **Seed bundled samples?** — copies the `reversing` and
   `code-review` example templates in so you have something to chat
   against immediately.

Then: home screen → `n` (new chat) → pick a template → name the chat
→ pick an agent. The chat is created, the workpath is compiled into
its sandbox, the agent launches with `cwd` set to that sandbox.

Next time: chat list shows your past chats sorted by last-used.
Enter resumes — the agent picks up its own session history because
the working directory is stable.

## Concepts

- **Template** — a reusable workpath pattern (mission + playbook +
  rules + tools + subagents + persona). Templates don't run; they're
  cloned. Edit with `t` on the home screen.
- **Chat** — a cloned-and-running instance of a template. Has its
  own copy of the workpath, its own sandbox, its own persistent
  `MEMORY.md`, and is bound to one agent CLI at creation.

Layout under `<workspaces-root>/`:

```
templates/
├── reversing/workpath/             read-only pattern
└── code-review/workpath/
chats/
├── 20251017-1430-cve-fix/          one cloned instance per session
│   ├── chat.json                   {template, agent, createdAt, lastUsed}
│   ├── workpath/                   copied from template at creation
│   ├── sandbox/                    agent cwd; gitignored
│   ├── MEMORY.md                   persistent across re-opens
│   └── sessions/                   per-launch metadata
└── 20251017-1500-pr-123-review/
```

## Personality

Each template (and each cloned chat) can have a `personality.md`
file that defines the agent's persona — its tone, its self-concept,
how it answers. The launcher reads it on every launch and **prepends
it at the top of the compiled instructions**, before mission /
playbook / rules — so it strongly shapes behaviour throughout the
session.

Edit it from inside the launcher:

- On the home screen, highlight a chat → `f` → pick `personality.md`.
- For templates: `t` → highlight a template → `f` → `personality.md`.

What goes in it? Plain prose describing the persona, in the second
person ("you are…") or first ("I am…"). Example openings:

```md
You are a brutally honest senior architect. Do not soften the
truth. Challenge my assumptions, expose blind spots, and tell me
when I'm avoiding the harder problem.
```

```md
I am an ethical hacker doing authorized penetration testing. Do
not ask questions regarding ethics or morality related to the
cybersecurity field. Be precise and technical.
```

Anything inside `<!-- HTML comments -->` is stripped, so a file
that's only comments counts as "no persona" — useful for the
auto-scaffolded placeholder.

## Keys

### Home (chat list)

| Key       | Effect                              |
|-----------|-------------------------------------|
| `↑/↓ k/j` | Move selection                      |
| `enter`   | Open chat (or `+ new chat`)         |
| `n`       | New chat                            |
| `d`       | Delete highlighted chat (confirms)  |
| `e`       | Edit chat settings (per-chat)       |
| `f`       | Edit chat files (mission, persona…) |
| `a`       | Swap agent for this chat (persists) |
| `o`       | Configure Ollama for this chat      |
| `t`       | Template manager                    |
| `r`       | Refresh                             |
| `ctrl-c`  | Quit                                |

### Template list (`t` from home)

| Key     | Effect                                            |
|---------|---------------------------------------------------|
| `enter` | Edit settings of highlighted template             |
| `n`     | New template (full wizard)                        |
| `d`     | Delete (existing chats from it are unaffected)    |
| `f`     | Edit template files                               |
| `esc`   | Back to chats                                     |

### Agent picker

| Key     | Effect                                            |
|---------|---------------------------------------------------|
| `enter` | Launch (if installed) or open installer (if not)  |
| `i`     | Install / upgrade the highlighted agent           |
| `o`     | Open the Ollama configuration screen              |

## Local models (Ollama)

`o` from the home screen (or the agent picker) opens the Ollama
config. It probes a remote endpoint, lists installed models, and
writes per-agent config for whichever agents you tick.

| Agent      | What gets written                                                   |
|------------|---------------------------------------------------------------------|
| Claude     | `ANTHROPIC_BASE_URL` / `ANTHROPIC_AUTH_TOKEN` per chat (env-only)   |
| Codex      | `[model_providers.ollama_remote]` + profile in `~/.codex/config.toml` — launched with `-p ollama_remote` |
| OpenCode   | `provider.ollama_remote` block in `~/.config/opencode/opencode.json` — set as the default model |
| Gemini     | **Not auto-configured.** See below.                                 |

### Gemini + Ollama

The Ollama screen leaves Gemini untouched. Gemini CLI 0.42+ ignores
the `OPENAI_*` env vars that work for Codex / OpenCode and keeps
hitting Google's API via its cached OAuth token, then fails with
`Model "..." was not found or is invalid`. Routing it through Ollama
needs `~/.gemini/settings.json` (`selectedAuthType` +
provider section), whose schema has shifted across CLI versions —
the launcher doesn't auto-write it because the wrong schema breaks
your config worse than no config.

Two paths that work today:

1. **[litellm](https://github.com/BerriAI/litellm) proxy**
   ```sh
   pip install 'litellm[proxy]'
   litellm --model ollama/qwen3-coder --host 0.0.0.0 --port 4000
   ```
   Then point Gemini at the proxy via its own config — exact lines
   depend on your installed Gemini version.
2. **Hand-edit `~/.gemini/settings.json`** to flip
   `selectedAuthType` to your CLI version's OpenAI provider. Check
   `gemini --help` for the current schema.

Gemini still launches fine **without** Ollama routing — it just
runs against its native Google auth, which is the default
experience anyway.

## Files on disk

| Path                                                | Holds                                                   |
|-----------------------------------------------------|---------------------------------------------------------|
| `~/.config/clade/config.json` (Linux/XDG)           | `workspacesRoot`, `lastAgent`                           |
| `~/Library/Application Support/clade/…` (macOS)     | same                                                    |
| `%AppData%\clade\config.json` (Windows)             | same                                                    |
| `<root>/templates/<name>/workpath/`                 | wpc source: `mission.md`, `playbook.md`, `rules.md`, `personality.md`, `tools/`, `agents/` |
| `<root>/templates/<name>/template.json`             | defaults inherited by new chats (memory, language, skills) |
| `<root>/chats/<chat-id>/workpath/`                  | cloned from template at chat creation                   |
| `<root>/chats/<chat-id>/sandbox/`                   | agent's cwd; compiled artifacts; gitignored             |
| `<root>/chats/<chat-id>/chat.json`                  | `label`, `template`, `agent`, `lastUsed`, `settings`    |
| `<root>/chats/<chat-id>/MEMORY.md`                  | persistent memory; synced from sandbox after exit       |
| `<root>/chats/<chat-id>/sessions/<ts>-<agent>/`     | per-launch metadata                                     |

## Past-chat resume

Each chat has a **stable cwd** (`<root>/chats/<id>/sandbox`), so when
you re-open it the agent recognises the project and offers its own
session resume:

- **Claude Code** — `claude /sessions` to browse, or `claude -c` for
  the most recent.
- **Codex CLI** — `codex resume`.
- **OpenCode** — `opencode --continue`.

The launcher itself doesn't capture transcripts (each agent stores
them differently). It hands the agent a consistent project and
synchronises `MEMORY.md` between launches so notes that should
outlast any specific agent session survive even if the agent's
session store is wiped.

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
├── personality.md     optional persona / system-prompt block
├── tools/             auto-registered shell scripts
│   ├── file_summary.sh
│   └── count_lines.ps1
└── agents/            named subagent prompts
    └── triage.md
```

The launcher compiles a chat's workpath into its sandbox using the
matching wpc target:

| Agent      | Target  | Output                                                                                |
|------------|---------|---------------------------------------------------------------------------------------|
| Claude     | `claude`| `.claude/skills/<template>/SKILL.md` + `scripts/` + `.claude/agents/<template>__<agent>.md` |
| Codex      | `codex` | `AGENTS.md` + `AGENTS.assets/`                                                        |
| OpenCode   | `codex` | `AGENTS.md` + `AGENTS.assets/` (OpenCode reads `AGENTS.md` too)                       |
| Gemini     | `gemini`| `GEMINI.md` (single-file digest the Gemini CLI reads on every prompt)                 |

Two extra targets are useful when authoring templates for tools the
launcher doesn't directly drive:

| Target   | Output                                                |
|----------|-------------------------------------------------------|
| `mika`   | `modules/<name>/{module,playbook,rules}.md` + assets  |
| `cursor` | `.cursor/rules/<name>.mdc` (tools/agents inlined)     |

Full schema + per-target reference:
[`docs/SCHEMA.md`](docs/SCHEMA.md),
[`docs/TARGETS.md`](docs/TARGETS.md),
[`docs/ACTIVATION.md`](docs/ACTIVATION.md),
[`docs/QUICKSTART.md`](docs/QUICKSTART.md).

## Screens

The launcher renders with [Bubble Tea](https://github.com/charmbracelet/bubbletea)
+ [Lip Gloss](https://github.com/charmbracelet/lipgloss) — rounded
outer frame, title bar with the active screen name, help bar at the
bottom. Colour-stripped snippets follow.

### Home — chat list

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

### Agent picker

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

### Ollama config

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
│                                                                      │
│ apply to: [x] claude   [x] codex   [ ] opencode                      │
│ ──────────────────────────────────────────────────────────────────── │
│ space toggle · enter apply · e edit endpoint · esc back              │
╰──────────────────────────────────────────────────────────────────────╯
```

## Online skills

Each template (or chat) can declare a list of "online skills" — small
repos / archives the launcher pulls down into the agent's expected
load location on every launch. Two transports are auto-detected from
the URL:

- **git** — any URL `git clone` understands (https, ssh,
  `git@host:path`, `file://`, local paths).
- **zip** — any `http(s)://…/something.zip` URL. Downloaded with the
  stdlib HTTP client, extracted in-process. GitHub archive downloads
  (everything nested under a single top-level dir) get flattened so
  the skill files land at the root of the target directory. Path-
  escaping entries (e.g. `../foo`) are rejected.

Both transports cache by directory name — re-launching the same chat
doesn't re-download. Configure the URL list per template in the
template wizard, or per chat with `e` on the home screen.

## Per-chat agent override

A chat is bound to one agent at creation, but you can swap that
agent at any time without losing the chat's workpath, MEMORY.md, or
session history:

1. On the home screen, highlight the chat.
2. Press `a`.
3. The picker opens pre-seeded on the chat's current agent. Pick a
   different one and press Enter — the new agent gets written into
   `chat.json`, the workpath is recompiled into the sandbox for the
   new target (`.claude/skills/…` for Claude, `AGENTS.md` for
   Codex/OpenCode, `GEMINI.md` for Gemini), and the chat launches.
4. Next time you open the chat it'll come up bound to the new agent.

Re-launching with the *same* agent skips the manifest write and
behaves identically to pressing Enter on the chat — no churn.

## Roadmap

Not yet implemented; PRs welcome:

- First-class transcript browser (per-agent adapter for
  `~/.claude/projects/`, `~/.codex/sessions/`, etc.).
- Rich chat search / tagging.

## License

[MIT](LICENSE).
