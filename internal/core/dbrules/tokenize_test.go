package dbrules

import "testing"

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTokenizeWords(t *testing.T) {
	cases := map[string][]string{
		"passwordChangedAt": {"password", "changed", "at"},
		"password_reset":    {"password", "reset"},
		"apiKey":            {"api", "key"},
		"api_key":           {"api", "key"},
		"refreshToken":      {"refresh", "token"},
		"SSN":               {"ssn"},
		"password":          {"password"},
		"phone1":            {"phone"}, // trailing digits split off
	}
	for in, want := range cases {
		if got := tokenizeWords(in); !eq(got, want) {
			t.Errorf("tokenizeWords(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestMatchSensitiveToken(t *testing.T) {
	fire := []string{"password", "apiKey", "api_key", "refreshToken", "accessToken", "creditCard", "ssn", "secret"}
	for _, n := range fire {
		if _, ok := matchSensitiveToken(n); !ok {
			t.Errorf("matchSensitiveToken(%q) should match", n)
		}
	}
	// These must NOT match as sensitive tokens (component-based, no substring FP).
	noFire := []string{"bio", "keyboard", "description", "isPublic", "count"}
	for _, n := range noFire {
		if tok, ok := matchSensitiveToken(n); ok {
			t.Errorf("matchSensitiveToken(%q) should NOT match, got %q", n, tok)
		}
	}
}

func TestHasEncryptionHint(t *testing.T) {
	for _, n := range []string{"passwordHash", "tokenHashed", "encryptedSecret", "digest"} {
		if !hasEncryptionHint(n) {
			t.Errorf("hasEncryptionHint(%q) should be true", n)
		}
	}
	for _, n := range []string{"password", "apiKey", "sequence"} { // "sequence" contains "enc" as substring but not as a component
		if hasEncryptionHint(n) {
			t.Errorf("hasEncryptionHint(%q) should be false", n)
		}
	}
}
