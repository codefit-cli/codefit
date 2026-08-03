package cache

import (
	"os"
	"sync"
	"testing"
)

// SetExecutable makes the package resolve the running analyzer with fn, and
// forgets the memoized identity so the next [Identity] call recomputes. The
// previous resolver and the memoized state are restored when the test ends.
//
// This exists so the "analyzer identity unavailable" branch (R2) — which
// os.Executable does not produce on any platform codefit ships to — is actually
// executed by a test instead of being described by one.
func SetExecutable(t *testing.T, fn func() (string, error)) {
	t.Helper()
	prev := executable
	executable = fn
	forgetIdentity()
	t.Cleanup(func() {
		executable = prev
		forgetIdentity()
	})
}

// RestoreExecutable pins the resolver back to the real os.Executable for a test
// that wants the genuine running binary, whatever ran before it.
func RestoreExecutable(t *testing.T) {
	t.Helper()
	SetExecutable(t, os.Executable)
}

func forgetIdentity() {
	identityOnce = sync.Once{}
	identityID, identityErr = "", nil
}

// Prune runs the cache's own directory maintenance directly, without the
// once-per-process guard [Open] applies, so a test can plant a fixture and watch
// exactly one prune act on it.
func Prune(c *Cache) { c.prune() }

// GenerationDir is the directory this cache's entries live in.
func GenerationDir(c *Cache) string { return c.genDir() }
