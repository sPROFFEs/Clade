# Code-review heuristics

A short cheat-sheet of patterns to look for during a PR review. Open
the file when you want to remind yourself of categories beyond what
the diff makes obvious.

## Correctness

- Are error paths actually returned to the caller, or silently
  swallowed? Look for `_ = err` and bare `catch:` blocks.
- Off-by-one in loop bounds and slice indexing.
- Integer overflow / signed-vs-unsigned mismatches in arithmetic on
  external input.
- Time-of-check vs time-of-use races: stat-then-open, exists-then-write.

## Readability

- Function name matches what the function actually does (verbs vs
  nouns; "load" vs "fetch" vs "open").
- Comments explain *why*, not *what*.
- Magic numbers / strings either become named constants or come from
  configuration.

## Security

- All user-controlled input crosses a validation boundary before
  reaching a sink: file path → realpath check, SQL → parameterised,
  shell → fork+exec with explicit argv (no shell interpolation), HTML
  → contextual escaping, regex → bounded backtracking.
- Secrets stay out of logs / error messages / tracebacks.
- Authorisation re-checked on every request, not cached client-side.

## Performance

- N+1 query patterns (loop body calling the DB).
- Linear scan in a hot path where a map / index would do.
- Re-compilation of regexes / templates inside loops.

## Style

- Function signatures fit one line at the team's preferred width.
- Public symbols have doc-comments; private symbols use them when
  the WHY is non-obvious.
- Tests target observable behaviour, not implementation details.
