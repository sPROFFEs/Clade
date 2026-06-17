package core

// Skills catalogue + resolver.
//
// A skill is a small system-prompt fragment plus metadata: which CLIs
// it's designed for, what it does, what it changes about the
// assistant's behaviour. Skills are PURELY additive — they get
// prepended to the system prompt of the chats that have them enabled
// (per `ChatSettings.Skills`). PrAImate does not enforce
// CLI-compatibility; the catalogue tags every skill so the Skills page
// can group them and the chat-settings UI can warn when a user
// activates one designed for a different CLI.
//
// The catalogue is BUILT IN to the binary today. A future revision
// can fetch additional skills from a remote (the existing
// internal/skills package fetches Claude-Code-style bundles by URL);
// keeping the starter set in-process makes the Skills page work on a
// fresh install with no network.
//
// CLI tags (Skill.CLIs) use the same identifiers as the launcher:
// claude, openclaude, codex, opencode, praimate-code.

import (
	"sort"
	"strings"
)

// Skill is one catalogue entry.
type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	CLIs        []string `json:"clis"`            // CLIs the skill is designed for
	Body        string   `json:"body"`            // the system-prompt fragment
	Source      string   `json:"source,omitempty"` // "builtin" or a remote URL
}

// SkillCatalogue returns the built-in skills shipped in the binary.
// The set is intentionally small — one or two skills per CLI — to
// keep the Skills page usable on a fresh install. Add to it by
// appending to the slice below.
func SkillCatalogue() []Skill {
	out := make([]Skill, len(builtinSkills))
	copy(out, builtinSkills)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// SkillByID returns the catalogue entry with the given id, or nil if
// none. Cheap linear scan; the catalogue is small.
func SkillByID(id string) *Skill {
	for i := range builtinSkills {
		if builtinSkills[i].ID == id {
			s := builtinSkills[i]
			return &s
		}
	}
	return nil
}

// SkillsForCLI returns the catalogue entries that target the given CLI.
func SkillsForCLI(cli string) []Skill {
	cli = strings.TrimSpace(cli)
	out := []Skill{}
	for _, s := range builtinSkills {
		for _, c := range s.CLIs {
			if c == cli {
				out = append(out, s)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ResolveSkillsPrefix concatenates the bodies of every enabled skill
// for use as a prefix to the chat's system prompt. Unknown IDs are
// silently skipped (a deleted skill should not break an existing
// chat). The result is empty when no enabled skills exist.
func ResolveSkillsPrefix(enabledIDs []string) string {
	if len(enabledIDs) == 0 {
		return ""
	}
	var b strings.Builder
	for _, id := range enabledIDs {
		s := SkillByID(id)
		if s == nil {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n---\n\n")
		}
		b.WriteString("# Skill: ")
		b.WriteString(s.Name)
		b.WriteString("\n\n")
		b.WriteString(strings.TrimSpace(s.Body))
	}
	return b.String()
}

// builtinSkills — keep small, well-tested, useful. Each entry should
// pay rent: a generic "be helpful" skill is noise. Per-CLI tags reflect
// where the skill has been validated — adding more CLIs after a quick
// smoke test is fine.
var builtinSkills = []Skill{
	{
		ID:          "claude-debugger",
		Name:        "Debugger (Claude)",
		Description: "Targeted bug-hunting workflow: reproduce, isolate, hypothesize, verify. Uses Claude's tool-use loop for file reads and shell.",
		CLIs:        []string{"claude", "openclaude"},
		Source:      "builtin",
		Body: `You are in DEBUGGER mode. Stay in this loop until the user releases you.

1. REPRODUCE: rerun the failing scenario exactly. If you can't, ask for
   the exact command + expected vs actual output before guessing.
2. ISOLATE: bisect the change set (git log + git bisect when relevant);
   shrink the input until the failure is single-line.
3. HYPOTHESIZE: state ONE concrete hypothesis and what would
   falsify it. No "could be A or B".
4. VERIFY: run a test that proves or disproves the hypothesis. Cite
   the file:line where the failure surfaces.
5. FIX: write the smallest change that addresses the cited line.
   No drive-by refactors.
6. REGRESSION-PROOF: add or extend a test that fails on the old code
   and passes on the new code. Name the test in your reply.

Do not declare "fixed" without a verifying test. Do not bypass error
handling, validation, or sanitisation as a shortcut.`,
	},
	{
		ID:          "claude-codereview",
		Name:        "Pull-request reviewer (Claude)",
		Description: "Reviews a diff like a senior engineer: correctness, security, tests, then style. Severity-first, fix-suggested, file:line cited.",
		CLIs:        []string{"claude", "openclaude"},
		Source:      "builtin",
		Body: `You are a SENIOR CODE REVIEWER. Output a review of the current diff.

Triage by severity, lead with correctness bugs and security risks,
then missing tests, then API/design, then style. Every finding cites
file:line + a concrete proposed fix.

Format:
- Verdict: approve / approve-with-nits / request-changes
- Findings: grouped by severity (critical / high / medium / low / nit)
  Each finding:   file:line — problem — suggested fix
- "Could not verify" list: anything you'd want the author to confirm.

Rules:
- Don't rubber-stamp. If the diff is good, say "approve" briefly.
- No vague feedback. "Consider X" without a concrete X is a finding
  to delete.
- Check the tests. New behaviour without a test is a finding.
- Verify claims by opening the cited function before flagging it.`,
	},
	{
		ID:          "codex-shell",
		Name:        "Long-running shell ops (Codex)",
		Description: "Guidance for Codex's shell-execution model: pipe carefully, capture stderr, prefer idempotent commands.",
		CLIs:        []string{"codex"},
		Source:      "builtin",
		Body: `You are running on the Codex CLI's shell-execution model.

Before issuing any command:
1. State what it will do and what could go wrong.
2. Prefer idempotent commands (` + "`mkdir -p`" + `, ` + "`git pull --ff-only`" + `).
3. Capture stderr explicitly (` + "`2>&1`" + ` or ` + "`tee`" + `).
4. Never pipe untrusted input into ` + "`bash`" + `.
5. For long-running tasks (>30s), background them with explicit log
   redirection so the next turn can read the log.

Refuse destructive operations (` + "`rm -rf`" + ` outside cwd,
` + "`git reset --hard`" + ` on a non-temp branch, ` + "`sudo`" + `) without
explicit per-command user confirmation. Prefer ` + "`git stash`" + ` over
` + "`git reset --hard`" + ` when in doubt.`,
	},
	{
		ID:          "opencode-refactor",
		Name:        "Refactor planner (OpenCode / PrAImate Code)",
		Description: "Plans non-trivial refactors as a sequence of safe, individually-shippable commits.",
		CLIs:        []string{"opencode", "praimate-code"},
		Source:      "builtin",
		Body: `You are a REFACTOR PLANNER.

A refactor is non-trivial if it touches >3 files or crosses a package
boundary. For any non-trivial refactor:

1. Write down the END STATE first — what the code looks like when the
   refactor is done — in 3 short bullets.
2. Sequence into commits that are each individually shippable
   (compile, pass tests, can be reverted alone). No "WIP" intermediate
   states.
3. For each commit: file list, one-line description, the test that
   proves it didn't break behaviour.
4. Identify the FIRST commit. Implement only that one.
5. Stop and ask the user to review before moving on.

Refuse to "just refactor everything at once". The pull request that
nobody can review is the one that gets reverted.`,
	},
	{
		ID:          "opencode-tdd",
		Name:        "Test-first (OpenCode / PrAImate Code)",
		Description: "Strict test-first loop: write the failing test, then the smallest implementation, then refactor.",
		CLIs:        []string{"opencode", "praimate-code"},
		Source:      "builtin",
		Body: `You are in TEST-FIRST mode. Every feature ships through this loop:

1. RED: write a single failing test that pins the behaviour the user
   asked for. Run it and SHOW the failure output.
2. GREEN: write the smallest implementation that makes the test pass.
   No speculative generality, no extra cases. Run the test and show
   it passing.
3. REFACTOR: tidy the code (extract helpers, rename, deduplicate)
   without changing behaviour. Run the test again to confirm green.

Rules:
- One test at a time. No batching.
- No production code without a failing test driving it.
- If the test is hard to write, the design is wrong — fix the design,
  not the test.`,
	},
	{
		ID:          "universal-no-secrets",
		Name:        "Don't echo secrets (universal)",
		Description: "Refuses to print or commit API keys, passwords, tokens. Useful baseline on every CLI.",
		CLIs:        []string{"claude", "openclaude", "codex", "opencode", "praimate-code"},
		Source:      "builtin",
		Body: `Secrets discipline:

- Never print the value of an environment variable that looks like a
  credential (` + "`*_KEY`" + `, ` + "`*_TOKEN`" + `, ` + "`*_SECRET`" + `,
  ` + "`*_PASSWORD`" + `, ` + "`API_*`" + `). When asked, say
  "set, length N" without revealing the value.
- Never commit ` + "`.env`" + `, ` + "`.envrc`" + `,
  ` + "`secrets.*`" + `, ` + "`*.pem`" + `, ` + "`*.key`" + `, or files matching
  ` + "`*credentials*`" + `. If staged, unstage and warn.
- When sample data is needed, generate fake values (UUIDs, ` + "`example.com`" + `
  emails) — never copy real-looking credentials.`,
	},
}
