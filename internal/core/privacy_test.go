package core

import (
	"strings"
	"testing"
)

func TestPrivacy_DetectsOpenAIKey(t *testing.T) {
	p := NewPrivacyScanner()
	text := "use sk-abcdefghijklmnopqrstuvwxyz0123456789 to call the API"
	hits := p.Match(text)
	if len(hits) != 1 || hits[0].Category != CatOpenAIKey {
		t.Fatalf("expected 1 OPENAI_KEY, got %+v", hits)
	}
}

func TestPrivacy_DetectsAnthropicKey(t *testing.T) {
	p := NewPrivacyScanner()
	hits := p.Match("export ANTHROPIC_API_KEY=sk-ant-api03-abcdefghijklmnopqrstuv")
	if len(hits) != 1 || hits[0].Category != CatAnthropicKey {
		t.Fatalf("expected 1 ANTHROPIC_KEY, got %+v", hits)
	}
}

func TestPrivacy_DetectsGitHubPAT(t *testing.T) {
	p := NewPrivacyScanner()
	hits := p.Match("token: ghp_abcdefghijklmnopqrstuvwxyz0123456789ABCD")
	if len(hits) != 1 || hits[0].Category != CatGitHubToken {
		t.Fatalf("expected 1 GITHUB_TOKEN, got %+v", hits)
	}
}

func TestPrivacy_DetectsAWSAccessKey(t *testing.T) {
	p := NewPrivacyScanner()
	hits := p.Match("aws id is AKIAIOSFODNN7EXAMPLE here")
	if len(hits) != 1 || hits[0].Category != CatAWSAccessID {
		t.Fatalf("expected AWS_ACCESS_KEY hit, got %+v", hits)
	}
}

func TestPrivacy_DetectsSSN(t *testing.T) {
	p := NewPrivacyScanner()
	hits := p.Match("his SSN is 123-45-6789 according to the form")
	if len(hits) != 1 || hits[0].Category != CatSSN {
		t.Fatalf("expected SSN hit, got %+v", hits)
	}
}

func TestPrivacy_RejectsInvalidSSNPrefixes(t *testing.T) {
	p := NewPrivacyScanner()
	for _, bad := range []string{"000-12-3456", "666-12-3456", "900-12-3456"} {
		if hits := p.Match(bad); len(hits) != 0 {
			t.Errorf("invalid SSN %q should not match, got %+v", bad, hits)
		}
	}
}

func TestPrivacy_DetectsCreditCard_LuhnPasses(t *testing.T) {
	p := NewPrivacyScanner()
	// Visa test number — Luhn-valid.
	hits := p.Match("card: 4111-1111-1111-1111 expires soon")
	if len(hits) != 1 || hits[0].Category != CatCreditCard {
		t.Fatalf("expected 1 CREDIT_CARD, got %+v", hits)
	}
}

func TestPrivacy_RejectsCreditCard_LuhnFails(t *testing.T) {
	p := NewPrivacyScanner()
	// Looks like a CC but fails Luhn.
	hits := p.Match("not-a-card 1234-5678-9012-3456")
	for _, h := range hits {
		if h.Category == CatCreditCard {
			t.Fatalf("Luhn-invalid number should not match: %+v", h)
		}
	}
}

func TestPrivacy_DetectsBearerToken(t *testing.T) {
	p := NewPrivacyScanner()
	hits := p.Match("Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6")
	if len(hits) != 1 || hits[0].Category != CatBearer {
		t.Fatalf("expected BEARER, got %+v", hits)
	}
}

func TestPrivacy_DetectsPrivateKeyPEM(t *testing.T) {
	p := NewPrivacyScanner()
	hits := p.Match("-----BEGIN RSA PRIVATE KEY-----\nMIIE...")
	if len(hits) != 1 || hits[0].Category != CatPrivateKey {
		t.Fatalf("expected PRIVATE_KEY, got %+v", hits)
	}
}

func TestPrivacy_CleanTextProducesNoMatches(t *testing.T) {
	p := NewPrivacyScanner()
	hits := p.Match("nothing sensitive here — just plain prose about clouds")
	if len(hits) != 0 {
		t.Fatalf("expected no hits, got %+v", hits)
	}
}

func TestPrivacy_Redact_ReplacesWithPlaceholder(t *testing.T) {
	p := NewPrivacyScanner()
	text := "send sk-aaaaaaaaaaaaaaaaaaaaaaaaaa now"
	scrubbed, matches := p.Redact(text)
	if !strings.Contains(scrubbed, "[OPENAI_KEY_1]") {
		t.Fatalf("placeholder missing: %q", scrubbed)
	}
	if strings.Contains(scrubbed, "sk-aaaa") {
		t.Fatalf("original secret leaked: %q", scrubbed)
	}
	if len(matches) != 1 || matches[0].Placeholder != "[OPENAI_KEY_1]" {
		t.Fatalf("match metadata wrong: %+v", matches)
	}
}

func TestPrivacy_Redact_NumbersByCategory(t *testing.T) {
	p := NewPrivacyScanner()
	text := "k1=sk-aaaaaaaaaaaaaaaaaaaaaaaaa and k2=sk-bbbbbbbbbbbbbbbbbbbbbbbbb"
	scrubbed, matches := p.Redact(text)
	if !strings.Contains(scrubbed, "[OPENAI_KEY_1]") || !strings.Contains(scrubbed, "[OPENAI_KEY_2]") {
		t.Fatalf("expected _1 and _2 placeholders, got %q", scrubbed)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
}

func TestPrivacy_Reveal_ReversesRedaction(t *testing.T) {
	p := NewPrivacyScanner()
	original := "use sk-abcdefghijklmnopqrstuvwxyzz in production"
	scrubbed, matches := p.Redact(original)
	revealed := p.Reveal(scrubbed, matches)
	if revealed != original {
		t.Fatalf("reveal didn't reverse: original=%q revealed=%q", original, revealed)
	}
}

func TestPrivacy_CustomPattern(t *testing.T) {
	p := NewPrivacyScanner()
	if err := p.AddCustomPattern(`internal-\d{6}`); err != nil {
		t.Fatalf("AddCustomPattern: %v", err)
	}
	hits := p.Match("ticket internal-987654 needs triage")
	if len(hits) != 1 || hits[0].Category != CatCustom {
		t.Fatalf("expected CUSTOM hit, got %+v", hits)
	}
}

func TestPrivacy_CustomPattern_InvalidErrors(t *testing.T) {
	p := NewPrivacyScanner()
	if err := p.AddCustomPattern(`[invalid`); err == nil {
		t.Fatal("expected compile error for invalid regex")
	}
}

func TestPrivacy_OverlappingMatches_LeftmostLongestWins(t *testing.T) {
	// `Bearer sk-...` could match both BEARER and OPENAI_KEY on the
	// inner sk- substring; leftmost-longest should keep just BEARER.
	p := NewPrivacyScanner()
	text := "Authorization: Bearer sk-aaaaaaaaaaaaaaaaaaaaaaaaa"
	hits := p.Match(text)
	if len(hits) != 1 || hits[0].Category != CatBearer {
		t.Fatalf("expected single BEARER, got %+v", hits)
	}
}
