package core

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatcherDispatchOptions controls how a matched watcher becomes a
// workflow run. The fsnotify daemon and tests both use this path.
type WatcherDispatchOptions struct {
	// CLI is the fallback CLI for agent-scoped watchers. Chat-scoped
	// watchers prefer chats.cli_agent.
	CLI string

	// Cwd is the fallback workflow working directory. Chat-scoped
	// watchers prefer chats.workspace_path; otherwise watcher.Path is
	// used.
	Cwd string

	// Now controls debounce comparisons. Nil uses time.Now.
	Now func() time.Time

	// OnFire observes each completed workflow run. It is called after
	// RunWorkflow returns.
	OnFire func(WatcherRun)
}

// WatcherRun is the result of dispatching one matching watcher.
type WatcherRun struct {
	Fire   WatcherFire
	Agent  Agent
	CLI    string
	Cwd    string
	Result *RunResult
}

// DispatchWatcherEvent matches one filesystem event and runs every
// workflow that should fire. It is synchronous by design; callers that
// want background dispatch should call it from their own goroutine.
func (c *Core) DispatchWatcherEvent(ctx context.Context, event WatcherEvent, opts WatcherDispatchOptions) ([]WatcherRun, error) {
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	fires, err := c.MatchWatchers(ctx, event, now())
	if err != nil {
		return nil, err
	}
	out := make([]WatcherRun, 0, len(fires))
	for _, fire := range fires {
		run, err := c.runWatcherFire(ctx, fire, opts)
		if err != nil {
			return out, err
		}
		out = append(out, run)
		if opts.OnFire != nil {
			opts.OnFire(run)
		}
	}
	return out, nil
}

func (c *Core) runWatcherFire(ctx context.Context, fire WatcherFire, opts WatcherDispatchOptions) (WatcherRun, error) {
	agent, cli, cwd, err := c.resolveWatcherTarget(ctx, fire.Watcher, opts)
	if err != nil {
		return WatcherRun{}, err
	}
	workflow := fire.Watcher.Workflow
	if workflow == "" {
		if wf := agent.ResolveDefaultWorkflow(); wf != nil {
			workflow = wf.Name
		}
	}
	inputs := watcherInputs(fire)
	res := c.RunWorkflow(ctx, RunOptions{
		Agent:        agent,
		WorkflowName: workflow,
		Inputs:       inputs,
		CLI:          cli,
		Cwd:          cwd,
		Persist:      true,
		ChatTitle:    agent.Name + " · watcher",
	})
	return WatcherRun{Fire: fire, Agent: *agent, CLI: cli, Cwd: cwd, Result: res}, nil
}

func (c *Core) resolveWatcherTarget(ctx context.Context, w Watcher, opts WatcherDispatchOptions) (*Agent, string, string, error) {
	if w.AgentID != "" {
		agent, err := c.GetAgent(ctx, w.AgentID)
		if err != nil {
			return nil, "", "", err
		}
		cli := opts.CLI
		if cli == "" && len(agent.Supports) > 0 {
			cli = agent.Supports[0]
		}
		if cli == "" {
			return nil, "", "", fmt.Errorf("watcher %d: no CLI configured", w.ID)
		}
		cwd := opts.Cwd
		if cwd == "" {
			cwd = w.Path
		}
		return agent, cli, cwd, nil
	}
	if w.ChatID == "" {
		return nil, "", "", fmt.Errorf("watcher %d: AgentID or ChatID required", w.ID)
	}
	ch, err := c.GetChat(ctx, w.ChatID)
	if err != nil {
		return nil, "", "", err
	}
	if ch.AgentID == "" {
		return nil, "", "", fmt.Errorf("watcher %d chat %s has no agent_id", w.ID, ch.ID)
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
		cwd = w.Path
	}
	return agent, cli, cwd, nil
}

