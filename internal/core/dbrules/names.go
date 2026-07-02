package dbrules

import "strings"

// This file holds the name-heuristic machinery shared by the slice-2b rules
// (DB-051/052/053/003). Name matching is case-insensitive and by NAME COMPONENT
// (camelCase / snake_case tokenized), never raw substring — so passwordResetCount
// does not match "password" as a whole word and then survive on type alone.
//
// The token/hint sets live here in the core, beside the rules, so every future ORM
// provider inherits them. They are NOT configurable today (YAGNI). Extension point
// (do not build now): a future sensors.db config could ADD project tokens to these
// sets — never replace the core set.

// sensitiveTokens are the single-component and joined-pair names that flag a
// column as possibly holding a secret. Multi-word names are matched via adjacent
// component pairs (api+key -> "apikey"), and "token" as a component covers
// refreshToken/accessToken/sessionToken/idToken.
var sensitiveTokens = map[string]bool{
	"password": true, "passwd": true, "pwd": true, "secret": true,
	"apikey": true, "token": true, "accesstoken": true, "refreshtoken": true,
	"privatekey": true, "ssn": true, "dni": true,
	"creditcard": true, "cardnumber": true, "cvv": true, "cvc": true,
}

// encryptionHints are name components that suggest the value is already protected.
// Exposed as a FACT (encryption_hint_in_name), never used to suppress a finding
// (ADR 0017): a name is not a guarantee, and hiding a possible plaintext secret
// behind a name would be a silent false negative.
var encryptionHints = map[string]bool{
	"hash": true, "hashed": true, "encrypted": true, "enc": true, "digest": true,
}

// tokenizeWords splits an identifier into lowercase alphabetic components, breaking
// on '_'/'-', on lower/digit -> upper transitions, and on letter <-> digit
// transitions. Trailing/embedded digits are dropped from the word list (digit
// grouping for repeating-group detection is handled separately).
func tokenizeWords(name string) []string {
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
			if i > 0 && (isLowerOrDigit(runes[i-1])) {
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

// matchSensitiveToken reports whether a column name contains a sensitive token as
// a component or as an adjacent component pair, returning the matched token.
func matchSensitiveToken(name string) (string, bool) {
	comps := tokenizeWords(name)
	for _, c := range comps {
		if sensitiveTokens[c] {
			return c, true
		}
	}
	for i := 0; i+1 < len(comps); i++ {
		if joined := comps[i] + comps[i+1]; sensitiveTokens[joined] {
			return joined, true
		}
	}
	return "", false
}

// hasEncryptionHint reports whether any name component is an encryption hint.
func hasEncryptionHint(name string) bool {
	for _, c := range tokenizeWords(name) {
		if encryptionHints[c] {
			return true
		}
	}
	return false
}
