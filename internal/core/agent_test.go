package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAgentYAML_Minimal(t *testing.T) {
	body := `schema: praimate.agent/v1
id: tiny
name: Tiny
instructions: hi
supports: [claude]
`
	a, err := ParseAgentYAML(strings.NewReader(body))
	if err != nil {
		t.Fatalf("ParseAgentYAML: %v", err)
	}
	if a.ID != "tiny" || a.Name != "Tiny" {
		t.Fatalf("unexpected agent: %+v", a)
	}
	if len(a.Workflows) != 0 {
		t.Fatalf("expected zero workflows, got %d", len(a.Workflows))
	}
}

func TestParseAgentYAML_RejectsMissingSchema(t *testing.T) {
	_, err := ParseAgentYAML(strings.NewReader(`id: x
name: X
instructions: x
supports: [claude]
`))
	if err == nil {
		t.Fatal("expected error for missing schema")
	}
}

func TestParseAgentYAML_RejectsUnknownCLI(t *testing.T) {
	_, err := ParseAgentYAML(strings.NewReader(`schema: praimate.agent/v1
id: x
name: X
instructions: x
supports: [nonsense]
`))
	if err == nil || !strings.Contains(err.Error(), "unknown CLI") {
		t.Fatalf("expected unknown-CLI error, got: %v", err)
	}
}

func TestParseAgentYAML_RejectsUnknownStepKind(t *testing.T) {
	_, err := ParseAgentYAML(strings.NewReader(`schema: praimate.agent/v1
id: x
name: X
instructions: x
supports: [claude]
workflows:
  - name: bad
    steps:
      - kind: pirouette
`))
	if err == nil || !strings.Contains(err.Error(), "unknown kind") {
		t.Fatalf("expected unknown-kind error, got: %v", err)
	}
}

func TestParseAgentYAML_RejectsEmptyUserMessageTemplate(t *testing.T) {
	_, err := ParseAgentYAML(strings.NewReader(`schema: praimate.agent/v1
id: x
name: X
instructions: x
supports: [claude]
workflows:
  - name: bad
    steps:
      - kind: user_message
        template: ""
`))
	if err == nil || !strings.Contains(err.Error(), "requires a template") {
		t.Fatalf("expected empty-template error, got: %v", err)
	}
}

func TestParseAgentYAML_RejectsDefaultWorkflowMismatch(t *testing.T) {
	_, err := ParseAgentYAML(strings.NewReader(`schema: praimate.agent/v1
id: x
name: X
instructions: x
supports: [claude]
default_workflow: ghost
workflows:
  - name: real
    steps:
      - kind: user_message
        template: hi
`))
	if err == nil || !strings.Contains(err.Error(), "default_workflow") {
		t.Fatalf("expected default_workflow error, got: %v", err)
	}
}

func TestParseAgentYAML_RejectsMacOSRequirements(t *testing.T) {
	_, err := ParseAgentYAML(strings.NewReader(`schema: praimate.agent/v1
id: mac-only
name: Mac only
instructions: x
supports: [claude]
requirements:
  os: darwin
  script: setup.sh
workflows:
  - name: run
    steps:
      - kind: user_message
        template: hello
`))
	if err == nil || !strings.Contains(err.Error(), "linux or windows") {
		t.Fatalf("expected unsupported requirements OS error, got %v", err)
	}
}

func TestRoundTripYAML_PreservesFields(t *testing.T) {
	in := &Agent{
		ID:           "rt",
		Name:         "Round Trip",
		Description:  "test",
		Instructions: "you do things",
		Supports:     []string{"claude", "codex"},
		Tools:        []string{"file_read"},
		Workflows: []Workflow{
			{
				Name: "Run",
				Inputs: []WorkflowInput{
					{Name: "task", Prompt: "task?", Type: "string", Required: true},
				},
				Steps: []WorkflowStep{
					{Kind: StepUserMessage, Template: "Do: {{ .task }}"},
					{Kind: StepWaitForAssistant},
				},
			},
		},
		DefaultWorkflow: "Run",
	}
	body, err := MarshalAgentYAML(in)
	if err != nil {
		t.Fatalf("MarshalAgentYAML: %v", err)
	}
	out, err := ParseAgentYAML(strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("ParseAgentYAML: %v", err)
	}
	if out.ID != in.ID || out.Name != in.Name {
		t.Fatalf("identity drift: in=%v out=%v", in, out)
	}
	if len(out.Workflows) != 1 || len(out.Workflows[0].Steps) != 2 {
		t.Fatalf("workflow drift: %+v", out.Workflows)
	}
	if out.DefaultWorkflow != "Run" {
		t.Fatalf("default workflow lost: %q", out.DefaultWorkflow)
	}
}

