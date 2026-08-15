package core

// Managed single-agent runtime integration. The runtime engine is a leaf
// package; this file adapts PrAImate's existing CLI, routing, privacy, and data
// root boundaries without changing the native workflow runner.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"git.jtsec.local/lab/PrAImate/internal/agentic"
	"git.jtsec.local/lab/PrAImate/internal/appdata"
)

type ManagedRunEvent struct {
	RunID   string         `json:"runId"`
	AgentID string         `json:"agentId"`
	Type    string         `json:"type"`
	Turn    int            `json:"turn,omitempty"`
	Tool    string         `json:"tool,omitempty"`
	Detail  string         `json:"detail,omitempty"`
	OK      bool           `json:"ok,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`
}

type ManagedRunRequest struct {
	Surface      ExecutionSurface
	Agent        *Agent
	CLI          string
	Cwd          string
	Model        string
	Local        *ChatLocalEndpoint
	Task         string
	Instructions string
	Env          map[string]string
	OnEvent      func(ManagedRunEvent)
}

type ManagedRun struct {
	ID          string                 `json:"id"`
	AgentID     string                 `json:"agentId"`
	AgentName   string                 `json:"agentName"`
	State       string                 `json:"state"`
	Turns       int                    `json:"turns"`
	SessionID   string                 `json:"sessionId,omitempty"`
	StartedAt   string                 `json:"startedAt"`
	UpdatedAt   string                 `json:"updatedAt"`
	CompletedAt string                 `json:"completedAt,omitempty"`
	Error       string                 `json:"error,omitempty"`
	Final       string                 `json:"final,omitempty"`
	Artifacts   []ManagedRunArtifact   `json:"artifacts"`
	Memory      []ManagedRunMemoryItem `json:"memory,omitempty"`
}

type ManagedRunArtifact struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	CreatedAt string `json:"createdAt"`
}

type ManagedRunMemoryItem struct {
	Kind      string `json:"kind"`
	Title     string `json:"title,omitempty"`
	Content   string `json:"content"`
	Status    string `json:"status,omitempty"`
	CreatedAt string `json:"createdAt"`
}

func (c *Core) RunManagedAgent(ctx context.Context, req ManagedRunRequest) (*ManagedRun, error) {
	if req.Agent == nil {
		return nil, errors.New("managed run requires an agent")
	}
	effectiveAgent, err := c.ResolveEffectiveAgentConfig(ctx, req.Agent)
	if err != nil {
		return nil, err
	}
	if effectiveAgent.Mode != RuntimeAgentic {
		return nil, fmt.Errorf("agent %q uses the native runtime", req.Agent.Name)
	}
	if !effectiveAgent.AgenticCompatible {
		return nil, fmt.Errorf("agent %q requires unavailable managed features: %s", req.Agent.Name, strings.Join(effectiveAgent.UnsupportedFeatures, ", "))
	}
	if !contains(req.Agent.Supports, req.CLI) {
		return nil, fmt.Errorf("agent %q does not support CLI %q", req.Agent.ID, req.CLI)
	}
	gate := string(req.Surface)
	if req.Surface == SurfaceStudio {
		gate = "editor"
	}
	if (gate == "chat" || gate == "terminal" || gate == "editor") && !req.Agent.AllowsSurface(gate) {
		return nil, fmt.Errorf("agent %q is not allowed on the %s surface", req.Agent.Name, req.Surface)
	}
	if req.Surface == SurfaceTerminal {
		return nil, errors.New("managed Autonomous runs are unavailable in the interactive Terminal surface")
	}
	if req.Agent.Knowledge == "rag" {
		return nil, errors.New("managed Autonomous runs cannot query Graphify until the policy-aware knowledge broker is available; use Raw documents or a native runtime preset")
	}
	if len(req.Agent.MCPServers) > 0 {
		return nil, errors.New("managed Autonomous runs cannot expose MCP servers until the policy-aware MCP broker is available")
	}
	adapter, err := GetCLIAdapter(strings.TrimSpace(req.CLI))
	if err != nil {
		return nil, err
	}
	if !supportsManagedSafeMode(adapter) {
		return nil, fmt.Errorf("CLI %q cannot enforce managed safe mode and is blocked for Autonomous runs", req.CLI)
	}
	if err := adapter.Available(ctx); err != nil {
		return nil, err
	}
	// Resolve routing without passing the agent: ResolveExecutionConfig is the
	// native-run contract and intentionally rejects agentic manifests. Managed
	// tools are brokered by internal/agentic; the underlying CLI is always safe.
	execution, err := c.ResolveExecutionConfig(ctx, ExecutionRequest{
		Surface: req.Surface, CLI: req.CLI, Cwd: req.Cwd,
		Model: req.Model, Tools: "", Local: req.Local,
	})
	if err != nil {
		return nil, err
	}
	if err := c.PrepareExecution(ctx, execution); err != nil {
		return nil, err
	}
	root, err := managedRunsRoot()
	if err != nil {
		return nil, err
	}
	model := &managedCLIModel{
		adapter: adapter, cwd: execution.Cwd, model: execution.Model,
		env: mergeStringMaps(req.Env, execution.Env),
	}
	manifest := effectiveAgent.Manifest
	limits := agentic.Limits{}
	if manifest != nil {
		limits.MaxTurns = manifest.Limits.MaxTurns
		limits.MaxContextChars = manifest.Limits.MaxContextChars
		limits.MaxOutputChars = manifest.Limits.MaxOutputChars
	}
	instructions := strings.TrimSpace(req.Instructions)
	if instructions == "" {
		instructions = AgentSystemPrompt(req.Agent)
	}
	result, runErr := agentic.Run(ctx, agentic.Config{
		RootDir: root, AgentID: req.Agent.ID, AgentName: req.Agent.Name,
		Instructions: instructions, Task: req.Task, Limits: limits, Model: model,
		OnEvent: func(ev agentic.Event) {
			if req.OnEvent != nil {
				req.OnEvent(ManagedRunEvent{
					RunID: ev.RunID, AgentID: ev.AgentID, Type: ev.Type, Turn: ev.Turn,
					Tool: ev.Tool, Detail: ev.Detail, OK: ev.OK, Payload: ev.Payload,
				})
			}
		},
	})
	if result == nil {
		return nil, runErr
	}
	managed := managedRunFromResult(result)
	return managed, runErr
}

