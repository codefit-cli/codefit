package mcp

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// schemaParserForPaths resolves the schema parser by the INPUT's shape, not by the
// backend language (a schema is orthogonal to the app language — PlantaLinda is
// Java with SQL-DDL migrations; ADR 0018). .prisma → the TS provider's parser;
// .sql (or a directory of migrations) → the SQL-DDL parser, bound to the SQL
// DIALECT named by dbType (design §4/§5) — mysql/postgresql/sqlserver select the
// matching sqlddl.Dialect descriptor; sqlite is an EXPLICIT not-yet-supported
// note (never a silent PostgreSQL parse, RF-03.6); ""/"none" keeps today's
// default (Postgres-parsed by-input resolution) — the heuristic sniff fallback
// (design's Unit J) is NOT YET IMPLEMENTED, so this deliberately does not
// pretend to sniff. A project mixing .prisma and .sql is a declared
// out-of-scope limit for this slice. Returns a note when no parser applies. The
// MCP adapter is the single place that maps input+dbType → concrete parser.
func schemaParserForPaths(root string, paths []string, dbType string) (providers.SchemaParser, string) {
	kinds := map[string]bool{}
	for _, p := range paths {
		kinds[classifySchemaPath(root, p)] = true
	}
	switch {
	case kinds["prisma"] && kinds["sql"]:
		return nil, "mixed schema types (.prisma and .sql) are not supported yet"
	case kinds["prisma"]:
		return typescript.New(), ""
	case kinds["sql"]:
		return sqlDialectParser(dbType)
	default:
		return nil, "no recognized schema files (.prisma / .sql) in database.schema_paths"
	}
}

// sqlDialectParser maps a config database.type string to the sqlddl parser
// bound to the matching dialect descriptor (design §4). sqlite has no
// descriptor and must never fall back to a silent PostgreSQL parse — it
// returns an explicit not-supported note instead (RF-03.6).
//
// TODO(J): "" and "none" currently default to today's behavior (Postgres via
// sqlddl.New()'s default dialect) rather than a heuristic sniff of the SQL
// content (backtick/AUTO_INCREMENT→MySQL, [bracket]/IDENTITY→T-SQL). The sniff
// fallback is design Unit J, deliberately deferred out of this PR — this is
// NOT a silent guess, it is today's pre-existing default, documented honestly.
func sqlDialectParser(dbType string) (providers.SchemaParser, string) {
	switch dbType {
	case "mysql":
		return sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())), ""
	case "sqlserver":
		return sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())), ""
	case "postgresql":
		return sqlddl.New(sqlddl.WithDialect(sqlddl.Postgres())), ""
	case "sqlite":
		return nil, "sqlite DDL parsing is not supported yet"
	default: // "" or "none": today's default (Postgres), sniff fallback not yet implemented (TODO(J))
		return sqlddl.New(), ""
	}
}

// classifySchemaPath labels a schema path by its extension; an extensionless path
// that is a directory is treated as a SQL migration directory (Flyway convention).
func classifySchemaPath(root, p string) string {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".prisma":
		return "prisma"
	case ".sql":
		return "sql"
	}
	if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err == nil && info.IsDir() {
		return "sql"
	}
	return "unknown"
}
