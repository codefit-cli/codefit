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
// .sql (or a directory of migrations) → the SQL-DDL parser. A project mixing both
// is a declared out-of-scope limit for this slice. Returns a note when no parser
// applies. The MCP adapter is the single place that maps input → concrete parser.
func schemaParserForPaths(root string, paths []string) (providers.SchemaParser, string) {
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
		return sqlddl.New(), ""
	default:
		return nil, "no recognized schema files (.prisma / .sql) in database.schema_paths"
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
