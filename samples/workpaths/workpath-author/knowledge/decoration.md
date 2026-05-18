# Launcher decoration pipeline

Things the launcher adds to the compiled instructions on **every
chat launch**, on top of whatever the workpath author put in.
Important to know — these are features your workpath gets for
free; don't try to reproduce them by hand in `mission.md` or
`rules.md`.

Order of operations (see `internal/launcher/decorate.go`):

1. **Personality prepend** — if `personality.md` exists and isn't
   comments-only, its body is wrapped in a `## Persona` section
   and inserted at the very top of the compiled instructions
   (after YAML frontmatter on the Claude target).

2. **Language directive prepend** — if `template.json` or
   `chat.json` has a non-empty `settings.language`, a one-line
   directive (`> Language directive: respond in <lang>.`) goes
   in just below the persona.

3. **Memory** — if `memoryEnabled: true`:
   - Workspace's `MEMORY.md` is staged into the sandbox
     (`<chat>/sandbox/MEMORY.md`).
   - A session-start marker (`## YYYY-MM-DD HH:MM — Session
     opened`) is appended to the workspace's `MEMORY.md`.
   - A `## Persistent memory — required workflow` section is
     appended to the compiled instructions. This is the
     "you MUST read MEMORY.md first" directive — verbatim text
     in decorate.go.

4. **Online skills** — if `settings.onlineSkills` is a non-empty
   array of URLs:
   - Each URL is fetched (git clone for non-zip, http download +
     unzip for `.zip`).
   - For Claude, skills land in `<sandbox>/.claude/skills/` so
     Claude Code's auto-loader picks them up.
   - For everyone else, they land in `<sandbox>/online-skills/`.
   - A `## Online skills — required reading workflow` section
     gets appended to the compiled instructions, listing each
     skill's relative path.

5. **Knowledge** — handled by the wpc compiler, not the
   decorator: every file under `<workpath>/knowledge/` is
   copied verbatim into `<sandbox>/knowledge/`, and a
   `## Knowledge base — required reading workflow` section is
   added to the compiled instructions listing files with title +
   summary.

6. **Session record** — `<workspace>/sessions/<timestamp>-<agent>/
   session.json` is written so the user can browse past
   sessions.

## What this means for you as a workpath author

- **Don't write your own "you must read MEMORY.md" instructions
  in `mission.md`** — the launcher already adds the canonical
  version. Yours will only conflict.
- **Same for knowledge and online-skills directives.** Just drop
  files in the right directories; the launcher writes the
  agent-facing wording.
- **Personality file overrides every default tone.** It comes
  before mission/playbook/rules, so use it sparingly — it's
  the strongest lever you have.
- **The agent reads memory + knowledge + skills on every user
  turn** (the directives are "required workflow" prescriptive),
  so over-stuffing those folders has a cost: every turn becomes
  a scan. Aim for a small, sharp set of files rather than
  dumping everything you have.

## After agent exits

`SyncMemoryBack` copies `<sandbox>/MEMORY.md` → `<workspace>/
MEMORY.md` if the sandbox version is newer. That's how the
agent's notes persist across launches.

Online skills don't sync back — they're treated as read-only
reference, re-fetched on demand.

Knowledge doesn't sync back either — the canonical copy is the
workpath's. To update knowledge, edit the workpath, not the
sandbox.
