package dbrules

import "github.com/codefit-cli/codefit/internal/core/namematch"

// This file holds the name-heuristic machinery shared by the slice-2b rules
// (DB-051/052/053/003). Name matching is case-insensitive and by NAME COMPONENT
// (camelCase / snake_case tokenized), never raw substring — so passwordResetCount
// does not match "password" as a whole word and then survive on type alone.
//
// The tokenizer and the sensitive-name vocabulary NO LONGER live here. They moved
// to internal/core/namematch, a stdlib-only leaf, because the same question —
// "does this identifier name denote a secret?" — is also asked by the Go
// provider's SEC-001 and SEC-050, which were answering it with raw substring
// matching and affirming enum constants as hardcoded credentials. One matcher
// with per-consumer vocabularies replaces three drifting copies (ADR 0075).
//
// What DB-053 consumes is namematch.DB053Union(), frozen name-for-name against
// main 0fb211d and locked by two controls: a verdict golden over the rules' own
// fixture names (identity_test.go) and the union golden in the leaf. SEC-001's
// vocabulary can grow without moving DB-053's.
//
// The hint set below stays here: it is DB-specific and has no second consumer.
// Extension point (do not build now): a future sensors.db config could ADD
// project tokens — never replace the core set.

// sensitiveTokens is the vocabulary DB-053 and DB-020 match against. Resolved
// once rather than per call: namematch returns a fresh map so callers cannot
// mutate the leaf's sets, and these rules ask per column.
var sensitiveTokens = namematch.DB053Union()

// encryptionHints are name components that suggest the value is already protected.
// Exposed as a FACT (encryption_hint_in_name), never used to suppress a finding
// (ADR 0017): a name is not a guarantee, and hiding a possible plaintext secret
// behind a name would be a silent false negative.
var encryptionHints = map[string]bool{
	"hash": true, "hashed": true, "encrypted": true, "enc": true, "digest": true,
}

// tokenizeWords splits an identifier into lowercase alphabetic components.
func tokenizeWords(name string) []string { return namematch.Components(name) }

// matchSensitiveToken reports whether a column name carries a sensitive token as
// a component or as an adjacent component pair, returning the matched token.
//
// The scan order is preserved verbatim through the delegation — all components
// in order, THEN all adjacent pairs, over the SINGLE union — because the
// returned token is EVIDENCE that reaches the user in DB-053's and DB-020's
// messages. A credential-then-PII two-pass would return the same boolean with a
// different token for names such as ssnPassword; identity_test.go's golden
// exists to catch exactly that.
func matchSensitiveToken(name string) (string, bool) {
	return namematch.MatchSet(name, sensitiveTokens)
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
