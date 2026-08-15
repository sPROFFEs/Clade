# PrAImate 1.2.0

PrAImate 1.2.0 introduces a managed single-agent runtime and unifies the
execution contract used by Chats, document Studio, and Workflows.

## Added

- Guided runtime presets with explicit, portable `runtime.json` manifests.
- Managed Autonomous runs with bounded context and output, per-run working
  memory, text artifacts, explicit completion, and inspectable lifecycle state.
- A Managed runs inspector in Agent Studio for final responses, memory, and
  artifacts.
- Unified launch preflight across GUI surfaces, including local-model routing
  and credential resolution from encrypted storage.

## Changed

- Managed chats preserve bounded on-chat conversation history without creating
  a cross-chat memory profile.
- Agent knowledge and Graphify calls resolve local-model credentials through
  the encrypted backend rather than renderer-supplied secrets.
- Privacy documentation now identifies managed-run state as local,
  permission-restricted files outside the encrypted database and Git backup.

## Security

- Autonomous execution is fail-closed: only adapters that explicitly enforce
  safe mode may run, and the underlying CLI receives no elevated tool level.
- MCP-backed Autonomous agents, sandbox claims, checkpoints, delegation, and
  Team execution remain blocked until their policy-aware runtime phases exist.
- Managed Autonomous agents with Graphify RAG remain blocked until the
  policy-aware knowledge broker exists; Raw documents and native RAG remain
  available.
- Codex local-model routing remains disabled; supported local routing is limited
  to Claude/OpenClaude and OpenCode/PrAImate Code.
- The managed runtime creates no event or diagnostic log files.

## Compatibility

- Linux amd64 desktop bundle.
- Windows amd64 and arm64 desktop bundles.
- macOS and GUI-less release bundles are not supported.
