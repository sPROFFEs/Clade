<p align="center">
  <img src="docs/assets/monke-icon.png" alt="PrAImate" width="160" />
</p>

# PrAImate

> Official releases and source updates are published from
> [sPROFFEs/PrAImate](https://github.com/sPROFFEs/PrAImate).

```
   .-"-.     ██████╗ ██████╗  █████╗ ██╗███╗   ███╗ █████╗ ████████╗███████╗
  /|6 6|\    ██╔══██╗██╔══██╗██╔══██╗██║████╗ ████║██╔══██╗╚══██╔══╝██╔════╝
 {/(_0_)\}   ██████╔╝██████╔╝███████║██║██╔████╔██║███████║   ██║   █████╗
  _/ ^ \_    ██╔═══╝ ██╔══██╗██╔══██║██║██║╚██╔╝██║██╔══██║   ██║   ██╔══╝
 (/ /^\ \)   ██║     ██║  ██║██║  ██║██║██║ ╚═╝ ██║██║  ██║   ██║   ███████╗
  ""' '""    ╚═╝     ╚═╝  ╚═╝╚═╝  ╚═╝╚═╝╚═╝     ╚═╝╚═╝  ╚═╝   ╚═╝   ╚══════╝

                  one harness, every agent - workflows & MCP
```

PrAImate is a local harness for Claude Code, OpenClaude, Codex CLI,
OpenCode, and the bundled **PrAImate Code** build. It provides the layer
around those tools: agents, workflows, per-chat `MEMORY.md`, MCP configuration,
automation, local tool management, and optional git-backed backup.

The current project version is **1.0.10**.

## Binaries

Release archives and source builds revolve around four executables:

| Binary | Purpose |
|---|---|
| `praimate` | GUI bootstrap, updater, managed-tool installer, automation CLI, and `praimate code` dispatcher. Running it without flags opens the desktop app. |
| `praimate-gui` | Wails/Svelte desktop application. It is mandatory in every supported release archive. |
| `praimate-code` | Bundled, version-pinned, rebranded OpenCode build. Launched directly or through `praimate code`. |
| `wpc` | Workpath compiler. Validates and compiles portable workpaths into Claude/Codex/OpenCode/Cursor/mika/generic target files. |

The desktop app keeps its persistent state under one OS config folder:
`$XDG_CONFIG_HOME/praimate` (normally `~/.config/praimate`) on Linux and
`%APPDATA%\praimate` on Windows. The AES-256-XTS encrypted SQLite database
and its password-protected key envelope live there with user-only permissions.
The raw database key exists only in process memory while the app is unlocked;
by default the database password is required on every launch. An explicit
“Remember on this device” opt-in uses Windows Credential Manager or the Linux
desktop Secret Service.
Saved API keys are stored in the encrypted database, not `config.json`.
PrAImate does not create application, Graphify-query, or terminal
log files. Live Code-terminal scrollback is memory-only and disappears
when its process closes. Agent CLIs may still maintain their own native
session data.

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
- **Automation**: folder watchers and cron schedules can trigger agent
  workflows.
- **Local model routing**: supported CLIs can be routed through
  Ollama/vLLM/GPUStack/LiteLLM style endpoints.
- **Managed tools**: graphify, gstack, scrapegraph, and bundled
  PrAImate Code can be installed into PrAImate-managed paths and picked
  up by the GUI and maintenance CLI.
- **Privacy redaction**: outbound prompts can be scanned for secrets,
  tokens, PII, and user-defined regexes before a CLI receives them.
- **First-run privacy disclosure**: explains provider data flow, agent
  file permissions, local encryption limits, and backup exposure before
  the app can be used.
- **About page**: shows the exact PrAImate version, platform, database
  encryption status, storage paths, and the privacy disclosure.
- **Git backup**: chats, templates, and shareable state can be synced
  across machines with plain git.

## Install

Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/sPROFFEs/praimate/main/scripts/install.sh | bash
```

Windows PowerShell:

```powershell
iwr -useb https://raw.githubusercontent.com/sPROFFEs/praimate/main/scripts/install.ps1 | iex
```

The installer resolves the latest GitHub release unless `RELEASE_TAG`
is set. Every supported archive contains `praimate`, `praimate-gui`, and
`wpc`. Optional managed binaries are installed when present.

Supported systems are **Linux** and **Windows**. macOS is not supported
and no macOS release archives are published.

Useful installer modes:

```sh
./scripts/install.sh --binary      # download a release archive
./scripts/install.sh --source      # build from source
./scripts/install.sh --user        # install into ~/.local/bin
./scripts/install.sh --system      # install into /usr/local/bin
./scripts/install.sh --yes         # non-interactive defaults
./scripts/install.sh --uninstall   # remove binaries, wrappers, desktop entries
./scripts/install.sh --uninstall --purge   # also remove config, tools, chat DB
```

```powershell
.\scripts\install.ps1 -Mode Binary
.\scripts\install.ps1 -Mode Source
.\scripts\install.ps1 -AllUsers
.\scripts\install.ps1 -Yes
.\scripts\install.ps1 -Uninstall          # remove binaries, shortcuts, PATH entry
.\scripts\install.ps1 -Uninstall -Purge   # also remove config, tools, chat DB
```

Uninstalling without `--purge` / `-Purge` keeps your config, managed
tools and chat database, so a later reinstall picks up where you left
off.

Source installs detect an existing checkout and build `praimate`,
`praimate-gui`, and `wpc` together. A missing GUI dependency is a hard
failure rather than producing a partial installation. On Debian/Kali-style Linux systems the GUI
build needs:

```sh
sudo apt-get install -y npm pkg-config libwebkit2gtk-4.1-dev libgtk-3-dev
```

Installers, release downloads, source builds, and `praimate -update`
resolve from the public GitHub repository using normal OS TLS verification.

## Quick Start

```sh
praimate                # desktop GUI
praimate --gui          # compatibility alias for the same action
praimate code           # bundled PrAImate Code CLI
praimate -check-update  # check GitHub for a newer release
praimate -update        # self-update installed binaries
praimate -version       # banner + version
```

First run asks for a workspaces root and can seed sample workpaths and
starter agents. From there:

1. Open PrAImate with `praimate`, complete first-run setup, and use
   **Chats** for normal
   conversations, **Code** for live project terminals, **Agents** for
   imported agents, and **MCP** for server connections.
2. Use **CLI & Tools** to detect/install CLIs and PrAImate-managed
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
scripts/build.sh --with-code
scripts/build.sh --with-graphify
scripts/build.sh --version=1.0.10
```

`scripts/build.sh` stamps the version into `praimate`, builds Linux and
Windows archives under `dist/`, and refuses to create an archive without
the GUI. PrAImate Code and graphify remain optional sidecars.

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
| `cmd/praimate` | GUI bootstrap, updater/installer entrypoints, and `praimate code`. |
| `cmd/praimate-gui` | Wails desktop backend and Svelte frontend. |
| `cmd/wpc` | Workpath compiler CLI. |
| `internal/core` | Shared business logic for agents, chats, MCP, workflows, schedules, and watchers. |
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

<p align="center">
  <img src="docs/assets/monke-mascot.png" alt="PrAImate mascot" width="150" />
</p>

## License

[MIT](LICENSE).
