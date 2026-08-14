package db

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/codefit-cli/codefit/internal/providers"
)

// ResolveSchemaPath resolves ONE database.schema_paths entry exactly as an audit
// would, and returns the ordered, DECODED sources it produced.
//
// It delegates to readSchemaSources rather than repeating it, and that is the
// whole point of the function existing. `codefit init` has to decide whether a
// directory reconstructs into a schema BEFORE it writes that directory into a
// config; if it answered that question against its own reader, init-time proof
// and scan-time behaviour could disagree about the same path — different apply
// order, or different bytes. Two things in particular are not reproducible by
// inspection and must come from the real reader: Flyway integer ordering, and
// the byte-order-mark decode (a UTF-16LE dump reaches a tokenizer as
// NUL-interleaved bytes and reduces to a schema with zero of everything, without
// complaining).
//
// A path that does not exist is an ERROR here, as it is at scan time: a
// misconfiguration is a different fact from "this project has no schema", and
// collapsing the two is how a typo becomes a clean bill of health.
func ResolveSchemaPath(root, path string) ([]providers.SourceFile, error) {
	res, err := readSchemaSources(root, []string{path})
	if err != nil {
		return nil, err
	}
	return res.sources(), nil
}

// OrderingIsProven reports whether the .sql files sitting AT dir's own level
// will be ordered by proof rather than by the alphabet, and how many .sql files
// that level holds.
//
// Proven means: at least one .sql file, and every .sql file at that level
// carries an integer version flywayVersion matches. One stray name is enough to
// unprove the whole level, because a name flywayOrderedSQL cannot version lands
// in its LEXICAL bucket — and from that point the directory's apply order is a
// fact about the alphabet, not about its contents. golang-migrate's `1_init.up.sql`
// is the case that matters in practice: `10_x` sorts before `1_init`.
//
// DISCOVERY DEPTH IS NOT READ DEPTH. flywayOrderedSQL reads exactly one level
// (its own DECLARED LIMIT, ADR 0072), so this reports on exactly one level too.
// A caller that walks a tree looking for candidate directories may go as deep as
// it likes; what it must never do is write a path whose schema files live below
// the level the resolver reads. Answering about one level is how this function
// keeps the two numbers from being confused.
//
// It re-applies flywayVersion instead of sharing flywayOrderedSQL's loop. That
// duplication is deliberate — extracting a shared helper would edit sources.go,
// which this change keeps byte-identical as its ADR 0072 evidence — and it is
// LOCKED by TestOrderingIsProven_AgreesWithTheResolversOrdering rather than left
// to drift. Because the predicate lives here, beside the regex, a future
// flywayOrderedSQL that learns golang-migrate ordering widens init's strictness
// automatically and no scaffold line changes.
func OrderingIsProven(dir string) (proven bool, sqlFiles int, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, 0, fmt.Errorf("listing schema dir %q: %w", dir, err)
	}
	allVersioned := true
	for _, e := range entries {
		// Same predicate as flywayOrderedSQL: directories are not files at this
		// level, and the extension match is case-insensitive.
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".sql") {
			continue
		}
		sqlFiles++
		if !flywayVersion.MatchString(e.Name()) {
			allVersioned = false
		}
	}
	return sqlFiles > 0 && allVersioned, sqlFiles, nil
}
