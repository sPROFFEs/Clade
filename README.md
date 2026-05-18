# Clade
<p align="center">
  <img width="493" height="149" alt="{534C2F7D-0525-4B12-84B6-4F7D9C703413}" src="https://github.com/user-attachments/assets/29b7ab44-6390-43e6-8159-03fbf47d8252" />
</p>

A terminal launcher for agent CLIs — **Claude Code**, **Codex CLI**,
**OpenCode**, **Gemini CLI**, **DeepSeek-TUI** — that pairs each
session with a self-contained *template* (mission, playbook, rules,
tools, subagents, persona) and clones it into a fresh isolated
*chat* every time you start working on something new.

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
clade                     # interactive — opens with a brief boot splash
clade --no-splash         # skip the splash (also via CLADE_NO_SPLASH=1)
clade -version            # print banner + version and exit
clade -check-update       # ask GitHub if a newer release exists
clade -update             # download + install the latest release (prompts y/N)
clade -update -y          # same, non-interactive (for CI / scripts)
```

The updater queries `api.github.com/repos/sPROFFEs/Clade/releases/latest`,
picks the archive whose name matches your OS+arch
(`clade-<os>-<arch>.{tar.gz,zip}`), extracts just the `clade` binary,
and swaps it in place of the running executable. On Windows the
previous binary is preserved as `clade.exe.old` (a running `.exe`
can be renamed but not deleted); the next update cleans it up.

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

## Session-start workflow
<img width="2512" height="1191" alt="{1541F765-0203-495E-A7C6-EBA380378653}" src="https://github.com/user-attachments/assets/50a4b190-5761-475e-b96c-42bc0091e9e4" />
Every time you open or resume a chat, the launcher decorates the
compiled instructions with **required-reading directives** so the
agent actually consults the workpath's content instead of riffing
off the mission statement alone. Concretely, the agent is told it
MUST, before answering the first user message:

1. **Read `MEMORY.md`** end-to-end (if memory is enabled) and open
   its reply with `📒 Recalled: …` if non-trivial notes carried
   across from a past session.
2. **Scan the Knowledge-base inventory** in its instructions and
   open any file under `knowledge/` whose title/summary overlaps
   with the user's current question.
3. **Scan the Online-skills list** and open any cloned skill whose
   name/topic matches the user's question.
4. **Cite** every file path it draws on inline, so you can audit
   the sources.

If nothing matches a step, the agent is told to say so briefly
("nothing relevant in knowledge for this question") rather than
silently skipping — that way you can tell when the directives are
working vs being ignored.

These directives are appended automatically to `SKILL.md` /
`AGENTS.md` / `GEMINI.md` during compilation; you don't author
them in the workpath yourself. Personality, language, tools, and
subagents follow their own rules and are described below.

## Personality
<img width="2509" height="1188" alt="{97FA1923-3992-4F80-8508-A81CD5625331}" src="https://github.com/user-attachments/assets/8d5cc677-a339-4ea9-a4a3-d7b9f7cc47e3" />
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

## Memory

When a template (or chat) has `memoryEnabled: true` in its settings,
the launcher manages a persistent `MEMORY.md` file that survives
across launches:

- Lives at `<chat>/MEMORY.md` (canonical) and `<chat>/sandbox/MEMORY.md`
  (the agent's working copy).
- Every launch **stages** the workspace copy into the sandbox + appends
  a `## YYYY-MM-DD HH:MM — Session opened` marker so each session
  leaves a visible trace even if the agent writes nothing else.
- On exit, the sandbox copy syncs back to the canonical workspace
  file (`SyncMemoryBack`).
- The required-reading directive injected into the compiled
  instructions tells the agent it MUST read `MEMORY.md` at session
  start, open its first reply with `📒 Recalled: …` if there's
  non-trivial context, and **append** new durable facts under that
  session's marker as `### Title` subsections. Existing entries are
  never overwritten.

Toggle memory per template (template wizard) or per chat (`e` on the
home screen). Disabling it stops the launcher from staging /
syncing the file; existing notes stay on disk.

## Keys
<img width="2506" height="1190" alt="{5D687895-AE30-461F-AC76-004FA789BFE6}" src="https://github.com/user-attachments/assets/a966d13b-4a62-4ca6-8579-19ec7d2cf909" />
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

| Agent       | What gets written                                                   |
|-------------|---------------------------------------------------------------------|
| Claude      | `ANTHROPIC_BASE_URL` / `ANTHROPIC_AUTH_TOKEN` per chat (env-only)   |
| Codex       | `[model_providers.ollama_remote]` + profile in `~/.codex/config.toml` — launched with `-p ollama_remote` |
| OpenCode    | `provider.ollama_remote` block in `~/.config/opencode/opencode.json` — set as the default model |
| DeepSeek-TUI | `[providers.ollama]` block in `~/.deepseek/config.toml` with `provider = "ollama"` + default model set; wrapped in marker comments so re-applies don't touch surrounding config |
| Gemini      | **Not auto-configured.** See below.                                 |

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
| `<root>/templates/<name>/workpath/`                 | wpc source: `mission.md`, `playbook.md`, `rules.md`, `personality.md`, `tools/`, `agents/`, `knowledge/` |
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
├── agents/            named subagent prompts
│   └── triage.md
└── knowledge/         optional background-reading library (see below)
    ├── papers/
    ├── tools/
    └── references/
