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

// readSchemaSources resolves the configured schema paths from disk into ORDERED
// SourceFiles (the sensor is the filesystem-side caller, ADR 0014). A path that is
// a directory is expanded to its *.sql files in Flyway version order (V<n>__…); a
// path that is a file is read as-is. Order across entries is config order; within a
// directory it is Flyway version order. A configured-but-unreadable path is a hard
// error (a real misconfiguration, not "no DB").
//
// Bytes become TEXT here, through sourcetext.Decode, and this is the right layer
// for it by ADR 0014's own division: the parser is filesystem-free and receives
// text, while a byte-order mark is a property of the FILE, not of the SQL in it.
// Before this, a pg_dump written under PowerShell (UTF-16LE with a mark) reached
// the tokenizer as NUL-interleaved bytes and produced a schema with zero of
// everything and no complaint. The decoded content is what is stored in the
// content map too, so snippets, fingerprints and file:line positions are all
// computed against the same text the parser saw.
func readSchemaSources(root string, paths []string) ([]providers.SourceFile, map[string][]byte, error) {
	var sources []providers.SourceFile
	content := map[string][]byte{}
	add := func(abs string) error {
		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Errorf("reading schema %q: %w", abs, err)
		}
		text, _ := sourcetext.Decode(data)
		rel, relErr := filepath.Rel(root, abs)
		if relErr != nil {
			rel = abs
		}
		rel = filepath.ToSlash(rel)
		sources = append(sources, providers.SourceFile{Path: rel, Content: text})
		content[rel] = text
		return nil
	}

	for _, p := range paths {
		abs := filepath.Join(root, filepath.FromSlash(p))
		info, err := os.Stat(abs)
		if err != nil {
			return nil, nil, fmt.Errorf("reading schema %q: %w", p, err)
		}
		if !info.IsDir() {
			if err := add(abs); err != nil {
				return nil, nil, err
			}
			continue
		}
		files, err := flywayOrderedSQL(abs)
		if err != nil {
			return nil, nil, err
		}
		for _, f := range files {
			if err := add(f); err != nil {
				return nil, nil, err
			}
		}
	}
	return sources, content, nil
}

var flywayVersion = regexp.MustCompile(`^V(\d+)__`)

// flywayOrderedSQL lists the *.sql files of dir ordered by Flyway version. Files
// named V<n>__*.sql sort by the integer <n>; DECLARED LIMIT — versions with dots
// (V1.1) do NOT match and fall to the non-versioned bucket (lexical order, after
// all versioned files). Flyway R (repeatable) / U (undo) prefixes are out of scope.
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
