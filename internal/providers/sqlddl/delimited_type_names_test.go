package sqlddl_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// A DELIMITED type name — T-SQL's [int], MySQL's `int`, ANSI's "int" — is the
// SAME type as the bare word, and every one of the three dialects this package
// supports writes DDL that way. Microsoft's own generated install scripts
// bracket EVERY column's type ([CustomerKey] [int] IDENTITY(1,1) NOT NULL), so
// this is not an exotic spelling: it is the default output of the tooling of
// the dialect codefit added most recently.
//
// THE SEAM. split() is the sole owner of quoting (design §2) and re-emits every
// dialect-quoted identifier canonicalized to ANSI "..." before reduce.go ever
// runs. A type name occupies an identifier position, so by the time the column
// reducer looks a type up in the dialect's TypeMap, [int] / `int` / "int" have
// ALL become the single canonical spelling "int". That is why one dialect-free
// unwrap at the lookup site closes the defect on all three dialects at once,
// with no per-dialect branch — and why the fix is not, and must not be, a
// bracket strip.
//
// The tests below are all through the REAL parser: no hand-built db.Column ever
// appears, because a hand-built one could not reproduce the canonicalization
// that is the whole subject.

// The parser helper is body_test.go's parseDialect (this is one test package).

// colOf returns table.column from a parsed schema, failing when either is absent.
func colOf(t *testing.T, s *db.Schema, table, column string) db.Column {
	t.Helper()
	for _, tb := range s.Tables {
		if tb.Name != table {
			continue
		}
		for _, c := range tb.Columns {
			if c.Name == column {
				return c
			}
		}
		t.Fatalf("column %q not found on table %q", column, table)
	}
	t.Fatalf("table %q not found", table)
	return db.Column{}
}

// TestDelimitedTypeName_TSQLBrackets_ResolveOnRealVendoredDDL is the primary
// lock, over Microsoft's own DDL as vendored — not a constructed approximation.
//
// Every expectation is transcribed FROM testdata/tsql/adventureworksdw_real_objects.sql
// (the type the DDL spells) THROUGH sqlserverTypeMap (the neutral type that
// keyword maps to), never from what the parser happened to emit.
func TestDelimitedTypeName_TSQLBrackets_ResolveOnRealVendoredDDL(t *testing.T) {
	path := filepath.Join("testdata", "tsql", "adventureworksdw_real_objects.sql")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())).ParseSchema(
		[]providers.SourceFile{{Path: path, Content: content}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}

	cases := []struct {
		table, column, ddlType string
		want                   db.Type
	}{
		// DimCustomer — one per distinct bracketed keyword the table declares.
		{"DimCustomer", "CustomerKey", "[int]", db.TypeInt},
		{"DimCustomer", "CustomerAlternateKey", "[nvarchar](15)", db.TypeString},
		{"DimCustomer", "NameStyle", "[bit]", db.TypeBool},
		{"DimCustomer", "BirthDate", "[date]", db.TypeDateTime},
		{"DimCustomer", "MaritalStatus", "[nchar](1)", db.TypeString},
		{"DimCustomer", "YearlyIncome", "[money]", db.TypeFloat},
		{"DimCustomer", "TotalChildren", "[tinyint]", db.TypeInt},
		// DimDate.
		{"DimDate", "DateKey", "[int]", db.TypeInt},
		{"DimDate", "FullDateAlternateKey", "[date]", db.TypeDateTime},
		{"DimDate", "DayNumberOfYear", "[smallint]", db.TypeInt},
		// FactInternetSales.
		{"FactInternetSales", "SalesOrderNumber", "[nvarchar](20)", db.TypeString},
		{"FactInternetSales", "UnitPrice", "[money]", db.TypeFloat},
		{"FactInternetSales", "UnitPriceDiscountPct", "[float]", db.TypeFloat},
		{"FactInternetSales", "OrderDate", "[datetime]", db.TypeDateTime},
	}
	for _, tc := range cases {
		got := colOf(t, s, tc.table, tc.column)
		if got.Type != tc.want {
			t.Errorf("%s.%s declared %s -> Type %q, want %q (RawType %q)",
				tc.table, tc.column, tc.ddlType, got.Type, tc.want, got.RawType)
		}
	}

	// The census, so a partial fix cannot pass: the whole corpus types.
	unclassified := 0
	total := 0
	for _, tb := range s.Tables {
		for _, c := range tb.Columns {
			total++
			if c.Type == db.TypeUnknown || c.Type == "" {
				unclassified++
			}
		}
	}
	if total != 74 {
		t.Fatalf("censused %d columns, want the 74 this fixture declares — the fixture changed", total)
	}
	if unclassified != 0 {
		t.Errorf("%d of %d columns unclassified, want 0 — every type this corpus declares is in sqlserverTypeMap",
			unclassified, total)
	}
}

