// Package namematch decides one question, in one place: does this IDENTIFIER
// NAME denote a secret?
//
// It is a leaf. It imports no codefit package and nothing outside the standard
// library, because three unrelated consumers depend on it and any one of them
// pulling the others in through it would break the core's layering:
//
//	SEC-001 (Go provider)   -> MatchSet(name, Credential())
//	SEC-050 (Go provider)   -> MatchSet(name, SecurityValue())
//	DB-053 / DB-020 (core)  -> MatchSet(name, DB053Union())
//
// The MATCHER is shared. The VOCABULARIES are not, and that separation is
// load-bearing rather than tidy:
//
//   - a personal identifier (ssn, cvv) is not a credential, so SEC-001 — which
//     affirms "this looks like a hardcoded credential" at Confidence 1.0 — must
//     not consume DB-053's PII names;
//   - crypto material (nonce, salt, iv) is not a credential either, so SEC-050
//     keeps its own set;
//   - DB-053's vocabulary is frozen against main 0fb211d, so SEC-001 can grow
//     without moving a rule measured over 29 corpora that are no longer cloned.
//
// The convention is: case-insensitive, by name COMPONENT with adjacent-pair
// joining, NEVER raw substring and never value length. Raw substring is what
// this package exists to remove — it made `tokenizer` match "token" and, worse,
// made a length-guarded bare "key" report enum constants such as
// CategoryDWDimensionNoSurrogateKey as hardcoded credentials. That guard was
// inverted in practice: descriptive kebab/snake values grow past 16 bytes
// BECAUSE they are descriptive, while a real credential has no length floor, so
// the gate admitted the false-positive class and rejected a true-positive one.
//
// Component matching cannot split an all-lowercase concatenation. That gap is
// real, is declared in LimitLowercaseConcatenation, and is rendered to agents
// by codefit-coverage — see ADR 0075.
package namematch
