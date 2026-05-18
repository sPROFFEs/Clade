# Playbook for authoring a workpath

Follow this staged process when the user asks you to create a
new workpath. Skip stages that don't apply, but acknowledge each
one out loud so the user can object before you commit code.

## Stage 1 — Scope

Before writing anything, get four answers from the user (one short
question per gap; don't grill them):

1. **Purpose** — one sentence: "this workpath is for…". This
   becomes `workpath.json`'s `description`.
2. **Tasks** — the 2-5 concrete things the agent should be able to
   do inside chats cloned from this template. Drives `mission.md`
   and `playbook.md`.
3. **Hard rules** — anything that's a *don't* (e.g., "never edit
   production config", "always confirm before deletes"). Drives
   `rules.md`.
4. **Reference material** — does the user have docs, papers, tool
   manuals, schemas, cheat-sheets they want the chat to have on
   hand? If yes, they go under `knowledge/`. Don't invent
   knowledge content the user didn't supply; recommend that they
   drop their own files in instead.

When in doubt, **propose a name** for the workpath
(kebab-case-and-numbers, e.g. `incident-response`) and confirm
before creating the directory.

## Stage 2 — Skeleton

Use `tools/new-workpath.sh` (or `.ps1` on Windows) if available to
scaffold the standard layout. Otherwise create by hand:

```
<workpath-name>/
├── workpath.json          minimal: just description + version
├── mission.md             required, non-empty
├── playbook.md            staged process
├── rules.md               hard constraints
├── personality.md         optional persona (HTML-comment template OK to start)
├── tools/                 only if you have shell tools to ship
├── agents/                only if you have subagents to ship
└── knowledge/             only if the user supplied reference material
```

Don't create empty `tools/` or `agents/` directories — leave them
out entirely until there's something to put in them. The launcher
treats missing directories as "no tools" cleanly.

## Stage 3 — Content

Write the bodies. Conventions, in priority order:

1. **`mission.md`**: open with an H1 of the workpath name, then a
   blockquote one-line description, then 2-4 sentences of
   *what the agent does and how it should behave at a high level*.
   Keep it under ~30 lines — agents read it on every launch.
2. **`playbook.md`**: numbered H2 stages ("## Stage 1 — Foo"),
   each with a bullet list of concrete steps. Match the style in
   the bundled `code-review` and `reversing` samples.
3. **`rules.md`**: short markdown bullet list. Each bullet is one
   imperative ("Do X" / "Never Y"). 5-15 bullets is plenty.
4. **`personality.md`** (optional): the persona prepended at the
   top of the compiled instructions. HTML comments at the top of
   the file are stripped — use them to scaffold guidance for
   future editors without leaking it into the agent's context.

## Stage 4 — Tools (only if needed)

Each `tools/*.sh` (Linux/macOS) or `*.ps1` (Windows) becomes a
shell-callable command. Conventions:

- Filename without extension is the tool name. Pair `foo.sh` +
  `foo.ps1` to give cross-platform coverage; the launcher
  detects the pair and ships both.
- First non-shebang comment line is the description.
- Tools should accept their target(s) as positional args, write
  to stdout, exit non-zero on failure.

If you want to expose only a subset of the scripts (e.g., one
public name backed by a script that's not at the obvious path),
add a `"tools": [...]` array to `workpath.json` — auto-discovery
is disabled the moment that array is set.

## Stage 5 — Subagents (only if needed)

Each `agents/*.md` is a sub-persona the main agent can adopt or
hand off to. Frontmatter is optional; if absent, the agent name
is the filename and the description is the first H1 / first
non-empty line.

Keep the prompt short and focused. Subagents that try to do
everything tend to perform worse than specialised ones.

## Stage 6 — Knowledge (only if the user supplied it)

If the user has reference files, copy them into
`<workpath>/knowledge/` at whatever subdir structure they
prefer (`papers/`, `tools/`, `references/`, etc.). The launcher
will:

- Stage the whole tree into the chat's sandbox at every launch.
- Auto-generate a required-reading manifest in the compiled
  instructions listing every file with title + summary.
- Extract a summary from `.md`, `.txt`, `.rst`, `.org`, `.json`,
  `.yaml`, `.toml`, `.csv`, `.log`. Anything else (PDFs, images)
  is listed by name + size with a `(binary)` marker.

If the user has docs they'd LIKE you to summarise into shorter
knowledge files, do that — but check with them first; sometimes
they want the original verbatim.

## Stage 7 — Validate

After you write the files, validate by reading them back and
checking against `knowledge/schema.md`'s rules:

- `mission.md` exists and is non-empty.
- Workpath name (from directory or `workpath.json#name`) matches
  `^[a-z0-9][a-z0-9_-]*$`.
- Every tool/agent name matches the same pattern.
- No duplicate tool/agent names.
- Every referenced tool/script/prompt path exists on disk.

Report any failures with the exact line to fix. If everything
passes, list the files you wrote (with paths) and tell the user
they can now create a chat from this template (`n` on the home
screen → pick the new template).
