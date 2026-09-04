package namematch

import "strings"

// LimitLowercaseConcatenation is the DECLARED gap component matching cannot
// close, and the reason it is declared rather than fixed. It lives here, beside
// the tokenizer that causes it, so the claim and its cause cannot drift apart:
// a consumer cites this const by reference and a compile-time dependency has no
// way to go stale. Copying the text into a provider, a manifest, or COVERAGE.md
// would create a second place to be wrong.
const LimitLowercaseConcatenation = "SEC-001 (go) matches by NAME COMPONENT " +
	"(camelCase/snake_case/kebab-case tokenized, with adjacent-pair joining and " +
	"regular +s plurals, so `passwords`, `apiKeys` and `userPasswords` are " +
	"reported), never by raw substring and never by value length. THE GAP: an " +
	"ALL-LOWERCASE CONCATENATION carries no boundary to tokenize on, so " +
	"`secretkey`, `dbpassword`, `mypassword` and `authtoken` are NOT reported, " +
	"while `secretKey`, `secret_key`, `SECRET_KEY`, `db_password`, `myPassword` " +
	"and `auth_token` are. Only the regular +s plural is folded: no other " +
	"inflection is recognised. This is a known under-detection, declared rather " +
	"than closed: the substring matcher it replaced reported enum constants as " +
	"hardcoded credentials at Confidence 1.0, and a false affirmation is a worse " +
	"failure than a declared gap."

// credentialShared are the credential names BOTH SEC-001 and DB-053 recognise.
// It is the intersection by construction, not by coincidence — see the three-set
// split below.
var credentialShared = map[string]bool{
	"password": true, "passwd": true, "pwd": true, "secret": true,
	"apikey": true, "token": true, "accesstoken": true, "refreshtoken": true,
	"privatekey": true,
}

// piiShared are the personal-identifier names only DB-053 recognises. An SSN is
// not a credential, and SEC-001 is an affirmation channel that says "this looks
// like a hardcoded credential" — so SEC-001 must never consume these.
var piiShared = map[string]bool{
	"ssn": true, "dni": true, "creditcard": true,
	"cardnumber": true, "cvv": true, "cvc": true,
}

// securityOnlyTokens are credential names only SEC-001 recognises. Every one is
// a RESTORATION of measured current behaviour, not a speculative widening: the
// deleted bare-"key" arm (strings.Contains(name,"key") && len(value) >= 16)
// affirms all three today at Confidence 1.0, and TypeScript's SEC-001 $NAME
// regex already carries accesskey. Omitting them would trade one false
// affirmation for three silent misses.
//
// publickey is deliberately absent, with its reason recorded: a public key is
// not a credential, so admitting it would be the speculative widening these
// three are not.
var securityOnlyTokens = map[string]bool{
	"accesskey": true, "signingkey": true, "encryptionkey": true,
	// "credential" lives here and NOT in credentialShared because a credential
	// is not a DB-053 sensitive column name, and credentialShared feeds
	// DB053Union(), frozen across 29 corpora (ADR 0047) nobody can re-measure.
	// It closes a divergence the cross-provider table already declared: the
	// TypeScript regex carried a literal "credential" alternative that this set
	// never had, so `const credential = "abc123"` fired in TS and was SILENT in
	// Go. Measured over 41 names, it is the ONE true positive TypeScript would
	// have lost by switching from substring to component matching (issue #152).
	"credential": true,
}

// securityValueTokens is SEC-050's OWN vocabulary: crypto MATERIAL, not
// credentials. It is kept verbatim from the matcher it replaces, because only
// the matching CONVENTION is changing here, not what SEC-050 looks for.
//
// A bare "key" component is admissible here and would not be in Credential():
// SEC-050 additionally requires the value to come from a math/rand call, a
// guard SEC-001 has no equivalent of.
var securityValueTokens = map[string]bool{
	"token": true, "secret": true, "nonce": true, "salt": true,
	"key": true, "password": true, "iv": true, "session": true,
}

// Credential is the vocabulary SEC-001 consumes: credentialShared plus the
// SEC-001-only restorations, PLURAL-FOLDED, and never the PII names.
//
// The fold is SEC-001's alone. DB053Union() and SecurityValue() are deliberately
// not folded: DB-053's vocabulary is frozen name for name against main 0fb211d
// across 29 measured corpora (ADR 0047), and folding it here would move it as a
// side effect of credential work — the exact coupling the three-set split exists
// to prevent.
func Credential() map[string]bool {
	return withPlurals(union(credentialShared, securityOnlyTokens))
}