type managedCLIModel struct {
	adapter   CLIAdapter
	cwd       string
	model     string
	env       map[string]string
	sessionID string
}

func (m *managedCLIModel) Turn(ctx context.Context, input agentic.ModelInput, emit agentic.EventSink) (*agentic.ModelOutput, error) {
	stream := func(ev StreamEvent) {
		if emit == nil || ev.Type == "text" {
			return
		}
		emit(agentic.Event{Type: "model." + ev.Type, Tool: ev.Tool, Detail: ev.Detail, OK: ev.OK})
	}
	first := m.sessionID == "" || !m.adapter.SupportsResume()
	var reply *Reply
	var err error
	if streaming, ok := m.adapter.(streamingAdapter); ok {
		if first {
			reply, err = streaming.SingleShotStream(ctx, SingleShotOpts{
				Cwd: m.cwd, Message: input.Message, SystemPrompt: input.SystemPrompt,
				Model: m.model, Tools: "", Env: m.env,
			}, stream)
		} else {
			reply, err = streaming.ResumeStream(ctx, m.sessionID, ResumeOpts{
				Cwd: m.cwd, Message: input.Message, Model: m.model, Tools: "", Env: m.env,
			}, stream)
		}
		if errors.Is(err, ErrStreamUnsupported) {
			reply, err = nil, nil
		}
	}
	if reply == nil && err == nil {
		if first {
			reply, err = m.adapter.SingleShot(ctx, SingleShotOpts{
				Cwd: m.cwd, Message: input.Message, SystemPrompt: input.SystemPrompt,
				Model: m.model, Tools: "", Env: m.env,
			})
		} else {
			reply, err = m.adapter.Resume(ctx, m.sessionID, ResumeOpts{
				Cwd: m.cwd, Message: input.Message, Model: m.model, Tools: "", Env: m.env,
			})
		}
	}
	if err != nil {
		return nil, err
	}
	if reply.ExitCode != 0 {
		return nil, fmt.Errorf("%s exited with code %d: %s", m.adapter.Name(), reply.ExitCode, reply.Text)
	}
	if reply.SessionID != "" {
		m.sessionID = reply.SessionID
	}
	return &agentic.ModelOutput{Text: reply.Text, SessionID: m.sessionID}, nil
}

func managedRunsRoot() (string, error) {
	root, err := appdata.Root()
	if err != nil {
		return "", err
	}
	root = filepath.Join(root, "runs")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	return root, nil
}

func (c *Core) ListManagedRuns(agentID string) ([]ManagedRun, error) {
	root, err := managedRunsRoot()
	if err != nil {
		return nil, err
	}
	instances, err := agentic.ListInstances(root, agentID)
	if err != nil {
		return nil, err
	}
	sort.Slice(instances, func(i, j int) bool { return instances[i].StartedAt.After(instances[j].StartedAt) })
	out := make([]ManagedRun, 0, len(instances))
	for i := range instances {
		out = append(out, *managedRunFromInstance(&instances[i], nil))
	}
	return out, nil
}

func (c *Core) GetManagedRun(runID string) (*ManagedRun, error) {
	root, err := managedRunsRoot()
	if err != nil {
		return nil, err
	}
	instance, err := agentic.LoadInstance(root, runID)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(root, runID, "memory.json"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var memory agentic.Memory
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &memory); err != nil {
			return nil, err
		}
	}
	return managedRunFromInstance(instance, &memory), nil
}

func (c *Core) ReadManagedArtifact(runID, name string) ([]byte, error) {
	root, err := managedRunsRoot()
	if err != nil {
		return nil, err
	}
	return agentic.ReadArtifact(root, runID, name)
}

func managedRunFromResult(result *agentic.Result) *ManagedRun {
	return managedRunFromInstance(result.Instance, &result.Memory)
}

func managedRunFromInstance(instance *agentic.Instance, memory *agentic.Memory) *ManagedRun {
	if instance == nil {
		return nil
	}
	out := &ManagedRun{
		ID: instance.ID, AgentID: instance.AgentID, AgentName: instance.AgentName,
		State: string(instance.State), Turns: instance.Turns, SessionID: instance.SessionID,
		StartedAt: instance.StartedAt.Format(time.RFC3339Nano), UpdatedAt: instance.UpdatedAt.Format(time.RFC3339Nano),
		Error: instance.Error, Final: instance.Final,
		Artifacts: make([]ManagedRunArtifact, 0, len(instance.Artifacts)),
	}
	if instance.CompletedAt != nil {
		out.CompletedAt = instance.CompletedAt.Format(time.RFC3339Nano)
	}
	for _, artifact := range instance.Artifacts {
		out.Artifacts = append(out.Artifacts, ManagedRunArtifact{Name: artifact.Name, Size: artifact.Size, CreatedAt: artifact.CreatedAt.Format(time.RFC3339Nano)})
	}
	if memory != nil {
		for _, item := range memory.Items {
			out.Memory = append(out.Memory, ManagedRunMemoryItem{Kind: item.Kind, Title: item.Title, Content: item.Content, Status: item.Status, CreatedAt: item.CreatedAt.Format(time.RFC3339Nano)})
		}
	}
	return out
}
