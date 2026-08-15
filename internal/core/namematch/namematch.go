package namematch

import "strings"

// LimitLowercaseConcatenation is the DECLARED gap component matching cannot
// close, and the reason it is declared rather than fixed. It lives here, beside
// the tokenizer that causes it, so the claim and its cause cannot drift apart:
// a consumer cites this const by reference and a compile-time dependency has no
// way to go stale. Copying the text into a provider, a manifest, or COVERAGE.md
// would create a second place to be wrong.
const LimitLowercaseConcatenation = "SEC-001 (go) matches by NAME COMPONENT " +
	"(camelCase/snake_case/kebab-case tokenized, with adjacent-pair joining), " +
	"never by raw substring. An ALL-LOWERCASE CONCATENATION carries no boundary " +
	"to tokenize on, so `secretkey := \"...\"` is NOT reported, while `secretKey`, " +
	"`secret_key` and `SECRET_KEY` are. This is a known under-detection, declared " +
	"rather than closed: the substring matcher it replaced reported enum " +
	"constants as hardcoded credentials at Confidence 1.0, and a false " +
	"affirmation is a worse failure than a declared gap."

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
// SEC-001-only restorations, and never the PII names.
func Credential() map[string]bool { return union(credentialShared, securityOnlyTokens) }

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
