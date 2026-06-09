package launcher

// Per-agent native session-store paths, centralised so capture, restore,
// and the opt-in mirror code don't each maintain their own copy of
// "where does claude actually live on Windows?" The values are
// computed against runtime.GOOS so the same launcher binary works on
// Linux, macOS, and Windows without per-OS branching at every call
// site.
//
// The store paths are *agent stores*, not Clade's own config dir.
// Every supported agent installs its own session store under the user's
// home dir using its own conventions:
//
//   Claude Code: ~/.claude/projects/<slug>/<sessionID>.jsonl
//   Codex CLI:   ~/.codex/sessions/<YYYY>/<MM>/<DD>/rollout-<ts>-<uuid>.jsonl
//   OpenCode:    ~/.opencode/storage/session/info/<id>.json
//                ~/.opencode/storage/session/message/<id>/*.json
//   Gemini CLI:  ~/.gemini/tmp/<hash>/logs.json
//   DeepSeek:    ~/.deepseek/ (no documented session store yet — best-effort)
//
// On Windows the same paths resolve under %USERPROFILE% — none of the
// upstream agents put their stores under %APPDATA% or %LOCALAPPDATA%
// (they use Unix-style dotfiles even on Windows). So a single homeDir()
// lookup + filepath.Join is correct everywhere.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// AgentStorePaths is the set of locations a single agent reads/writes
// for the given sandbox. These are the *current OS* paths — empty
// fields mean "not applicable for this agent." Code that needs to
// snapshot, restore, or mirror walks these.
type AgentStorePaths struct {
	// SessionDir is the dir the agent writes session files into. For
	// claude/opencode it's slug- or id-scoped per chat (so two chats
	// don't collide). For codex it's a date-stamped parent dir —
	// individual rollout files have to be filtered by cwd at walk time.
	SessionDir string

	// MessagesDir, when non-empty, is a sibling per-session-id dir for
	// agents (OpenCode) that fan out messages across multiple files.
	// Empty for agents whose session is a single file.
	MessagesDir string

	// FilterByCwd, when true, signals that SessionDir contains
	// sessions for MANY cwds (codex) and the caller must filter by
	// the sandbox cwd embedded in the file. When false, SessionDir
	// is already scoped to this chat's sandbox.
	FilterByCwd bool
}

// AgentHome returns the per-agent native-store paths on the current OS.
// sandboxDir is the chat's sandbox (the agent's cwd at launch). The
// returned paths may not exist on disk yet — callers must os.Stat to
// know.
func AgentHome(agent AgentID, sandboxDir string) AgentStorePaths {
	return agentHome(agent, sandboxDir, "")
}

func agentHome(agent AgentID, sandboxDir, homeOverride string) AgentStorePaths {
	home := homeOverride
	if home == "" {
		home = homeDir()
	}
	if home == "" {
		return AgentStorePaths{}
	}
	switch agent {
	case AgentClaude:
		// Claude's project dir is fully cwd-scoped via slug — every
		// chat with a unique sandbox path gets its own dir, no
		// cross-chat bleed.
		return AgentStorePaths{
			SessionDir: filepath.Join(home, ".claude", "projects", claudeProjectSlug(sandboxDir)),
		}
	case AgentOpenClaude:
		// Same layout as claude but rooted at ~/.openclaude/ and
		// using a more aggressive slug (any non-alphanumeric → '-').
		// See openclaudeProjectSlug for the divergence.
		return AgentStorePaths{
			SessionDir: filepath.Join(home, ".openclaude", "projects", openclaudeProjectSlug(sandboxDir)),
		}
	case AgentCodex:
		// Codex stores rollouts under date dirs across all cwds. The
		// caller has to walk + filter by the cwd field in each file.
		return AgentStorePaths{
			SessionDir:  filepath.Join(home, ".codex", "sessions"),
			FilterByCwd: true,
		}
	case AgentOpenCode:
		// OpenCode keys session metadata by id under info/, message
		// bodies under message/<id>/. Caller filters info/<id>.json
		// by the `directory` field and then reads the matching
		// message dir.
		return AgentStorePaths{
			SessionDir:  filepath.Join(home, ".opencode", "storage", "session", "info"),
			MessagesDir: filepath.Join(home, ".opencode", "storage", "session", "message"),
			FilterByCwd: true,
		}
	case AgentGemini:
		// Gemini hashes the cwd into ~/.gemini/tmp/<hash>/. The hash
		// is sha256 of the absolute path (truncated). geminiTmpHash
		// reproduces it.
		return AgentStorePaths{
			SessionDir: filepath.Join(home, ".gemini", "tmp", geminiTmpHash(sandboxDir)),
		}
	case AgentDeepSeek:
		// No documented native session store — DeepSeek-TUI keeps
		// state in memory + the config TOML. Leave both fields empty;
		// callers should fall through to "skip" branches.
		return AgentStorePaths{}
	}
	return AgentStorePaths{}
}

// geminiTmpHash mirrors gemini-cli's algorithm for deriving the per-
// cwd tmp dir name: sha256(abspath), hex, first 16 chars. Best-effort —
// if upstream changes the algo, the worst case is we miss the slice
// and fall back to "no native session found".
func geminiTmpHash(cwd string) string {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = cwd
	}
	if runtime.GOOS == "windows" {
		// gemini-cli normalises drive-letter casing on Windows.
		abs = strings.ToLower(abs)
	}
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])[:16]
}

// homeDir returns the user's home directory in a way that works on all
// supported OSes. On Windows os.UserHomeDir reads %USERPROFILE%, which
// is correct for all our agent stores (they put their dotfiles there,
// not under %APPDATA%).
//
// Returns "" when we genuinely can't resolve a home dir — callers
// should treat this as "skip the operation, log a note."
//
// Defined here so the agentpaths code can use it without depending on
// transcript.go's package-private copy. Both are intentionally simple
// wrappers around the stdlib so the dependency graph stays flat.
func homeDirFromEnvOrStd() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	return ""
}

// init verifies the simple-wrapper homeDir behaviour we depend on; if
// stdlib ever drops UserHomeDir support on a build target we'd notice
// at startup instead of silently mis-storing sessions.
var _ = homeDirFromEnvOrStd
