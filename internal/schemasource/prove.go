package schemasource

import (
	"fmt"
	"path/filepath"

	"github.com/codefit-cli/codefit/internal/sensors/db"
)

// Proof is the measured account of one candidate directory: whether its apply
// order is proven, how many .sql files sit at its own level, how many tables the
// REAL parser reconstructed from it, and — when no parser applied — why.
//
// Every field is a measurement. Nothing here is prose about a project: the
// wording that reaches a developer is internal/scaffold's job, and keeping it
// out of this package is what stops a reason sentence from drifting away from
// the number it explains.
type Proof struct {
	// Ordered reports db.OrderingIsProven for this directory's own level: the
	// apply order comes from integer versions rather than from the alphabet.
	Ordered bool
	// SQLFiles counts the .sql files at the directory's own level.
	SQLFiles int
	// Tables is the number of tables the real parser reconstructed. It is the
	// only number that may promote a candidate to a live schema_paths entry.
	Tables int
	// Note carries the parser-binding note when no parser applied to this path.
	Note string
}

// Prove answers the one question `codefit init` is allowed to answer before it
// writes a database block: does this directory RECONSTRUCT into a schema?
//
// It never guesses. The chain is the scan-time chain, in scan-time order:
//
//	db.OrderingIsProven   — the ^V(\d+)__ shape gate, beside the regex that owns it
//	db.ResolveSchemaPath  — the literal scan-time reader: Flyway ordering + BOM decode
//	ParserForPaths(root, []string{relDir}, "")
//	                      — the SAME binding scandb.go makes when database.type is unset
//	ParseSchema           — the real parser, over the real bytes
//
// Running the scan-time binding is the point, not a convenience. If init proved
// a path against its own reader, init-time proof and scan-time behaviour could
// disagree about the same directory, and the config would promise a schema the
// audit never reads. What remains is a declared residual: the DIALECT may be
// wrong (codefit does not sniff it), never that init and the scan read different
// schemas. Callers must state that residual where the developer can see it.
//
// An unordered directory short-circuits: its files are not read and no parser
// runs, because a directory whose apply order is a fact about the alphabet
// cannot be proven by parsing it in that order.
//
// relDir is project-relative and slash-spelled, exactly as it would be written
// into database.schema_paths.
func Prove(root, relDir string) (Proof, error) {
	ordered, sqlFiles, err := db.OrderingIsProven(filepath.Join(root, filepath.FromSlash(relDir)))
	if err != nil {
		return Proof{}, fmt.Errorf("proving schema candidate %q: %w", relDir, err)
	}
	p := Proof{Ordered: ordered, SQLFiles: sqlFiles}
	if !ordered {
		return p, nil
	}

	sources, err := db.ResolveSchemaPath(root, relDir)
	if err != nil {
		return Proof{}, fmt.Errorf("proving schema candidate %q: %w", relDir, err)
	}

	parser, note := ParserForPaths(root, []string{relDir}, "")
	if parser == nil {
		p.Note = note
		return p, nil
	}
	schema, err := parser.ParseSchema(sources)
	if err != nil {
		// A parse error is a measurement too: this directory did not reconstruct.
		// It is not an error of Prove's, and turning it into one would make a
		// malformed migration indistinguishable from a broken path.
		p.Note = err.Error()
		return p, nil
	}
	if schema != nil {
		p.Tables = len(schema.Tables)
	}
	return p, nil
}
