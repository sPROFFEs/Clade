package main

import (
	"reflect"
	"testing"
)

func TestTerminalResumeCommand(t *testing.T) {
	tests := []struct {
		cli       string
		model     string
		wantName  string
		wantArgs  []string
		supported bool
	}{
		{"praimate-code", "openai/gpt-5", "praimate-code", []string{"--model", "openai/gpt-5", "--continue"}, true},
		{"opencode", "", "opencode", []string{"--continue"}, true},
		{"claude", "sonnet", "claude", []string{"--model", "sonnet", "--continue"}, true},
		{"codex", "gpt-5", "codex", []string{"resume", "--last", "--model", "gpt-5"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.cli, func(t *testing.T) {
			name, args, supported, err := terminalResumeCommand(tt.cli, tt.model)
			if err != nil {
				t.Fatal(err)
			}
			if name != tt.wantName || !reflect.DeepEqual(args, tt.wantArgs) || supported != tt.supported {
				t.Fatalf("got name=%q args=%q supported=%v", name, args, supported)
			}
		})
	}
}
