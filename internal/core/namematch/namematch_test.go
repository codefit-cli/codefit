package namematch_test

import (
	"maps"
	"slices"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/namematch"
)

func TestComponents(t *testing.T) {
	cases := map[string][]string{
		"passwordChangedAt": {"password", "changed", "at"},
		"password_reset":    {"password", "reset"},
		"apiKey":            {"api", "key"},
		"api_key":           {"api", "key"},
		"API_KEY":           {"api", "key"},
		"SIGNING_KEY":       {"signing", "key"},
		"refreshToken":      {"refresh", "token"},
		"SSN":               {"ssn"},
		"password":          {"password"},
		"phone1":            {"phone"},
		"signing-key":       {"signing", "key"},
		// The measured shape the false affirmation lived on: a consecutive-caps
		// run is NOT split, which is exactly why CategoryDWDimensionNoSurrogateKey
		// yields "dwdimension" rather than "dw"+"dimension".
		"CategoryDWDimensionNoSurrogateKey": {"category", "dwdimension", "no", "surrogate", "key"},
		// The declared limit, as data: an all-lowercase concatenation is ONE
		// component. There is no boundary to find, so nothing can split it.
		"secretkey": {"secretkey"},
	}
	for in, want := range cases {
		if got := namematch.Components(in); !slices.Equal(got, want) {
			t.Errorf("Components(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestMatchSetScanOrder locks the order that makes the matched token EVIDENCE:
// all components in order, THEN all adjacent pairs, over ONE set. Any other
// order returns a different token for the same boolean.
func TestMatchSetScanOrder(t *testing.T) {
	set := map[string]bool{"apikey": true, "token": true, "cvv": true, "ssn": true, "password": true}
	cases := []struct {
		name  string
		token string
		ok    bool
	}{
		// A pair only wins when no single component matched first.
		{"apiKey", "apikey", true},
		{"API_KEY", "apikey", true},
		// Components beat pairs: "cvv" is a component of apiKeyCvv, so it wins
		// over the api+key pair even though the pair also matches.
		{"apiKeyCvv", "cvv", true},
		{"ssnPassword", "ssn", true},
		{"passwordSsn", "password", true},
		// Substring is never a match: "token" is not a component of "tokenizer".
		{"tokenizer", "", false},
		{"monkeyIndex", "", false},
		{"keyboard", "", false},
	}
	for _, c := range cases {
		tok, ok := namematch.MatchSet(c.name, set)
		if tok != c.token || ok != c.ok {
			t.Errorf("MatchSet(%q) = (%q, %v), want (%q, %v)", c.name, tok, ok, c.token, c.ok)
		}
	}
}

// TestCredentialSet pins SEC-001's vocabulary exactly. A silent widening is as
// much a regression as a silent narrowing: this set feeds an AFFIRMATION
// channel that tells a user "this looks like a hardcoded credential" at
// Confidence 1.0, so every member must be a deliberate, recorded decision.
func TestCredentialSet(t *testing.T) {
	want := []string{
		"accesskey", "accesstoken", "apikey", "encryptionkey", "passwd",
		"password", "privatekey", "pwd", "refreshtoken", "secret",
		"signingkey", "token",
	}
	got := slices.Sorted(maps.Keys(namematch.Credential()))
	if !slices.Equal(got, want) {
		t.Errorf("Credential() = %v, want %v", got, want)
	}
}

// TestSecurityValueSet pins SEC-050's OWN vocabulary. It is crypto material,
// not credentials, and must never become an alias of Credential(): a bare "key"
// component is admissible here only because SEC-050 additionally requires a
// math/rand call, a guard SEC-001 does not have.
func TestSecurityValueSet(t *testing.T) {
	want := []string{"iv", "key", "nonce", "password", "salt", "secret", "session", "token"}
	got := slices.Sorted(maps.Keys(namematch.SecurityValue()))
	if !slices.Equal(got, want) {
		t.Errorf("SecurityValue() = %v, want %v", got, want)
	}
}

// TestDB053UnionFrozen is the frozen golden: the EXACT 15 names of
// dbrules.sensitiveTokens at main 0fb211d, name for name. DB-053 is
// load-bearing across 29 measured corpora (ADR 0047) whose corpora are no
// longer cloned, so the union is not allowed to move as a side effect of
// SEC-001 vocabulary work. Widening either contributing subset alone fails here.
func TestDB053UnionFrozen(t *testing.T) {
	want := []string{
		"apikey", "accesstoken", "cardnumber", "creditcard", "cvc", "cvv",
		"dni", "passwd", "password", "privatekey", "pwd", "refreshtoken",
		"secret", "ssn", "token",
	}
	slices.Sort(want)
	got := slices.Sorted(maps.Keys(namematch.DB053Union()))
	if !slices.Equal(got, want) {
		t.Errorf("DB053Union() = %v, want %v", got, want)
	}
	if len(want) != 15 {
		t.Fatalf("the frozen union is 15 names, got %d — the golden itself was edited", len(want))
	}
}

// TestSecurityOnlyTokensAreNotInTheUnion is the structural half of D2: the
// three restorations SEC-001 needs (accesskey, signingkey, encryptionkey) must
// reach Credential() and must NOT reach DB053Union(). A two-set split would
// make every SEC-001 widening a DB-053 change; this asserts the split holds
// rather than trusting that it was implemented that way.
func TestSecurityOnlyTokensAreNotInTheUnion(t *testing.T) {
	union := namematch.DB053Union()
	cred := namematch.Credential()
	for _, tok := range []string{"accesskey", "signingkey", "encryptionkey"} {
		if !cred[tok] {
			t.Errorf("Credential() is missing %q — Arm B affirms it today at Confidence 1.0; dropping it is a silent miss", tok)
		}
		if union[tok] {
			t.Errorf("DB053Union() contains %q — a SEC-001-only token leaked into DB-053's frozen vocabulary", tok)
		}
	}
	// publickey is deliberately excluded: a public key is not a credential.
	if cred["publickey"] {
		t.Error("Credential() contains \"publickey\" — a public key is not a credential")
	}
}

// TestLimitIsDeclaredNotEmpty guards the limit's SOURCE. An empty or
// placeholder const would render as a blank caveat in codefit-coverage, which
// reads to an agent as "no limit" — worse than no claim at all.
func TestLimitIsDeclaredNotEmpty(t *testing.T) {
	if len(namematch.LimitLowercaseConcatenation) < 40 {
		t.Fatalf("LimitLowercaseConcatenation is %q — too short to state a limit",
			namematch.LimitLowercaseConcatenation)
	}
	// The limit must be true of the code beside it, not merely prose. This is
	// the shape it declares.
	if _, ok := namematch.MatchSet("secretkey", namematch.Credential()); ok {
		t.Error("secretkey matches — then the declared limit is describing a gap that does not exist")
	}
	if _, ok := namematch.MatchSet("secret_key", namematch.Credential()); !ok {
		t.Error("secret_key does NOT match — the limit is about the concatenated spelling only, not the delimited one")
	}
}
