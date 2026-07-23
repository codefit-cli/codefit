package query

import "github.com/codefit-cli/codefit/internal/core/db"

// QueryFilter is one WHERE clause of a code-issued database query, in NEUTRAL
// terms — the code side of the code↔schema cross, mirroring how db.Schema is the
// schema side. A provider's QueryExtractor produces it; a core cross-rule
// (internal/core/crossrules) consumes it together with db.Schema.
//
// It carries LOGICAL names (model / field), the same names db.Table.Name and
// db.Column.Name carry — never the physical DB name (@@map/@map), which no query
// in code ever references. The core matches Model EXACTLY against db.Table.Name
// and never re-normalises (all naming-convention knowledge lives in the provider
// that produced this — ADR 0029).
type QueryFilter struct {
	// Model is the model/table the query targets, already normalized by the provider
	// to the schema's naming (e.g. the Prisma Client accessor "user" is emitted as
	// "User").
	Model string
	// Columns are the columns the WHERE constrains, as schema field names
	// (["email"]), in source order, deduplicated. Empty is never produced — a query
	// that filters no column yields no QueryFilter, there is nothing to cross.
	Columns []string
	// Composite is true when the WHERE constrains more than one column together (an
	// implicit or explicit AND) — the DB-013 (missing composite index) signal. A
	// single-column WHERE is not composite.
	Composite bool
	// Pos is the call-site anchor (file:line), reusing the schema-world anchor type
	// so a cross finding anchors exactly like a schema finding.
	Pos db.Pos
}
