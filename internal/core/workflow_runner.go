package core

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// RunOptions parameterise one workflow execution.
type RunOptions struct {
	// Agent is the agent whose workflow should run. Required.
	Agent *Agent

	// WorkflowName, if empty, uses agent.ResolveDefaultWorkflow().
	WorkflowName string

	// Inputs maps WorkflowInput.Name → user-supplied value.
	Inputs map[string]string

	// CLI is the third-party CLI to drive (claude, codex, ...).
	// Must appear in agent.Supports or the run is refused.
	CLI string

	// Cwd is the working directory the CLI sees.
	Cwd string

	// Env is per-launch environment overrides (e.g. local-LLM routing).
	Env map[string]string

	// OnTurn, if non-nil, is invoked with each completed turn as it
	// arrives. Lets a TUI/GUI stream the conversation without waiting
	// for the whole workflow to finish.
	OnTurn func(turn TurnResult)

	// Persist toggles DB-backed chat persistence. When true the runner
	// creates a chats row at start, writes one messages row per turn
	// (user + assistant), and calls EndChat at completion with the
	// outcome mapped to chats.exit_kind.
	//
	// Persistence is silently skipped when Core has no store (e.g.
	// in legacy launcher-only configurations).
	Persist bool

	// ChatTitle, if Persist is true, becomes chats.title. Falls back
	// to the agent + workflow name.
	ChatTitle string

	// MemoryInjection, if non-empty, is prepended to the first
	// rendered user_message body. The retrieval planner builds this
	// via Core.BuildMemoryInjection before calling RunWorkflow.
	// Empty = no injection.
	MemoryInjection string
}

// TurnResult records one round of (user message, assistant reply).
type TurnResult struct {
	Index      int
	UserMsg    string
	Reply      *Reply
	DurationMs int64
}

// RunResult is the terminal outcome of a workflow execution.
type RunResult struct {
	AgentID      string
	WorkflowName string
	Turns        []TurnResult
	SessionID    string // last seen session id from the adapter
	Outcome      RunOutcome
	Err          error

	// ChatID is non-empty when Persist was set and a chat row was
	// successfully created. Callers can pass it to DistillChat() to
	// trigger memory distillation against the stored turns.
	ChatID string
}

// RunOutcome enumerates how a workflow can end. Mirrors the exit
// taxonomy from plan §3 / Osaurus's exit_kind.
type RunOutcome string

const (
	OutcomeCompleted   RunOutcome = "completed"
	OutcomeCancelled   RunOutcome = "cancelled"
	OutcomeAgentFailed RunOutcome = "agent_failed"
	OutcomeAdapterErr  RunOutcome = "adapter_error"
)

