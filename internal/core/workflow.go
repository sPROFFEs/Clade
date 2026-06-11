package core

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"text/template"
)

// RenderedStep is the output of expanding one WorkflowStep against a
// caller-supplied input map. It carries everything Phase 2b's executor
// will need to drive the underlying CLI agent — for now, only the
// rendered body is computed; the executor lives in a future patch.
type RenderedStep struct {
	Kind      StepKind
	Body      string // rendered user_message body (empty for non-message kinds)
	UntilTool string // pass-through from the source step
}

// RenderedWorkflow is the whole script after substitution. Steps are
// in execution order; if any input fails validation the function
// returns no steps and the first error.
type RenderedWorkflow struct {
	AgentID string
	Name    string
	Steps   []RenderedStep
}

// RenderWorkflow expands every step of w against the given inputs and
// agent metadata. Inputs missing a required value cause an error;
// optional inputs default to their Default field (or empty string).
//
// Template syntax is Go's text/template; the dot context is a map:
//
//	{{ .agent      }}   // agent name
//	{{ .agent_id   }}   // agent id
//	{{ .workflow   }}   // workflow name
//	{{ .inputs.foo }}   // user-supplied input "foo"
//	{{ .foo        }}   // shortcut: same as .inputs.foo
//
// The two forms are equivalent; the long form is provided so users
// who add an input named "agent" can still reach it via .inputs.agent.
func RenderWorkflow(a *Agent, w *Workflow, inputs map[string]string) (*RenderedWorkflow, error) {
	if a == nil {
		return nil, errors.New("RenderWorkflow: nil agent")
	}
	if w == nil {
		return nil, errors.New("RenderWorkflow: nil workflow")
	}

	merged, err := resolveInputs(w, inputs)
	if err != nil {
		return nil, err
	}
	ctx := map[string]any{
		"agent":    a.Name,
		"agent_id": a.ID,
		"workflow": w.Name,
		"inputs":   merged,
	}
	// Flatten inputs as top-level keys for the convenience syntax,
	// without overwriting reserved keys.
	for k, v := range merged {
		if _, taken := ctx[k]; taken {
			continue
		}
		ctx[k] = v
	}

	out := &RenderedWorkflow{AgentID: a.ID, Name: w.Name}
	for i, st := range w.Steps {
		rs := RenderedStep{Kind: st.Kind, UntilTool: st.UntilTool}
		if st.Kind == StepUserMessage {
			body, err := renderTemplate(st.Template, ctx)
			if err != nil {
				return nil, fmt.Errorf("workflow %q step #%d: %w", w.Name, i+1, err)
			}
			rs.Body = body
		}
		out.Steps = append(out.Steps, rs)
	}
	return out, nil
}

// resolveInputs validates the supplied input map against the workflow
// declaration: required inputs must be present and non-empty; unknown
// inputs are silently dropped so callers can reuse a single map across
// many workflows.
func resolveInputs(w *Workflow, supplied map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(w.Inputs))
	known := make(map[string]bool, len(w.Inputs))
	for _, in := range w.Inputs {
		known[in.Name] = true
		val, ok := supplied[in.Name]
		if !ok || strings.TrimSpace(val) == "" {
			val = in.Default
		}
		if in.Required && strings.TrimSpace(val) == "" {
			return nil, fmt.Errorf("workflow %q: input %q is required", w.Name, in.Name)
		}
		out[in.Name] = val
	}
	return out, nil
}

func renderTemplate(body string, ctx map[string]any) (string, error) {
	t, err := template.New("step").Option("missingkey=error").Parse(body)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}
