# PrAImate 1.2.1

PrAImate 1.2.1 completes the managed Autonomous single-agent runtime introduced
in 1.2.0. Autonomous agents can now perform useful project work through a
PrAImate-owned policy and approval boundary instead of depending on unrestricted
CLI tools.

## Added

- Capability-gated project listing, reading, literal search, atomic file writes,
  Git operations, argv-only commands, and bounded HTTP GET requests.
- Direct managed MCP clients for local stdio, Streamable HTTP, and legacy SSE
  servers, with approval before connection and before each tool call.
- Raw-document and Graphify RAG knowledge tools with index validation and
  backend-only local-model credential resolution.
- Durable transcript checkpoints and resume controls for stopped, failed,
  stalled, and crash-interrupted runs.
- Agent Studio controls to inspect, resume, stop, and approve tools for managed
  runs, plus clear Managed Autonomous and Native CLI labels.

## Changed

- Guided creation no longer offers Team agents until delegation has a real
  coordinator. Existing Team manifests remain fail-closed.
- Managed Autonomous agents are hidden from the interactive Terminal surface;
  Chats, Studio, and Workflows use the managed runtime.
- Each resume attempt receives a fresh bounded turn budget while preserving the
  same run ID, working memory, artifacts, and model session.
- Documentation and privacy disclosures now describe the complete managed-tool
  surface and the plaintext, permission-restricted run files outside the
  encrypted database.

## Security

- File tools resolve paths beneath the selected project or knowledge root and
  reject traversal and symlink escapes.
- File writes, commands, mutating Git operations, network requests, MCP
  connections, and MCP calls require explicit GUI approval.
- MCP credentials remain in the backend and MCP schemas/output are treated as
  untrusted data with bounded model-facing content.
- Interrupted tool calls are recorded with an unknown outcome so resume tells
  the agent to inspect current state before retrying.
- Sandbox and delegation claims remain blocked; approved commands are real host
  processes and are not presented as OS-level isolation.

## Compatibility

- Linux amd64 desktop bundle.
- Windows amd64 and arm64 desktop bundles.
- macOS and GUI-less release bundles are not supported.