```

The launcher compiles a chat's workpath into its sandbox using the
matching wpc target:

| Agent        | Target  | Output                                                                                |
|--------------|---------|---------------------------------------------------------------------------------------|
| Claude       | `claude`| `.claude/skills/<template>/SKILL.md` + `scripts/` + `.claude/agents/<template>__<agent>.md` |
| Codex        | `codex` | `AGENTS.md` + `AGENTS.assets/`                                                        |
| OpenCode     | `codex` | `AGENTS.md` + `AGENTS.assets/` (OpenCode reads `AGENTS.md` too)                       |
| Gemini       | `gemini`| `GEMINI.md` (single-file digest the Gemini CLI reads on every prompt)                 |
| DeepSeek-TUI | `claude`| Same `.claude/skills/<template>/SKILL.md` layout — DeepSeek lists `.claude/skills/` in its discovery fallbacks, so one compile feeds both Claude Code and DeepSeek-TUI |

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

<img width="2509" height="1194" alt="{240DA2B1-66EB-4EE1-8F0F-90F156B49E58}" src="https://github.com/user-attachments/assets/24985863-d756-46f2-8ed1-e355b3f1992a" />


### Agent picker

<img width="2507" height="1193" alt="{8304EDD7-FA23-416D-B692-20BDCE394075}" src="https://github.com/user-attachments/assets/b5bce049-cd23-4480-9ab2-f536acdba722" />

### Ollama config

<img width="2505" height="1195" alt="{F7066226-6E62-4790-A960-ABFC5B14B4EB}" src="https://github.com/user-attachments/assets/e277fbad-fcde-4230-a114-43718dad083e" />

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

Once cloned, the launcher injects a required-reading directive into
the compiled instructions: the agent is told the skills exist
(listed by relative path), and that it MUST scan the list at the
start of every user turn and open the primary `SKILL.md` /
`README.md` of any skill whose topic matches the current question.
On the Claude target, skills also land under `.claude/skills/`
which Claude Code auto-loads; the directive reinforces the
auto-load and applies the same workflow to Codex / OpenCode /
Gemini, which don't auto-load anything.

## Knowledge base

Each template (and each cloned chat) can ship a `knowledge/`
directory full of background reading the agent can pull on
demand — docs, papers, tool descriptions, schema cheat-sheets,
anything that helps it reason but doesn't belong in the
mission/playbook/rules trio.

```
my-workpath/
└── knowledge/
    ├── papers/
    │   └── secure-boot.md
    ├── tools/
    │   └── binwalk.md
    └── datasheets/
        └── stm32f4.pdf
```

On every launch, the compiler:

1. **Stages the whole tree** into the chat's sandbox at the same
   relative path (`<sandbox>/knowledge/...`). The agent's file-
   reading tools (Claude's `Read`, Codex's `view`, etc.) find it
   immediately under the working directory.
2. **Auto-generates a required-reading manifest** in the compiled
   instructions (`SKILL.md` / `AGENTS.md` / `GEMINI.md`) listing
   every file with title + short summary. The instructions tell the
   agent it MUST scan that list at the start of every user turn,
   open any file whose title/summary overlaps with the current
   question, and cite the file path when it draws on the contents.
   Contents are **not** pre-loaded into context — the agent opens
   them on demand via its own file-reading tool.

Preferred format is **markdown**; the launcher also extracts
summaries from `.txt`, `.rst`, `.org`, `.json`, `.yaml`, `.toml`,
and `.csv` files. Anything else (PDFs, images, archives) gets
listed by name + size with a `(binary)` marker so the agent knows
it's there but isn't expected to parse it directly.

Hidden files / dirs (anything starting with `.`) are skipped.
Symlinks and entries containing `..` are rejected for safety.

The bundled samples ship a `knowledge/` directory you can read
to see the manifest format in action:
[`samples/workpaths/reversing/knowledge/`](samples/workpaths/reversing/knowledge),
[`samples/workpaths/code-review/knowledge/`](samples/workpaths/code-review/knowledge).

### Meta-template: `workpath-author`

The bundled `workpath-author` template is a chat that already
knows the workpath system cold — the schema, the per-target
outputs, the decoration pipeline. Start a chat from it and you
can say *"make me a template for X"* and the agent will scaffold
the directory, write `mission.md` / `playbook.md` / `rules.md`,
ask whether you want memory / persona / knowledge, and validate
the result against the schema before reporting done.

It ships with the canonical docs in
[`samples/workpaths/workpath-author/knowledge/`](samples/workpaths/workpath-author/knowledge)
(schema, targets, activation, quickstart, decoration pipeline)
plus a `new-workpath.{sh,ps1}` scaffolding tool. Pick it from
the template list (`t` on home) on first run, or any time you
need to author a new template.

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
