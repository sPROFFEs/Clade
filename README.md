<p align="center">
  <img src="docs/assets/monke-icon.png" alt="PrAImate" width="160" />
</p>

# PrAImate

```
   .-"-.     ██████╗ ██████╗  █████╗ ██╗███╗   ███╗ █████╗ ████████╗███████╗
  /|6 6|\    ██╔══██╗██╔══██╗██╔══██╗██║████╗ ████║██╔══██╗╚══██╔══╝██╔════╝
 {/(_0_)\}   ██████╔╝██████╔╝███████║██║██╔████╔██║███████║   ██║   █████╗
  _/ ^ \_    ██╔═══╝ ██╔══██╗██╔══██║██║██║╚██╔╝██║██╔══██║   ██║   ██╔══╝
 (/ /^\ \)   ██║     ██║  ██║██║  ██║██║██║ ╚═╝ ██║██║  ██║   ██║   ███████╗
  ""' '""    ╚═╝     ╚═╝  ╚═╝╚═╝  ╚═╝╚═╝╚═╝     ╚═╝╚═╝  ╚═╝   ╚═╝   ╚══════╝

                  one harness, every agent — shared memory & MCP
```

**One harness, every agent.** PrAImate wraps the third-party coding CLIs
you already use — **Claude Code**, **Codex CLI**, **OpenCode**,
**Gemini CLI**, **DeepSeek-TUI**, plus the bundled **PrAImate Code** —
behind one launcher with shared agents, memory, MCP, and automation.
The models stay theirs; the layer around them is yours.

Two surfaces over one shared SQLite database (`~/.praimate/db.sqlite`):

- **`praimate`** — the TUI. Single static Go binary, no runtime deps.
- **`praimate-gui`** — the desktop app (launch with `praimate --gui`).
  Same chats, agents, memory, and automation as the TUI; clean chats on
  any CLI/model; light/dark/system theme.

What the harness adds on top of your CLIs:

- **Portable YAML agents** — instructions + workflows + inputs in one
  shareable file; import/export from TUI or GUI.
- **Starter agents out of the box** — first-run setup can import a
  curated set (Reverse Ghidra, Code Review, Dev Team, Security Review,
  Agent Builder) so a fresh install is useful immediately.
- **Clean chats on any model** — pick a CLI, optionally pin its model
  (`claude --model`, `codex -m`, `opencode --model provider/model`, …),
  chat. No agent persona required.
- **Cross-chat memory** — identity facts, pinned facts, episode
  summaries; opt-in, ≤800 tokens injected, self-pruning.
- **MCP catalogue** — connect ~25 known providers once; PrAImate writes
  per-CLI MCP config at launch. Secrets stay out of project files.
- **Automation** — folder watchers and cron schedules that fire agent
  workflows.
- **Bundled tools, your way** — ship-with prebuilt **PrAImate Code** and
  **graphify** where available, or **build them from source** inside the
  GUI (Settings → *Build bundled tools from source*) on any platform
  that has the toolchain.
- **Privacy redaction** — outbound prompts scanned for keys / tokens /
  PII (+ your regexes) and scrubbed before any CLI sees them.
- **Chats with native resume** — each chat gets an isolated sandbox and
  resumes the CLI's own session on re-open; chat dirs are portable
  across machines.
- **Optional git backup** — sync chats + templates across machines over
  plain git.

## Install

One command, picks the right archive, drops `praimate` + `wpc` on PATH:

```sh
# Linux / macOS
curl -fsSL https://raw.githubusercontent.com/sPROFFEs/PrAImate/main/scripts/install.sh | bash
```

```powershell
# Windows (PowerShell)
iwr -useb https://raw.githubusercontent.com/sPROFFEs/PrAImate/main/scripts/install.ps1 | iex
```

Manual: grab the archive from
[Releases](https://github.com/sPROFFEs/PrAImate/releases), extract, run
`./scripts/install.sh` (or `.\scripts\install.ps1`) from inside.

## Quick start

```sh
praimate                # the TUI: new chat → pick agent → go
praimate --gui          # the desktop app
praimate -update        # self-update to the latest release
praimate -version       # banner + version
```

First run asks where your workspaces live and offers to seed the sample
templates **and** import the starter agents, then: home screen → `n` →
pick a template → name the chat → pick an agent. The agent launches
inside the chat's sandbox; PrAImate stays alive underneath and captures
the session. Re-opening the chat resumes the CLI's own session natively
(`claude --continue`, `codex resume`, …).

In the GUI: **Chats → + New chat** starts a clean conversation on any
installed CLI with an optional pinned model; the **Code** page runs any
agent's CLI live in a project folder; the left sidebar collapses to an
icon rail; **CLI & Tools** detects and installs your CLIs; TUI chats
appear under "Workspace chats" and reopen in the terminal with native
resume.

<p align="center">
  <img src="docs/assets/monke-mascot.png" alt="PrAImate mascot" width="150" />
</p>

## Documentation

| Doc | Covers |
|---|---|
| [docs/GUIDE.md](docs/GUIDE.md) | The full manual: concepts, keys, memory, personality, session resume, local LLM endpoints (Ollama/vLLM/GPUStack/LiteLLM), settings, imports + managed tools + hooks, online skills, knowledge bases, git backup, roadmap |
| [docs/QUICKSTART.md](docs/QUICKSTART.md) | Workpath authoring in 5 minutes |
| [docs/SCHEMA.md](docs/SCHEMA.md) | Workpath source format reference |
| [docs/TARGETS.md](docs/TARGETS.md) | Per-CLI compile targets |
| [docs/ACTIVATION.md](docs/ACTIVATION.md) | How compiled instructions activate per CLI |

> PrAImate 1.0 is the successor to **Clade** (fresh app, no data
> migration). The desktop GUI's UI/UX is inspired by
> [OpenGUI](https://github.com/akemmanuel/OpenGUI) (MIT).

## License

[MIT](LICENSE).
