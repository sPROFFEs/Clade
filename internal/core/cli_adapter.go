package core

import (
	"context"
	"fmt"
	"sync"
)

// SingleShotOpts groups everything a CLI adapter needs to run one
// non-interactive turn. Adapters are free to ignore fields that don't
// apply to their CLI.
type SingleShotOpts struct {
	// Cwd is the working directory the CLI should treat as the project
	// root. Required.
	Cwd string

	// Message is the user turn — typically a rendered workflow step
	// body. Adapters pass this through stdin or as a CLI argument
	// depending on what their CLI supports.
	Message string

	// SystemPrompt, if non-empty, is the agent's `instructions` field.
	// Some CLIs accept it as a flag; others as a stdin preamble; some
	// ignore it. Adapters document their behavior in their package
	// doc comment.
	SystemPrompt string

	// Env extends the parent process environment for the child run.
	// Used for per-launch routing (e.g. local-LLM endpoint overrides).
	Env map[string]string
}

// Reply is what a CLI adapter returns after one turn. Fields beyond
// Text are advisory — empty values are fine.
type Reply struct {
	// Text is the assistant's complete reply, trimmed of trailing
	// whitespace. Required.
	Text string

	// SessionID, if non-empty, is an opaque handle the same adapter can
	// pass to Resume() to continue the conversation. Empty when the CLI
	// has no session concept.
	SessionID string

	// ExitCode of the child process. 0 is success; non-zero means the
	// CLI itself errored (separate from a successful CLI run that
	// returned an angry assistant reply).
	ExitCode int
}

// CLIAdapter is what the workflow runner talks to. One implementation
// per supported third-party CLI (claude, codex, opencode, ...).
//
// Adapters must be safe for concurrent use across goroutines — the
// workflow runner may invoke Cancel() from one goroutine while a
// turn is in-flight on another.
type CLIAdapter interface {
	// Name returns the CLI identifier this adapter handles. Matches
	// the `supports:` strings in agent YAML.
	Name() string

	// Available returns nil if the adapter's CLI is installed and
	// invocable, or an error explaining what's missing.
	Available(ctx context.Context) error

	// SingleShot runs one non-interactive turn and returns the
	// assistant reply.
	SingleShot(ctx context.Context, opts SingleShotOpts) (*Reply, error)

	// SupportsResume reports whether Resume() is implemented. The
	// runner uses this to pick between option-C paths.
	SupportsResume() bool

	// Resume continues a session started by a prior SingleShot/Resume
	// call. Returns an error if SupportsResume() is false.
	Resume(ctx context.Context, sessionID, message string) (*Reply, error)
}

// adapterRegistry holds the live set of adapters keyed by Name(). Use
// RegisterCLIAdapter / GetCLIAdapter to mutate it.
var (
	adapterMu       sync.RWMutex
	adapterRegistry = map[string]CLIAdapter{}
)

// RegisterCLIAdapter installs a in the global registry under a.Name().
// A second register for the same name replaces the first; this lets
// tests swap a real adapter for a fake without unregistering.
func RegisterCLIAdapter(a CLIAdapter) {
	adapterMu.Lock()
	defer adapterMu.Unlock()
	adapterRegistry[a.Name()] = a
}

// UnregisterCLIAdapter removes the named adapter. Used by tests; the
// production path never calls this.
func UnregisterCLIAdapter(name string) {
	adapterMu.Lock()
	defer adapterMu.Unlock()
	delete(adapterRegistry, name)
}

// GetCLIAdapter returns the adapter for name, or an error listing the
// known names so the user knows what to install.
func GetCLIAdapter(name string) (CLIAdapter, error) {
	adapterMu.RLock()
	defer adapterMu.RUnlock()
	a, ok := adapterRegistry[name]
	if !ok {
		known := make([]string, 0, len(adapterRegistry))
		for k := range adapterRegistry {
			known = append(known, k)
		}
		return nil, fmt.Errorf("no CLI adapter registered for %q (known: %v)", name, known)
	}
	return a, nil
}
