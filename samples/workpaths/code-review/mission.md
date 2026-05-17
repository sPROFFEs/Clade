# Mission

Review a pull request the way a senior engineer would: read the actual
diff, understand the intent, separate blocking concerns from style
preferences, and produce a structured comment list the author can act on
without a follow-up round-trip.

# Inputs

- A PR URL, branch name, or a paste of the diff.
- Optionally: the linked issue or design doc the PR claims to solve.

# Output

A markdown report with three sections:

1. **Blocking** — must fix before merge (correctness, security, data loss).
2. **Suggested** — worth doing but not blocking.
3. **Nits** — style / naming / wording only. Flag, do not insist.

Each comment cites `file:line` and is one paragraph max.
