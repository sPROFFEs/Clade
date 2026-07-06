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

                  one harness, every agent - shared memory & MCP
```

PrAImate is a local harness for the coding CLIs already used in the
team: Claude Code, Codex CLI, OpenCode, Gemini CLI, DeepSeek-TUI, and
the bundled **PrAImate Code** build. It provides the layer around those
tools: agents, workflows, memory, MCP configuration, automation, local
tool management, and optional git-backed backup.

The current project version is **1.0.7**.

## Binaries

Release archives and source builds revolve around four executables:

| Binary | Purpose |
|---|---|
| `praimate` | Main TUI, chat/workspace launcher, updater, managed-tool installer, and `praimate code` dispatcher. |
| `praimate-gui` | Wails/Svelte desktop app launched through `praimate --gui`. Shares the same DB, chats, agents, memory, MCP, and automation as the TUI. |
| `praimate-code` | Bundled, version-pinned, rebranded OpenCode build. Launched directly or through `praimate code`. |
| `wpc` | Workpath compiler. Validates and compiles portable workpaths into Claude/Codex/OpenCode/Gemini/Cursor/mika/generic target files. |

Both app surfaces use the same local SQLite database at
`~/.praimate/db.sqlite`.

## What It Does

- **Portable agents**: YAML agents can carry instructions, workflows,
  inputs, knowledge, allowed surfaces, and MCP server references.
- **Starter agents**: first-run setup can import Reverse Ghidra, Code
  Review, Dev Team, Security Review, and Agent Builder.
- **Clean chats**: start a plain chat on any supported CLI/model without
  selecting an agent persona.
- **Native resume**: chat sandboxes reopen through the underlying CLI's
  own resume mechanism where available.
- **Live Code page**: the GUI can launch a terminal session in any
  project folder, with optional agent instructions exported as native
  context (`CLAUDE.md` or `AGENTS.md`).
- **MCP wiring**: connected MCP servers are written into the selected
  CLI's config at launch time. Secrets are passed through environment
  variables instead of being written into project config files.
- **Custom MCP servers**: stdio commands support quoted arguments,
  `$VAR` / `${VAR}` expansion, and `~/` expansion before launch.
- **PrAImate Code MCP support**: `praimate code` and clean GUI terminal
  sessions receive enabled MCP servers even when no agent is selected.
- **Cross-chat memory**: opt-in identity facts, pinned facts, and
  episode summaries with bounded prompt injection.
- **Automation**: folder watchers and cron schedules can trigger agent
  workflows.
- **Local model routing**: supported CLIs can be routed through
  Ollama/vLLM/GPUStack/LiteLLM style endpoints.
- **Managed tools**: graphify, gstack, scrapegraph, and bundled
  PrAImate Code can be installed into PrAImate-managed paths and picked
  up by both the TUI and GUI.
- **Privacy redaction**: outbound prompts can be scanned for secrets,
  tokens, PII, and user-defined regexes before a CLI receives them.
- **Git backup**: chats, templates, and shareable state can be synced
  across machines with plain git.

## Install

Linux / macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/sPROFFEs/PrAImate/main/scripts/install.sh | bash
```

Windows PowerShell:

```powershell
iwr -useb https://raw.githubusercontent.com/sPROFFEs/PrAImate/main/scripts/install.ps1 | iex
```

The installer resolves the latest GitHub release unless `RELEASE_TAG`
is set. Prebuilt archives always install `praimate` and `wpc`; they also
install `praimate-gui`, `praimate-code`, `praimate-graphify`, samples,
and desktop shortcuts when those assets are present in the archive.

Useful installer modes:

```sh
./scripts/install.sh --binary      # download a release archive
./scripts/install.sh --source      # build from source
./scripts/install.sh --user        # install into ~/.local/bin
./scripts/install.sh --system      # install into /usr/local/bin
./scripts/install.sh --yes         # non-interactive defaults
```

```powershell
.\scripts\install.ps1 -Mode Binary
.\scripts\install.ps1 -Mode Source
.\scripts\install.ps1 -AllUsers
.\scripts\install.ps1 -Yes
```

