package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Pure helpers ------------------------------------------------------

func TestRenderDistillInput_FormatsRoles(t *testing.T) {
	out := RenderDistillInput([]DistillMessage{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	})
	if !strings.Contains(out, "user: hi") || !strings.Contains(out, "assistant: hello") {
		t.Fatalf("format wrong: %q", out)
	}
	if !strings.Contains(out, DistillPrompt) {
		t.Fatal("system prompt missing from rendered body")
	}
}

func TestRenderDistillInput_TruncatesLongMessages(t *testing.T) {
	huge := strings.Repeat("a", 5000)
	out := RenderDistillInput([]DistillMessage{{Role: "user", Content: huge}})
	if !strings.Contains(out, "...") {
		t.Fatal("expected truncation marker")
	}
	// The body contains the prompt (which itself has lowercase 'a's),
	// plus the truncated message. The exact prompt-'a' count isn't
	// load-bearing — what matters is that we don't pass the full
	// 5000-a payload through.
	if strings.Contains(out, strings.Repeat("a", 5000)) {
		t.Fatal("expected the 5000-a payload to be truncated, but it survived intact")
	}
}

func TestParseDistillJSON_HandlesPlainJSON(t *testing.T) {
	body := `{"summary":"hi","topics":["x"],"entities":[],"decisions":[],"actions":[],"pinned_candidates":[]}`
	r, err := ParseDistillJSON(body)
	if err != nil {
		t.Fatalf("ParseDistillJSON: %v", err)
	}
	if r.Summary != "hi" || len(r.Topics) != 1 || r.Topics[0] != "x" {
		t.Fatalf("decoded wrong: %+v", r)
	}
}

func TestParseDistillJSON_StripsCodeFences(t *testing.T) {
	body := "```json\n{\"summary\":\"hi\",\"topics\":[],\"entities\":[],\"decisions\":[],\"actions\":[],\"pinned_candidates\":[]}\n```"
	r, err := ParseDistillJSON(body)
	if err != nil {
		t.Fatalf("ParseDistillJSON: %v", err)
	}
	if r.Summary != "hi" {
		t.Fatalf("decoded wrong: %+v", r)
	}
}

func TestParseDistillJSON_ToleratesLeadingProse(t *testing.T) {
	body := "Sure! Here is the JSON:\n{\"summary\":\"hi\",\"topics\":[],\"entities\":[],\"decisions\":[],\"actions\":[],\"pinned_candidates\":[]}\nLet me know if you need anything else."
	r, err := ParseDistillJSON(body)
	if err != nil {
		t.Fatalf("ParseDistillJSON: %v", err)
	}
	if r.Summary != "hi" {
		t.Fatalf("decoded wrong: %+v", r)
	}
}

func TestParseDistillJSON_NormalisesNilSlicesToEmpty(t *testing.T) {
	body := `{"summary":"x"}` // missing arrays entirely
	r, err := ParseDistillJSON(body)
	if err != nil {
		t.Fatalf("ParseDistillJSON: %v", err)
	}
	if r.Topics == nil || r.Entities == nil || r.Decisions == nil || r.Actions == nil || r.PinnedCandidates == nil {
		t.Fatalf("expected nil slices to be normalised: %+v", r)
	}
}

func TestParseDistillJSON_RejectsEmpty(t *testing.T) {
	if _, err := ParseDistillJSON(""); err == nil {
		t.Fatal("expected error on empty")
	}
}

func TestParseDistillJSON_RejectsNoJSON(t *testing.T) {
	if _, err := ParseDistillJSON("no JSON here, sorry"); err == nil {
		t.Fatal("expected error when reply has no JSON")
	}
}

// --- Factory routing ---------------------------------------------------

func TestNewDistiller_NoneReturnsNil(t *testing.T) {
	d, err := NewDistiller(DistillEndpoint{Kind: DistillKindNone})
	if err != nil {
		t.Fatalf("NewDistiller: %v", err)
	}
	if d != nil {
		t.Fatal("expected nil distiller for DistillKindNone")
	}
}

func TestNewDistiller_RejectsUnknownKind(t *testing.T) {
	if _, err := NewDistiller(DistillEndpoint{Kind: "fairy-dust"}); err == nil {
		t.Fatal("expected unknown-kind error")
	}
}

