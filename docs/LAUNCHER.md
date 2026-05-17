# `clade` — full reference

For an overview, see the [top-level README](../README.md).

## Status

**v0.2 — chats + templates model** ships in this version.

**What works**
- Templates + chats split with auto-migration from v0.1.
- Home = chat list, sorted by last-used; new / open / delete.
- New-chat wizard: pick template → name → pick agent (locked).
- Template management: list / edit settings / new / delete.
- 5-step new-template wizard (name, description, language, memory, online skills).
- Per-chat sandbox auto-created and gitignored.
- Agent CLI detection (`exec.LookPath` + `--version` must succeed).
- pnpm auto-resolve: corepack enable + pnpm setup + PNPM_HOME injection so installs work on fresh Windows.
- Install missing CLIs from inside the launcher (`i`).
- Ollama config screen (`o`): probe + model picker + per-agent apply.
- Per-chat language directive, MEMORY.md staging/sync-back, online-skill git fetch on every launch.
- Clean TTY hand-off; chat lastUsed bumped after the agent exits.

**Phase 3 (not started)**
- Transcript browser (per-agent adapter for `~/.claude/projects/`, `~/.codex/sessions/`, ...).
- Zip-archive online skills (git-only today).
- Per-chat agent override (locked at creation today).
- Rich chat search / tagging.

## Past chatlog review

The launcher does not capture agent transcripts itself. Each chat has a
**stable cwd** under `<root>/chats/<chat-id>/sandbox/`, so when you
re-open a chat:

| Agent      | What happens                                                                 |
|------------|------------------------------------------------------------------------------|
| Claude Code| Sees the same project-hash (cwd-derived). `claude /sessions` lists past sessions; `claude -c` continues the most recent. |
| Codex CLI  | `codex resume` from the chat dir picks up its per-project session history.   |
| OpenCode   | `opencode --continue` from the chat dir resumes the most recent.             |

The MEMORY.md the launcher syncs back gives you a portable,
agent-agnostic place to keep notes that survive even if the agent's
session store is wiped.

## On-disk layout

```
<root>/
├── templates/
│   └── <name>/
│       ├── workpath/                  read-only wpc source
│       └── template.json              default settings (language, memory, online skills, ollama)
└── chats/
    └── <chat-id>/                     <UTC-timestamp>-<slug>
        ├── chat.json                  {label, template, agent, createdAt, lastUsed, settings}
        ├── workpath/                  cloned from template at creation
        ├── sandbox/                   agent cwd; compiled artifacts
        ├── sessions/<ts>-<agent>/     per-launch metadata
        └── MEMORY.md                  persistent agent memory
```

## Launch sequence

```
Home → pick chat → pick agent
                  │
                  ▼
   wpc compiles <chat>/workpath → <chat>/sandbox (target=claude or codex)
   language directive prepended · MEMORY.md staged · online skills cloned
                  │
                  ▼
   Bubble Tea releases the TTY; agent spawned with cwd=<chat>/sandbox
                  │
                  ▼
   Agent runs interactively · sees the stable cwd, offers session resume
                  │ (agent exits)
                  ▼
   MEMORY.md synced back to <chat>/MEMORY.md
   chat.json's lastUsed bumped
   Exit code propagated to the shell
```

## Keys

### Home (chat list)
| Key       | Effect                              |
|-----------|-------------------------------------|
| `↑/↓ k/j` | Move selection                      |
| `enter`   | Open chat (or `+ new chat`)         |
| `n`       | New chat                            |
| `d`       | Delete highlighted chat (confirms)  |
| `t`       | Template management                 |
| `r`       | Refresh                             |
| `ctrl-c`  | Quit                                |

### Template list (`t` from home)
| Key     | Effect                                            |
|---------|---------------------------------------------------|
| `enter` | Edit settings of highlighted template             |
| `n`     | New template                                      |
| `d`     | Delete (existing chats are unaffected)            |
| `esc`   | Back to chats                                     |

### Agents screen
| Key     | Effect                                            |
|---------|---------------------------------------------------|
| `enter` | Launch (if installed) or open installer (if not)  |
| `i`     | Install / upgrade the highlighted agent           |
| `o`     | Open the Ollama configuration screen              |

### Anywhere
| Key      | Effect                  |
|----------|-------------------------|
| `ctrl-c` | Quit immediately        |
| `esc`    | Step back one screen    |
