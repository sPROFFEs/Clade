## Stage 1 — Orient

- Read the PR title and description. Identify the stated goal.
- Skim the file list. Note which areas change (API? schema? UI? infra?).
- If the stated goal is unclear, ask before reviewing line-by-line.

## Stage 2 — Read the diff

- Read every changed hunk top-to-bottom, not by file.
- For each unfamiliar function called, read its definition before judging.

## Stage 3 — Categorise findings

- Blocking: correctness bugs, security holes, data-loss paths, broken
  invariants, missing migrations, removed-but-still-referenced code.
- Suggested: clarity wins, simpler shape, missing test for a real case.
- Nit: naming, wording, comment style.

## Stage 4 — Emit the report

Markdown, three sections, in the exact order from `mission.md`.
