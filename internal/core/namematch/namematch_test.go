package namematch_test

import (
	"maps"
	"slices"
	"strings"
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
// The set is PLURAL-FOLDED, and the fold is pinned here in full rather than
// computed: computing the expectation from the same helper the code uses would
// assert nothing. Twelve tokens, each with its regular plural, is what the fold
// is allowed to produce — a fourteenth stem or a second inflection rule shows up
// here as a diff.
//
// "credential" is the thirteenth stem, admitted by issue #152 with a measurement
// rather than by inference: it is the one true positive TypeScript would have
// lost by switching $NAME from substring to component matching, and it closes a
// divergence the cross-provider table had already declared.
func TestCredentialSet(t *testing.T) {
	want := []string{
		"accesskey", "accesskeys", "accesstoken", "accesstokens",
		"apikey", "apikeys", "credential", "credentials",
		"encryptionkey", "encryptionkeys",
		"passwd", "passwds", "password", "passwords",
		"privatekey", "privatekeys", "pwd", "pwds",
		"refreshtoken", "refreshtokens", "secret", "secrets",
		"signingkey", "signingkeys", "token", "tokens",
	}
	got := slices.Sorted(maps.Keys(namematch.Credential()))
	if !slices.Equal(got, want) {
		t.Errorf("Credential() = %v, want %v", got, want)
	}
}

// TestPluralFoldIsSEC001Only is the containment half of the plural repair, and
// it is the one that keeps DB-053 out of it.
//
// The fold answers a measured SEC-001 narrowing: substring matching used to
// carry "passwords" for free and component matching does not. DB-053's
// vocabulary is frozen name for name against main 0fb211d across 29 measured
// corpora (ADR 0047) that are no longer available to re-measure, and SEC-050's
// set is crypto material with its own scan. Neither asked for this fold, so
// neither may receive it as a side effect — a plural leaking into DB053Union()
// would move a rule this change never measured.
func TestPluralFoldIsSEC001Only(t *testing.T) {
	cred := namematch.Credential()
	union := namematch.DB053Union()
	secval := namematch.SecurityValue()

	for _, tok := range []string{"passwords", "secrets", "tokens", "apikeys", "privatekeys", "credentials"} {
		if !cred[tok] {
			t.Errorf("Credential() is missing %q — the plural fired before component matching and its loss was undeclared", tok)
		}
		if union[tok] {
			t.Errorf("DB053Union() contains %q — the SEC-001 plural fold leaked into DB-053's frozen vocabulary", tok)
		}
		if secval[tok] {
			t.Errorf("SecurityValue() contains %q — the SEC-001 plural fold leaked into SEC-050's crypto vocabulary", tok)
		}
	}
	// The fold widens SPELLINGS, never the vocabulary: a stem that was
	// deliberately refused does not get in through its plural.
	for _, tok := range []string{"publickey", "publickeys", "keys", "ssns"} {
		if cred[tok] {
			t.Errorf("Credential() contains %q — the fold admitted a stem the vocabulary refuses", tok)
		}
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

// TestLimitTextIsTrueOfTheCode binds the DECLARED limit to the behaviour it
// declares, in both directions and example by example.
//
// codefit-coverage renders this const verbatim to an agent, which has no way to
// check it: whatever it says about SEC-001 is what the agent believes. So each
// example is asserted twice — that the const really names it (a silently edited
// const cannot drift away from this control) and that the matcher really behaves
// that way (the const cannot describe a gap that does not exist, nor hide one
// that does). The previous text scoped the losses to names carrying "key" with a
// long value; six of the nine measured losses carried no "key" at all and eight
// stopped at any length, which is precisely the kind of over-narrow claim this
// control now makes impossible to leave standing.
func TestLimitTextIsTrueOfTheCode(t *testing.T) {
	cred := namematch.Credential()
	limit := namematch.LimitLowercaseConcatenation

	// Named by the const as NOT reported: the all-lowercase concatenations.
	for _, name := range []string{"secretkey", "dbpassword", "mypassword", "authtoken"} {
		if !strings.Contains(limit, "`"+name+"`") {
			t.Errorf("the declared limit no longer names %q as an example, but this control still asserts it — the const and its proof drifted apart", name)
		}
		if tok, ok := namematch.MatchSet(name, cred); ok {
			t.Errorf("%q matches (on %q), yet the declared limit tells an agent it is NOT reported", name, tok)
		}
	}
	// Named by the const as reported: the delimited spellings and the plurals.
	for _, name := range []string{
		"secretKey", "secret_key", "SECRET_KEY", "db_password", "myPassword",
		"auth_token", "passwords", "apiKeys", "userPasswords",
	} {
		if !strings.Contains(limit, "`"+name+"`") {
			t.Errorf("the declared limit no longer names %q as an example, but this control still asserts it — the const and its proof drifted apart", name)
		}
		if _, ok := namematch.MatchSet(name, cred); !ok {
			t.Errorf("%q does NOT match, yet the declared limit tells an agent it IS reported", name)
		}
	}
}
