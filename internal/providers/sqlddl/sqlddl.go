package sqlddl

import (
	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
)

// Parser is the SQL-DDL schema parser: it reconstructs the neutral db.Schema by
// applying DDL statements IN ORDER (an incremental reducer over migrations). It
// implements providers.SchemaParser and nothing else — SQL is a schema source,
// not a programming language codefit audits for security/surface (ADR 0018).
type Parser struct{}

// New returns a SQL-DDL schema parser.
func New() *Parser { return &Parser{} }

// compile-time check: the SQL-DDL parser is a schema parser (and only that).
var _ providers.SchemaParser = (*Parser)(nil)

// ParseSchema parses ordered SQL-DDL sources (e.g. Flyway migrations, already
// version-ordered by the caller) into the accumulated final schema. It is
// filesystem-free: the caller reads and orders the files (ADR 0014). Statements
// outside the declared subset are skipped, never an error.
func (*Parser) ParseSchema(sources []providers.SourceFile) (*db.Schema, error) {
	b := newBuilder()
	for _, src := range sources {
		for _, st := range split(src.Content) {
			b.apply(src.Path, st)
		}
	}
	return b.schema(), nil
}
