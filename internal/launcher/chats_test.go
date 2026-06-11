package launcher

import (
	"regexp"
	"strings"
	"testing"
)

var workpathNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func TestCreateChatFromInstructions_WorkpathNameIsValid(t *testing.T) {
	root := t.TempDir()
	// Uppercase id exercises the slug normaliser; the result must satisfy
	// the wpc workpath-name grammar so the launch compile validates.
	chat, err := CreateChatFromInstructions(root, "My Chat", AgentClaude, "Dev-Team", "desc", "you are a dev team")
	if err != nil {
		t.Fatalf("CreateChatFromInstructions: %v", err)
	}
	if !workpathNameRe.MatchString(chat.Template) {
		t.Fatalf("workpath name %q does not match the wpc grammar", chat.Template)
	}
	// The synthesised workpath must validate. An unavailable agent binary
	// is acceptable in CI; the name-validation error we guard against is not.
	if _, _, err := OpenChatWithOptions(chat, OpenChatOptions{SkipResume: true}); err != nil {
		if strings.Contains(err.Error(), "must match") {
			t.Fatalf("workpath validation regression: %v", err)
		}
	}
}

func TestWorkpathSlug(t *testing.T) {
	cases := map[string]string{
		"dev-team":        "dev-team",
		"Agent Builder":   "agentbuilder",
		"agent:dev-team":  "agentdev-team", // colon dropped, parts merge
		"security-review": "security-review",
		"123abc":          "123abc",
		"-leading-dash":   "leading-dash",
	}
	for in, want := range cases {
		if got := workpathSlug(in); got != want {
			t.Errorf("workpathSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
