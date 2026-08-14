package db

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/codefit-cli/codefit/internal/core/sourcetext"
	"github.com/codefit-cli/codefit/internal/providers"
)

// resolvedPath is ONE entry of database.schema_paths together with everything
// it resolved to.
//
// Files may legitimately be EMPTY, and carrying that emptiness is the entire
// reason this type exists. A flat []SourceFile subtracts a path that resolved to
// nothing from both sides of every downstream count, so the path disappears from
// the audit as though it had never been configured — which is how a directory
// holding no schema file at all produced a score of 100.
type resolvedPath struct {
	// Configured is the entry EXACTLY as written in .codefit.yaml, because that
	// is the string the developer has to go and edit. A cleaned or absolute form
	// would be a different string from the one in their file.
	Configured string
	// Files is what the entry resolved to, in resolution order. Empty is a fact,
	// not an absence: it means codefit read nothing from this entry.
	Files []providers.SourceFile
}

// schemaResolution is the whole outcome of resolving database.schema_paths:
// one entry per CONFIGURED PATH, plus the decoded content keyed by resolved-file
// path.
//
// The two halves are keyed differently on purpose. Paths is the unit of ACCOUNT
// (what was configured, and what each entry produced); Content stays keyed by
// resolved FILE because it feeds snippets, fingerprints and file:line positions,
// which are properties of a file and of nothing else.
type schemaResolution struct {
	Paths   []resolvedPath
	Content map[string][]byte
}

// sources flattens the per-path resolution back into the ordered SourceFile
// slice the parsers take. Order is config order across entries and resolution
// order within an entry — the same order the flat reader produced, so every
// parser sees exactly what it saw before.
func (r schemaResolution) sources() []providers.SourceFile {
	var out []providers.SourceFile
	for _, p := range r.Paths {
		out = append(out, p.Files...)
	}
	return out
}

// readSchemaSources resolves the configured schema paths from disk into ORDERED
// SourceFiles grouped BY CONFIGURED PATH (the sensor is the filesystem-side
// caller, ADR 0014). A path that is a directory is expanded to its *.sql files in
// Flyway version order (V<n>__…); a path that is a file is read as-is. Order
// across entries is config order; within a directory it is Flyway version order.
// A configured-but-unreadable path is a hard error (a real misconfiguration, not
// "no DB").
//
// EVERY configured path yields an entry, including one that resolved to no file.
// That is the invariant the floor is built on: len(result.Paths) always equals
// len(paths), so a path cannot be dropped on the way to the floor without the
// omission being visible.
//
// Bytes become TEXT here, through sourcetext.Decode, and this is the right layer
// for it by ADR 0014's own division: the parser is filesystem-free and receives
// text, while a byte-order mark is a property of the FILE, not of the SQL in it.
// Before this, a pg_dump written under PowerShell (UTF-16LE with a mark) reached
// the tokenizer as NUL-interleaved bytes and produced a schema with zero of
// everything and no complaint. The decoded content is what is stored in the
// content map too, so snippets, fingerprints and file:line positions are all
// computed against the same text the parser saw.
func readSchemaSources(root string, paths []string) (schemaResolution, error) {
	res := schemaResolution{Content: map[string][]byte{}}
	read := func(abs string) (providers.SourceFile, error) {
		data, err := os.ReadFile(abs)
		if err != nil {
			return providers.SourceFile{}, fmt.Errorf("reading schema %q: %w", abs, err)
		}
		text, _ := sourcetext.Decode(data)
		rel, relErr := filepath.Rel(root, abs)
		if relErr != nil {
			rel = abs
		}
		rel = filepath.ToSlash(rel)
		res.Content[rel] = text
		return providers.SourceFile{Path: rel, Content: text}, nil
	}

	for _, p := range paths {
		// Appended UNCONDITIONALLY, before anything can go wrong with the
		// resolution: an entry whose Files stay empty is the fact this whole
		// change carries, so there is no branch in which a configured path fails
		// to produce one.
		entry := resolvedPath{Configured: p}
		abs := filepath.Join(root, filepath.FromSlash(p))
		info, err := os.Stat(abs)
		if err != nil {
			return schemaResolution{}, fmt.Errorf("reading schema %q: %w", p, err)
		}
		if !info.IsDir() {
			f, err := read(abs)
			if err != nil {
				return schemaResolution{}, err
			}
			entry.Files = append(entry.Files, f)
			res.Paths = append(res.Paths, entry)
			continue
		}
		files, err := flywayOrderedSQL(abs)
		if err != nil {
			return schemaResolution{}, err
		}
		for _, path := range files {
			f, err := read(path)
			if err != nil {
				return schemaResolution{}, err
			}
			entry.Files = append(entry.Files, f)
		}
		res.Paths = append(res.Paths, entry)
	}
	return res, nil
}

var flywayVersion = regexp.MustCompile(`^V(\d+)__`)

// flywayOrderedSQL lists the *.sql files of dir ordered by Flyway version. Files
// named V<n>__*.sql sort by the integer <n>; DECLARED LIMIT — versions with dots
// (V1.1) do NOT match and fall to the non-versioned bucket (lexical order, after
// all versioned files). Flyway R (repeatable) / U (undo) prefixes are out of scope.
//
// DECLARED LIMIT — this listing is ONE LEVEL DEEP. A directory whose .sql files
// sit in a subdirectory resolves to zero files, and that is now SAID rather than
// scored: the path is reported as having resolved to no readable schema file and
// the scan is not a measurement (unread.go, ADR 0072). The capability — reading
// nested trees — is deliberately deferred, because recursion without a
// cross-directory ordering rule would have to pick an order silently, which is
// the same trap one level down that the golang-migrate naming already sets here.
// Not-measured with the path named is the honest shape until that rule exists.
func flywayOrderedSQL(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("listing schema dir %q: %w", dir, err)
	}
	type item struct {
		path      string
		versioned bool
		version   int
		name      string
	}
	var items []item
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".sql") {
			continue
		}
		it := item{path: filepath.Join(dir, e.Name()), name: e.Name()}
		if m := flywayVersion.FindStringSubmatch(e.Name()); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil {
				it.versioned, it.version = true, v
			}
		}
		items = append(items, it)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].versioned != items[j].versioned {
			return items[i].versioned // versioned files first
		}
		if items[i].versioned && items[i].version != items[j].version {
			return items[i].version < items[j].version
		}
		return items[i].name < items[j].name // lexical fallback
	})
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.path
	}
	return out, nil
}
