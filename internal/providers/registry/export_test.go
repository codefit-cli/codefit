package registry

import "testing"

// SetTableForTest replaces the package's table for the duration of a test,
// restoring the real table when the test ends. It exists so this package's
// own resolvers (ByMarkerFile today) can be exercised against hypothetical
// Exposure values — an entry with InitDetect: false, for instance — without
// ever mutating the real go/typescript entries that ship in production. Both
// registered entries want InitDetect: true forever, so there is no other way
// to drive the false branch with a real Entry rather than a hand-built one.
func SetTableForTest(t *testing.T, entries []Entry) {
	t.Helper()
	prev := table
	table = entries
	t.Cleanup(func() {
		table = prev
	})
}
