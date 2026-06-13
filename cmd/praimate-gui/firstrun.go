package main

// First-run setup — the GUI counterpart of the TUI's first-run flow:
// when no launcher config exists, the frontend shows a setup screen
// asking for the workspaces root, whether to seed the bundled sample
// templates, and (optionally) a git remote to clone an existing backup
// from. Completing it writes the same config.json the TUI reads and
// rebuilds Core on the new root, so the app continues without a
// restart.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sPROFFEs/PrAImate/internal/backup"
	"github.com/sPROFFEs/PrAImate/internal/core"
	"github.com/sPROFFEs/PrAImate/internal/launcher"
)

// FirstRunInfo tells the frontend whether to show setup and what to
// pre-fill.
type FirstRunInfo struct {
	Needed      bool   `json:"needed"`
	DefaultRoot string `json:"defaultRoot"`
}

// FirstRun reports whether the launcher config exists yet.
func (a *App) FirstRun() (*FirstRunInfo, error) {
	cfg, err := launcher.LoadConfig()
	if err != nil {
		return nil, err
	}
	home, _ := os.UserHomeDir()
	return &FirstRunInfo{
		Needed:      cfg == nil || cfg.WorkspacesRoot == "",
		DefaultRoot: filepath.Join(home, "praimate-workspaces"),
	}, nil
}

// CompleteFirstRun creates the workspaces root (or clones it from a
// backup remote), optionally seeds the bundled sample templates, saves
// the config and rebinds Core to the new root. Mirrors the TUI's three
// first-run options: empty root / root + samples / clone from remote.
func (a *App) CompleteFirstRun(root string, seedSamples bool, seedAgents bool, cloneURL string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return errors.New("a workspaces folder is required")
	}
	cloneURL = strings.TrimSpace(cloneURL)

	cfg := &launcher.Config{WorkspacesRoot: root}
	if cloneURL != "" {
		// Probe before cloning so a typo'd URL fails with a clear
		// message instead of a half-created root.
		ctx, cancel := context.WithTimeout(a.ctx, 60*time.Second)
		defer cancel()
		if _, err := backup.LsRemote(ctx, cloneURL); err != nil {
			return fmt.Errorf("can't reach the remote: %w", err)
		}
		if entries, err := os.ReadDir(root); err == nil && len(entries) > 0 {
			return fmt.Errorf("%s exists and is not empty — git clone needs a new or empty folder", root)
		}
		cctx, ccancel := context.WithTimeout(a.ctx, 10*time.Minute)
		defer ccancel()
		if err := backup.Clone(cctx, cloneURL, root); err != nil {
			return err
		}
		cfg.BackupEnabled = true
		cfg.BackupRemoteURL = cloneURL
	} else {
		for _, sub := range []string{"chats", "templates"} {
			if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
				return fmt.Errorf("create %s: %w", root, err)
			}
		}
		if seedSamples {
			exe, err := os.Executable()
			if err == nil {
				execDir := filepath.Dir(exe)
				// Best-effort: a bundle without samples just seeds nothing.
				_, _ = launcher.SeedSamples(root, launcher.SampleCandidates(execDir))
			}
		}
	}
	if err := launcher.SaveConfig(cfg); err != nil {
		return err
	}

	// Rebind Core to the new root so workspace chats and the backup tab
	// work immediately, no restart needed.
	if a.st != nil {
		if c, err := core.New(core.Options{Store: a.st, WorkspacesRoot: root}); err == nil {
			a.core = c
			c.SetApprovalProvider(a.approvalProvider)
			backup.SetStateSyncer(coreStateSyncer{core: c})
		}
	}

	// Opt-in: import the curated sample agents (reverse-ghidra,
	// code-review, dev-team, security-review, agent-builder). Best-effort
	// — a bundle without samples/agents/ just imports nothing. Skipped
	// when cloning a backup (that already carries the user's agents).
	if seedAgents && cloneURL == "" && a.core != nil {
		exe, err := os.Executable()
		if err == nil {
			dir := launcher.FirstExistingDir(launcher.SampleAgentCandidates(filepath.Dir(exe)))
			if dir != "" {
				ctx, cancel := context.WithTimeout(a.ctx, 60*time.Second)
				_, _ = a.core.SeedSampleAgents(ctx, dir)
				cancel()
			}
		}
	}
	return nil
}