Source installs now detect an existing checkout, build `praimate` and
`wpc`, build the GUI when dependencies are available, and install the
resulting binaries together. On Debian/Kali-style Linux systems the GUI
build needs:

```sh
sudo apt-get install -y npm pkg-config libwebkit2gtk-4.1-dev libgtk-3-dev
```

## Quick Start

```sh
praimate                # TUI
praimate --gui          # desktop GUI
praimate code           # bundled PrAImate Code CLI
praimate -check-update  # check GitHub for a newer release
praimate -update        # self-update installed binaries
praimate -version       # banner + version
```

First run asks for a workspaces root and can seed sample workpaths and
starter agents. From there:

1. Open the TUI with `praimate`, press `n`, choose a template, name the
   chat, and pick an agent/CLI.
2. Open the GUI with `praimate --gui`, use **Chats** for normal
   conversations, **Code** for live project terminals, **Agents** for
   imported agents, and **MCP** for server connections.
3. Use **CLI & Tools** to detect/install CLIs and PrAImate-managed
   tools.

Workpath authoring still goes through `wpc`:

```sh
wpc init code-review
wpc validate code-review
wpc compile code-review --target all --out dist/
```

## Build

Build release artifacts from the repo root:

```sh
scripts/build.sh
scripts/build.sh --with-gui
scripts/build.sh --with-code
scripts/build.sh --with-graphify
scripts/build.sh --version=1.0.7
```

`scripts/build.sh` stamps the version into `praimate`, builds supported
OS/arch archives under `dist/`, copies samples/docs into each bundle,
and optionally includes the GUI, PrAImate Code, and graphify assets.

PrAImate Code is built from the vendored `third_party/opencode` tree:

```sh
OUT=dist/$(go env GOOS)-$(go env GOARCH) scripts/build-praimate-code.sh
```

It requires `bun` and around 8 GB of free scratch space. Set
`PRAIMATE_BUILD_DIR=/path/on/a/larger/disk` when `/tmp` is too small.

The GUI can also be built directly:

```sh
cd cmd/praimate-gui
./build.sh
```

## Project Layout

| Path | Contents |
|---|---|
| `cmd/praimate` | TUI, updater entrypoints, GUI launcher, `praimate code`, and chat/workspace screens. |
| `cmd/praimate-gui` | Wails desktop backend and Svelte frontend. |
| `cmd/wpc` | Workpath compiler CLI. |
| `internal/core` | Shared business logic for agents, chats, MCP, memory, workflows, schedules, and watchers. |
| `internal/launcher` | Workpath/chat launcher, session resume, transcript capture, and config migration. |
| `internal/installer` | CLI/tool detection and managed-tool installers. |
| `internal/backup` | Git-backed backup and state sync. |
| `pkg/workpath`, `pkg/targets` | Workpath parsing/validation and per-CLI target emitters. |
| `samples/` | Starter agents and workpath templates. |
| `third_party/opencode` | Vendored source used to build PrAImate Code. |

## Documentation

| Doc | Covers |
|---|---|
| [docs/GUIDE.md](docs/GUIDE.md) | Full user manual: surfaces, update flow, memory, session resume, keys, local LLM endpoints, managed tools, skills, knowledge, backup, and roadmap. |
| [docs/QUICKSTART.md](docs/QUICKSTART.md) | Workpath authoring in five minutes. |
| [docs/SCHEMA.md](docs/SCHEMA.md) | Workpath source format reference. |
| [docs/TARGETS.md](docs/TARGETS.md) | Per-CLI compile targets. |
| [docs/ACTIVATION.md](docs/ACTIVATION.md) | How compiled instructions activate per CLI. |
| [docs/LAUNCHER.md](docs/LAUNCHER.md) | Launcher behavior and UI notes. |

<p align="center">
  <img src="docs/assets/monke-mascot.png" alt="PrAImate mascot" width="150" />
</p>

## License

[MIT](LICENSE).
