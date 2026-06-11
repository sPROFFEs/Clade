<p align="center">
  <img src="assets/icon.png" alt="PrAImate GUI" width="160" />
</p>

<h1 align="center">PrAImate GUI</h1>

<p align="center">
  Desktop + web command center for PrAImate's coding agents. Run <b>PrAImate Code</b> (bundled OpenCode build), Claude Code, Codex, and Pi across multiple projects with streaming chat, prompt queue, model switching, voice input, and MCP tools.
</p>

PrAImate GUI gives you a desktop and browser workflow for long agent sessions: manage multiple projects visually, run different agent backends from one UI, watch responses stream live, queue prompts while the agent works, and switch providers/models/agents without terminal juggling.

## Highlights

- **Multi-agent workspace** for PrAImate Code, Claude Code, Codex, and Pi
- **Multi-project workspaces** for parallel coding sessions
- **Real-time streaming** over SSE with live token/context usage
- **Prompt queue** that auto-dispatches when the assistant goes idle
- **Model, backend, and agent selection** directly from the chat workflow
- **Slash commands** from the prompt box
- **Syntax highlighting**, dark/light theme, MCP + skills configuration
- **Desktop, web, and Docker deployment options**

## Backends

- **PrAImate Code** — PrAImate's bundled, rebranded OpenCode build. Resolved automatically from PrAImate's managed bin dir (`<config>/praimate/bin/praimate-code`) or `PATH`, with fallback to a stock `opencode` install.
- **Claude Code** — local `claude` CLI.
- **Codex** — local `codex` CLI.
- **Pi** — bundled Pi runtime.

## Build from source

Prerequisites: Node.js 24+, pnpm 10+.

```bash
cd gui
pnpm install
vp run dev        # Electron app with HMR
vp run dev:web    # web app at http://127.0.0.1:3000
vp build          # production frontend bundle
vp run dist:win   # Windows installer (dist:linux / dist:mac for the others)
```

## Attribution

PrAImate GUI is based on [OpenGUI](https://github.com/akemmanuel/OpenGUI) by Emmanuel (akemsoft), used under the MIT license. The original license text is preserved in [LICENSE](LICENSE). All PrAImate-specific modifications are © the PrAImate project, MIT licensed.
