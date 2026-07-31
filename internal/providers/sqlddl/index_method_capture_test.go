package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// index-method-capture: reCreateIndex (reduce.go) is widened to actually READ
// three forms it previously only recognized as a genuinely unrecognized
// CREATE INDEX-shaped head (reIndexShapedHead) and dropped, marking the table
// unproven: T-SQL CLUSTERED/NONCLUSTERED, PostgreSQL's anonymous (unnamed)
// index, and MySQL's post-column-list USING BTREE|HASH. This file drives the
// REAL parser end to end (ParseSchema), never a hand-built db.Index literal —
// a hand-built fixture holding values production cannot produce is the
// recurring defect this project's own history names explicitly.

// parseTable parses sql under dialect and returns the table matching name.
func parseTable(t *testing.T, dialect sqlddl.Dialect, sql, name string) db.Table {
	t.Helper()
	p := sqlddl.New(sqlddl.WithDialect(dialect))
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "x.sql", Content: []byte(sql)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	for _, tb := range s.Tables {
		if tb.Name == name {
			return tb
		}
	}
	t.Fatalf("table %q not found in parsed schema (tables: %v)", name, s.Tables)
	return db.Table{}
}

func TestSQLDDL_TSQL_ClusteredNonclusteredIndex_ParsesWithMethod(t *testing.T) {
	tests := []struct {
		name       string
		sql        string
		wantMethod string
		wantUnique bool
	}{
		{
			name:       "NONCLUSTERED",
			sql:        "CREATE TABLE orders (order_id int, customer_id int);\nCREATE NONCLUSTERED INDEX idx_orders_customer ON orders (customer_id);\n",
			wantMethod: "nonclustered",
		},
		{
			name:       "CLUSTERED",
			sql:        "CREATE TABLE orders (order_id int, customer_id int);\nCREATE CLUSTERED INDEX idx_orders_customer ON orders (customer_id);\n",
			wantMethod: "clustered",
		},
		{
			name:       "UNIQUE NONCLUSTERED regression — unique group must not shift",
			sql:        "CREATE TABLE orders (order_id int, customer_id int);\nCREATE UNIQUE NONCLUSTERED INDEX idx_orders_customer ON orders (customer_id);\n",
			wantMethod: "nonclustered",
			wantUnique: true,
		},
		{
			name:       "ordinary CREATE INDEX with no CLUSTERED/NONCLUSTERED qualifier stays empty",
			sql:        "CREATE TABLE orders (order_id int, customer_id int);\nCREATE INDEX idx_orders_customer ON orders (customer_id);\n",
			wantMethod: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tb := parseTable(t, sqlddl.SQLServer(), tt.sql, "orders")
			if !tb.Complete {
				t.Fatalf("Complete = false, want true — this form is now recognized. Note=%q Unreduced=%v", tb.Note, tb.Unreduced)
			}
			if len(tb.Indexes) != 1 {
				t.Fatalf("Indexes = %v, want exactly 1", tb.Indexes)
			}
			ix := tb.Indexes[0]
			if ix.Method != tt.wantMethod {
				t.Errorf("Method = %q, want %q", ix.Method, tt.wantMethod)
			}
			if ix.Unique != tt.wantUnique {
				t.Errorf("Unique = %v, want %v", ix.Unique, tt.wantUnique)
			}
			if len(ix.Columns) != 1 || ix.Columns[0] != "customer_id" {
				t.Errorf("Columns = %v, want [customer_id]", ix.Columns)
			}
		})
	}
}

func TestSQLDDL_PG_AnonymousIndex_ParsesWithMethod(t *testing.T) {
	sql := "CREATE TABLE event_log (event_id integer, payload text);\n" +
		"CREATE INDEX ON event_log USING brin (event_id);\n"
	tb := parseTable(t, sqlddl.Postgres(), sql, "event_log")
	if !tb.Complete {
		t.Fatalf("Complete = false, want true — anonymous CREATE INDEX is now recognized. Note=%q", tb.Note)
	}
	if len(tb.Indexes) != 1 {
		t.Fatalf("Indexes = %v, want exactly 1", tb.Indexes)
	}
	ix := tb.Indexes[0]
	if ix.Method != "brin" {
		t.Errorf("Method = %q, want %q", ix.Method, "brin")
	}
	if len(ix.Columns) != 1 || ix.Columns[0] != "event_id" {
		t.Errorf("Columns = %v, want [event_id]", ix.Columns)
	}
}

