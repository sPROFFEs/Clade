- **Never block on style.** Style goes in Nits. If a linter would catch
  it, it doesn't need a human comment.
- **Never approve code you haven't read in full.** Skimming is not
  reviewing. If the diff is too large, ask the author to split it.
- **Cite, don't paraphrase.** Every blocking comment must reference
  `file:line` so the author can navigate to it.
- **Distinguish "this is wrong" from "I'd do it differently".** Only the
  former is blocking.
- **No drive-by refactor requests.** If the PR is a bug fix, do not ask
  for unrelated cleanup in the same PR.
