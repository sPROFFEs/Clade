package main

import (
	"strings"
	"testing"
)

// Token-cap parser is the load-bearing piece for the "blank = CLI
// default" semantics the user asked for. These tests pin the
// behaviour so a future "convenience" patch can't silently reintroduce
// the 4096/1024 footgun.

func TestParseTokenLimit_BlankIsZero(t *testing.T) {
	got, err := parseTokenLimit("", "context tokens")
	if err != nil {
		t.Fatalf("blank should not error, got %v", err)
	}
	if got != 0 {
		t.Errorf("blank input → %d, want 0 (= no cap; writers skip the field)", got)
	}
}

func TestParseTokenLimit_WhitespaceIsBlank(t *testing.T) {
	got, err := parseTokenLimit("   \t  ", "context tokens")
	if err != nil {
		t.Fatalf("whitespace should not error, got %v", err)
	}
	if got != 0 {
		t.Errorf("whitespace → %d, want 0", got)
	}
}

func TestParseTokenLimit_PositiveInteger(t *testing.T) {
	got, err := parseTokenLimit("8192", "context tokens")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got != 8192 {
		t.Errorf("got %d, want 8192", got)
	}
}

func TestParseTokenLimit_RejectsNonPositive(t *testing.T) {
	for _, in := range []string{"0", "-1", "abc", "3.5", "10x"} {
		_, err := parseTokenLimit(in, "context tokens")
		if err == nil {
			t.Errorf("%q should have errored", in)
			continue
		}
		// Error message points the user at the blank-=-default escape hatch.
		if !strings.Contains(err.Error(), "blank") {
			t.Errorf("error for %q should mention 'blank for the CLI's default'; got %v", in, err)
		}
	}
}

func TestParseTokenLimits_CrossCheckOnlyWhenBothSet(t *testing.T) {
	// Both blank → both zero, no constraint check.
	c, o, err := parseTokenLimits("", "")
	if err != nil || c != 0 || o != 0 {
		t.Errorf("(blank, blank) → (%d, %d, %v), want (0, 0, nil)", c, o, err)
	}

	// Only context set → no constraint on output (output stays 0 = unset).
	c, o, err = parseTokenLimits("4096", "")
	if err != nil || c != 4096 || o != 0 {
		t.Errorf("('4096', '') → (%d, %d, %v), want (4096, 0, nil)", c, o, err)
	}

	// Only output set → constraint doesn't apply (context is 0 = unset, the
	// CLI's own context default is in force).
	c, o, err = parseTokenLimits("", "2048")
	if err != nil || c != 0 || o != 2048 {
		t.Errorf("('', '2048') → (%d, %d, %v), want (0, 2048, nil) — output > context check should NOT trigger when context is unset", c, o, err)
	}

	// Both set + valid (output < context) → ok.
	c, o, err = parseTokenLimits("8192", "1024")
	if err != nil || c != 8192 || o != 1024 {
		t.Errorf("('8192', '1024') → (%d, %d, %v), want (8192, 1024, nil)", c, o, err)
	}

	// Both set + output > context → error.
	_, _, err = parseTokenLimits("1024", "8192")
	if err == nil {
		t.Error("output > context should error when both are explicitly set")
	}
}
