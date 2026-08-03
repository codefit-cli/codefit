package cache_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/cache"
	"github.com/codefit-cli/codefit/internal/core/findings"
)

// entry is the raw output of one file's analysis: findings AND surface. The
// cache that stored only findings would silently drop the surface and make a
// warm scan differ from a cold one (spec R1/R3).
func entry() cache.Entry {
	return cache.Entry{
		Findings: []findings.Finding{{
			ID: "SEC-001", Dimension: findings.DimensionSecurity,
			Severity: findings.SeverityCritical, File: "a.ts", Line: 3,
			Title: "Possible hardcoded API key", Confidence: 1.0,
			RequiresConsent: true, Fingerprint: "abc123def456",
		}},
		Surface: []findings.SurfaceItem{{
			ID: "idor:a.ts:3", Category: "idor", File: "a.ts", Line: 3,
			Snippet:           "params.id",
			StructuralSignals: []string{"reads params.id"},
			StructuralFacts:   map[string]bool{"local_access_detected": true},
			ReasonToReview:    "Is this resource scoped to the caller?",
			Fingerprint:       "fed654cba321",
		}},
	}
}

func newCache(t *testing.T, dir, analyzer string) *cache.Cache {
	t.Helper()
	return &cache.Cache{Dir: dir, Analyzer: analyzer}
}

// R3 — the entry holds BOTH halves of scanFile's output, and both survive the
// JSON round trip unchanged.
func TestEntryRoundTripCarriesFindingsAndSurface(t *testing.T) {
	c := newCache(t, filepath.Join(t.TempDir(), "cache"), "analyzer-a")
	want := entry()

	if err := c.Set("k1", want); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok := c.Get("k1")
	if !ok {
		t.Fatal("Get returned not-found after Set")
	}
	if len(got.Findings) != 1 || got.Findings[0].ID != "SEC-001" {
		t.Errorf("Findings = %+v, want the stored finding", got.Findings)
	}
	if len(got.Surface) != 1 {
		t.Fatalf("Surface = %+v, want the stored surface item — a cache that drops "+
			"surface makes a warm scan differ from a cold one (R1)", got.Surface)
	}
	s := got.Surface[0]
	if s.ID != "idor:a.ts:3" || s.Category != "idor" || s.Fingerprint != "fed654cba321" {
		t.Errorf("surface item = %+v, want the stored one intact", s)
	}
	if !s.StructuralFacts["local_access_detected"] {
		t.Errorf("StructuralFacts = %v, want the stored fact to survive the round trip", s.StructuralFacts)
	}
}

