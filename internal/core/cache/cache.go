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
type Entry struct {
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
func Open(dir string) (*Cache, error) {
	id, err := Identity()
	if err != nil {
		return nil, fmt.Errorf("resolving analyzer identity: %w", err)
	}
	return &Cache{Dir: dir, Analyzer: id}, nil
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
// forever would make the cache do nothing where it matters most.
func (c *Cache) Get(key string) (Entry, bool) {
	if key == "" {
		return Entry{}, false
	}
	data, err := os.ReadFile(c.entryPath(key))
	if err != nil {
		return Entry{}, false
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return Entry{}, false
	}
	return e, true
}

// Set stores an entry as a JSON file under key. The error is returned rather than
// swallowed so the caller can report it; it is never a reason to fail a scan.
func (c *Cache) Set(key string, e Entry) error {
	if key == "" {
		return fmt.Errorf("refusing to store a cache entry without a key")
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return fmt.Errorf("creating cache dir: %w", err)
	}
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("encoding cache entry: %w", err)
	}
	if err := os.WriteFile(c.entryPath(key), data, 0o644); err != nil {
		return fmt.Errorf("writing cache entry: %w", err)
	}
	return nil
}

func (c *Cache) entryPath(key string) string {
	return filepath.Join(c.Dir, key+".json")
}
