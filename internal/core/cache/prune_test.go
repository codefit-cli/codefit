package cache_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/codefit-cli/codefit/internal/core/cache"
)

// These tests drive code that DELETES FILES. Every one of them asserts in both
// directions in the same run — what was removed AND what survived — because a
// prune test whose fixture has nothing to delete passes trivially and proves
// nothing about the deletion it claims to guard.

// hexIdentity has the shape a real analyzer identity has: the hex SHA-256 of the
// running binary.
const hexIdentity = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

var shapedGeneration = regexp.MustCompile(`^[0-9a-f]{16}$`)

func entryKey(c byte) string { return strings.Repeat(string(c), 64) }

// stamp sets a path's modification time to age ago. Generation ordering and
// entry staleness are both decided on ModTime, so the fixtures set it rather
// than hoping the filesystem produces the ordering the test needs.
func stamp(t *testing.T, path string, age time.Duration) {
	t.Helper()
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("stamping %q: %v", path, err)
	}
}

func writeEntryFile(t *testing.T, dir, name string, age time.Duration) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(`{"findings":null,"surface":null}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stamp(t, p, age)
	return p
}

// plantGeneration creates a generation directory holding one entry file. The
// directory is stamped AFTER its content, because writing into a directory
// updates its own modification time.
func plantGeneration(t *testing.T, dir, name string, age time.Duration) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	writeEntryFile(t, p, entryKey('a')+".json", 0)
	stamp(t, p, age)
	return p
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("stat %q: %v", path, err)
	}
	return err == nil
}

func mustExist(t *testing.T, path, why string) {
	t.Helper()
	if !exists(t, path) {
		t.Errorf("%s was DELETED: %s", filepath.Base(path), why)
	}
}

func mustNotExist(t *testing.T, path, why string) {
	t.Helper()
	if exists(t, path) {
		t.Errorf("%s SURVIVED: %s", filepath.Base(path), why)
	}
}

// Entries live under a per-generation directory, not flat in Dir. That is what
// makes a generation removable at all: an orphaned generation is a directory to
// drop, not a set of files to identify one by one.
func TestEntriesLiveUnderTheGenerationDirectory(t *testing.T) {
	dir := t.TempDir()
	c := &cache.Cache{Dir: dir, Analyzer: hexIdentity}
	key := c.Key("src/a.ts", []byte("const x = 1;\n"))

	if err := c.Set(key, entry()); err != nil {
		t.Fatalf("Set: %v", err)
	}

	want := filepath.Join(dir, hexIdentity[:16], key+".json")
	if !exists(t, want) {
		t.Errorf("no entry at %q — entries must live under Dir/<generation>/", want)
	}
	if exists(t, filepath.Join(dir, key+".json")) {
		t.Error("the entry was written FLAT in Dir; a flat entry cannot be dropped by generation")
	}
	if _, ok := c.Get(key); !ok {
		t.Error("Get missed an entry Set just wrote — reader and writer disagree on the layout")
	}
}

// The generation directory is a LABEL, so it is always well shaped and always
// directly under Dir. This is a safety property, not cosmetics: the prune only
// ever removes directories matching ^[0-9a-f]{16}$, and a name derived straight
// from an arbitrary Analyzer string could both escape Dir and never match.
func TestGenerationDirectoryIsAlwaysWellShapedAndInsideDir(t *testing.T) {
	dir := t.TempDir()

	for _, analyzer := range []string{hexIdentity, "analyzer-a", "../../escape", "x"} {
		c := &cache.Cache{Dir: dir, Analyzer: analyzer}
		got := cache.GenerationDir(c)
		if filepath.Dir(got) != dir {
			t.Errorf("analyzer %q produced generation dir %q, which is not directly under %q",
				analyzer, got, dir)
		}
		if base := filepath.Base(got); !shapedGeneration.MatchString(base) {
			t.Errorf("analyzer %q produced generation name %q, want ^[0-9a-f]{16}$ — a name "+
				"the prune cannot recognise is a generation nothing will ever collect", analyzer, base)
		}
	}

	// A real identity labels its generation with its own first 16 hex chars.
	c := &cache.Cache{Dir: dir, Analyzer: hexIdentity}
	if got, want := filepath.Base(cache.GenerationDir(c)), hexIdentity[:16]; got != want {
		t.Errorf("generation for a real identity = %q, want %q", got, want)
	}

	// No identity, no generation: a cache that cannot key cannot address a directory.
	if got := cache.GenerationDir(&cache.Cache{Dir: dir}); got != "" {
		t.Errorf("GenerationDir with no analyzer = %q, want \"\"", got)
	}
}

// The prune keeps the current generation plus the TWO most recently modified
// others — three in all. Not one: a developer alternating between an installed
// binary and a dev build would otherwise have each run destroy the other's
// generation and never see a hit again.
func TestPruneKeepsTheCurrentGenerationPlusTheTwoNewest(t *testing.T) {
	dir := t.TempDir()
	c := &cache.Cache{Dir: dir, Analyzer: hexIdentity}

	current := plantGeneration(t, dir, hexIdentity[:16], 0)
	newest := plantGeneration(t, dir, "1111111111111111", time.Hour)
	second := plantGeneration(t, dir, "2222222222222222", 2*time.Hour)
	third := plantGeneration(t, dir, "3333333333333333", 3*time.Hour)
	oldest := plantGeneration(t, dir, "4444444444444444", 4*time.Hour)

	cache.Prune(c)

	mustExist(t, current, "the current generation is never collected")
	mustExist(t, newest, "the most recently used other generation is kept")
	mustExist(t, second, "the second most recently used other generation is kept")
	mustNotExist(t, third, "only the 2 newest other generations are kept")
	mustNotExist(t, oldest, "only the 2 newest other generations are kept")
}

// The current generation is never removed, however old it is. Its age says the
// developer has not rebuilt in a while, not that the cache they are about to use
// is garbage.
func TestPruneNeverRemovesTheCurrentGenerationHoweverOld(t *testing.T) {
	dir := t.TempDir()
	c := &cache.Cache{Dir: dir, Analyzer: hexIdentity}

	current := plantGeneration(t, dir, hexIdentity[:16], 400*24*time.Hour)
	newest := plantGeneration(t, dir, "1111111111111111", time.Hour)
	second := plantGeneration(t, dir, "2222222222222222", 2*time.Hour)
	third := plantGeneration(t, dir, "3333333333333333", 3*time.Hour)

	cache.Prune(c)

	mustExist(t, current, "the OLDEST generation was the current one and must survive")
	mustExist(t, newest, "kept as one of the 2 newest others")
	mustExist(t, second, "kept as one of the 2 newest others")
	mustNotExist(t, third, "the current generation must not occupy one of the 2 other slots")
}

// The prune only ever removes things matching a shape it wrote itself. Anything
// else under the cache directory — a user's note, another tool's file, a
// directory nobody here created — is never touched, and this is asserted in the
// same run as a deletion that really happens, so the test cannot pass by having
// nothing to do.
func TestPruneOnlyRemovesKnownShapes(t *testing.T) {
	dir := t.TempDir()
	c := &cache.Cache{Dir: dir, Analyzer: hexIdentity}

	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("why this directory exists\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(dir, "notes")
	if err := os.MkdirAll(notes, 0o755); err != nil {
		t.Fatal(err)
	}
	noteFile := filepath.Join(notes, "todo.md")
	if err := os.WriteFile(noteFile, []byte("keep me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stamp(t, notes, 400*24*time.Hour)
	keep := filepath.Join(dir, "keep-me.json")
	if err := os.WriteFile(keep, []byte(`{"mine":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stamp(t, keep, 400*24*time.Hour)

	current := plantGeneration(t, dir, hexIdentity[:16], 0)
	kept := plantGeneration(t, dir, "1111111111111111", time.Hour)
	alsoKept := plantGeneration(t, dir, "2222222222222222", 2*time.Hour)
	collected := plantGeneration(t, dir, "3333333333333333", 3*time.Hour)

	cache.Prune(c)

	// The deletion really happened…
	mustNotExist(t, collected, "an orphaned generation is what this prune exists to collect")
	mustExist(t, current, "the current generation")
	mustExist(t, kept, "one of the 2 newest others")
	mustExist(t, alsoKept, "one of the 2 newest others")

	// …and it stayed inside its own shapes while it happened.
	mustExist(t, readme, "a file the prune did not write is never its to delete")
	mustExist(t, notes, "a directory that is not a generation is never its to delete")
	mustExist(t, noteFile, "the content of a foreign directory")
	mustExist(t, keep, "a .json file that is not entry-shaped is not an entry")
}

// A cache hit does not rewrite its entry, so a live entry ages out too. That is
// deliberate and costs one re-analysis; an unbounded pile of entries for files
// that were edited or deleted months ago costs disk forever.
func TestPruneRemovesStaleEntriesFromTheCurrentGeneration(t *testing.T) {
	dir := t.TempDir()
	c := &cache.Cache{Dir: dir, Analyzer: hexIdentity}
	gen := filepath.Join(dir, hexIdentity[:16])
	if err := os.MkdirAll(gen, 0o755); err != nil {
		t.Fatal(err)
	}

	stale := writeEntryFile(t, gen, entryKey('a')+".json", 31*24*time.Hour)
	borderline := writeEntryFile(t, gen, entryKey('b')+".json", 29*24*time.Hour)
	fresh := writeEntryFile(t, gen, entryKey('c')+".json", time.Hour)
	// Not entry-shaped: never the prune's to delete, whatever its age. A temp
	// file left behind by a crashed Set is deliberately in this group.
	foreign := writeEntryFile(t, gen, "notes.txt", 400*24*time.Hour)
	tmp := writeEntryFile(t, gen, ".entry-1234.tmp", 400*24*time.Hour)
	short := writeEntryFile(t, gen, "deadbeef.json", 400*24*time.Hour)

	cache.Prune(c)

	mustNotExist(t, stale, "an entry older than 30 days is collected")
	mustExist(t, borderline, "an entry younger than 30 days is kept")
	mustExist(t, fresh, "a fresh entry is kept")
	mustExist(t, foreign, "a file that is not entry-shaped is never the prune's to delete")
	mustExist(t, tmp, "a stray temp file is not entry-shaped")
	mustExist(t, short, "a .json file that is not a 64-hex key is not an entry")
}

// Entries written by the pre-generation layout sit flat in Dir and are
// unreadable now: nothing will ever look at that path again. They are removed
// once, as a migration, not carried as a permanent compatibility path.
func TestPruneRemovesLegacyFlatEntries(t *testing.T) {
	dir := t.TempDir()
	c := &cache.Cache{Dir: dir, Analyzer: hexIdentity}

	legacy := writeEntryFile(t, dir, entryKey('a')+".json", time.Hour)
	alsoLegacy := writeEntryFile(t, dir, entryKey('b')+".json", 0)
	keep := writeEntryFile(t, dir, "keep-me.json", 0)
	short := writeEntryFile(t, dir, "deadbeef.json", 0)
	current := plantGeneration(t, dir, hexIdentity[:16], 0)

	cache.Prune(c)

	mustNotExist(t, legacy, "a flat entry is unreadable under the generation layout")
	mustNotExist(t, alsoLegacy, "a flat entry is unreadable under the generation layout, fresh or not")
	mustExist(t, keep, "a .json file that is not entry-shaped is not a legacy entry")
	mustExist(t, short, "a .json file that is not a 64-hex key is not a legacy entry")
	mustExist(t, current, "the current generation")
}

// A cache that cannot clean itself still has to work. Every failure the prune
// can meet is swallowed: it is maintenance, never a reason a scan does not
// happen.
func TestPruneIsBestEffortAndNeverFailsAScan(t *testing.T) {
	t.Run("cache directory does not exist yet", func(t *testing.T) {
		c := &cache.Cache{Dir: filepath.Join(t.TempDir(), "never-created"), Analyzer: hexIdentity}
		cache.Prune(c)
		if err := c.Set(c.Key("a.ts", []byte("x")), entry()); err != nil {
			t.Errorf("Set after a prune over a missing directory: %v", err)
		}
	})

	t.Run("cache directory is a file", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "not-a-dir")
		if err := os.WriteFile(dir, []byte("someone put a file here"), 0o600); err != nil {
			t.Fatal(err)
		}
		c := &cache.Cache{Dir: dir, Analyzer: hexIdentity}
		cache.Prune(c)
		if !exists(t, dir) {
			t.Error("the prune deleted the file standing where the cache directory should be")
		}
		if _, ok := c.Get(c.Key("a.ts", []byte("x"))); ok {
			t.Error("Get reported a hit from a cache directory that is a file")
		}
	})

	t.Run("no analyzer identity", func(t *testing.T) {
		dir := t.TempDir()
		gen := plantGeneration(t, dir, "1111111111111111", 400*24*time.Hour)
		other := plantGeneration(t, dir, "2222222222222222", 401*24*time.Hour)
		another := plantGeneration(t, dir, "3333333333333333", 402*24*time.Hour)
		fourth := plantGeneration(t, dir, "4444444444444444", 403*24*time.Hour)

		cache.Prune(&cache.Cache{Dir: dir})

		for _, p := range []string{gen, other, another, fourth} {
			mustExist(t, p, "a cache with no identity does not know which generation is current, "+
				"so it must not decide which ones are orphans")
		}
	})
}

