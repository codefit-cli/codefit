package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// The analyzer identity is part of every key (spec R2), so every codefit build
// mints a whole GENERATION of entries: on a dev branch, where the binary changes
// several times an hour, each build orphans the entire previous generation. With
// entries stored flat there was nothing to collect them — the store grew for the
// life of the project and only `rm -rf` ever shrank it.
//
// So entries live under Dir/<generation>/, and a generation is a directory that
// can be dropped whole.
const (
	// generationNameLen is how much of the analyzer identity labels its
	// generation directory. SIXTEEN hex chars, not the full 64: the full
	// identity is already inside the key hash, so this name is a label and not
	// the boundary that keeps two analyzers apart. Sixty-four here would make
	// every entry path .codefit/cache/<64>/<64>.json and put a project with a
	// deep root within reach of Windows' MAX_PATH.
	generationNameLen = 16

	// generationsKept is how many generation directories survive a prune: the
	// CURRENT one, which is never collected, plus the two most recently
	// modified others. Not one: a developer alternating between an installed
	// codefit and a dev build would then have every run destroy the other
	// binary's generation and never see a hit again.
	generationsKept = 3

	// entryMaxAge is how long an entry survives without being written. A hit
	// does not rewrite its entry, so a live entry ages out too — that costs one
	// re-analysis. The entries this collects are the ones nothing can ever hit
	// again: every edit to a file mints a new key and orphans the old entry, and
	// a file deleted from the project leaves its entry behind forever.
	entryMaxAge = 30 * 24 * time.Hour
)

// This code DELETES FILES from a directory a user can also put things in, so it
// only ever recognises the two shapes it writes itself. Anything else under Dir
// — another tool's file, a note, a directory nobody here created — is not the
// cache's to delete, at any age, under any pressure.
var (
	analyzerIdentityShape = regexp.MustCompile(`^[0-9a-f]{64}$`)
	generationShape       = regexp.MustCompile(`^[0-9a-f]{16}$`)
	entryShape            = regexp.MustCompile(`^[0-9a-f]{64}\.json$`)
)

// pruned records the generation directories this process has already pruned.
// The maintenance is a startup cost, not a per-scan one: codefit is a long-lived
// MCP server that opens the same cache on every tool call, and a second pass in
// the same process has nothing new to find.
var pruned sync.Map

// generation is the name of the directory this cache's entries live in: the
// first [generationNameLen] hex characters of the analyzer identity.
//
// An identity that is not the expected 64-hex SHA-256 is HASHED into that shape
// rather than truncated as-is. Production only ever passes the real thing, but
// the name is used as a path element and matched against [generationShape], and
// both of those break badly for an arbitrary string: "../../x" would address a
// directory outside Dir, and any name the prune cannot recognise is a generation
// nothing will ever collect. Deriving the label keeps both properties total.
//
// It returns "" when there is no analyzer identity, matching [Cache.Key]: a
// cache that cannot key cannot address a directory either.
func (c *Cache) generation() string {
	if c.Analyzer == "" {
		return ""
	}
	if analyzerIdentityShape.MatchString(c.Analyzer) {
		return c.Analyzer[:generationNameLen]
	}
	sum := sha256.Sum256([]byte(c.Analyzer))
	return hex.EncodeToString(sum[:])[:generationNameLen]
}

// genDir is the directory holding this cache's entries, or "" when the cache has
// no analyzer identity.
func (c *Cache) genDir() string {
	g := c.generation()
	if g == "" {
		return ""
	}
	return filepath.Join(c.Dir, g)
}

// pruneOnce prunes unless this process already pruned this generation directory.
func (c *Cache) pruneOnce() {
	dir := c.genDir()
	if dir == "" {
		return
	}
	if _, already := pruned.LoadOrStore(dir, struct{}{}); already {
		return
	}
	c.prune()
}

// prune collects what nothing can hit again: the generation directories of
// codefit builds this one has superseded, the stale entries inside the current
// generation, and the flat entries left by the pre-generation layout.
//
// It is BEST EFFORT and reports nothing. Every failure it can meet — an
// unreadable directory, a file it may not remove, a race with another codefit
// process — is swallowed, because a cache that cannot clean itself still has to
// work. Maintenance is never the reason an audit does not happen.
func (c *Cache) prune() {
	current := c.generation()
	if current == "" {
		// No identity, so which generation is current is unknown — and a prune
		// that does not know that cannot tell an orphan from the one in use.
		return
	}
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		return // no cache directory yet, or one this process cannot read
	}

	type generation struct {
		name string
		mod  time.Time
	}
	var others []generation
	for _, e := range entries {
		name := e.Name()
		switch {
		case e.IsDir():
			if name == current || !generationShape.MatchString(name) {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue // vanished under us, or unstattable: leave it alone
			}
			others = append(others, generation{name: name, mod: info.ModTime()})
		case entryShape.MatchString(name):
			// An entry from the flat pre-generation layout. Nothing will ever
			// look at that path again, so it is removed once as a migration
			// rather than carried as a permanent compatibility path.
			_ = os.Remove(filepath.Join(c.Dir, name))
		}
	}

	// Most recently modified first, name-ordered on a tie so the outcome does
	// not depend on the filesystem's timestamp resolution.
	slices.SortFunc(others, func(a, b generation) int {
		if d := b.mod.Compare(a.mod); d != 0 {
			return d
		}
		return strings.Compare(a.name, b.name)
	})
	for _, g := range others[min(len(others), generationsKept-1):] {
		// The name matched [generationShape], so this directory is one codefit
		// wrote: its contents (entries and any temp file a crashed write left
		// behind) go with it.
		_ = os.RemoveAll(filepath.Join(c.Dir, g.name))
	}

	c.pruneStaleEntries(filepath.Join(c.Dir, current))
}

// pruneStaleEntries removes the entry files under dir that have not been written
// in [entryMaxAge]. Only entry-shaped files: whatever else is in there stays.
func (c *Cache) pruneStaleEntries(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-entryMaxAge)
	for _, e := range entries {
		if e.IsDir() || !entryShape.MatchString(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
}