// withPlurals adds the regular English plural of every member of set, and
// nothing else.
//
// WHY IT EXISTS. Component matching replaced a raw strings.Contains, and
// substring matching had been carrying the plural spellings for free:
// "passwords" contains "password". Tokenizing turns "passwords" into ONE
// component that is not in the set, so passwords, secrets, tokens, apiKeys,
// apikeys, privateKeys, refreshTokens, mySecrets and userPasswords all went
// silent — measured on the real AnalyzeSecurity path against the pre-change
// tree, and eight of the nine at ANY value length. `var passwords = []string{…}`
// is ordinary Go; losing it undeclared is a narrowing this project's doctrine
// does not permit, and the false-positive fix never required it.
//
// WHY IT IS A SUFFIX ADD AND NOT A SUFFIX STRIP. The two are equivalent in
// effect — a component matches iff it is a member or a member plus "s" — but
// only this direction is ENUMERABLE. The folded set is a value a test can print
// and pin (TestCredentialSet), so a widening cannot arrive unnoticed, and the
// cross-provider case table's Control 3 forces every folded token to carry a
// declared TypeScript verdict. A strip is a predicate: it admits an open set of
// names and nothing can enumerate what it just accepted.
//
// WHAT IT DELIBERATELY DOES NOT DO. No "-es"/"-ies" handling: no member of this
// vocabulary ends in s, x, z, ch, sh or a consonant+y, so those rules would add
// nothing but reach. No stemming, no singularisation, no inflection beyond this
// one suffix. publickey is absent from the set, so publicKeys stays silent too —
// the fold widens the SPELLINGS of the vocabulary, never the vocabulary.
func withPlurals(set map[string]bool) map[string]bool {
	out := make(map[string]bool, len(set)*2)
	for k := range set {
		out[k] = true
		out[k+"s"] = true
	}
	return out
}

// DB053Union is the vocabulary DB-053 and DB-020 consume: credentialShared plus
// piiShared, and NOTHING else. This is the whole reason the split is three sets
// rather than two — with a two-set split (credential ⊂ union) every SEC-001
// widening would silently become a DB-053 change across 29 measured corpora
// (ADR 0047) that are no longer available to re-measure against. Here, DB-053's
// vocabulary is provably byte-identical while SEC-001's moves.
func DB053Union() map[string]bool { return union(credentialShared, piiShared) }

// SecurityValue is the vocabulary SEC-050 consumes.
func SecurityValue() map[string]bool { return union(securityValueTokens, nil) }

// union returns a fresh map so a caller cannot mutate the package's sets.
func union(a, b map[string]bool) map[string]bool {
	out := make(map[string]bool, len(a)+len(b))
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}

// Components splits an identifier into lowercase alphabetic components,
// breaking on '_'/'-', on lower/digit -> upper transitions, and on
// letter <-> digit transitions. Digits are separators and are dropped from the
// word list.
//
// A consecutive run of capitals is NOT split (DWDimension -> "dwdimension"),
// because without a dictionary there is no boundary to find inside it. That is
// not a wart: it is the property that makes CategoryDWDimensionNoSurrogateKey
// tokenize into names none of which is a credential.
func Components(name string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, strings.ToLower(cur.String()))
			cur.Reset()
		}
	}
	runes := []rune(name)
	for i, r := range runes {
		switch {
		case r == '_' || r == '-':
			flush()
		case r >= '0' && r <= '9':
			flush() // digits are separators for the word list
		case r >= 'A' && r <= 'Z':
			// boundary before an uppercase that follows a lowercase or digit
			if i > 0 && isLowerOrDigit(runes[i-1]) {
				flush()
			}
			cur.WriteRune(r)
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

func isLowerOrDigit(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

// MatchSet reports whether name carries a member of set as a name COMPONENT or
// as an adjacent component PAIR, returning the matched token.
//
// The scan order is part of the contract, not an implementation detail: ALL
// components in order, THEN all adjacent pairs, over ONE set. The returned
// token is EVIDENCE — it reaches the user inside a finding's message — so a
// different order is a different answer even when the boolean is unchanged.
// ssnPassword returns "ssn" under this order and "password" under a
// credential-first two-pass; apiKeyCvv returns "cvv" here because a single
// component outranks the api+key pair. Both are locked by tests.
func MatchSet(name string, set map[string]bool) (string, bool) {
	comps := Components(name)
	for _, c := range comps {
		if set[c] {
			return c, true
		}
	}
	for i := 0; i+1 < len(comps); i++ {
		if joined := comps[i] + comps[i+1]; set[joined] {
			return joined, true
		}
	}
	return "", false
}