// Open prunes, and prunes ONCE per process for a given generation directory: the
// maintenance is a startup cost, not a per-scan one, and codefit is a long-lived
// MCP server that opens the same cache on every tool call.
func TestOpenPrunesOncePerGenerationDirectory(t *testing.T) {
	cache.RestoreExecutable(t)
	dir := t.TempDir()

	first, err := cache.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	gen := filepath.Base(cache.GenerationDir(first))
	if !shapedGeneration.MatchString(gen) {
		t.Fatalf("generation %q from the real identity is not well shaped", gen)
	}

	// Everything below is planted AFTER the first Open, so a second prune would
	// be the only thing that could collect it.
	collectible := plantGeneration(t, dir, "1111111111111111", 100*24*time.Hour)
	more := plantGeneration(t, dir, "2222222222222222", 101*24*time.Hour)
	evenMore := plantGeneration(t, dir, "3333333333333333", 102*24*time.Hour)
	yetMore := plantGeneration(t, dir, "4444444444444444", 103*24*time.Hour)

	if _, err := cache.Open(dir); err != nil {
		t.Fatalf("second Open: %v", err)
	}

	for _, p := range []string{collectible, more, evenMore, yetMore} {
		mustExist(t, p, "Open pruned a second time for the same generation directory")
	}

	// And the prune really is wired into Open: a fresh directory gets pruned.
	fresh := t.TempDir()
	plantGeneration(t, fresh, "1111111111111111", 100*24*time.Hour)
	plantGeneration(t, fresh, "2222222222222222", 101*24*time.Hour)
	plantGeneration(t, fresh, "3333333333333333", 102*24*time.Hour)
	doomed := plantGeneration(t, fresh, "4444444444444444", 103*24*time.Hour)
	if _, err := cache.Open(fresh); err != nil {
		t.Fatalf("Open on a fresh directory: %v", err)
	}
	mustNotExist(t, doomed, "Open must prune the first time it sees a cache directory")
}
