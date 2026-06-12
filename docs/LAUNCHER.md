# `praimate` — launcher reference

For an overview, see the [top-level README](../README.md). This page
is the technical reference: the lifecycle, the on-disk layout, the
per-agent integration model, the keys.

## Current status

**0.1.10** ships native session resume, the stay-in-Clade UX,
per-chat slice snapshotting, and the settings-as-menu refactor —
the architectural reset that closes out the 0.1.x line. What works:

- **Templates + chats split**, auto-migrating any v0.1 layout it
  finds on first launch.
- **Stay-in-Clade UX** — the Bubble Tea program never quits when an
  agent runs. `tea.ExecProcess` hands the TTY to the agent for the
  session; control returns to the chat list on agent exit.
- **Native session resume** — `claude --resume <UUID>` /
  `--continue` and `codex resume <UUID>` / `--last`, with picker
  fallback when 2+ matching sessions exist. Cross-machine restore
  via the captured rollout when the agent's native store is empty.
- **Full per-chat slice snapshot** on every agent exit
  (`<chat>/sessions/<ts>-<agent>/native/`), running asynchronously
  so the TUI redraws immediately. Makes chat dirs portable across
  machines.
- **Settings as a menu** — Language, Memory, Mirror state, Agent,
  Local endpoint, Online skills, all under `e` on the chat list.
  Per-chat agent picker + local-endpoint config both moved here.
