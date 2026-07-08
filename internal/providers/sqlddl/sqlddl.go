package sqlddl

import (
	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
)

// Parser is the SQL-DDL schema parser: it reconstructs the neutral db.Schema by
// applying DDL statements IN ORDER (an incremental reducer over migrations). It
// implements providers.SchemaParser and nothing else — SQL is a schema source,
// not a programming language codefit audits for security/surface (ADR 0018).
//
// A Parser is bound to exactly one Dialect at construction time (never
// threaded through ParseSchema/SchemaParser) — see Option/WithDialect.
type Parser struct {
	dialect Dialect
}

// Option configures a Parser at construction time.
type Option func(*Parser)

// WithDialect binds the parser to the given SQL dialect descriptor. Absent
// this option, New() defaults to Postgres() — every existing caller is
// unaffected.
func WithDialect(d Dialect) Option {
	return func(p *Parser) { p.dialect = d }
}

// New returns a SQL-DDL schema parser. Without options it defaults to the
// PostgreSQL dialect — today's behavior, unchanged.
func New(opts ...Option) *Parser {
	p := &Parser{dialect: Postgres()}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// compile-time check: the SQL-DDL parser is a schema parser (and only that).
var _ providers.SchemaParser = (*Parser)(nil)

// ParseSchema parses ordered SQL-DDL sources (e.g. Flyway migrations, already
// version-ordered by the caller) into the accumulated final schema. It is
// filesystem-free: the caller reads and orders the files (ADR 0014). Statements
// outside the declared subset are skipped, never an error.
func (p *Parser) ParseSchema(sources []providers.SourceFile) (*db.Schema, error) {
	b := newBuilder(&p.dialect)
	for _, src := range sources {
		for _, st := range split(src.Content, &p.dialect) {
			b.apply(src.Path, st)
		}
	}
	return b.schema(), nil
}
