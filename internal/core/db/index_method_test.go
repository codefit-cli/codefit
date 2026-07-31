package db_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
)

// S3 (db-olap-rules, DW-021) — db.Index gains Method, the analytic-index
// vocabulary DW-021 reads (PostgreSQL "USING brin"/"USING gin"). EMPTY means
// no USING clause was present in the source DDL (the honest default/no-remap
// convention Table.DBName and Trigger.ExecutesFunction already use) — it is
// NEVER defaulted to a guessed "btree".
//
// This is a compile-red test: Index.Method does not exist yet, so this file
// fails to COMPILE until the field is added — the RED signal itself, per
// strict-tdd.md ("the test MUST reference production code that does NOT
// exist yet").

func TestIndex_ZeroValue_MethodIsEmpty(t *testing.T) {
	var idx db.Index
	if idx.Method != "" {
		t.Errorf("Index{} zero value Method = %q, want empty (no USING clause, never defaulted to a guessed method)", idx.Method)
	}
}

func TestIndex_MethodField_CarriesDeclaredValue(t *testing.T) {
	idx := db.Index{Method: "brin"}
	if idx.Method != "brin" {
		t.Errorf("Index.Method = %q, want %q", idx.Method, "brin")
	}
}

// S3 also adds Schema.Dialect — the SQL dialect the schema was parsed from
// ("postgresql"/"mysql"/"sqlserver"), set ONLY by the SQL-DDL provider at its
// one schema-assembly site; the Prisma provider leaves it empty (not
// applicable — Prisma has no per-dialect grammar concept). This is a
// deliberately narrow escape hatch: ordinary DB-0xx rules (dbrules package)
// stay dialect-agnostic exactly as dbcoverage.go already documents (they
// never read this field) — only a DW rule with its own declared, test-locked
// per-dialect limit (DW-021) may consult it.

func TestSchema_ZeroValue_DialectIsEmpty(t *testing.T) {
	var s db.Schema
	if s.Dialect != "" {
		t.Errorf("Schema{} zero value Dialect = %q, want empty (not applicable / not yet set)", s.Dialect)
	}
}

func TestSchema_DialectField_CarriesDeclaredValue(t *testing.T) {
	s := db.Schema{Dialect: "mysql"}
	if s.Dialect != "mysql" {
		t.Errorf("Schema.Dialect = %q, want %q", s.Dialect, "mysql")
	}
}
