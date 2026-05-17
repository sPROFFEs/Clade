package launcher

// OpenChat is the high-level "resume this chat" call the TUI uses when
// the user hits Enter on the chat list. It:
//
//   1. Re-detects agents (so a newly installed one shows up without
//      restarting the launcher).
//   2. Looks up the chat's locked agent.
//   3. If unavailable, returns ErrAgentUnavailable so the TUI can route
//      to the install screen instead of crashing on a missing binary.
//   4. Otherwise builds the LaunchPlan (compile + decorate) — caller
//      runs it after Bubble Tea releases the TTY.

import (
	"context"
	"errors"
	"fmt"
)

// ErrAgentUnavailable is returned by OpenChat when the chat's locked
// agent isn't installed on this machine.
var ErrAgentUnavailable = errors.New("chat's locked agent isn't available on this machine")

// OpenChat returns a LaunchPlan ready to execute. The Agent returned
// alongside is the resolved one (with Available/Version populated) so
// the TUI can render the post-launch summary correctly.
func OpenChat(c Chat) (LaunchPlan, Agent, error) {
	if c.AgentID == "" {
		return LaunchPlan{}, Agent{}, fmt.Errorf("chat %q has no locked agent", c.Label)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agents := DetectAgents(ctx)

	var picked Agent
	for _, a := range agents {
		if a.ID == c.AgentID {
			picked = a
			break
		}
	}
	if picked.ID == "" {
		return LaunchPlan{}, Agent{}, fmt.Errorf("chat references unknown agent %q", c.AgentID)
	}
	if !picked.Available {
		return LaunchPlan{}, picked, ErrAgentUnavailable
	}

	plan, err := Plan(c.AsWorkspace(), picked)
	if err != nil {
		return LaunchPlan{}, picked, err
	}
	return plan, picked, nil
}
