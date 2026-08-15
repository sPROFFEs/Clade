package main

import (
	"fmt"
	"unicode/utf8"

	"git.jtsec.local/lab/PrAImate/internal/core"
)

func (a *App) ListManagedRuns(agentID string) ([]core.ManagedRun, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	return c.ListManagedRuns(agentID)
}

func (a *App) ManagedRunDetails(runID string) (*core.ManagedRun, error) {
	c, err := a.requireCore()
	if err != nil {
		return nil, err
	}
	return c.GetManagedRun(runID)
}

// ManagedArtifactText returns an artifact for inspection in the GUI. The 1.4
// broker creates text artifacts only; rejecting non-UTF-8 here keeps binary
// data out of the Wails JSON bridge if future brokers add screenshots/files.
func (a *App) ManagedArtifactText(runID, name string) (string, error) {
	c, err := a.requireCore()
	if err != nil {
		return "", err
	}
	body, err := c.ReadManagedArtifact(runID, name)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(body) {
		return "", fmt.Errorf("artifact %q is binary and cannot be previewed as text", name)
	}
	return string(body), nil
}
