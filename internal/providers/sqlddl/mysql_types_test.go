package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// parseMySQL parses files under the MySQL() dialect — mirrors reduce_test.go's
// parse() helper but binds the parser to MySQL instead of the New() default.
func parseMySQL(t *testing.T, files ...string) *db.Schema {
	t.Helper()
	srcs := make([]providers.SourceFile, len(files))
	for i, f := range files {
		srcs[i] = providers.SourceFile{Path: "m" + string(rune('0'+i)) + ".sql", Content: []byte(f)}
	}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	return s
}

func mysqlTable(t *testing.T, s *db.Schema, name string) db.Table {
	t.Helper()
	for _, tb := range s.Tables {
		if tb.Name == name {
			return tb
		}
	}
	t.Fatalf("table %q not found; have %v", name, s.Tables)
	return db.Table{}
}

func mysqlCol(t *testing.T, tb db.Table, name string) db.Column {
	t.Helper()
	for _, c := range tb.Columns {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("column %q not found on table %q; have %v", name, tb.Name, tb.Columns)
	return db.Column{}
}

// C1: UNSIGNED/AUTO_INCREMENT must be parsed-and-ignored (Modifiers), not
// corrupt the mapped type — and PRIMARY KEY must still be detected.
//
// NOTE (deviation from the tasks brief's literal example): the EXISTING
// convention (see the PG golden: "integer NOT NULL" -> RawType "integer", NOT
// "integer NOT NULL") is that RawType stops at the first Modifiers keyword,
// same as the type expression itself. Since UNSIGNED is itself a Modifiers
// entry (parse-and-ignore, per design §3), RawType is "INT" here, NOT
// "INT UNSIGNED" — matching the existing convention rather than the literal
// string in the task brief.
func TestMySQL_UnsignedAutoIncrementPrimaryKey(t *testing.T) {
	s := parseMySQL(t, "CREATE TABLE `t` (`id` INT UNSIGNED AUTO_INCREMENT PRIMARY KEY);")
	tb := mysqlTable(t, s, "t")
	col := mysqlCol(t, tb, "id")
	if col.Type != db.TypeInt {
		t.Errorf("Type = %s, want %s (raw %q)", col.Type, db.TypeInt, col.RawType)
	}
	if col.RawType != "INT" {
		t.Errorf("RawType = %q, want %q (UNSIGNED/AUTO_INCREMENT are parse-and-ignore Modifiers)", col.RawType, "INT")
	}
	if len(tb.PrimaryKey) != 1 || tb.PrimaryKey[0] != "id" {
		t.Errorf("PrimaryKey = %v, want [id]", tb.PrimaryKey)
	}
}

// C2: ENUM('a','b','c') maps to the neutral TypeEnum with RawType preserved
// verbatim (no depth-0 space inside the parens, so nothing truncates it).
func TestMySQL_EnumType(t *testing.T) {
	s := parseMySQL(t, "CREATE TABLE `t` (`status` ENUM('a','b','c'));")
	col := mysqlCol(t, mysqlTable(t, s, "t"), "status")
	if col.Type != db.TypeEnum {
		t.Errorf("Type = %s, want %s (raw %q)", col.Type, db.TypeEnum, col.RawType)
	}
	if col.RawType != "ENUM('a','b','c')" {
		t.Errorf("RawType = %q, want %q", col.RawType, "ENUM('a','b','c')")
	}
}

// C3: table-driven base-type mapping — RawType always preserved verbatim.
func TestMySQL_TypeMapping_TableDriven(t *testing.T) {
	cases := []struct {
		colDef  string
		colName string
		want    db.Type
		rawType string
	}{
		{"`created_at` datetime", "created_at", db.TypeDateTime, "datetime"},
		{"`flag` tinyint", "flag", db.TypeInt, "tinyint"},
		{"`count` mediumint", "count", db.TypeInt, "mediumint"},
		{"`body` longtext", "body", db.TypeText, "longtext"},
		{"`payload` blob", "payload", db.TypeBytes, "blob"},
		{"`roles` set('a','b')", "roles", db.TypeEnum, "set('a','b')"},
	}
	for _, tc := range cases {
		t.Run(tc.colName, func(t *testing.T) {
			s := parseMySQL(t, "CREATE TABLE `t` ("+tc.colDef+");")
			col := mysqlCol(t, mysqlTable(t, s, "t"), tc.colName)
			if col.Type != tc.want {
				t.Errorf("Type = %s, want %s (raw %q)", col.Type, tc.want, col.RawType)
			}
			if col.RawType != tc.rawType {
				t.Errorf("RawType = %q, want %q", col.RawType, tc.rawType)
			}
		})
	}
}
