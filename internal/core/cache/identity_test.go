package cache_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/cache"
)

// currentExecutable is the real running test binary — a real file to hash.
func currentExecutable(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable unavailable on this platform: %v", err)
	}
	return self
}

// writeBinary writes a stand-in "analyzer binary" with the given content.
func writeBinary(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "codefit")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// identityOf resolves the analyzer identity as if path were the running binary.
func identityOf(t *testing.T, path string) string {
	t.Helper()
	cache.SetExecutable(t, func() (string, error) { return path, nil })
	id, err := cache.Identity()
	if err != nil {
		t.Fatalf("Identity for %q: %v", path, err)
	}
	return id
}

// R2 — the analyzer identity is the SHA-256 of the RUNNING executable, not a
// version string. Under `go test` the executable is a fresh temporary build, so
// editing a rule changes it automatically: correct by construction in exactly the
// case `version.Version` (the constant "v0.1.0-dev" for any plain build) fails.
func TestIdentityIsTheHashOfTheRunningExecutable(t *testing.T) {
	cache.RestoreExecutable(t)

	id, err := cache.Identity()
	if err != nil {
		t.Fatalf("Identity: %v", err)
	}
	if len(id) != 64 {
		t.Errorf("Identity = %q (%d chars), want a 64-char hex SHA-256", id, len(id))
	}
	again, err := cache.Identity()
	if err != nil {
		t.Fatalf("Identity (second call): %v", err)
	}
	if again != id {
		t.Errorf("Identity is not stable within a process: %q then %q", id, again)
	}
}

// R2 — computed ONCE per process. A repository walk parses hundreds of files;
// re-hashing a ~5 MB binary per file would be the cache's own cost.
func TestIdentityIsComputedOncePerProcess(t *testing.T) {
	calls := 0
	self := currentExecutable(t)
	cache.SetExecutable(t, func() (string, error) {
		calls++
		return self, nil
	})

	for i := 0; i < 5; i++ {
		if _, err := cache.Identity(); err != nil {
			t.Fatalf("Identity: %v", err)
		}
	}
	if calls != 1 {
		t.Errorf("the executable was resolved %d times, want exactly 1 (memoized per process)", calls)
	}
}

// R2 — a different binary is a different identity.
func TestIdentityChangesWithTheBinary(t *testing.T) {
	first := identityOf(t, writeBinary(t, "one"))
	second := identityOf(t, writeBinary(t, "two"))

	if first == second {
		t.Error("two different binaries produced the same analyzer identity — new rules " +
			"would be served verdicts computed by the old ones (R2)")
	}
}

// R2 / contract item 4 — when the running executable cannot be resolved the
// identity is UNKNOWN, so the cache disables itself: Open fails and the caller
// scans without a cache. Never guess.
func TestIdentityUnavailableDisablesTheCache(t *testing.T) {
	cache.SetExecutable(t, func() (string, error) {
		return "", errors.New("no executable on this platform")
	})

	if _, err := cache.Identity(); err == nil {
		t.Error("Identity returned no error when the executable could not be resolved — " +
			"an unknown identity must never become a usable key")
	}
	if _, err := cache.Open(t.TempDir()); err == nil {
		t.Error("Open succeeded with an unresolvable analyzer identity, want an error so " +
			"the caller disables caching for the run")
	}
}

// An executable path that resolves but cannot be READ is the same unknown state.
func TestIdentityUnreadableExecutableDisablesTheCache(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone")
	cache.SetExecutable(t, func() (string, error) { return missing, nil })

	if _, err := cache.Identity(); err == nil {
		t.Error("Identity returned no error for an unreadable executable")
	}
}

// Open binds the cache to the running analyzer, so its keys are R2 keys.
func TestOpenBindsTheRunningAnalyzerIdentity(t *testing.T) {
	cache.RestoreExecutable(t)
	dir := filepath.Join(t.TempDir(), "cache")

	c, err := cache.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if c.Dir != dir {
		t.Errorf("Dir = %q, want %q", c.Dir, dir)
	}
	id, err := cache.Identity()
	if err != nil {
		t.Fatal(err)
	}
	if c.Analyzer != id {
		t.Errorf("Analyzer = %q, want the running analyzer identity %q", c.Analyzer, id)
	}
	if c.Key("a.ts", []byte("x")) == "" {
		t.Error("a cache from Open produced no key — it must be usable")
	}
}