func TestBuiltinAgents_AllParseAndValidate(t *testing.T) {
	agents, err := BuiltinAgents()
	if err != nil {
		t.Fatalf("BuiltinAgents: %v", err)
	}
	if len(agents) == 0 {
		t.Fatal("no built-in agents found")
	}
	wantIDs := map[string]bool{"agent-builder": false, "security-review": false, "dev-team": false}
	for _, a := range agents {
		if err := a.Validate(); err != nil {
			t.Errorf("builtin %s invalid: %v", a.ID, err)
		}
		if _, ok := wantIDs[a.ID]; ok {
			wantIDs[a.ID] = true
		}
	}
	for id, seen := range wantIDs {
		if !seen {
			t.Errorf("expected builtin %q, not found", id)
		}
	}
}

func TestSeedBuiltins_EvictsStaleBuiltinsButKeepsImports(t *testing.T) {
	s := openTempStore(t)
	defer s.Close()
	c, _ := New(Options{Store: s})
	ctx := context.Background()

	// Simulate a stale builtin from an earlier release + a user import.
	if _, err := c.upsertAgent(ctx, &Agent{
		ID: "freeform", Name: "Old Freeform", Instructions: "x",
		Supports: []string{"claude"}, SourcePath: "builtin:freeform.yaml",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.upsertAgent(ctx, &Agent{
		ID: "my-import", Name: "Mine", Instructions: "x",
		Supports: []string{"claude"}, SourcePath: "/home/u/mine.yaml",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := c.SeedBuiltins(ctx); err != nil {
		t.Fatalf("SeedBuiltins: %v", err)
	}

	// Stale builtin gone.
	if _, err := c.GetAgent(ctx, "freeform"); !errors.Is(err, ErrAgentNotFound) {
		t.Errorf("stale builtin should be evicted, got err=%v", err)
	}
	// User import survives.
	if _, err := c.GetAgent(ctx, "my-import"); err != nil {
		t.Errorf("user import should survive seeding, got %v", err)
	}
	// New builtins present.
	if _, err := c.GetAgent(ctx, "dev-team"); err != nil {
		t.Errorf("new builtin dev-team missing: %v", err)
	}
}

func TestSeedBuiltins_PopulatesDB(t *testing.T) {
	s := openTempStore(t)
	defer s.Close()
	c, _ := New(Options{Store: s})

	n, err := c.SeedBuiltins(context.Background())
	if err != nil {
		t.Fatalf("SeedBuiltins: %v", err)
	}
	if n < 3 {
		t.Fatalf("expected >=3 seeded, got %d", n)
	}

	agents, _ := c.ListAgents(context.Background())
	if len(agents) != n {
		t.Fatalf("list count %d != seeded %d", len(agents), n)
	}
}

func TestImportExportRoundTrip(t *testing.T) {
	s := openTempStore(t)
	defer s.Close()
	c, _ := New(Options{Store: s})

	src := filepath.Join(t.TempDir(), "ie.yaml")
	if err := os.WriteFile(src, []byte(`schema: praimate.agent/v1
id: ie
name: I/E
instructions: x
supports: [claude]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := c.ImportAgent(context.Background(), src)
	if err != nil {
		t.Fatalf("ImportAgent: %v", err)
	}
	if a.ID != "ie" {
		t.Fatalf("imported wrong agent: %+v", a)
	}

	dst := filepath.Join(t.TempDir(), "ie-export.yaml")
	if err := c.ExportAgent(context.Background(), "ie", dst); err != nil {
		t.Fatalf("ExportAgent: %v", err)
	}
	body, _ := os.ReadFile(dst)
	if !strings.Contains(string(body), "id: ie") {
		t.Fatalf("exported YAML missing id field:\n%s", body)
	}
}

func TestImport_PreservesDefaultWorkflow(t *testing.T) {
	s := openTempStore(t)
	defer s.Close()
	c, _ := New(Options{Store: s})

	src := filepath.Join(t.TempDir(), "dw.yaml")
	if err := os.WriteFile(src, []byte(`schema: praimate.agent/v1
id: dw
name: DW
instructions: x
supports: [claude]
default_workflow: Pick Me
workflows:
  - name: Pick Me
    steps:
      - kind: user_message
        template: hi
`), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := c.ImportAgent(context.Background(), src)
	if err != nil {
		t.Fatalf("ImportAgent: %v", err)
	}
	if a.DefaultWorkflow != "Pick Me" {
		t.Fatalf("DefaultWorkflow lost on import: %q", a.DefaultWorkflow)
	}

	got, err := c.GetAgent(context.Background(), "dw")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if got.DefaultWorkflow != "Pick Me" {
		t.Fatalf("DefaultWorkflow lost on DB round-trip: %q", got.DefaultWorkflow)
	}
	if w := got.ResolveDefaultWorkflow(); w == nil || w.Name != "Pick Me" {
		t.Fatalf("ResolveDefaultWorkflow returned %v", w)
	}
}

func TestDeleteAgent_NotFound(t *testing.T) {
	s := openTempStore(t)
	defer s.Close()
	c, _ := New(Options{Store: s})

	err := c.DeleteAgent(context.Background(), "ghost")
	if !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("expected ErrAgentNotFound, got %v", err)
	}
}

func TestRenderWorkflow_SubstitutesInputs(t *testing.T) {
	a := &Agent{ID: "x", Name: "X"}
	w := &Workflow{
		Name: "W",
		Inputs: []WorkflowInput{
			{Name: "thing", Prompt: "thing?", Required: true},
		},
		Steps: []WorkflowStep{
			{Kind: StepUserMessage, Template: "Do {{ .thing }} for {{ .agent }}"},
			{Kind: StepWaitForAssistant, UntilTool: "complete"},
		},
	}
	out, err := RenderWorkflow(a, w, map[string]string{"thing": "laundry"})
	if err != nil {
		t.Fatalf("RenderWorkflow: %v", err)
	}
	if len(out.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(out.Steps))
	}
	if out.Steps[0].Body != "Do laundry for X" {
		t.Fatalf("bad render: %q", out.Steps[0].Body)
	}
	if out.Steps[1].UntilTool != "complete" {
		t.Fatalf("UntilTool not propagated: %q", out.Steps[1].UntilTool)
	}
}

func TestRenderWorkflow_RejectsMissingRequiredInput(t *testing.T) {
	a := &Agent{ID: "x", Name: "X"}
	w := &Workflow{
		Name: "W",
		Inputs: []WorkflowInput{
			{Name: "must", Prompt: "must?", Required: true},
		},
		Steps: []WorkflowStep{{Kind: StepUserMessage, Template: "{{ .must }}"}},
	}
	_, err := RenderWorkflow(a, w, map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("expected required-input error, got %v", err)
	}
}

func TestRenderWorkflow_DefaultsApply(t *testing.T) {
	a := &Agent{ID: "x", Name: "X"}
	w := &Workflow{
		Name: "W",
		Inputs: []WorkflowInput{
			{Name: "tone", Default: "polite"},
		},
		Steps: []WorkflowStep{{Kind: StepUserMessage, Template: "Be {{ .tone }}"}},
	}
	out, err := RenderWorkflow(a, w, map[string]string{})
	if err != nil {
		t.Fatalf("RenderWorkflow: %v", err)
	}
	if out.Steps[0].Body != "Be polite" {
		t.Fatalf("default not applied: %q", out.Steps[0].Body)
	}
}

func TestResolveDefaultWorkflow(t *testing.T) {
	a := &Agent{Workflows: []Workflow{
		{Name: "A", Steps: []WorkflowStep{{Kind: StepUserMessage, Template: "x"}}},
	}}
	if w := a.ResolveDefaultWorkflow(); w == nil || w.Name != "A" {
		t.Fatalf("expected sole workflow, got %v", w)
	}

	a.Workflows = append(a.Workflows, Workflow{Name: "B",
		Steps: []WorkflowStep{{Kind: StepUserMessage, Template: "x"}}})
	if w := a.ResolveDefaultWorkflow(); w != nil {
		t.Fatalf("expected nil when multiple workflows and no default, got %v", w)
	}

	a.DefaultWorkflow = "B"
	if w := a.ResolveDefaultWorkflow(); w == nil || w.Name != "B" {
		t.Fatalf("expected workflow B, got %v", w)
	}
}

// Surfaces gate where an agent can launch from in the GUI. Empty =
// everywhere (backward compat); values round-trip through YAML and DB.
func TestAgentSurfaces_RoundTripAndGating(t *testing.T) {
	c, _ := New(Options{Store: openTempStore(t)})
	ctx := context.Background()

	yamlBody := []byte(`schema: praimate.agent/v1
id: docs-writer
name: Docs Writer
instructions: write docs
supports: [claude]
surfaces: [editor, chat]
`)
	a, err := c.ImportAgentYAML(ctx, yamlBody, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	got, err := c.GetAgent(ctx, a.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Surfaces) != 2 || !got.AllowsSurface("editor") || !got.AllowsSurface("chat") {
		t.Fatalf("surfaces = %v", got.Surfaces)
	}
	if got.AllowsSurface("terminal") {
		t.Error("terminal should be gated off")
	}
	// Re-export keeps the field.
	raw, err := MarshalAgentYAML(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), "surfaces:") {
		t.Errorf("exported yaml lost surfaces:\n%s", raw)
	}
	// Empty surfaces = allowed everywhere.
	empty := &Agent{}
	for _, s := range AllSurfaces {
		if !empty.AllowsSurface(s) {
			t.Errorf("empty surfaces must allow %s", s)
		}
	}
	// Unknown surface fails validation.
	if _, err := c.ImportAgentYAML(ctx, []byte(`schema: praimate.agent/v1
id: bad
name: Bad
instructions: x
supports: [claude]
surfaces: [browser]
`), ""); err == nil {
		t.Error("unknown surface must fail validation")
	}
}

// A builtin the user edited (source_path cleared by the YAML-editor
// save) must survive re-seeding — startup must not clobber user tweaks.
func TestSeedBuiltins_PreservesUserEditedBuiltin(t *testing.T) {
	c, _ := New(Options{Store: openTempStore(t)})
	ctx := context.Background()
	if _, err := c.SeedBuiltins(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a, err := c.GetAgent(ctx, "agent-builder")
	if err != nil {
		t.Fatalf("get builtin: %v", err)
	}
	// User edits it via the GUI editor (import with empty source path).
	a.Description = "my customized builder"
	raw, _ := MarshalAgentYAML(a)
	if _, err := c.ImportAgentYAML(ctx, raw, ""); err != nil {
		t.Fatalf("user edit: %v", err)
	}
	// Next startup re-seeds — the tweak must survive.
	if _, err := c.SeedBuiltins(ctx); err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	got, _ := c.GetAgent(ctx, "agent-builder")
	if got.Description != "my customized builder" {
		t.Fatalf("re-seed clobbered the user's edit; description = %q", got.Description)
	}
	// Deleting brings the pristine builtin back on the next seed.
	if err := c.DeleteAgent(ctx, "agent-builder"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := c.SeedBuiltins(ctx); err != nil {
		t.Fatalf("seed after delete: %v", err)
	}
	if _, err := c.GetAgent(ctx, "agent-builder"); err != nil {
		t.Fatalf("pristine builtin did not come back: %v", err)
	}
}
