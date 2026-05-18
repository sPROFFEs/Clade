# Rules

Hard constraints when authoring a workpath. Violations break the
launcher's compiler or produce a workpath that won't load.

- **Always read `knowledge/schema.md` and `knowledge/targets.md`
  before producing any workpath file.** Don't invent fields,
  filenames, or directory names. The schema is authoritative.
- **Never invent reference material for `knowledge/`.** If the
  user wants knowledge in the workpath, they supply it. You can
  summarise / reformat / split files they hand you, but never
  fabricate facts.
- **Write to disk with your file tools.** Don't return file
  bodies as chat messages unless explicitly asked. The user
  expects to find real files under `<workspaces-root>/templates/
  <name>/workpath/` (or the chat's own workpath dir) after you
  say you're done.
- **Use only the directory names the schema defines:**
  `tools/`, `agents/`, `knowledge/`. Don't introduce siblings
  like `docs/`, `assets/`, `data/` — the launcher will ignore
  them and they'll mislead readers.
- **Tool / agent names** must match `^[a-z0-9][a-z0-9_-]*$`.
  No spaces, no uppercase, no leading hyphen / underscore.
- **`mission.md` is required and non-empty.** Without it the
  workpath fails validation and chats can't be created from it.
- **`description` must be one line.** It feeds Claude Code's
  YAML frontmatter and Cursor's `.mdc` frontmatter, both of
  which break on multi-line values.
- **Don't create empty directories.** If there are no tools,
  leave out the `tools/` directory entirely. Same for `agents/`
  and `knowledge/`.
- **Cross-platform tool scripts** ship as paired `foo.sh` +
  `foo.ps1` in the same `tools/` directory. The launcher groups
  them as one logical tool. Don't try to put platform variants
  in subdirectories — auto-discovery only looks at the top of
  `tools/`.
- **`workpath.json`'s `tools` / `agents` arrays disable
  auto-discovery completely.** Use them only when you need to
  override the auto-discovered name or description; otherwise
  let auto-discovery do its job.
- **Personality files with only HTML comments are no-ops.**
  When scaffolding a `personality.md`, you can leave it as a
  comment block — the launcher treats it as "no persona
  configured" and won't inject anything.
- **`template.json` lives next to `workpath/`, not inside it.**
  It carries chat-creation defaults (`memoryEnabled`, `ollama`,
  `onlineSkills`, `language`). The agent never sees its
  contents — these are settings the launcher consumes.
- **After producing files, report exact paths.** End with a line
  like "Wrote: `/path/to/workpath/mission.md`, …" so the user
  can verify on disk and open them with the launcher's `f` key.
