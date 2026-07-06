package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// parseTSQL parses files under the SQLServer() dialect — mirrors
// mysql_types_test.go's parseMySQL helper but binds the parser to SQLServer.
func parseTSQL(t *testing.T, files ...string) *db.Schema {
	t.Helper()
	srcs := make([]providers.SourceFile, len(files))
	for i, f := range files {
		srcs[i] = providers.SourceFile{Path: "t" + string(rune('0'+i)) + ".sql", Content: []byte(f)}
	}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	return s
}

func tsqlTable(t *testing.T, s *db.Schema, name string) db.Table {
	t.Helper()
	for _, tb := range s.Tables {
		if tb.Name == name {
			return tb
		}
	}
	t.Fatalf("table %q not found; have %v", name, s.Tables)
	return db.Table{}
}

func tsqlCol(t *testing.T, tb db.Table, name string) db.Column {
	t.Helper()
	for _, c := range tb.Columns {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("column %q not found on table %q; have %v", name, tb.Name, tb.Columns)
	return db.Column{}
}

// F1: IDENTITY(1,1) must be parsed-and-ignored (Modifiers), not corrupt the
// mapped type — and PRIMARY KEY must still be detected.
//
// NOTE (deviation from the tasks brief's literal example): per the EXISTING
// convention (see the PG golden: "integer NOT NULL" -> RawType "integer", and
// Unit C's MySQL "INT UNSIGNED" -> RawType "INT"), RawType stops at the first
// Modifiers keyword, same cutoff as the mapped Type itself. IDENTITY is
// itself a Modifiers entry (parse-and-ignore, per design §3), so RawType is
// "INT" here, NOT "INT IDENTITY(1,1)" — matching the existing convention
// rather than the literal string in the task brief. splitTypeAndMods already
// strips a trailing "(...)" from the candidate word ("IDENTITY(1,1)" ->
// "IDENTITY") before the Modifiers lookup (added in Unit C for MySQL's
// AUTO_INCREMENT-with-no-parens case, but the paren-stripping itself was
// written generically) — confirmed this handles IDENTITY(1,1) unchanged.
func TestTSQL_IdentityPrimaryKey(t *testing.T) {
	s := parseTSQL(t, "CREATE TABLE [dbo].[Users] ([Id] INT IDENTITY(1,1) PRIMARY KEY);")
	tb := tsqlTable(t, s, "Users")
	col := tsqlCol(t, tb, "Id")
	if col.Type != db.TypeInt {
		t.Errorf("Type = %s, want %s (raw %q)", col.Type, db.TypeInt, col.RawType)
	}
	if col.RawType != "INT" {
		t.Errorf("RawType = %q, want %q (IDENTITY(1,1) is a parse-and-ignore Modifier)", col.RawType, "INT")
	}
	if len(tb.PrimaryKey) != 1 || tb.PrimaryKey[0] != "Id" {
		t.Errorf("PrimaryKey = %v, want [Id]", tb.PrimaryKey)
	}
}

// F2: table-driven T-SQL base-type mapping — RawType always preserved
// verbatim (these cases carry no trailing modifier, so RawType is the bare
// type expression including its own "(...)" length/precision, if any).
func TestTSQL_TypeMapping_TableDriven(t *testing.T) {
	cases := []struct {
		colDef  string
		colName string
		want    db.Type
		rawType string
	}{
		{"[Name] nvarchar(255)", "Name", db.TypeString, "nvarchar(255)"},
		{"[Initial] nchar", "Initial", db.TypeString, "nchar"},
		{"[IsActive] bit", "IsActive", db.TypeBool, "bit"},
		{"[CreatedAt] datetime2", "CreatedAt", db.TypeDateTime, "datetime2"},
		{"[UpdatedAt] smalldatetime", "UpdatedAt", db.TypeDateTime, "smalldatetime"},
		{"[Guid] uniqueidentifier", "Guid", db.TypeString, "uniqueidentifier"},
		{"[Amount] money", "Amount", db.TypeFloat, "money"},
		{"[SmallAmount] smallmoney", "SmallAmount", db.TypeFloat, "smallmoney"},
		{"[Payload] varbinary(max)", "Payload", db.TypeBytes, "varbinary(max)"},
		{"[Count] int", "Count", db.TypeInt, "int"},
		{"[BigCount] bigint", "BigCount", db.TypeInt, "bigint"},
	}
	for _, tc := range cases {
		t.Run(tc.colName, func(t *testing.T) {
			s := parseTSQL(t, "CREATE TABLE [dbo].[T] ("+tc.colDef+");")
			col := tsqlCol(t, tsqlTable(t, s, "T"), tc.colName)
			if col.Type != tc.want {
				t.Errorf("Type = %s, want %s (raw %q)", col.Type, tc.want, col.RawType)
			}
			if col.RawType != tc.rawType {
				t.Errorf("RawType = %q, want %q", col.RawType, tc.rawType)
			}
		})
	}
}

// F4: a schema-qualified FOREIGN KEY ... REFERENCES [dbo].[Orders](...)
// resolves to the same RefTable/RefColumns shape PG/MySQL FKs produce. The
// reducer is dialect-free (reReferences + normalizeName), so this locks that
// the bracket-quoted, schema-qualified REFERENCES target canonicalizes and
// strips its schema qualifier exactly like PG's "public.orders" does.
func TestTSQL_ForeignKey_SchemaQualifiedReferences(t *testing.T) {
	s := parseTSQL(t, `
CREATE TABLE [dbo].[Orders] ([Id] INT IDENTITY(1,1) PRIMARY KEY);
CREATE TABLE [dbo].[OrderItems] (
  [Id] INT IDENTITY(1,1) PRIMARY KEY,
  [OrderId] INT NOT NULL,
  CONSTRAINT [FK_OrderItems_Orders] FOREIGN KEY ([OrderId]) REFERENCES [dbo].[Orders]([Id])
);`)
	tb := tsqlTable(t, s, "OrderItems")
	if len(tb.ForeignKeys) != 1 {
		t.Fatalf("ForeignKeys = %v, want exactly 1", tb.ForeignKeys)
	}
	fk := tb.ForeignKeys[0]
	if len(fk.Columns) != 1 || fk.Columns[0] != "OrderId" {
		t.Errorf("fk.Columns = %v, want [OrderId]", fk.Columns)
	}
	if fk.RefTable != "Orders" {
		t.Errorf("fk.RefTable = %q, want %q (schema qualifier must be stripped)", fk.RefTable, "Orders")
	}
	if len(fk.RefColumns) != 1 || fk.RefColumns[0] != "Id" {
		t.Errorf("fk.RefColumns = %v, want [Id]", fk.RefColumns)
	}
}