// RawType stays VERBATIM. The unwrap belongs to the TypeMap lookup only: the
// raw signal an agent (or a future rule) reads must keep the spelling the DDL
// used, canonicalized quoting included.
func TestDelimitedTypeName_RawTypeStaysVerbatim(t *testing.T) {
	s := parseDialect(t, sqlddl.SQLServer(), "CREATE TABLE [dbo].[T] ([A] [int] NOT NULL, [B] [nvarchar](50) NULL);")
	if a := colOf(t, s, "T", "A"); a.RawType != `"int"` {
		t.Errorf("A.RawType = %q, want %q — the canonicalized source spelling, not the lookup key", a.RawType, `"int"`)
	}
	if b := colOf(t, s, "T", "B"); b.RawType != `"nvarchar"(50)` {
		t.Errorf("B.RawType = %q, want %q", b.RawType, `"nvarchar"(50)`)
	}
}

// MySQL backtick-quoted type names are the SAME defect through the SAME
// canonical form — probed on main before the fix, not assumed.
func TestDelimitedTypeName_MySQLBackticksResolve(t *testing.T) {
	s := parseDialect(t, sqlddl.MySQL(), "CREATE TABLE t (`a` `int` NOT NULL, `b` `varchar`(10), `c` `datetime`, d int);")
	for _, tc := range []struct {
		col  string
		want db.Type
	}{{"a", db.TypeInt}, {"b", db.TypeString}, {"c", db.TypeDateTime}, {"d", db.TypeInt}} {
		if got := colOf(t, s, "t", tc.col); got.Type != tc.want {
			t.Errorf("t.%s Type = %q, want %q (RawType %q)", tc.col, got.Type, tc.want, got.RawType)
		}
	}
}

// ANSI double-quoted type names under PostgreSQL — the third spelling of the
// one canonical form.
func TestDelimitedTypeName_ANSIDoubleQuotesResolve(t *testing.T) {
	s := parseDialect(t, sqlddl.Postgres(), `CREATE TABLE t ("a" "int4" NOT NULL, "b" "text", "c" "timestamptz", d integer);`)
	for _, tc := range []struct {
		col  string
		want db.Type
	}{{"a", db.TypeInt}, {"b", db.TypeText}, {"c", db.TypeDateTime}, {"d", db.TypeInt}} {
		if got := colOf(t, s, "t", tc.col); got.Type != tc.want {
			t.Errorf("t.%s Type = %q, want %q (RawType %q)", tc.col, got.Type, tc.want, got.RawType)
		}
	}
}

// THE ONE THING THE FIX MUST NOT BREAK. PostgreSQL's trailing [] is an ARRAY
// MARKER, not a delimiter: a different meaning, in a different position, in the
// one dialect where '[' is NOT an identifier quote at all. typeBase already
// strips it as a SUFFIX; the unwrap only ever removes a MATCHED PAIR of the
// canonical '"' that WRAPS the remaining name, and never touches '[' or ']'.
//
// The third case is what proves the two compose rather than merely coexist:
// "text"[] is BOTH delimited and an array, and both facts must survive.
func TestDelimitedTypeName_PostgresArrayMarkerSurvives(t *testing.T) {
	s := parseDialect(t, sqlddl.Postgres(), `CREATE TABLE t (tags text[], n integer[], q "text"[]);`)
	for _, tc := range []struct {
		col  string
		want db.Type
	}{{"tags", db.TypeText}, {"n", db.TypeInt}, {"q", db.TypeText}} {
		got := colOf(t, s, "t", tc.col)
		if got.Type != tc.want {
			t.Errorf("t.%s Type = %q, want %q (RawType %q)", tc.col, got.Type, tc.want, got.RawType)
		}
		if !got.List {
			t.Errorf("t.%s List = false, want true — the [] array marker must survive the unwrap", tc.col)
		}
	}
}

