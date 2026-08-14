package schemasource

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
)

func TestSchemaParserForPaths_ByInput(t *testing.T) {
	root := t.TempDir()

	if p, note := ParserForPaths(root, []string{"prisma/schema.prisma"}, "postgresql"); p == nil || note != "" {
		t.Errorf(".prisma should resolve a parser, got nil=%v note=%q", p == nil, note)
	}
	if p, note := ParserForPaths(root, []string{"db/migration/V1__x.sql"}, "postgresql"); p == nil || note != "" {
		t.Errorf(".sql should resolve a parser, got nil=%v note=%q", p == nil, note)
	}
	// A directory (no extension) is treated as a SQL migration dir.
	if err := os.MkdirAll(filepath.Join(root, "db", "migration"), 0o755); err != nil {
		t.Fatal(err)
	}
	if p, note := ParserForPaths(root, []string{"db/migration"}, "postgresql"); p == nil || note != "" {
		t.Errorf("a directory should resolve the SQL parser, got nil=%v note=%q", p == nil, note)
	}
	// Mixed .prisma + .sql is a declared out-of-scope limit → no parser + note.
	if p, note := ParserForPaths(root, []string{"a.prisma", "b.sql"}, "postgresql"); p != nil || note == "" {
		t.Errorf("mixed schema types must not resolve (nil + note), got nil=%v note=%q", p == nil, note)
	}
	// The relocated "no parser" case: an unrecognized schema type.
	if p, note := ParserForPaths(root, []string{"schema.txt"}, "postgresql"); p != nil || note == "" {
		t.Errorf("unrecognized schema type must not resolve (nil + note), got nil=%v note=%q", p == nil, note)
	}
}

