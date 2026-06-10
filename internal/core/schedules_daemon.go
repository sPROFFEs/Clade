package core

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"
)

// ScheduleDispatchOptions controls how a due schedule becomes a
// workflow run. The wall-clock daemon and tests both use this path.
type ScheduleDispatchOptions struct {
	// CLI is the fallback CLI for agent-scoped schedules. Chat-scoped
	// schedules prefer chats.cli_agent.
	CLI string

	// Cwd is the fallback workflow working directory. Chat-scoped
	// schedules prefer chats.workspace_path.
	Cwd string

	// Now controls due comparisons. Nil uses time.Now.
	Now func() time.Time

	// OnFire observes each attempted workflow run. It is called after
	// RunWorkflow returns and after the schedule row is advanced.
	OnFire func(ScheduleRun)
}

// ScheduleRun is the result of dispatching one due schedule.
type ScheduleRun struct {
	Fire   ScheduleFire
	Agent  Agent
	CLI    string
	Cwd    string
	Result *RunResult
}

// DispatchDueSchedules ticks schedules and runs every due workflow
// synchronously. It advances schedule state after each workflow attempt
// so an adapter failure does not spin the same row every daemon tick.
func (c *Core) DispatchDueSchedules(ctx context.Context, opts ScheduleDispatchOptions) ([]ScheduleRun, error) {
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	fires, err := c.TickSchedules(ctx, now())
	if err != nil {
		return nil, err
	}
	out := make([]ScheduleRun, 0, len(fires))
	for _, fire := range fires {
		run, err := c.runScheduleFire(ctx, fire, opts)
		if err != nil {
			return out, err
		}
		if err := c.MarkScheduleFired(ctx, fire.Schedule.ID, fire.FiredAt); err != nil {
			return out, err
		}
		out = append(out, run)
		if opts.OnFire != nil {
			opts.OnFire(run)
		}
	}
	return out, nil
}

func (c *Core) runScheduleFire(ctx context.Context, fire ScheduleFire, opts ScheduleDispatchOptions) (ScheduleRun, error) {
	agent, cli, cwd, err := c.resolveScheduleTarget(ctx, fire.Schedule, opts)
	if err != nil {
		return ScheduleRun{}, err
	}
	workflow := fire.Schedule.Workflow
	if workflow == "" {
		if wf := agent.ResolveDefaultWorkflow(); wf != nil {
			workflow = wf.Name
		}
	}
	inputs := scheduleInputs(fire)
	res := c.RunWorkflow(ctx, RunOptions{
		Agent:        agent,
		WorkflowName: workflow,
		Inputs:       inputs,
		CLI:          cli,
		Cwd:          cwd,
		Persist:      true,
		ChatTitle:    agent.Name + " · schedule",
	})
	return ScheduleRun{Fire: fire, Agent: *agent, CLI: cli, Cwd: cwd, Result: res}, nil
}

func (c *Core) resolveScheduleTarget(ctx context.Context, s Schedule, opts ScheduleDispatchOptions) (*Agent, string, string, error) {
	if s.AgentID != "" {
		agent, err := c.GetAgent(ctx, s.AgentID)
		if err != nil {
			return nil, "", "", err
		}
		cli := opts.CLI
		if cli == "" && len(agent.Supports) > 0 {
			cli = agent.Supports[0]
		}
		if cli == "" {
			return nil, "", "", fmt.Errorf("schedule %d: no CLI configured", s.ID)
		}
		cwd := opts.Cwd
		if cwd == "" {
			cwd, err = os.Getwd()
			if err != nil {
				cwd = "."
			}
		}
		return agent, cli, cwd, nil
	}
	if s.ChatID == "" {
		return nil, "", "", fmt.Errorf("schedule %d: AgentID or ChatID required", s.ID)
	}
	ch, err := c.GetChat(ctx, s.ChatID)
	if err != nil {
		return nil, "", "", err
	}
	if ch.AgentID == "" {
		return nil, "", "", fmt.Errorf("schedule %d chat %s has no agent_id", s.ID, ch.ID)
	}
	agent, err := c.GetAgent(ctx, ch.AgentID)
	if err != nil {
		return nil, "", "", err
	}
	cli := ch.CLIAgent
	if cli == "" {
		cli = opts.CLI
	}
	if cli == "" && len(agent.Supports) > 0 {
		cli = agent.Supports[0]
	}
	cwd := ch.WorkspacePath
	if cwd == "" {
		cwd = opts.Cwd
	}
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			cwd = "."
		}
	}
	return agent, cli, cwd, nil
}

func scheduleInputs(fire ScheduleFire) map[string]string {
	out := copyStringMap(fire.Schedule.Inputs)
	if out == nil {
		out = map[string]string{}
	}
	setDefault := func(k, v string) {
		if _, ok := out[k]; !ok {
			out[k] = v
		}
	}
	setDefault("schedule_id", fmt.Sprintf("%d", fire.Schedule.ID))
	setDefault("schedule_cron", fire.Schedule.Cron)
	setDefault("scheduled_at", fire.FiredAt.UTC().Format(time.RFC3339Nano))
	setDefault("workflow", fire.Schedule.Workflow)
	return out
}

// ScheduleDaemonOptions controls the wall-clock scheduler loop.
type ScheduleDaemonOptions struct {
	ScheduleDispatchOptions

	// Interval defaults to one minute.
	Interval time.Duration

	// OnError observes non-fatal daemon errors.
	OnError func(error)
}

// ScheduleDaemon is a running schedule loop.
type ScheduleDaemon struct {
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

// Stop asks the daemon to exit and waits for its goroutine to return.
func (d *ScheduleDaemon) Stop() {
	if d == nil {
		return
	}
	d.once.Do(d.cancel)
	<-d.done
}

// StartScheduleDaemon periodically dispatches due schedules into
// RunWorkflow. Setup cannot fail; per-tick errors are reported through
// OnError so the TUI keeps running.
func (c *Core) StartScheduleDaemon(ctx context.Context, opts ScheduleDaemonOptions) (*ScheduleDaemon, error) {
	if opts.Interval <= 0 {
		opts.Interval = time.Minute
	}
	child, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	d := &ScheduleDaemon{cancel: cancel, done: done}
	go c.runScheduleLoop(child, opts, done)
	return d, nil
}

func (c *Core) runScheduleLoop(ctx context.Context, opts ScheduleDaemonOptions, done chan<- struct{}) {
	defer close(done)
	dispatch := func() {
		if _, err := c.DispatchDueSchedules(ctx, opts.ScheduleDispatchOptions); err != nil && opts.OnError != nil {
			opts.OnError(err)
		}
	}
	dispatch()
	ticker := time.NewTicker(opts.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			dispatch()
		}
	}
}