// Contract item 8 — a file that produced NOTHING caches that empty result and is
// found again. Otherwise the clean files (the majority) never benefit and the
// cache quietly does nothing on a healthy repository.
func TestEmptyResultIsCachedAndFound(t *testing.T) {
	c := newCache(t, filepath.Join(t.TempDir(), "cache"), "analyzer-a")

	if err := c.Set("empty", cache.Entry{}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok := c.Get("empty")
	if !ok {
		t.Fatal("an empty result must be a HIT, not a miss — otherwise clean files are re-analysed forever")
	}
	if len(got.Findings) != 0 || len(got.Surface) != 0 {
		t.Errorf("Get = %+v, want an empty entry", got)
	}
}

func TestGetMiss(t *testing.T) {
	c := newCache(t, filepath.Join(t.TempDir(), "cache"), "analyzer-a")
	if _, ok := c.Get("nonexistent"); ok {
		t.Error("Get on a missing key should return ok=false")
	}
}

// R4 — a corrupt entry is a MISS, never an error.
//
// The corrupt file is planted at the path the READER actually reads — inside the
// generation directory. Writing it flat in Dir would make this test pass by
// finding nothing at all, which is a different behaviour wearing this one's name.
func TestGetCorruptEntry(t *testing.T) {
	c := newCache(t, filepath.Join(t.TempDir(), "cache"), "analyzer-a")
	gen := cache.GenerationDir(c)
	if err := os.MkdirAll(gen, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gen, "bad.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Get("bad"); ok {
		t.Error("Get on a corrupt entry should return ok=false")
	}
}

// R2 — the key names the ANALYZER as well as the file. Same file, same bytes, a
// different analyzer: a different key, so a rules upgrade cannot serve a verdict
// it never computed.
func TestKeyIncludesAnalyzerIdentity(t *testing.T) {
	dir := t.TempDir()
	content := []byte("const x = 1;\n")

	a := newCache(t, dir, "analyzer-a").Key("src/a.ts", content)
	b := newCache(t, dir, "analyzer-b").Key("src/a.ts", content)

	if a == "" || b == "" {
		t.Fatalf("Key returned empty for a cache WITH an analyzer identity: a=%q b=%q", a, b)
	}
	if a == b {
		t.Error("two analyzers produced the SAME key for the same file — a codefit upgrade " +
			"would serve findings computed under the old rules (R2)")
	}
}

// R2 — the key names the FILE, path included. Two files with identical bytes at
// different paths must not share an entry: every finding and surface item carries
// its File and a fingerprint derived from it, so sharing would report the wrong
// path on a warm scan and break R1.
func TestKeyIncludesFilePath(t *testing.T) {
	c := newCache(t, t.TempDir(), "analyzer-a")
	content := []byte("const key = \"sk-ant-abcdefgh12345678\";\n")

	if c.Key("src/a.ts", content) == c.Key("src/b.ts", content) {
		t.Error("two paths with identical bytes shared a key — a warm scan would report " +
			"the first file's path for the second file")
	}
}

// R2 — the key names the file's CONTENT: changed bytes are a miss.
func TestKeyIncludesContent(t *testing.T) {
	c := newCache(t, t.TempDir(), "analyzer-a")

	if c.Key("a.ts", []byte("one")) == c.Key("a.ts", []byte("two")) {
		t.Error("changed bytes produced the same key")
	}
}

// R2, fail closed — without an analyzer identity there is no key, so nothing is
// ever read or written. Unknown key inputs mean do not reuse.
func TestKeyIsEmptyWithoutAnalyzerIdentity(t *testing.T) {
	c := newCache(t, t.TempDir(), "")
	if k := c.Key("a.ts", []byte("x")); k != "" {
		t.Errorf("Key = %q with no analyzer identity, want \"\" — an identity-less key "+
			"would key on file content alone, exactly what R2 forbids", k)
	}
}

// Concurrent writers must never leave a reader holding a torn entry. codefit is
// an MCP server: an agent can fire scan-security and scan-all over one project
// at once, and both reach the same entry path.
//
// This is a HAMMER guard, not a red-green proof. The property is probabilistic
// in the failing direction — os.WriteFile's truncate-then-write does not tear on
// every interleaving — so a passing run does not prove atomicity the way the
// mutation proofs elsewhere in this package do. It is here to catch a regression
// to a non-atomic write under -race, and it is honest about being that.
func TestSetIsSafeUnderConcurrentWriters(t *testing.T) {
	c := &cache.Cache{Dir: filepath.Join(t.TempDir(), "cache"), Analyzer: "analyzer-x"}
	key := c.Key("src/a.ts", []byte("const a = 1;\n"))
	want := cache.Entry{Findings: []findings.Finding{{ID: "SEC-001", Title: "x"}}}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.Set(key, want)
			c.Get(key) // a reader racing the writers must never panic
		}()
	}
	wg.Wait()

	got, ok := c.Get(key)
	if !ok {
		t.Fatal("entry unreadable after concurrent writers — a torn write survived")
	}
	if len(got.Findings) != 1 || got.Findings[0].ID != "SEC-001" {
		t.Errorf("entry corrupt after concurrent writers: %+v", got)
	}
}

// A Set that FAILS must not leave its temp file behind. The success path proves
// nothing here — the rename consumes the temp either way, so a test over a
// successful Set passes even with the cleanup deleted (measured, not assumed).
// The failure path is where the cleanup is the only thing standing between a
// long-lived cache directory and an unbounded pile of .tmp litter.
//
// The rename is forced to fail portably by putting a DIRECTORY where the entry
// file belongs: os.Rename refuses to replace it on both Windows and Linux.
func TestFailedSetLeavesNoTempFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	c := &cache.Cache{Dir: dir, Analyzer: "analyzer-x"}
	key := c.Key("src/a.ts", []byte("x"))
	gen := cache.GenerationDir(c)

	if err := os.MkdirAll(filepath.Join(gen, key+".json"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := c.Set(key, cache.Entry{}); err == nil {
		t.Fatal("Set should fail when the entry path is a directory")
	}

	entries, err := os.ReadDir(gen)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file survived a FAILED Set: %s", e.Name())
		}
	}
}