// db.TypeUnknown must stay the HONEST fallback: making delimited names resolve
// must not make an UNRECOGNIZED keyword resolve to anything. Each case below is
// a real T-SQL/PostgreSQL type that is deliberately absent from the dialect's
// declared vocabulary, plus the schema-qualified user-type form, which is not
// ONE quoted identifier and therefore is never unwrapped.
func TestDelimitedTypeName_UnrecognizedKeywordStaysUnknown(t *testing.T) {
	s := parseDialect(t, sqlddl.SQLServer(),
		"CREATE TABLE [dbo].[T] ([a] [hierarchyid] NULL, [b] [geography] NULL, [c] [sql_variant] NULL, [d] [dbo].[MyType] NULL);")
	for _, col := range []string{"a", "b", "c", "d"} {
		if got := colOf(t, s, "T", col); got.Type != db.TypeUnknown {
			t.Errorf("T.%s Type = %q, want %q — an unmapped keyword must stay the honest fallback (RawType %q)",
				col, got.Type, db.TypeUnknown, got.RawType)
		}
	}

	pg := parseDialect(t, sqlddl.Postgres(), `CREATE TABLE t ("a" "tsvector", "b" "myenum");`)
	for _, col := range []string{"a", "b"} {
		if got := colOf(t, pg, "t", col); got.Type != db.TypeUnknown {
			t.Errorf("t.%s Type = %q, want %q (RawType %q)", col, got.Type, db.TypeUnknown, got.RawType)
		}
	}
}

// THE OTHER typeBase CONSUMER, locked so the fix cannot reach it.
//
// isInlineKeyIndexForm (reduce.go) asks a DIFFERENT question of the same token:
// after a bare, unquoted leading KEY/INDEX, is the next token a TYPE (so this is
// a column named `key`) or an index NAME? There the delimiters are not noise,
// they are the discriminator — an author who DELIMITS the token in that position
// is naming an index precisely BECAUSE the name collides with a keyword, and
// MySQL, the dialect that has this inline form, spells its types bare.
//
// So the unwrap lives at the COLUMN-TYPE lookup site and typeBase itself is
// untouched. Had it gone into typeBase, this schema would lose its index and
// gain a fabricated column named "KEY" of type int.
func TestDelimitedTypeName_InlineKeyIndexFormNotCorrupted(t *testing.T) {
	s := parseDialect(t, sqlddl.MySQL(), "CREATE TABLE t (a int, KEY `int` (a));")
	tb := s.Tables[0]
	if len(tb.Columns) != 1 || tb.Columns[0].Name != "a" {
		t.Fatalf("columns = %+v, want exactly [a] — an index named `int` must not be read as a column", tb.Columns)
	}
	if len(tb.Indexes) != 1 || len(tb.Indexes[0].Columns) != 1 || tb.Indexes[0].Columns[0] != "a" {
		t.Errorf("indexes = %+v, want exactly one index on (a)", tb.Indexes)
	}

	// The complementary direction, so the lock is not one-sided: a real column
	// NAMED key, whose type is bare, still reduces as a column.
	s2 := parseDialect(t, sqlddl.MySQL(), "CREATE TABLE t (`key` varchar(255), b int);")
	if got := colOf(t, s2, "t", "key"); got.Type != db.TypeString {
		t.Errorf("t.key Type = %q, want %q", got.Type, db.TypeString)
	}
}