// TestSQLDDL_PG_AnonymousIndex_NamedIndexStartingWithOn_StillAttributes is the
// probe the task explicitly calls for: making the index-name capture group
// optional (to support the anonymous form) must not break attribution when a
// REAL index name happens to start with the literal token "on" — e.g.
// "on_hand_idx" — nor when the TABLE itself is named "on".
func TestSQLDDL_PG_AnonymousIndex_NamedIndexStartingWithOn_StillAttributes(t *testing.T) {
	sql := "CREATE TABLE inventory (product_id int, on_hand int);\n" +
		"CREATE INDEX on_hand_idx ON inventory (on_hand);\n"
	tb := parseTable(t, sqlddl.Postgres(), sql, "inventory")
	if !tb.Complete {
		t.Fatalf("Complete = false, want true. Note=%q Unreduced=%v", tb.Note, tb.Unreduced)
	}
	if len(tb.Indexes) != 1 {
		t.Fatalf("Indexes = %v, want exactly 1", tb.Indexes)
	}
	if got := tb.Indexes[0].Columns; len(got) != 1 || got[0] != "on_hand" {
		t.Errorf("Columns = %v, want [on_hand] — an index NAMED on_hand_idx must still attribute to inventory, not vanish or misattribute", got)
	}
}

// TestSQLDDL_PG_AnonymousIndex_OnTableNamedOn covers the OTHER direction of
// the same trap: a table literally named "on", indexed anonymously.
func TestSQLDDL_PG_AnonymousIndex_OnTableNamedOn(t *testing.T) {
	sql := `CREATE TABLE "on" (id int);` + "\n" +
		`CREATE INDEX ON "on" USING gin (id);` + "\n"
	tb := parseTable(t, sqlddl.Postgres(), sql, "on")
	if !tb.Complete {
		t.Fatalf("Complete = false, want true. Note=%q Unreduced=%v", tb.Note, tb.Unreduced)
	}
	if len(tb.Indexes) != 1 {
		t.Fatalf("Indexes = %v, want exactly 1 on table \"on\" — anonymous index must still attribute to the table literally named on", tb.Indexes)
	}
	if got := tb.Indexes[0].Method; got != "gin" {
		t.Errorf("Method = %q, want %q", got, "gin")
	}
}

func TestSQLDDL_MySQL_PostColumnListUsing_ParsesWithMethod(t *testing.T) {
	tests := []struct {
		name       string
		sql        string
		wantMethod string
	}{
		{
			name:       "USING BTREE after column list",
			sql:        "CREATE TABLE t (c int);\nCREATE INDEX ix ON t (c) USING BTREE;\n",
			wantMethod: "btree",
		},
		{
			name:       "USING HASH after column list",
			sql:        "CREATE TABLE t (c int);\nCREATE INDEX ix ON t (c) USING HASH;\n",
			wantMethod: "hash",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tb := parseTable(t, sqlddl.MySQL(), tt.sql, "t")
			if !tb.Complete {
				t.Fatalf("Complete = false, want true. Note=%q Unreduced=%v", tb.Note, tb.Unreduced)
			}
			if len(tb.Indexes) != 1 {
				t.Fatalf("Indexes = %v, want exactly 1", tb.Indexes)
			}
			if got := tb.Indexes[0].Method; got != tt.wantMethod {
				t.Errorf("Method = %q, want %q", got, tt.wantMethod)
			}
		})
	}
}

// TestSQLDDL_PG_ExistingUsingBeforeColumns_Regression locks the PRE-EXISTING
// leading-USING position (already parsed before this slice, just discarded)
// now populates Method instead of being silently thrown away.
func TestSQLDDL_PG_ExistingUsingBeforeColumns_Regression(t *testing.T) {
	sql := "CREATE TABLE t (c int);\nCREATE INDEX ix ON t USING gin (c);\n"
	tb := parseTable(t, sqlddl.Postgres(), sql, "t")
	if len(tb.Indexes) != 1 {
		t.Fatalf("Indexes = %v, want exactly 1", tb.Indexes)
	}
	if got := tb.Indexes[0].Method; got != "gin" {
		t.Errorf("Method = %q, want %q", got, "gin")
	}
}

// TestSQLDDL_OrdinaryNamedIndex_MethodEmpty is the positive control: a plain
// "CREATE INDEX ix ON t (c)" with no USING/CLUSTERED/NONCLUSTERED clause at
// all still parses exactly as before, with Method honestly empty (not
// defaulted to a guessed "btree").
func TestSQLDDL_OrdinaryNamedIndex_MethodEmpty(t *testing.T) {
	sql := "CREATE TABLE t (c int);\nCREATE INDEX ix ON t (c);\n"
	tb := parseTable(t, sqlddl.Postgres(), sql, "t")
	if len(tb.Indexes) != 1 {
		t.Fatalf("Indexes = %v, want exactly 1", tb.Indexes)
	}
	if got := tb.Indexes[0].Method; got != "" {
		t.Errorf("Method = %q, want empty", got)
	}
	if got := tb.Indexes[0].Columns; len(got) != 1 || got[0] != "c" {
		t.Errorf("Columns = %v, want [c]", got)
	}
}
