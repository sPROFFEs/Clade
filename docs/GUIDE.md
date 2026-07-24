<p align="center">
  <img src="assets/monke-icon.png" alt="PrAImate" width="120" />
</p>

# PrAImate — the full guide

The complete manual. For the short version (install + quick start),
see the [README](../README.md).

Gemini and DeepSeek references below document compatibility with legacy
persisted chats only. They are no longer offered in the selectable CLI catalog;
new chats use Claude Code, OpenClaude, Codex, OpenCode, or PrAImate Code.

- [Surfaces](#surfaces)
- [Updating](#updating)
- [First run](#first-run)
- [Concepts](#concepts)
- [Session-start workflow](#session-start-workflow)
- [Personality](#personality)
- [Memory](#memory)
- [Keys](#keys)
- [Local endpoint — Ollama / GPUStack / vLLM / LiteLLM](#local-endpoint--ollama--gpustack--vllm--litellm)
- [Files on disk](#files-on-disk)
- [Session resume](#session-resume)
- [Workpath source format](#workpath-source-format)
- [Imports, managed tools, hooks](#imports-managed-tools-hooks)
- [Screens](#screens)
- [Online skills](#online-skills)
- [Knowledge base](#knowledge-base)
- [Settings menu](#settings-menu)
- [Backup (optional cloud sync over git)](#backup-optional-cloud-sync-over-git)
- [Roadmap](#roadmap)

## Surfaces

Two surfaces over one shared SQLite database (`~/.praimate/db.sqlite`):

- **`praimate`** — the TUI. Single static Go binary, no runtime deps.
- **`praimate-gui`** — the desktop app (Wails + Svelte; UI/UX inspired
  by [OpenGUI](https://github.com/akemmanuel/OpenGUI)). Same chats,
  same agents, same memory, same automation as the TUI — a run
  launched in one surface shows up in the other. Light/dark/system
  theme with accent presets.

Launch the GUI with `praimate --gui` — the TUI binary finds the
`praimate-gui` binary shipped next to it (or on PATH) and starts it.

Release archives include the prebuilt GUI for **linux-amd64** and
**Windows amd64/arm64** (Windows needs no extra runtime — WebView2 is
system-provided on Windows 10+; Linux needs the `webkit2gtk-4.1`
runtime package). linux-arm64 and macOS archives ship the TUI only
because the GUI is cgo + webkit there and can't be cross-compiled;
build it from source with one script (needs node+npm; on Linux also
`libwebkit2gtk-4.1-dev libgtk-3-dev`):

```sh
cd cmd/praimate-gui && ./build.sh   # produces ./praimate-gui
```

## Updating

```sh
praimate -check-update       # ask GitHub if a newer release exists
praimate -update             # download + install the latest release (prompts y/N)
praimate -update -y          # same, non-interactive (for CI / scripts)
```

The updater queries `api.github.com/repos/sPROFFEs/praimate/releases/latest`,
picks the archive whose name matches your OS+arch
(`praimate-<os>-<arch>.{tar.gz,zip}`), extracts just the `praimate` binary,
and swaps it in place of the running executable. Sibling binaries
shipped in the same archive (`praimate-gui`, `praimate-code`) are
refreshed too when you have them installed. On Windows the previous
binary is preserved as `praimate.exe.old` (a running `.exe` can be
renamed but not deleted); the next update cleans it up.

The updater and installers use normal TLS verification when connecting
to GitHub.

## First run

First run asks where your data lives and what to seed:

1. **Workspaces root** — where templates + chats live. Default
   `~/praimate-workspaces`.
2. **Seed bundled samples?** — copies the `reversing`,
   `code-review`, and `workpath-author` example templates in so you
   have something to chat against immediately.
3. **Import the starter agents?** (GUI setup) — imports a curated
   agent set so the app is useful out of the box: **Reverse Ghidra**
   (a Ghidra-first reverse-engineering workflow with its helper
   scripts), **Code Review**, **Dev Team**, **Security Review**, and
   **Agent Builder**. They ship in `samples/agents/` as portable YAML
   and `.praimate-agent` packs; the import skips any agent you already
   have, so it never clobbers your edits. You can re-import or share
   them any time from the Agents tab.

Then: home screen → `n` (new chat) → pick a template → name the chat
→ pick an agent. The chat is created, the workpath is compiled into
its sandbox, the agent launches with `cwd` set to that sandbox.

PrAImate **does not quit** when the agent runs. The TUI stays alive
across the child's lifetime — when you exit the agent (`/exit`,
`Ctrl-D`, etc.), control returns to the chat list with the
just-ended session's diagnostics already visible. Launch another
chat without leaving PrAImate.

Next time you re-open a chat: the launcher inspects the agent's own
session store, finds your previous session(s) for that chat's
sandbox, and resumes natively — `claude --continue` or `claude
--resume <UUID>` for Claude, `codex resume --last` / `codex resume
<UUID>` for Codex. When two or more matching sessions exist, the
agent's own picker opens scoped to this chat. Press `F` on the chat
list (instead of Enter) for a deliberate fresh launch that skips
resume but leaves the captured sessions on disk.

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
│   ├── chat.json                   {template, agent, createdAt, lastUsed, settings.{ollama,mirror,…}}
│   ├── workpath/                   copied from template at creation
│   ├── sandbox/                    agent cwd; gitignored
│   ├── MEMORY.md                   persistent across re-opens
│   └── sessions/<ts>-<agent>/      per-launch artifacts
│       ├── transcript.jsonl        canonical rollout (search / cross-agent recap)
│       ├── summary.md              rule-based digest (turns, tools, headline)
│       ├── summary.json            structured metadata
│       └── native/                 full slice of the agent's home-dir store
│                                   for this chat (claude project dir, codex
│                                   rollouts matching cwd, opencode info+messages,
│                                   gemini tmp). Makes the chat dir fully portable.
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

There is also **cross-chat memory** (identity facts, salience-scored
pinned facts, per-session episode summaries distilled by a local
Ollama or CLI-billed model) — opt-in, ≤800 tokens injected, decays
and self-prunes. Off by default; toggle it in the GUI's Memory page
or the TUI settings.

## Keys
<img width="2506" height="1190" alt="{5D687895-AE30-461F-AC76-004FA789BFE6}" src="https://github.com/user-attachments/assets/a966d13b-4a62-4ca6-8579-19ec7d2cf909" />
### Home (chat list)

| Key       | Effect                                                      |
|-----------|-------------------------------------------------------------|
| `↑/↓ k/j` | Move selection                                              |
| `enter`   | Open chat (auto-resume if a native session exists)          |
| `F`       | Fresh launch — skip resume, leave captured sessions on disk |
| `n`       | New chat                                                    |
| `d`       | Delete highlighted chat (confirms)                          |
| `e`       | Settings (agent / language / memory / mirror / local endpoint / online skills) |
| `f`       | Edit chat files (mission, persona…)                         |
| `/`       | Cross-chat search                                           |
| `t`       | Template manager                                            |
| `r`       | Refresh                                                     |
| `ctrl-c`  | Quit                                                        |

The per-chat **agent picker** and **local endpoint config** live in
the settings menu (`e`). The left-nav Agents tab (`Ctrl-3`) is
install-management only.

### Template list (`t` from home)

| Key     | Effect                                            |
|---------|---------------------------------------------------|
| `enter` | Edit settings of highlighted template             |
| `n`     | New template (full wizard)                        |
| `d`     | Delete (existing chats from it are unaffected)    |
| `f`     | Edit template files                               |
| `esc`   | Back to chats                                     |

### Agents tab (left nav, install-only)

The left-nav Agents tab is for install management — installing,
updating, or repairing the agent CLIs. Per-chat agent swap lives in
the chat settings menu (`e` on the chat list → Agent row).

| Key     | Effect                                                  |
|---------|---------------------------------------------------------|
| `↑/↓`   | Move selection                                          |
| `enter` | Open the installer for the highlighted agent (install or upgrade) |
| `i`     | Same as Enter — explicit install                        |
| `esc`   | Back                                                    |

On Windows the install screen has an opt-in `n` keybinding to also
install Node.js LTS via `winget` when the selected method needs
Node — useful for fresh boxes that don't have a Node runtime yet.
Off by default.

## Local endpoint — Ollama / GPUStack / vLLM / LiteLLM

The Local-endpoint wizard (settings menu → "Local endpoint" row)
routes a chat through any **OpenAI-compatible local endpoint**, not
just vanilla Ollama. Supported: Ollama, GPUStack, vLLM, LiteLLM,
llama.cpp's server, LocalAI, anything that speaks
`/v1/chat/completions`. With Bearer auth: GPUStack, gated vLLM /
LiteLLM, anything else that requires a key.

Five wizard steps:

1. **Endpoint** — `http://host[:port]`. The launcher normalises
   missing schemes and trailing slashes.
2. **API key** (optional) — Bearer token. Blank = no auth
   (vanilla Ollama path). Sent as `Authorization: Bearer <key>` on
   the probe and at launch.
3. **Model** — probes the endpoint for available models (tries
   `/api/tags`, falls back to `/v1/models`), falls back to manual
   entry if the probe times out.
4. **Agents to configure** — multi-select. The chat's locked agent
   is pre-ticked. The tick state is **round-trip-correct**: tick =
   apply, untick = strip. Disk state and the chat-level `Agents`
   list stay in sync.
5. **Apply** — writes per-agent config + a chat-level
   `OllamaSettings` block with the list of opted-in agents.

What gets written, per agent:

| Agent | Where | Activated by |
|---|---|---|
| **Claude** | `chat.json` `settings.ollama` block — per-chat | `Plan()` injects `ANTHROPIC_BASE_URL` / `ANTHROPIC_AUTH_TOKEN` / `OPENAI_API_KEY` env + `--model <model>` flag |
| **Codex** | `~/.codex/config.toml` `[model_providers.ollama_remote]` + `[profiles.ollama_remote]` | Launched with `-p ollama_remote`; codex reads `OPENAI_API_KEY` via `env_key` |
| **OpenCode** | `opencode.json` `provider.ollama_remote` block, including inline `options.apiKey` when a key is set | OpenCode picks it up automatically; `OPENAI_API_KEY` also exported as fallback |
| **DeepSeek-TUI** | `~/.deepseek/config.toml` managed block with `provider = "ollama"`, `[providers.ollama]`, and optional inline `api_key`. Wrapped in marker comments. | Read by deepseek-tui on startup |
| **Gemini** | Not auto-configured (see below) | — |

### Codex wire_api probe

Before writing the codex profile, the wizard issues a stub `POST
/v1/responses` to the endpoint and classifies the response:

| Outcome | Behavior |
|---|---|
| 2xx, 401, 403 | Pass — route exists |
| 502, 503, 504 | **Pass with warning** — route exists, upstream sick (e.g. GPUStack worker temporarily down) |
| 404, 405, 501 | **Refuse** — endpoint doesn't implement `/v1/responses`. Codex 0.130+ requires this endpoint; OpenAI-compatible servers that only implement `/v1/chat/completions` (vanilla Ollama, many vLLM versions) are flagged. |
| 400 with `chat/completions` hint in body | **Refuse** — chat-completions-only server |

When the probe refuses, the wizard strips any stale
`ollama_remote` block left by a previous apply, so the user isn't
left with a config that codex would pick up at launch and choke on.
The other three agents (claude / opencode / deepseek) use
`/v1/chat/completions`, which every OpenAI-compat server speaks, so
they work against the same endpoint without the probe.

### Codex + GPUStack

GPUStack's API gateway often routes `/v1/responses` to an internal
worker chain that may not be fully implemented. If the probe
returns 503 with a body like `Cannot connect to host …`, the route
exists but your worker is unreachable — the wizard applies the
config with a warning and you fix the worker side. If the probe
returns 404, GPUStack's `/v1/responses` isn't implemented at all on
your version; route through [LiteLLM](https://github.com/BerriAI/litellm)
or switch the chat to claude / opencode / deepseek.

### Gemini + Local endpoint

The wizard leaves Gemini untouched. Gemini CLI 0.42+ ignores the
`OPENAI_*` env vars that work for Codex / OpenCode and keeps
hitting Google's API via cached OAuth, then fails with `Model "..."
was not found or is invalid`. Routing it through a local endpoint
needs `~/.gemini/settings.json` (`selectedAuthType` + provider
section), whose schema has shifted across CLI versions — the
launcher doesn't auto-write it because the wrong schema breaks
your config worse than no config.

Paths that work today:

1. **[litellm](https://github.com/BerriAI/litellm) proxy** in front
   of your endpoint; point Gemini at it via its own config (exact
   lines depend on your installed Gemini version).
2. **Hand-edit `~/.gemini/settings.json`** to your CLI version's
   OpenAI provider section.

Gemini still launches fine **without** local-endpoint routing — it
just uses its native Google auth, which is the default.

## Files on disk

| Path                                                | Holds                                                   |
|-----------------------------------------------------|---------------------------------------------------------|
| `~/.config/praimate/config.json` (Linux/XDG)           | `workspacesRoot`, `lastAgent`                           |
| `~/Library/Application Support/praimate/…` (macOS)     | same                                                    |
| `%AppData%\praimate\config.json` (Windows)             | same                                                    |
| `<root>/templates/<name>/workpath/`                 | wpc source: `mission.md`, `playbook.md`, `rules.md`, `personality.md`, `tools/`, `agents/`, `knowledge/` |
| `<root>/templates/<name>/template.json`             | defaults inherited by new chats (memory, language, skills) |
| `<root>/chats/<chat-id>/workpath/`                  | cloned from template at chat creation                   |
| `<root>/chats/<chat-id>/sandbox/`                   | agent's cwd; compiled artifacts; gitignored             |
| `<root>/chats/<chat-id>/chat.json`                  | `label`, `template`, `agent`, `lastUsed`, `settings`    |
| `<root>/chats/<chat-id>/MEMORY.md`                  | persistent memory; synced from sandbox after exit       |
| `<root>/chats/<chat-id>/sessions/<ts>-<agent>/`     | per-launch artifacts: `transcript.jsonl` (canonical rollout), `summary.md` / `summary.json` (rule-based digest), `native/` (full per-chat slice of the agent's home-dir store — see Session resume below) |

## Session resume

When you re-open a chat from the chat list, PrAImate inspects the
agent's own session store, picks the right resume flag, and stays
out of the way:

| Native sessions for this chat | Args passed to the agent |
|---|---|
| 2 or more | `claude --resume` / `codex resume` — opens the **agent's native picker** scoped to this chat |
| Exactly 1 | `claude --continue` / `codex resume <UUID>` — direct resume, no picker |
| 0, but a captured transcript exists (e.g. chat dir copied from another machine) | Restore the captured rollout into the agent's store with the correct UUID-named file, then `--continue` / `resume` it |
| 0 and nothing captured | Fresh launch |

**`F` instead of Enter** on the chat list bypasses resume on purpose:
useful when you want to start a clean conversation on the same chat
without deleting the captured sessions. The slice on disk is left
intact so a subsequent plain Enter resumes normally.

Per-session artifacts the launcher captures on every exit:

```
<chat>/sessions/<ts>-<agent>/
├── transcript.jsonl          # the agent's native rollout, canonical copy
├── summary.md                # rule-based digest (turns, tools, headline)
├── summary.json              # structured metadata for diagnostics + search
└── native/                   # full per-chat slice of the agent's store
    └── <agent-subdir>/...    # claude project dir, codex rollouts matching
                              # cwd, opencode info+messages, gemini tmp...
```

The `native/` snapshot runs in a background goroutine — the TUI
redraws immediately on exit. The slice makes the chat dir fully
self-contained: copy it to another machine and the next launch
restores the conversation state into the agent's home dir on the
new machine.

The slice **restore** at launch is opt-in per chat (`e` → Mirror
agent state). SIGKILL-safe: if PrAImate was force-killed mid-session,
mirror-in compares per-file mtimes and preserves the agent's
home-dir copy when it's newer than the slice. You don't lose
turns to a partial snapshot.

## Workpath source format

A workpath is a directory. Minimum:

```
my-workpath/
└── mission.md         required, non-empty
```

Full shape:

```
my-workpath/
├── workpath.json      optional metadata + tool/agent overrides + imports
├── mission.md         required
├── playbook.md        optional staged process
├── rules.md           optional hard constraints
├── personality.md     optional persona / system-prompt block
├── hooks.json         optional chat-lifecycle hooks (see docs/SCHEMA.md)
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

A workpath.json can also declare `"imports": ["_common/<bundle>"]` to
pull a shared capability bundle (knowledge + tools + agents + hooks +
playbook/rules fragments) from `templates/_common/` at compile time.
Today's canonical bundle is `_common/graphify` — a graph-based
impact-analysis capability used by every relationship-heavy template.
See [SCHEMA.md](SCHEMA.md) for the full imports + hooks reference.

The launcher compiles a chat's workpath into its sandbox using the
matching wpc target:

| Agent        | Target  | Output                                                                                |
|--------------|---------|---------------------------------------------------------------------------------------|
| Claude       | `claude`| `.claude/skills/<template>/SKILL.md` + `scripts/` + `.claude/agents/<template>__<agent>.md` + `.claude/settings.json` (hooks) |
| OpenClaude   | `claude`| Same `.claude/skills/` layout — OpenClaude is a Claude Code fork and reads the same files |
| Codex        | `codex` | `AGENTS.md` + `AGENTS.assets/`                                                        |
| OpenCode     | `codex` | `AGENTS.md` + `AGENTS.assets/` (OpenCode reads `AGENTS.md` too)                       |
| Gemini       | `gemini`| `GEMINI.md` (single-file digest the Gemini CLI reads on every prompt)                 |
| DeepSeek-TUI | `codex` | `AGENTS.md` at the sandbox root — DeepSeek auto-loads it via its `init` subcommand convention |

Two extra targets are useful when authoring templates for tools the
launcher doesn't directly drive:

| Target   | Output                                                |
|----------|-------------------------------------------------------|
| `mika`   | `modules/<name>/{module,playbook,rules}.md` + assets  |
| `cursor` | `.cursor/rules/<name>.mdc` (tools/agents inlined)     |

Full schema + per-target reference: [SCHEMA.md](SCHEMA.md),
[TARGETS.md](TARGETS.md), [ACTIVATION.md](ACTIVATION.md),
[QUICKSTART.md](QUICKSTART.md).

## Imports, managed tools, hooks

Three features that extend the wpc compile-once-for-N-agents model.
This section is the practical "what to type" tour. The **Tools** tab
in the launcher TUI (Ctrl-4) hosts the install / update UX for
graphify and any future managed tools.

### Shared capability bundles via `imports:`

Workpath templates can pull in shared knowledge / tools / agents / hooks
from sibling bundles under `templates/_common/`. The six shipped
analysis templates (`code-review`, `reversing`, `reverse-ghidra`,
`cve-analysis`, `cc-evidence-dossier`, `praimate-dev`) already import
`_common/graphify` — no extra work to use them; just launch:

```
praimate                       # pick code-review (or any analysis template)
# the agent's sandbox now contains graphify_impact.sh, graphify_query.sh,
# and a graphify-usage.md cheat-sheet — all merged from _common/graphify.
```

To add the import to a new template you author:

```json
{
  "description": "...",
  "imports": ["_common/graphify"]
}
```

For nested templates (e.g. the `templates/<name>/workpath/` shape that
`praimate-dev` uses), or for adding the import directly to an existing
chat's workpath, use the relative form that escapes the extra depth:

```json
{ "imports": ["../_common/graphify"] }              // nested template
{ "imports": ["../../templates/_common/graphify"] } // chat workpath
```

The bundle's playbook + rules fragments append to the consumer's own
sections under `## Imported capabilities: <bundle>` headings; tool /
agent / knowledge / hook collisions are resolved in the consumer's
favor. Missing imports are a hard compile error.

See [SCHEMA.md](SCHEMA.md) for the full reference and
`templates/_common/README.md` for the bundle inventory.

### PrAImate-managed tools (the graphify case)

Templates that import `_common/<bundle>` assume the wrapped binary is
on PATH. PrAImate installs its known tools into an isolated managed prefix
so they don't touch your global Python / pnpm state.

The shipped tool today is **graphify** (a tree-sitter-AST + LLM
knowledge-graph builder for code). Two ways to install:

```
# In the TUI: Ctrl-4 → Tools tab → enter on graphify
praimate

# Or from the CLI:
praimate -install-tool graphify
```

This calls `uv tool install graphifyy` with `UV_TOOL_DIR` and
`UV_TOOL_BIN_DIR` pointed at `<config>/praimate/tools/graphify/`, and the
PyPI index pinned to `https://pypi.org/simple/`. The bin dir is
prepended to `PATH` on every PrAImate startup, so the graphify-wrapper
scripts in the imported bundle find the binary by name without you
editing your shell rc.

If `uv` isn't installed, PrAImate refuses to auto-install it (single-Go-
binary policy — you opt into the uv install yourself) and prints the
official one-liner:

```
curl -LsSf https://astral.sh/uv/install.sh | sh    # Linux/macOS
irm https://astral.sh/uv/install.ps1 | iex         # Windows
brew install uv                                    # macOS (Homebrew)
winget install --id=astral-sh.uv -e                # Windows (winget)
```

### Building bundled tools from source (GUI)

Some bundled binaries can't be cross-compiled, so a release only ships
prebuilt **PrAImate Code** and **graphify** for the platforms we build
them on (graphify standalone is currently Linux/amd64; PrAImate Code is
the native target of each release host). On any other platform you can
build them locally instead of downloading:

**GUI → Settings → Build bundled tools from source.** Each tool shows
the toolchain it needs (`git` always; `bun` for PrAImate Code, `uv` for
graphify; plus `bash` on Windows, which ships with Git for Windows) and
a ✓/✗ for each. When everything's present, **Build from source**:

1. clones the PrAImate repo (shallow) into a temp directory,
2. runs the matching build script (`scripts/build-praimate-code.sh` /
   `scripts/build-graphify.sh`), streaming the output live,
3. installs the finished binary into `<config>/praimate/bin/` — exactly
   where the resolver looks for the bundled copy,
4. deletes the temporary checkout so nothing lingers on disk.

The PrAImate Code build downloads OpenCode's full dependency tree (~2 GB)
and takes a few minutes; graphify freezes a PyInstaller standalone. Both
are one-and-done — the result resolves on the next launch with no PATH
edits.

After uv lands, re-run `praimate -install-tool graphify`. The bin ends up
at `<config>/praimate/tools/graphify/bin/graphify`.

**Friction signal on launch.** If a chat's workpath imports a `_common/
<bundle>` whose underlying tool isn't reachable yet, the launching
screen surfaces a one-line note with the exact install command. No
silent failure — the agent would just hit "graphify: command not
found" inside the chat otherwise.

### Cross-harness hooks (`hooks.json`)

Drop a `hooks.json` next to `mission.md` to register chat-lifecycle
triggers. Source schema is target-agnostic; each wpc target compiles
the hooks into its native format.

```json
{
  "hooks": [
    {
      "event": "pre_tool",
      "matcher": "Bash",
      "command": "echo \"[$(date -Iseconds)] bash invoked\" >> .praimate-audit.log",
      "description": "Audit every bash call"
    },
    {
      "event": "session_start",
      "command": "git fetch --quiet origin main"
    }
  ]
}
```

Today the `claude` target emits a real `.claude/settings.json` that
Claude Code reads on every turn. Other targets (codex / opencode /
gemini / mika) append a `## Hooks (declared, NOT wired for <target>)`
section to the compiled instructions listing each declared hook, so
the agent can see what was intended even though PrAImate can't fire them
automatically yet. Real emitters land per target when each upstream
agent grows a stable hook spec.

Portable events (PrAImate name → Claude Code name):

| PrAImate            | Claude Code         |
|------------------|---------------------|
| `pre_tool`       | `PreToolUse`        |
| `post_tool`      | `PostToolUse`       |
| `user_input`     | `UserPromptSubmit`  |
| `session_start`  | `SessionStart`      |
| `session_stop`   | `Stop`              |
| `subagent_stop`  | `SubagentStop`      |
| `notification`   | `Notification`      |

Hooks can also live in a `_common/<bundle>/hooks.json` and travel with
imports. Collisions resolve in the consumer's favor, keyed on
`event + matcher` — your `pre_tool/Bash` overrides an imported one,
but an imported `pre_tool/Edit` still fires alongside.

See [SCHEMA.md](SCHEMA.md) for per-field semantics and validation rules.

### Verify end-to-end

```
# 1. confirm the import merged into an analysis template:
wpc compile templates/code-review --target claude --out /tmp/cr-test
ls /tmp/cr-test/.claude/skills/code-review/scripts/
#  → graphify_impact.sh, graphify_query.sh

# 2. confirm a hooks.json compiled into settings.json:
ls /tmp/cr-test/.claude/settings.json   # exists when the template has hooks

# 3. confirm graphify is reachable:
graphify --version
```

## Screens

The launcher renders with [Bubble Tea](https://github.com/charmbracelet/bubbletea)
+ [Lip Gloss](https://github.com/charmbracelet/lipgloss) — rounded
outer frame, title bar with the active screen name, help bar at the
bottom.

### Home — chat list

<img width="2509" height="1194" alt="{240DA2B1-66EB-4EE1-8F0F-90F156B49E58}" src="https://github.com/user-attachments/assets/24985863-d756-46f2-8ed1-e355b3f1992a" />

### Agent picker

<img width="2507" height="1193" alt="{8304EDD7-FA23-416D-B692-20BDCE394075}" src="https://github.com/user-attachments/assets/b5bce049-cd23-4480-9ab2-f536acdba722" />

### Local-endpoint wizard

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
[`samples/workpaths/reversing/knowledge/`](../samples/workpaths/reversing/knowledge),
[`samples/workpaths/code-review/knowledge/`](../samples/workpaths/code-review/knowledge).

### Meta-template: `workpath-author`

The bundled `workpath-author` template is a chat that already
knows the workpath system cold — the schema, the per-target
outputs, the decoration pipeline. Start a chat from it and you
can say *"make me a template for X"* and the agent will scaffold
the directory, write `mission.md` / `playbook.md` / `rules.md`,
ask whether you want memory / persona / knowledge, and validate
the result against the schema before reporting done.

It ships with the canonical docs in
[`samples/workpaths/workpath-author/knowledge/`](../samples/workpaths/workpath-author/knowledge)
(schema, targets, activation, quickstart, decoration pipeline)
plus a `new-workpath.{sh,ps1}` scaffolding tool. Pick it from
the template list (`t` on home) on first run, or any time you
need to author a new template.

## Settings menu

Each chat has a settings menu reached with `e` on the chat list.
Six items, all editable in place — Esc on the list saves and
returns:

| Row | What it does |
|---|---|
| **Language** | Prepends a `respond in <lang>` directive to the agent's first turn. |
| **Persistent MEMORY.md** | Toggle the staging/sync-back of `MEMORY.md` between sandbox and chat-root. |
| **Mirror agent state** | Opt-in: restore the chat's captured slice into the agent's home dir before launch (cross-machine restore). Snapshot-on-exit always runs regardless of this flag. SIGKILL-safe via mtime comparison. |
| **Agent** | Per-chat agent picker. Pick a different installed agent to swap; pick a missing one to open the install screen. Writes through to `chat.json` immediately so the swap survives a restart. |
| **Local endpoint** | Opens the Local-endpoint wizard for this chat. Returns to settings when applied. |
| **Online skills** | Multi-add list editor for git/zip URLs the launcher pulls into the sandbox on every launch. |

## Backup (optional cloud sync over git)

Optional, off by default. When enabled, the workspaces root becomes a
git repository whose history mirrors your chats and templates across
machines. No GitHub / Gitea REST API calls — pure git protocol over
HTTPS or SSH, using the credentials your `git` client already has.

### Three ways to start

The first-run wizard offers three options after the workspaces-root
prompt:

1. **Empty folder** — just `chats/` + `templates/`. Backup stays off.
2. **Empty folder + bundled samples** — same plus the bundled
   starter templates. Backup stays off.
3. **Clone from a git remote** — pulls existing chats and templates
   from a remote you already pushed to from another machine. The
   wizard runs a connection probe (`git ls-remote`) before cloning;
   on failure it falls back to option 1 but preserves the URL in
   your config so the Backup tab opens pre-filled for retry.
   Successful clone implicitly enables backup.

### Master switch

The Backup tab opens with the feature OFF. Flipping the master switch
is the single explicit action that turns backup on. It initialises
the workspaces root as a git repository, writes a managed
`.gitignore` + `.gitattributes`, and unlocks the rest of the tab.
Flipping it back off clears the remote URL and disables auto-sync;
the local `.git` directory stays so re-enabling later is cheap.

### What gets tracked

PrAImate's managed `.gitignore` excludes every file at the workspaces
root **except** `chats/`, `templates/` and `.praimate-state/`. Inside
the first two, **everything is tracked** — sandbox, captured
transcripts, native session slices, the full per-chat
`MEMORY.md` — so a fresh clone on another machine restores not just
the workpaths but the actual conversation state. Stray files at the
root (scratch notes, environment overrides, etc.) never propagate.

`.praimate-state/` is how the **database travels too**: before every
backup commit, PrAImate snapshots its SQLite DB (DB chats, messages,
agents, MCP servers, settings, memory) plus the shareable slice of
your config (local-LLM endpoint defaults) into that directory. After
every pull / merge / reset / first clone, the remote machine's
snapshot is **row-merged** into the live local DB — newer edits win on
keyed rows, messages dedupe on content, and local-only rows are never
lost. That's what lets multiple hosts share the same chats: git moves
the snapshot, PrAImate merges it. (Deletions don't propagate yet — a
chat deleted on one host can reappear after syncing with a host that
still has it.)

If you hand-edit `.gitignore`, the absence of the PrAImate-managed
marker line tells the launcher to leave it alone on the next sync.

### Sync flow

Once enabled, the Backup tab exposes:

- **Remote URL** — edit at any time.
- **Test connection** — lightweight `git ls-remote` probe.
- **Sync now** — commits local changes, fetches, then decides:
  in-sync → no-op; local-ahead-only → push; remote-ahead-only →
  fast-forward pull; diverged → resolution popup.
- **Reset from remote** — discards local; two confirmations.
- **Force push** — overwrites remote; one confirmation.
- **Disconnect** — clears the remote URL and disables auto-sync;
  local files untouched.
- **Auto-sync** — optional sync on every PrAImate startup and exit.
- **Force always local** — sub-option of auto-sync. Forces local
  state to win on divergence. Guarded by a per-commit Machine-ID
  trailer and a 24-hour window: PrAImate refuses to overwrite when
  another machine pushed recently.

### Divergence resolution

When both sides have unique commits, the launcher shows the
local and remote commit lists with timestamps and offers four
reconciliations:

- **[m] Merge** — keep both sides via a merge commit.
- **[r] Rebase** — replay local on top of remote.
- **[p] Force push** — discard remote, keep local.
- **[R] Reset** — discard local, keep remote.

Merge or rebase conflicts surface a panel naming the conflicting
files and pointing the user at the workspaces root for manual
resolution.

### MEMORY.md custom merge

`MEMORY.md` in any chat or template uses a custom merge driver
registered automatically when backup is enabled. Concurrent edits
across machines concatenate under a
`## --- merged from another machine at <ts> ---` separator instead
of producing `<<<<<<<` conflict markers — so the agent's running
notes are preserved on both sides rather than turning into a Git
merge artefact the next session has to parse.

### Credentials

The launcher never prompts for credentials. Whatever auth your
local `git` client uses (HTTPS credential helper, SSH agent,
git-credentials file, …) is what backup uses. For a private repo:
configure credentials on the command line first (`git clone <url>`
in a terminal once is enough to confirm) before pointing PrAImate at
it.

## Roadmap

Not yet implemented; PRs welcome:

- **OpenCode native session resume.** Slice snapshot is wired, but
  the resume branch in `internal/launcher/resume.go` falls back to
  the markdown-summary inject.
- **Codex shape-aware probe.** The current probe checks status
  codes; an endpoint that returns 200 with a non-responses-shaped
  body (some Ollama versions) sneaks through.
- **Auto-spawn LiteLLM as a sidecar** for codex+GPUStack-style
  setups.
- **Per-chat conversation tagging / saved searches.** Cross-chat
  search exists (`/` on the chat list); structured tagging on top
  would help.
