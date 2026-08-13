package scaffold

// DBOnlyClauseForTest exposes the shared db-only fragment to the external
// scaffold_test package.
//
// The fragment stays unexported in production: it is an implementation detail
// of D4 ("one vocabulary, made mechanical"), not a second public API for
// callers to assemble their own sentences from. But the guarantee it exists to
// provide — that UndetectedStatement and CapabilityStatementForExposure
// interpolate the SAME bytes rather than two wordings that drift apart — can
// only be checked by comparing against the fragment itself. Asserting that both
// sentences merely "mention the database" would pass on exactly the divergence
// this is meant to prevent.
var DBOnlyClauseForTest = dbOnlyClause
