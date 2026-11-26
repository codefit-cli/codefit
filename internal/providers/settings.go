package providers

// SettingsFingerprinter is the optional capability a provider implements when
// settings OUTSIDE its binary can change what it reports for the same bytes —
// for the TypeScript provider, the environment variables its parser reads
// (ADR 0089). It returns an opaque fingerprint of those settings as the running
// process sees them, or "" when none is set.
//
// The finding cache mixes the fingerprint into every key, so a result computed
// under one setting is never served under another: a warm run equals a cold
// run. The analyzer identity (the hash of the binary) already covers the rules
// and the parser's code; this covers what the binary reads at run time.
//
// Over-inclusion is the safe direction. A fingerprint that also changes with a
// setting the parser ignores costs one extra miss; one that misses a setting
// the parser obeys serves a verdict computed under different rules — a false
// all-clear when that setting stops parses early. So an implementation
// fingerprints every setting under the names its parser may read, not only the
// ones known today to matter.
//
// It is resolved by type assertion, like FileAnalyzer. A provider without it
// contributes nothing to the key, which is correct only for a provider whose
// output depends on nothing but its binary and the file.
type SettingsFingerprinter interface {
	SettingsFingerprint() string
}