func TestNewDistiller_OllamaRequiresBaseURLAndModel(t *testing.T) {
	if _, err := NewDistiller(DistillEndpoint{Kind: DistillKindOllama}); err == nil {
		t.Fatal("expected error when BaseURL+Model missing")
	}
	if _, err := NewDistiller(DistillEndpoint{Kind: DistillKindOllama, BaseURL: "http://x"}); err == nil {
		t.Fatal("expected error when Model missing")
	}
}

// --- Ollama HTTP path --------------------------------------------------

func TestOllamaDistiller_PostsAndDecodes(t *testing.T) {
	want := DistillResult{
		Summary:          "fixed the launcher bug",
		Topics:           []string{"launcher", "bug"},
		Entities:         []string{"slice.go"},
		Decisions:        []string{"revert the goroutine refactor"},
		Actions:          []string{"add a regression test"},
		PinnedCandidates: []string{"user prefers verbose error messages"},
	}
	wantJSON, _ := json.Marshal(want)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/generate":
			if r.Method != "POST" {
				t.Errorf("expected POST, got %s", r.Method)
			}
			var req ollamaGenerateRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Format != "json" {
				t.Errorf("missing format=json")
			}
			if !strings.Contains(req.Prompt, DistillPrompt[:60]) {
				t.Errorf("prompt not used")
			}
			_ = json.NewEncoder(w).Encode(ollamaGenerateResponse{Response: string(wantJSON), Done: true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	d, err := NewDistiller(DistillEndpoint{Kind: DistillKindOllama, BaseURL: srv.URL, Model: "qwen2.5:1.5b"})
	if err != nil {
		t.Fatalf("NewDistiller: %v", err)
	}
	res, err := d.Distill(context.Background(), []DistillMessage{{Role: "user", Content: "fixed the bug"}})
	if err != nil {
		t.Fatalf("Distill: %v", err)
	}
	if res.Summary != want.Summary || len(res.Topics) != 2 {
		t.Fatalf("decoded wrong: %+v", res)
	}
}

func TestOllamaDistiller_PropagatesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer srv.Close()
	d, _ := NewDistiller(DistillEndpoint{Kind: DistillKindOllama, BaseURL: srv.URL, Model: "nope"})
	_, err := d.Distill(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 error, got %v", err)
	}
}

func TestOllamaDistiller_Available(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			hit = true
			_, _ = w.Write([]byte(`{"models":[]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	d, _ := NewDistiller(DistillEndpoint{Kind: DistillKindOllama, BaseURL: srv.URL, Model: "x"})
	if err := d.Available(context.Background()); err != nil {
		t.Fatalf("Available: %v", err)
	}
	if !hit {
		t.Fatal("Available didn't hit /api/tags")
	}
}

// --- CLI distiller -----------------------------------------------------

func TestCLIDistiller_UsesRegisteredAdapter(t *testing.T) {
	want := `{"summary":"via-cli","topics":[],"entities":[],"decisions":[],"actions":[],"pinned_candidates":[]}`
	mock := &mockAdapter{name: "mockcli", replies: []string{want}}
	withMockAdapter(t, mock)

	d, err := NewDistiller(DistillEndpoint{Kind: DistillKindCLI, CLIName: "mockcli"})
	if err != nil {
		t.Fatalf("NewDistiller: %v", err)
	}
	res, err := d.Distill(context.Background(), []DistillMessage{{Role: "user", Content: "x"}})
	if err != nil {
		t.Fatalf("Distill: %v", err)
	}
	if res.Summary != "via-cli" {
		t.Fatalf("decoded wrong: %+v", res)
	}
	if len(mock.shots) != 1 {
		t.Fatalf("expected 1 SingleShot, got %d", len(mock.shots))
	}
	if mock.shots[0].SystemPrompt != "" {
		t.Fatalf("distiller should pass empty SystemPrompt, got %q", mock.shots[0].SystemPrompt)
	}
}

func TestCLIDistiller_UnknownCLIErrors(t *testing.T) {
	if _, err := NewDistiller(DistillEndpoint{Kind: DistillKindCLI, CLIName: "ghost-cli"}); err == nil {
		t.Fatal("expected error for unregistered CLI")
	}
}
