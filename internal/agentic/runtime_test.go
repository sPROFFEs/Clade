package agentic

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type scriptedModel struct {
	outputs []string
	inputs  []ModelInput
	err     error
}

func (m *scriptedModel) Turn(_ context.Context, input ModelInput, _ EventSink) (*ModelOutput, error) {
	m.inputs = append(m.inputs, input)
	if m.err != nil {
		return nil, m.err
	}
	if len(m.outputs) == 0 {
		return nil, errors.New("script exhausted")
	}
	out := m.outputs[0]
	m.outputs = m.outputs[1:]
	return &ModelOutput{Text: out, SessionID: "session-1"}, nil
}

func TestRunLifecycleMemoryArtifactsAndExplicitFinish(t *testing.T) {
	root := t.TempDir()
	model := &scriptedModel{outputs: []string{
		`{"action":"tool","tool":"memory.task","arguments":{"title":"Inspect","content":"Review the target","status":"active"}}`,
		`{"action":"tool","tool":"memory.fact","arguments":{"content":"The parser is strict"}}`,
		`{"action":"tool","tool":"artifact.write","arguments":{"name":"report.md","content":"# Report\nComplete"}}`,
		`{"action":"finish","message":"Review completed."}`,
	}}
	var events []Event
	result, err := Run(context.Background(), Config{
		RootDir: root, AgentID: "reviewer", AgentName: "Reviewer",
		Instructions: "Review carefully.", Task: "Inspect the project.", Model: model,
		OnEvent: func(event Event) { events = append(events, event) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Instance.State != StateCompleted || result.Instance.Turns != 4 || result.Instance.Final != "Review completed." {
		t.Fatalf("instance = %#v", result.Instance)
	}
	if len(result.Memory.Items) != 2 || len(result.Instance.Artifacts) != 1 {
		t.Fatalf("memory=%#v artifacts=%#v", result.Memory, result.Instance.Artifacts)
	}
	artifactPath := filepath.Join(root, result.Instance.ID, "artifacts", "report.md")
	if body, err := os.ReadFile(artifactPath); err != nil || !strings.Contains(string(body), "Complete") {
		t.Fatalf("artifact body=%q err=%v", body, err)
	}
	for _, forbidden := range []string{"events.jsonl", "runtime.log", "agent.log"} {
		if _, err := os.Stat(filepath.Join(root, result.Instance.ID, forbidden)); !os.IsNotExist(err) {
			t.Fatalf("runtime created forbidden log file %s", forbidden)
		}
	}
	if len(events) == 0 || events[0].Type != "run.started" || events[len(events)-1].Type != "run.finished" {
		t.Fatalf("events = %#v", events)
	}
	loaded, err := LoadInstance(root, result.Instance.ID)
	if err != nil || loaded.State != StateCompleted {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
}

func TestRunBoundsFinalOutputIntoArtifact(t *testing.T) {
	long := strings.Repeat("x", 1200)
	model := &scriptedModel{outputs: []string{`{"action":"finish","message":"` + long + `"}`}}
	root := t.TempDir()
	result, err := Run(context.Background(), Config{
		RootDir: root, AgentID: "bounded", AgentName: "Bounded",
		Task: "Answer.", Model: model,
		Limits: Limits{MaxTurns: 2, MaxContextChars: 2000, MaxOutputChars: 500, MaxArtifactSize: 4096},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Instance.Final) >= len(long) || !strings.Contains(result.Instance.Final, "artifact://final-output.txt") {
		t.Fatalf("final was not bounded: %d %q", len(result.Instance.Final), result.Instance.Final)
	}
	if len(result.Instance.Artifacts) != 1 || result.Instance.Artifacts[0].Size != int64(len(long)) {
		t.Fatalf("artifacts = %#v", result.Instance.Artifacts)
	}
	body, err := ReadArtifact(root, result.Instance.ID, "final-output.txt")
	if err != nil || string(body) != long {
		t.Fatalf("full output artifact size=%d err=%v", len(body), err)
	}
}

func TestRunRejectsRepeatedPlainTextCompletion(t *testing.T) {
	model := &scriptedModel{outputs: []string{"I am done", "still done", "finished"}}
	result, err := Run(context.Background(), Config{
		RootDir: t.TempDir(), AgentID: "strict", AgentName: "Strict",
		Task: "Answer.", Model: model, Limits: Limits{MaxTurns: 5},
	})
	if err == nil || !strings.Contains(err.Error(), "violated runtime protocol") {
		t.Fatalf("err = %v", err)
	}
	if result == nil || result.Instance.State != StateFailed || result.Instance.Turns != 3 {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunStopsAtTurnLimitWithoutFinish(t *testing.T) {
	model := &scriptedModel{outputs: []string{
		`{"action":"continue","message":"one"}`,
		`{"action":"continue","message":"two"}`,
	}}
	result, err := Run(context.Background(), Config{
		RootDir: t.TempDir(), AgentID: "stalled", AgentName: "Stalled",
		Task: "Continue.", Model: model, Limits: Limits{MaxTurns: 2},
	})
	if err == nil || result.Instance.State != StateStalled {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestReadArtifactRejectsPathTraversal(t *testing.T) {
	if _, err := ReadArtifact(t.TempDir(), "../run", "secret"); err == nil {
		t.Fatal("run traversal accepted")
	}
	if _, err := ReadArtifact(t.TempDir(), "run", "../secret"); err == nil {
		t.Fatal("artifact traversal accepted")
	}
}

func TestMemorySummaryKeepsNewestItemsWhenBounded(t *testing.T) {
	memory := Memory{Items: []MemoryItem{
		{Kind: "fact", Content: strings.Repeat("old", 40)},
		{Kind: "decision", Content: "keep the newest decision"},
	}}
	summary := memory.summary(90)
	if strings.Contains(summary, "oldold") || !strings.Contains(summary, "keep the newest decision") {
		t.Fatalf("summary = %q", summary)
	}
	if !strings.Contains(summary, "older working-memory items omitted") {
		t.Fatalf("missing omission marker: %q", summary)
	}
}
