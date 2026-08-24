# PrAImate 1.2.2

PrAImate 1.2.2 makes installed agents usable from external scripts and keeps
their identity visible when sessions move between Code, Chats, Sessions, and
Studio. It also closes permission and lifecycle gaps found while exercising the
1.2 managed runtime.

## Added

- A versioned `praimate agent run` automation interface with JSON, JSONL, and
  text output, deadlines, temporary or persisted chats, and explicit Safe,
  edits, or full tool policies.
- Deterministic `--cli` and `--model` selection plus `--endpoint saved` routing
  through the Local LLM endpoint configured in the GUI.
- Secure interactive database unlock with terminal echo disabled when the
  password is not remembered; unattended runs fail with structured output.
- A complete CLI API guide and a cross-platform Python caller example.

## Fixed

- Explicit Safe mode no longer inherits an agent runtime's wider default tool
  policy.
- Code and Studio sessions preserve and display the selected agent when they
  are closed, reopened, or resumed.
- Studio filesystem watchers now stop and release their resources when the
  application context closes.
- Local endpoint automation loads its API key from encrypted storage and
  refuses redirects to endpoints other than the saved GUI configuration.

## Packaging

- Linux and Windows bundles now include the full user guide, agent manual, CLI
  API guide, and runnable Python automation example referenced by the README.
- Supported release targets remain Linux amd64 and Windows amd64/arm64. macOS
  and GUI-less bundles are not supported.