// RunWorkflow executes opts.Workflow against opts.CLI using the
// option-C hybrid strategy:
//
//   - Step 0 always runs via adapter.SingleShot.
//   - Subsequent user_message steps use adapter.Resume if the adapter
//     supports it (cheaper: keeps state inside the CLI), or fall back
//     to SingleShot per turn.
//
// wait_for_assistant steps are no-ops in this implementation — the
// reply is already consumed by the matching user_message. They exist
// in the YAML so future executors can pause on specific tool calls
// (UntilTool) without changing the format.
func (c *Core) RunWorkflow(ctx context.Context, opts RunOptions) *RunResult {
	res := &RunResult{Outcome: OutcomeAdapterErr}

	if err := validateRunOptions(opts); err != nil {
		res.Err = err
		return res
	}

	workflow := opts.Agent.FindWorkflow(opts.WorkflowName)
	if workflow == nil {
		workflow = opts.Agent.ResolveDefaultWorkflow()
		if workflow == nil {
			res.Err = fmt.Errorf("agent %q: no workflow named %q and no default", opts.Agent.ID, opts.WorkflowName)
			return res
		}
	}
	res.AgentID = opts.Agent.ID
	res.WorkflowName = workflow.Name

	if !contains(opts.Agent.Supports, opts.CLI) {
		res.Err = fmt.Errorf("agent %q does not support CLI %q (supports: %v)",
			opts.Agent.ID, opts.CLI, opts.Agent.Supports)
		return res
	}

	adapter, err := GetCLIAdapter(opts.CLI)
	if err != nil {
		res.Err = err
		return res
	}
	if mcpEnv, err := c.prepareMCPForRun(ctx, opts.Agent, opts.CLI, opts.Cwd); err != nil {
		res.Err = err
		return res
	} else if len(mcpEnv) > 0 {
		opts.Env = mergeStringMaps(opts.Env, mcpEnv)
	}

	rendered, err := RenderWorkflow(opts.Agent, workflow, opts.Inputs)
	if err != nil {
		res.Err = err
		return res
	}
	privacy := c.PrivacyScanner().NewRedactionSession()
	systemPrompt, _ := privacy.Redact(AgentSystemPrompt(opts.Agent))

	// Optional DB-backed chat persistence. Failures here are logged
	// via res.Err only if no other path sets it later; they do NOT
	// abort the run — a user shouldn't lose a workflow because the
	// DB hiccupped.
	chatID := c.maybeCreateChat(ctx, opts)
	res.ChatID = chatID

	turnIdx := 0
	var lastReply *Reply
	for _, step := range rendered.Steps {
		if step.Kind != StepUserMessage {
			continue
		}
		if err := ctx.Err(); err != nil {
			res.Outcome = OutcomeCancelled
			res.Err = err
			c.maybeEndChat(ctx, chatID, res.Outcome)
			return res
		}

		body := step.Body
		// Memory injection rides on the FIRST rendered user_message.
		// Subsequent turns already have model context, and prepending
		// the same injection block per turn would burn tokens.
		if turnIdx == 0 && opts.MemoryInjection != "" {
			body = opts.MemoryInjection + "\n\n" + body
		}

		adapterBody, _ := privacy.Redact(body)
		c.maybeAddMessage(ctx, chatID, "user", body)

		start := time.Now()
		var reply *Reply
		var runErr error
		if turnIdx == 0 || !adapter.SupportsResume() || lastReply == nil || lastReply.SessionID == "" {
			reply, runErr = adapter.SingleShot(ctx, SingleShotOpts{
				Cwd:          opts.Cwd,
				Message:      adapterBody,
				SystemPrompt: systemPrompt,
				Env:          opts.Env,
			})
		} else {
			reply, runErr = adapter.Resume(ctx, lastReply.SessionID, ResumeOpts{Message: adapterBody})
		}
		if runErr != nil {
			res.Outcome = OutcomeAdapterErr
			res.Err = runErr
			c.maybeEndChat(ctx, chatID, res.Outcome)
			return res
		}
		reply = revealReply(privacy, reply)
		if reply.ExitCode != 0 {
			res.Outcome = OutcomeAgentFailed
			res.Err = fmt.Errorf("%s exited with code %d", adapter.Name(), reply.ExitCode)
			res.Turns = append(res.Turns, TurnResult{
				Index:      turnIdx,
				UserMsg:    body,
				Reply:      reply,
				DurationMs: time.Since(start).Milliseconds(),
			})
			c.maybeAddMessage(ctx, chatID, "assistant", reply.Text)
			c.maybeEndChat(ctx, chatID, res.Outcome)
			return res
		}

		c.maybeAddMessage(ctx, chatID, "assistant", reply.Text)

		turn := TurnResult{
			Index:      turnIdx,
			UserMsg:    body,
			Reply:      reply,
			DurationMs: time.Since(start).Milliseconds(),
		}
		res.Turns = append(res.Turns, turn)
		if opts.OnTurn != nil {
			opts.OnTurn(turn)
		}
		lastReply = reply
		turnIdx++
	}
	if lastReply != nil {
		res.SessionID = lastReply.SessionID
	}
	// Stamp the session id so a workflow-started chat can be continued
	// interactively (ContinueChat) afterwards.
	if chatID != "" && res.SessionID != "" {
		_ = c.SetChatSessionID(ctx, chatID, res.SessionID)
	}
	res.Outcome = OutcomeCompleted
	c.maybeEndChat(ctx, chatID, res.Outcome)
	return res
}

func revealReply(redaction *PrivacyRedaction, reply *Reply) *Reply {
	if reply == nil {
		return nil
	}
	out := *reply
	out.Text = redaction.Reveal(reply.Text)
	return &out
}

// maybeCreateChat is a no-op when persistence is off, store is nil, or
// the CreateChat call errors. It deliberately swallows DB errors so
// they can't fail a workflow — the worst case is a non-persisted run.
func (c *Core) maybeCreateChat(ctx context.Context, opts RunOptions) string {
	if !opts.Persist || c.store == nil {
		return ""
	}
	title := opts.ChatTitle
	if title == "" {
		title = opts.Agent.Name + " · " + opts.WorkflowName
	}
	ch, err := c.CreateChat(ctx, CreateChatRequest{
		Title:    title,
		AgentID:  opts.Agent.ID,
		CLIAgent: opts.CLI,
	})
	if err != nil {
		return ""
	}
	return ch.ID
}

func (c *Core) maybeAddMessage(ctx context.Context, chatID, role, content string) {
	if chatID == "" || c.store == nil {
		return
	}
	_, _ = c.AddMessage(ctx, chatID, role, content, nil)
}

func (c *Core) maybeEndChat(ctx context.Context, chatID string, outcome RunOutcome) {
	if chatID == "" || c.store == nil {
		return
	}
	_ = c.EndChat(ctx, chatID, string(outcome))
}

func validateRunOptions(opts RunOptions) error {
	if opts.Agent == nil {
		return errors.New("RunWorkflow: nil agent")
	}
	if opts.CLI == "" {
		return errors.New("RunWorkflow: empty CLI")
	}
	if opts.Cwd == "" {
		return errors.New("RunWorkflow: empty Cwd")
	}
	return nil
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func mergeStringMaps(a, b map[string]string) map[string]string {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	out := make(map[string]string, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
