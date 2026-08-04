package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/codefit-cli/codefit/internal/core/findings"
)

// Entry is the RAW output of one file's analysis: everything the sensor computes
// for that file, before any severity is adjusted by path criticality.
//
// It holds BOTH halves on purpose. The sensor's per-file analysis returns
// findings AND mapped surface, so an entry that stored only the findings would
// serve a warm scan that silently lost the surface — a cache that can change the
// output is not a cache, it is a blind spot (spec R1/R3).
//
// Storing the PRE-criticality output is likewise not an implementation detail:
// path criticality is applied by the sensor after the per-file analysis returns,
// so a cached entry survives an edit to .codefit.yaml's path_criticality and the
// next scan re-weights severities without invalidating a single entry. Caching
// the adjusted findings would serve stale severities after every config edit.
//
// The entry is SELF-DESCRIBING: Key names the key it was stored under, stamped
// by [Cache.Set] and verified by [Cache.Get]. It is not a second addressing
// scheme — nothing reads a path out of an entry — it is the entry's proof of
// provenance, because VALID JSON IS NOT ONE (ADR 0053).
type Entry struct {
	// Key is the key this entry is the answer to. Written by Set, verified by
	// Get, which returns a miss when it does not match the key it was asked
	// for. Without it, any syntactically valid JSON that simply lacks these
	// fields — null, {}, {"unrelated":1} — unmarshals into a zero Entry and is
	// served as a HIT, and a hit with no findings and no surface asserts that
	// this analyzer analysed exactly these bytes and found nothing. That is the
	// cache manufacturing a clean verdict for a file nothing ever read.
	Key      string                 `json:"key"`
	Findings []findings.Finding     `json:"findings"`
	Surface  []findings.SurfaceItem `json:"surface"`
}

// Cache stores one [Entry] per (analyzer, file, content) under Dir. A hit means
// the same analyzer already analysed exactly these bytes at exactly this path, so
// its output can be reused instead of recomputed (PRD §19, optimization 2).
//
// Analyzer is the identity of the binary whose rules produced the entries — see
// [Identity]. Build a cache with [Open] to bind it to the running analyzer. A
// Cache with an empty Analyzer produces no keys at all, so it can never read or
// write: an unknown analyzer means do not reuse.
type Cache struct {
	Dir      string
	Analyzer string
}

// Open builds a cache under dir bound to the RUNNING analyzer's identity. It
// fails when that identity cannot be resolved, which is the caller's signal to
// scan without a cache for this run rather than key on the file alone.
//
// Opening also PRUNES, once per process per generation: superseded generations,
// stale entries and the flat entries of the pre-generation layout are collected
// here. The prune is best effort and reports nothing — see [Cache.prune]. It is
// done on Open rather than on a schedule because the store only ever grows
// between runs, and Open is the moment codefit knows which generation is the one
// in use.
func Open(dir string) (*Cache, error) {
	id, err := Identity()
	if err != nil {
		return nil, fmt.Errorf("resolving analyzer identity: %w", err)
	}
	c := &Cache{Dir: dir, Analyzer: id}
	c.pruneOnce()
	return c, nil
}

// Key is the entry key for a file: the SHA-256 of the analyzer identity, the
// file's project-relative path and the file's content.
//
// All three are named because all three can change the answer (spec R2):
//
//   - the ANALYZER, because the rules, the detectors, the parser and the surface
//     queries all live in the binary. Keying on the file alone would let a codefit
//     upgrade report "clean" under rules it never ran.
//   - the PATH, because every finding and surface item carries its File and a
//     fingerprint derived from it. Two files with identical bytes are a real and
//     ordinary thing; sharing one entry between them would report the first file's
//     path for the second and break R1.
//   - the CONTENT, which is what the cache exists to notice.
//
// It returns "" when the cache has no analyzer identity, so a cache that does not
// know what produced its entries silently reads and writes nothing.
func (c *Cache) Key(file string, content []byte) string {
	if c.Analyzer == "" {
		return ""
	}
	h := sha256.New()
	h.Write([]byte(c.Analyzer))
	h.Write([]byte{0})
	h.Write([]byte(filepath.ToSlash(file)))
	h.Write([]byte{0})
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

// Get returns the entry stored under key and whether it was present. A missing,
// unreadable or corrupt entry is a MISS, never an error: the caller analyses the
// file normally. The cache may never be the reason an audit does not happen.
//
// An entry that was stored EMPTY is a hit. A file that produced no findings and
// no surface is the ordinary case in a healthy repository, and re-analysing it
// forever would make the cache do nothing where it matters most. That is
// precisely why an entry has to PROVE it is one: parsing is not provenance, and
// an unproven empty hit is a clean verdict codefit never computed (ADR 0053).
//
// So the payload must name this key. Anything else — a stray {} an editor or a
// sync tool left in .codefit/cache, a half-restored backup, a well-formed entry
// sitting at another key's path, an entry written before the stamp existed — is
// a miss, and a miss is just an ordinary analysis. The check does not enumerate
// the shapes that can go wrong; it rejects everything that cannot answer for
// itself, which is the only version of that question with a complete answer.
func (c *Cache) Get(key string) (Entry, bool) {
	if key == "" {
		return Entry{}, false
	}
	path := c.entryPath(key)
	if path == "" {
		return Entry{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Entry{}, false
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return Entry{}, false
	}
	if e.Key != key {
		return Entry{}, false
	}
	return e, true
}

// Set stores an entry as a JSON file under key. The error is returned rather than
// swallowed so the caller can report it; it is never a reason to fail a scan.
//
// The write is ATOMIC — a temp file in the same directory, then a rename, the
// same shape the committed baseline uses. codefit is an MCP server, so two tools
// over one project (an agent firing scan-security and scan-all together) can
// reach the same entry path at once, and os.WriteFile truncates before it writes.
// A torn entry degrades safely on read (invalid JSON is a miss), but it degrades
// into re-analysing the file the cache exists to skip, and a crash mid-write
// would leave that behind permanently.
//
// Set STAMPS the key into the entry before marshalling, so what it writes can
// prove to a later reader whose answer it is. The caller's e.Key is overwritten
// rather than trusted: the key an entry claims is the key it was stored under,
// never a value the caller supplied alongside a different one.
func (c *Cache) Set(key string, e Entry) error {
	if key == "" {
		return fmt.Errorf("refusing to store a cache entry without a key")
	}
	dir := c.genDir()
	if dir == "" {
		return fmt.Errorf("refusing to store a cache entry without an analyzer identity")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating cache dir: %w", err)
	}
	e.Key = key
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("encoding cache entry: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".entry-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp cache entry: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp cache entry: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp cache entry: %w", err)
	}
	if err := os.Rename(tmpName, c.entryPath(key)); err != nil {
		return fmt.Errorf("replacing cache entry %q: %w", key, err)
	}
	return nil
}

// entryPath is where the entry for key lives: under this cache's generation
// directory, never flat in Dir. It is "" when the cache has no analyzer
// identity, which every caller treats as "no entry".
func (c *Cache) entryPath(key string) string {
	dir := c.genDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, key+".json")
}