func watcherInputs(fire WatcherFire) map[string]string {
	out := copyStringMap(fire.Watcher.Inputs)
	if out == nil {
		out = map[string]string{}
	}
	setDefault := func(k, v string) {
		if _, ok := out[k]; !ok {
			out[k] = v
		}
	}
	setDefault("event_path", fire.Event.Path)
	setDefault("event_op", fire.Event.Op)
	setDefault("watch_path", fire.Watcher.Path)
	setDefault("workflow", fire.Watcher.Workflow)
	return out
}

// WatcherDaemonOptions controls the fsnotify-backed watcher loop.
type WatcherDaemonOptions struct {
	WatcherDispatchOptions

	// OnError observes non-fatal daemon errors. Fatal setup errors are
	// returned by StartWatcherDaemon.
	OnError func(error)
}

// WatcherDaemon is a running fsnotify loop.
type WatcherDaemon struct {
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

// Stop asks the daemon to exit and waits for its goroutine to return.
func (d *WatcherDaemon) Stop() {
	if d == nil {
		return
	}
	d.once.Do(d.cancel)
	<-d.done
}

// StartWatcherDaemon watches every enabled watcher path and dispatches
// matching filesystem events into RunWorkflow.
func (c *Core) StartWatcherDaemon(ctx context.Context, opts WatcherDaemonOptions) (*WatcherDaemon, error) {
	paths, err := c.enabledWatcherPaths(ctx)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		child, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			<-child.Done()
		}()
		return &WatcherDaemon{cancel: cancel, done: done}, nil
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	added := 0
	for _, path := range paths {
		if err := w.Add(path); err != nil {
			if opts.OnError != nil {
				opts.OnError(fmt.Errorf("watch %s: %w", path, err))
			}
			continue
		}
		added++
	}
	if added == 0 {
		_ = w.Close()
		child, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			<-child.Done()
		}()
		return &WatcherDaemon{cancel: cancel, done: done}, nil
	}
	child, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	d := &WatcherDaemon{cancel: cancel, done: done}
	go c.runWatcherLoop(child, w, opts, done)
	return d, nil
}

func (c *Core) runWatcherLoop(ctx context.Context, w *fsnotify.Watcher, opts WatcherDaemonOptions, done chan<- struct{}) {
	defer close(done)
	defer w.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			if opts.OnError != nil {
				opts.OnError(err)
			}
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			wev, ok := watcherEventFromFSNotify(ev)
			if !ok {
				continue
			}
			if _, err := c.DispatchWatcherEvent(ctx, wev, opts.WatcherDispatchOptions); err != nil && opts.OnError != nil {
				opts.OnError(err)
			}
		}
	}
}

func (c *Core) enabledWatcherPaths(ctx context.Context) ([]string, error) {
	watchers, err := c.ListWatchers(ctx, true)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, w := range watchers {
		if w.Path == "" {
			continue
		}
		path := filepath.Clean(w.Path)
		if path == "." || path == "" {
			continue
		}
		seen[path] = true
	}
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sortStrings(paths)
	return paths, nil
}

func watcherEventFromFSNotify(ev fsnotify.Event) (WatcherEvent, bool) {
	if ev.Name == "" {
		return WatcherEvent{}, false
	}
	switch {
	case ev.Has(fsnotify.Create):
		return WatcherEvent{Path: ev.Name, Op: "create"}, true
	case ev.Has(fsnotify.Write):
		return WatcherEvent{Path: ev.Name, Op: "write"}, true
	case ev.Has(fsnotify.Remove):
		return WatcherEvent{Path: ev.Name, Op: "remove"}, true
	case ev.Has(fsnotify.Rename):
		return WatcherEvent{Path: ev.Name, Op: "rename"}, true
	case ev.Has(fsnotify.Chmod):
		return WatcherEvent{Path: ev.Name, Op: "chmod"}, true
	}
	return WatcherEvent{}, false
}

func sortStrings(xs []string) {
	if len(xs) < 2 {
		return
	}
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && strings.Compare(xs[j-1], xs[j]) > 0; j-- {
			xs[j-1], xs[j] = xs[j], xs[j-1]
		}
	}
}