- **Local-endpoint wizard** — any OpenAI-compatible endpoint
  (Ollama, GPUStack, vLLM, LiteLLM, llama.cpp's server, LocalAI…)
  with optional Bearer auth (GPUStack et al.). Codex apply is gated
  by a `/v1/responses` probe so wire_api mismatches don't ship as
  silent runtime failures.
- **Per-chat slice mirror IN** — opt-in via "Mirror agent state"
  in chat settings. Restores the captured slice into the agent's
  home dir before launch; SIGKILL-safe via per-file mtime
  comparison.
- **`F` key fresh launch** — skip resume on a chat that has
  captured sessions, without deleting them.
- **Cross-chat search** — `/` on the chat list, greps MEMORY.md +
  summary.md + transcript.jsonl across every chat.
- **Per-agent install method registry** + pnpm auto-install via
  corepack + opt-in Node-via-winget on Windows.

What's not in 0.1.10:

- OpenCode native session resume (slice snapshot wired; the resume
  branch in `internal/launcher/resume.go` falls back to markdown
  summary inject).
- Shape-aware codex probe (status-code only today; an endpoint that
  returns 200 with non-responses-shaped body sneaks through).
- Auto-spawned LiteLLM sidecar for codex+chat-completions-only
  servers (user runs LiteLLM externally and points the wizard at
  it).
- Structured tagging on top of cross-chat search.

## On-disk layout

```
<root>/
├── templates/
│   └── <name>/
│       ├── workpath/                  read-only wpc source
│       └── template.json              default settings (language, memory, mirror,
│                                      local endpoint, online skills, ollama)
└── chats/
    └── <chat-id>/                     <UTC-timestamp>-<slug>
        ├── chat.json                  {label, template, agent, createdAt,
        │                              lastUsed, settings.{ollama,memoryEnabled,
        │                              mirrorAgentState,language,onlineSkills}}
        ├── workpath/                  cloned from template at creation
        ├── sandbox/                   agent cwd; compiled artifacts; gitignored
        ├── MEMORY.md                  persistent agent memory; sandbox copy
        │                              syncs back here after every launch
        └── sessions/
            └── <ts>-<agent>/          per-launch artifacts
                ├── transcript.jsonl   canonical rollout (search + summary)
                ├── summary.md         rule-based digest
                ├── summary.json       structured metadata
                └── native/            full per-chat slice of the agent's
                    └── <agent>-…/     home-dir store at exit time
```

## Launch sequence

```
Home → Enter on chat (or F for fresh)
                  │
                  ▼
   OpenChatWithOptions(c, opts) runs:
     - DetectAgents → resolve chat.AgentID to a real binary
     - If MirrorAgentState ON: WaitForMirror (drain prior in-flight
       snapshot), then MirrorInSlice → copy <chat>/sessions/<latest>
       /native/<agent>/ back into ~/.<agent>/...
     - RestoreNativeSession (skipped under SkipResume):
         · 2+ native sessions matching this sandbox → agent's picker
         · 1 native session                         → --continue / resume <UUID>
         · 0 native sessions + captured transcript  → restore + resume
     - Plan(ws, agent) → buildAgentCmd
                  │
                  ▼
   tea.ExecProcess(cmd, callback) hands the TTY to the agent.
   Bubble Tea program keeps running.
                  │
                  ▼
   Agent runs interactively. Workpath is compiled into the sandbox,
   MEMORY.md is staged, online skills are cloned, recent-sessions
   directive is injected into the compiled instructions.
                  │ (agent exits)
                  ▼
   Callback runs synchronously:
     - SyncMemoryBack — sandbox MEMORY.md → chat-root MEMORY.md
     - CapturePostExit:
         · CaptureTranscript → transcript.jsonl
         · WriteSummary      → summary.md + summary.json
         · StartMirrorOutAsync → background goroutine snapshots
                                 the full slice into native/
                  │
                  ▼
   agentExitedMsg fires. rootModel routes to newLayoutModel(cfg).
   TUI redraws on the chat list. No shell round-trip.
```

## Keys

### Home (chat list)
| Key       | Effect                                                      |
|-----------|-------------------------------------------------------------|
| `↑/↓ k/j` | Move selection                                              |
| `enter`   | Open chat — auto-resume if a native session exists          |
| `F`       | Fresh launch — skip resume; leave captured sessions on disk |
| `n`       | New chat                                                    |
| `e`       | Settings menu (agent, language, memory, mirror, endpoint, skills) |
| `f`       | Edit chat files (mission.md, personality.md, …)             |
| `p`       | Pin chat to the tab strip                                   |
| `d`       | Delete chat (confirms)                                      |
| `/`       | Cross-chat search                                           |
| `t`       | Template manager                                            |
| `r`       | Refresh                                                     |
| `ctrl-c`  | Quit                                                        |

### Settings menu (per chat, opened with `e`)
| Key       | Effect                                                |
|-----------|-------------------------------------------------------|
| `↑/↓`     | Move row                                              |
| `enter`   | Open sub-editor for that row (textinput / picker)     |
| `space`   | Toggle the highlighted boolean inline (memory / mirror) |
| `esc`     | Save + return to chat list                            |

### Local-endpoint wizard (settings → "Local endpoint")
| Step | Keys |
|---|---|
| Endpoint input | `enter` advance · `esc` back |
| API key input | `enter` advance (blank = no auth) · `esc` back |
| Probe + model pick | `↑/↓` select probed model · `enter` accept · `esc` back to edit |
| Agents multi-select | `↑/↓` move · `space` / `x` toggle · `enter` apply · `esc` back |
| Apply | `enter` / `esc` return to settings list |

### Template list (`t` from home)
| Key     | Effect                                            |
|---------|---------------------------------------------------|
| `enter` | Edit settings of highlighted template             |
| `n`     | New template (wizard)                             |
| `d`     | Delete (existing chats from it are unaffected)    |
| `f`     | Edit template files                               |
| `esc`   | Back to chats                                     |

### Agents tab (`Ctrl-3` from anywhere — install-only)
| Key     | Effect                                                  |
|---------|---------------------------------------------------------|
| `↑/↓`   | Move selection                                          |
| `enter` | Open the installer for the highlighted agent (install or upgrade) |
| `i`     | Same as Enter                                           |
| `esc`   | Back                                                    |

### Install screen
| Key     | Effect                                                  |
|---------|---------------------------------------------------------|
| `↑/↓`   | Pick install method                                     |
| `n`     | (Windows + winget only) Toggle "also install Node.js LTS" opt-in |
| `enter` | Run the method                                          |
| `esc`   | Back                                                    |

### Anywhere
| Key      | Effect                                                  |
|----------|---------------------------------------------------------|
| `ctrl-c` | Quit immediately                                        |
| `esc`    | Step back one screen                                    |
| `ctrl-p` | Command palette                                         |
| `F1`     | Help screen                                             |

## Per-agent integration touchpoints

Each agent's behavior is split across these files; adding a new
agent means adding cases in all of them:

| Aspect | Where |
|---|---|
| Binary detection on PATH + known install dirs | `internal/launcher/agents.go` |
| Cross-OS home-dir store paths | `internal/launcher/agentpaths.go` |
| Install method catalog (per OS) | `internal/installer/installer.go:allMethods` |
| Per-launch `Plan()` env / arg injection | `internal/launcher/launch.go:Plan` |
| Local-endpoint config writer / stripper | `internal/ollama/ollama.go:Apply{Codex,OpenCode,DeepSeek}` / `Disable*` |
| Transcript locator | `internal/launcher/transcript.go:locate{Claude,Codex,OpenCode,Gemini}Transcript` |
| Full slice snapshot in / out | `internal/launcher/slice.go:mirror{In,Out}{Claude,Codex,OpenCode,Gemini}` |
| Native session resume decision | `internal/launcher/resume.go:resume{Claude,Codex}` |
