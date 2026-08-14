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

// SkippedDiscoveryDirNames and RouteWalkSkippedDirNames expose the two skip sets
// to the external scaffold_test package, sorted for a deterministic subtest
// order.
//
// They are read back rather than retyped in the test on purpose. The union lock
// asserts that discovery skips at least everything the route walk skips, and a
// test holding its own hand-copied list would go on passing the day a name is
// added to only one of them — which is the exact drift the lock exists to
// catch. Reading the real maps means a new name arrives in the test the moment
// it arrives in the code.
func SkippedDiscoveryDirNames() []string { return sortedKeys(discoverySkipDirs) }

// RouteWalkSkippedDirNames is dirsToSkip: what countRouteHandlers prunes.
func RouteWalkSkippedDirNames() []string { return sortedKeys(dirsToSkip) }