// parseOne is a small test helper: runs a resolved parser over one inline SQL
// source and fails the test on a parse error.
func parseOne(t *testing.T, p providers.SchemaParser, sql string) *db.Schema {
	t.Helper()
	schema, err := p.ParseSchema([]providers.SourceFile{{Path: "schema.sql", Content: []byte(sql)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	return schema
}

// assertMySQLTableParsed asserts the backtick/ENUM MySQL construct parsed
// correctly: table name canonicalized, ENUM mapped to db.TypeEnum (not
// TypeUnknown), column captured as NOT NULL.
func assertMySQLTableParsed(t *testing.T, schema *db.Schema) {
	t.Helper()
	if len(schema.Tables) != 1 || schema.Tables[0].Name != "orders" {
		t.Fatalf("expected one table named orders, got %+v", schema.Tables)
	}
	tbl := schema.Tables[0]
	if len(tbl.Columns) != 1 || tbl.Columns[0].Type != db.TypeEnum {
		t.Fatalf("expected one ENUM column, got %+v", tbl.Columns)
	}
	if tbl.Columns[0].Nullable {
		t.Errorf("status column has NOT NULL, want Nullable=false")
	}
}

// assertSQLServerTableParsed asserts the bracket/IDENTITY T-SQL construct
// parsed correctly: schema-qualified bracket table name canonicalized, PK
// detected, IDENTITY parsed-and-ignored (INT still maps to db.TypeInt, not
// TypeUnknown).
func assertSQLServerTableParsed(t *testing.T, schema *db.Schema) {
	t.Helper()
	if len(schema.Tables) != 1 || schema.Tables[0].Name != "Orders" {
		t.Fatalf("expected one table named Orders, got %+v", schema.Tables)
	}
	tbl := schema.Tables[0]
	if len(tbl.PrimaryKey) != 1 || tbl.PrimaryKey[0] != "Id" {
		t.Fatalf("expected PK=[Id], got %+v", tbl.PrimaryKey)
	}
	if len(tbl.Columns) != 1 || tbl.Columns[0].Type != db.TypeInt {
		t.Fatalf("expected one INT column (IDENTITY ignored, not corrupting the type), got %+v", tbl.Columns)
	}
}

// assertPostgresTableParsed asserts the plain PostgreSQL construct parsed
// correctly: unchanged from before dbType existed.
func assertPostgresTableParsed(t *testing.T, schema *db.Schema) {
	t.Helper()
	if len(schema.Tables) != 1 || schema.Tables[0].Name != "orders" {
		t.Fatalf("expected one table named orders, got %+v", schema.Tables)
	}
	tbl := schema.Tables[0]
	if len(tbl.PrimaryKey) != 1 || tbl.PrimaryKey[0] != "id" {
		t.Fatalf("expected PK=[id], got %+v", tbl.PrimaryKey)
	}
	if len(tbl.Columns) != 1 || tbl.Columns[0].Type != db.TypeInt {
		t.Fatalf("expected one SERIAL->int column, got %+v", tbl.Columns)
	}
}

// containsFold reports whether s contains substr, case-insensitively.
func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// TestSchemaParserForPaths_DBTypeBindsDialect locks design §4/§5's dbType→dialect
// mapping: an explicit database.type binds the matching sqlddl dialect
// descriptor, proven by parsing a construct that ONLY that dialect understands
// correctly (a construct any other dialect would mis-tokenize or leave
// TypeUnknown).
func TestSchemaParserForPaths_DBTypeBindsDialect(t *testing.T) {
	root := t.TempDir()

	t.Run("mysql binds the MySQL dialect", func(t *testing.T) {
		p, note := ParserForPaths(root, []string{"schema.sql"}, "mysql")
		if p == nil {
			t.Fatalf("expected a parser, got note=%q", note)
		}
		// Backtick identifiers + ENUM are MySQL-only constructs: under the wrong
		// dialect the backtick-quoted table name would not canonicalize/parse as
		// a CREATE TABLE at all, or ENUM would fall to TypeUnknown.
		schema := parseOne(t, p, "CREATE TABLE `orders` (`status` ENUM('a','b') NOT NULL);")
		assertMySQLTableParsed(t, schema)
	})

	t.Run("sqlserver binds the SQLServer dialect", func(t *testing.T) {
		p, note := ParserForPaths(root, []string{"schema.sql"}, "sqlserver")
		if p == nil {
			t.Fatalf("expected a parser, got note=%q", note)
		}
		// Bracket identifiers + IDENTITY are T-SQL-only constructs.
		schema := parseOne(t, p, "CREATE TABLE [dbo].[Orders] ([Id] INT IDENTITY(1,1) PRIMARY KEY);")
		assertSQLServerTableParsed(t, schema)
	})

	t.Run("postgresql binds the Postgres dialect, unchanged from before dbType existed", func(t *testing.T) {
		p, note := ParserForPaths(root, []string{"schema.sql"}, "postgresql")
		if p == nil {
			t.Fatalf("expected a parser, got note=%q", note)
		}
		schema := parseOne(t, p, `CREATE TABLE "orders" ("id" SERIAL PRIMARY KEY);`)
		assertPostgresTableParsed(t, schema)
	})
}

// TestSchemaParserForPaths_SQLiteIsExplicitlyNotSupported locks RF-03.6: an
// explicit sqlite database.type must return an honest not-supported note and
// NEVER a silent PostgreSQL parse (the cardinal sin this unit exists to lock).
func TestSchemaParserForPaths_SQLiteIsExplicitlyNotSupported(t *testing.T) {
	root := t.TempDir()
	p, note := ParserForPaths(root, []string{"schema.sql"}, "sqlite")
	if p != nil {
		t.Fatalf("sqlite must never resolve a parser (would silently PG-parse), got %v", p)
	}
	if note == "" || !containsFold(note, "sqlite") {
		t.Fatalf("sqlite must return an explicit not-supported note, got %q", note)
	}
}

// TestSchemaParserForPaths_UnrecognizedDBTypeIsExplicit locks that a
// NON-EMPTY, unrecognized dbType (e.g. a typo, or a garbage value that
// bypassed config.validate) must NEVER silently fall back to the Postgres
// parser — codefit's "never silently guess" doctrine. Unlike ""/"none"
// (today's honest default, TODO(J)), an unrecognized-but-non-empty type gets
// an explicit not-bound note naming the type, mirroring the sqlite branch.
func TestSchemaParserForPaths_UnrecognizedDBTypeIsExplicit(t *testing.T) {
	root := t.TempDir()
	p, note := ParserForPaths(root, []string{"schema.sql"}, "oracle")
	if p != nil {
		t.Fatalf("an unrecognized dbType must never resolve a parser (would silently PG-parse), got %v", p)
	}
	if note == "" || !containsFold(note, "oracle") {
		t.Fatalf("an unrecognized dbType must return an explicit note naming the type, got %q", note)
	}
}

// TestSchemaParserForPaths_NoDBTypeKeepsTodaysDefault locks that an empty or
// "none" dbType preserves the pre-Unit-H default (Postgres-parsed by-input
// resolution) — the sniff heuristic (Unit J) is not yet implemented, and this
// must not silently pretend to sniff.
func TestSchemaParserForPaths_NoDBTypeKeepsTodaysDefault(t *testing.T) {
	root := t.TempDir()
	for _, dbType := range []string{"", "none"} {
		p, note := ParserForPaths(root, []string{"schema.sql"}, dbType)
		if p == nil {
			t.Fatalf("dbType=%q must still resolve a parser (today's default), got note=%q", dbType, note)
		}
		schema := parseOne(t, p, `CREATE TABLE "orders" ("id" SERIAL PRIMARY KEY);`)
		assertPostgresTableParsed(t, schema)
	}
}
