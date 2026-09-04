package namematch_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/namematch"
)

// SPEC (issue #152) — "credential" joins SEC-001's vocabulary, and ONLY
// SEC-001's.
//
// WHY IT IS MISSING TODAY, and why nobody noticed. TypeScript's SEC-001 carries
// a literal `credential` alternative inside its unanchored $NAME regex, so
// `const credential = "abc123"` fires there. Go matches by component against
// this package's set, which has no such member, so the same declaration is
// SILENT in Go. The cross-provider case table already records that divergence —
// it is declared, not hidden — but declaring a false all-clear does not stop it
// being one.
//
// Closing the divergence is what makes #152 fixable at all. TypeScript's fix is
// to stop matching $NAME by raw substring (which reports `tokenizer` as a
// credential at Confidence 1.0) and match by COMPONENT like Go. Measured across
// 41 names, that switch kills 9 false positives and costs exactly ONE true
// positive: `credential`, the one spelling the regex carried and the set does
// not. Adding it here is what turns a narrowing into a pure repair, and it
// fixes Go's silent gap at the same time.
//
// IT GOES IN securityOnlyTokens, NOT credentialShared, and that placement is
// the whole point of the three-set split. credentialShared feeds DB053Union(),
// frozen name for name across 29 corpora (ADR 0047) that can no longer be
// re-measured. A credential is not a DB-053 sensitive column name, and DB-053
// never asked for this. The test below proves the union did not move rather
// than assuming it.
func TestCredentialIsACredentialComponent(t *testing.T) {
	cred := namematch.Credential()
	cases := []struct {
		name string
		want bool
		why  string
	}{
		{"credential", true, "the bare spelling the TypeScript regex carried and the set did not"},
		{"credentials", true, "the plural fold must cover it like every other member"},
		{"apiCredential", true, "as a camelCase component"},
		{"CLIENT_CREDENTIAL", true, "as a SCREAMING_SNAKE component"},
		{"credentials_note", true, "a credential name with a suffix is still a credential name"},

		{"credentialing", false, "a component in its own right, not the credential word"},
		{"credenza", false, "shares a prefix and nothing else"},
	}
	for _, tc := range cases {
		tok, got := namematch.MatchSet(tc.name, cred)
		if got != tc.want {
			t.Errorf("MatchSet(%q) = %v (token %q), want %v — %s", tc.name, got, tok, tc.want, tc.why)
		}
	}
}

// The containment half. Adding a member to SEC-001's vocabulary must not move
// DB-053's, and the only thing that guarantees it is asserting it: a member
// placed in credentialShared by mistake would satisfy the test above and
// silently widen a rule measured against corpora nobody can clone again.
func TestCredentialTokenStaysOutOfDB053(t *testing.T) {
	for _, tok := range []string{"credential", "credentials"} {
		if namematch.DB053Union()[tok] {
			t.Errorf("DB053Union() contains %q — the SEC-001 addition leaked into DB-053's frozen vocabulary", tok)
		}
		if namematch.SecurityValue()[tok] {
			t.Errorf("SecurityValue() contains %q — a credential is not crypto material; SEC-050 never asked for this", tok)
		}
	}
}
