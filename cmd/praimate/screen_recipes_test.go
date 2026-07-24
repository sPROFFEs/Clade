package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"git.jtsec.local/lab/PrAImate/internal/core"
	"git.jtsec.local/lab/PrAImate/internal/launcher"
)

// fakeCfg is the bare minimum Config the recipes screen needs. The TUI
// surrounding it does much more with cfg, but for state-machine tests
// only WorkspacesRoot is read.
func fakeCfg(t *testing.T) *launcher.Config {
	t.Helper()
	return &launcher.Config{WorkspacesRoot: t.TempDir()}
}

func loadedRecipes(agents ...core.Agent) recipesModel {
	m := recipesModel{cfg: &launcher.Config{WorkspacesRoot: "/tmp"}, cli: "claude", inputValues: map[string]string{}}
	m.loaded = true
	m.agents = agents
	return m
}

func TestRecipes_Loaded_NoCoreShowsError(t *testing.T) {
	m := newRecipesModel(fakeCfg(t))
	// Init returns a Cmd that probes getAppCore(); we simulate the
	// "core unavailable" outcome directly to avoid touching package
	// globals from a test.
	mdl, _ := m.Update(recipesLoadedMsg{err: fmtCoreInitErr()})
	got := mdl.(recipesModel).Body()
	if !strings.Contains(got, "error:") {
		t.Fatalf("expected error body, got: %q", got)
	}
}

func TestRecipes_PickAgent_AdvancesToWorkflowPickerWhenMultiple(t *testing.T) {
	a := core.Agent{
		ID: "a", Name: "Multi", Supports: []string{"claude"},
		Workflows: []core.Workflow{
			{Name: "One", Steps: []core.WorkflowStep{{Kind: core.StepUserMessage, Template: "x"}}},
			{Name: "Two", Steps: []core.WorkflowStep{{Kind: core.StepUserMessage, Template: "y"}}},
		},
	}
	m := loadedRecipes(a)
	mdl, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := mdl.(recipesModel)
	if got.step != stepPickWorkflow {
		t.Fatalf("expected stepPickWorkflow, got %d", got.step)
	}
	if got.selectedAgent == nil || got.selectedAgent.ID != "a" {
		t.Fatalf("selected agent not set: %+v", got.selectedAgent)
	}
}

func TestRecipes_PickAgent_SkipsWorkflowPickerWhenSingle(t *testing.T) {
	a := core.Agent{
		ID: "a", Name: "Solo", Supports: []string{"claude"},
		Workflows: []core.Workflow{
			{Name: "Only", Inputs: []core.WorkflowInput{{Name: "task", Required: true}},
				Steps: []core.WorkflowStep{{Kind: core.StepUserMessage, Template: "{{ .task }}"}}},
		},
	}
	m := loadedRecipes(a)
	mdl, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := mdl.(recipesModel)
	if got.step != stepFillInputs {
		t.Fatalf("expected stepFillInputs, got %d", got.step)
	}
	if len(got.inputs) != 1 {
		t.Fatalf("expected 1 input field, got %d", len(got.inputs))
	}
}

func TestRecipes_PickAgent_ErrorsWhenNoWorkflows(t *testing.T) {
	a := core.Agent{ID: "a", Name: "Empty", Supports: []string{"claude"}}
	m := loadedRecipes(a)
	mdl, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if mdl.(recipesModel).err == "" {
		t.Fatal("expected an error when agent has no workflow")
	}
}

func TestRecipes_FillInputs_EscReturnsToPickAgent(t *testing.T) {
	a := core.Agent{
		ID: "a", Name: "Solo", Supports: []string{"claude"},
		Workflows: []core.Workflow{
			{Name: "Only", Inputs: []core.WorkflowInput{{Name: "task", Required: true}},
				Steps: []core.WorkflowStep{{Kind: core.StepUserMessage, Template: "{{ .task }}"}}},
		},
	}
	m := loadedRecipes(a)
	mdl, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mdl, _ = mdl.(recipesModel).Update(tea.KeyMsg{Type: tea.KeyEsc})
	if mdl.(recipesModel).step != stepPickAgent {
		t.Fatalf("esc should return to pick-agent, got step %d", mdl.(recipesModel).step)
	}
}

func TestRecipes_FillInputs_PrivacyReviewBeforeRun(t *testing.T) {
	secret := "sk-abcdefghijklmnopqrstuvwxyzz"
	a := core.Agent{
		ID: "a", Name: "Solo", Supports: []string{"claude"},
		Workflows: []core.Workflow{
			{Name: "Only", Inputs: []core.WorkflowInput{{Name: "token", Required: true}},
				Steps: []core.WorkflowStep{{Kind: core.StepUserMessage, Template: "{{ .token }}"}}},
		},
	}
	m := loadedRecipes(a)
	mdl, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := mdl.(recipesModel)
	got.inputs[0].SetValue(secret)

	mdl, cmd := got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got = mdl.(recipesModel)
	if cmd != nil {
		t.Fatal("privacy review should not start the workflow command yet")
	}
	if got.step != stepPrivacyReview {
		t.Fatalf("expected stepPrivacyReview, got %d", got.step)
	}
	body := got.Body()
	if !strings.Contains(body, "OPENAI_KEY") {
		t.Fatalf("review should show match category, got %q", body)
	}
	if strings.Contains(body, secret) {
		t.Fatalf("review body should not echo the secret: %q", body)
	}

	mdl, cmd = got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got = mdl.(recipesModel)
	if got.step != stepRunning {
		t.Fatalf("expected stepRunning after confirming review, got %d", got.step)
	}
	if cmd == nil {
		t.Fatal("confirming privacy review should start workflow command")
	}
}

func TestRecipes_ResultEnter_StartsOver(t *testing.T) {
	m := loadedRecipes()
	m.step = stepShowResult
	m.runResult = &core.RunResult{Outcome: core.OutcomeCompleted}
	mdl, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if mdl.(recipesModel).step != stepPickAgent {
		t.Fatalf("expected reset to stepPickAgent, got %d", mdl.(recipesModel).step)
	}
}

func TestRecipes_Title_TracksStep(t *testing.T) {
	m := loadedRecipes(core.Agent{ID: "x", Name: "X"})
	if !strings.Contains(m.Title(), "pick an agent") {
		t.Fatalf("unexpected title at stepPickAgent: %q", m.Title())
	}
}
