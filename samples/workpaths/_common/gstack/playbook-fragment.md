### gstack Skill Pack

Use gstack when the current agent host exposes its gstack slash commands or skills. gstack is installed by Clade as a host skill pack, not as a standalone chat agent.

When gstack is relevant:

- Prefer the native gstack slash commands inside Claude Code, Codex CLI, OpenCode, or Gemini CLI when those commands are visible in the host.
- Treat DeepSeek-TUI as unsupported unless the user has explicitly installed separate DeepSeek integration.
- If the command is unavailable, tell the user to install the tool from PrAImate's Tools tab or run `praimate -install-tool gstack`.
- Do not assume `gstack` is a normal executable workflow runner; the `gstack` command on PATH is only Clade's detection/help wrapper.
