package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// S3 (db-olap-rules, DW-021): reCreateIndex's "USING <method>" group was
// non-capturing and discarded. This table-driven test proves the capturing
// change populates db.Index.Method WITHOUT breaking the existing
// unique/if-not-exists/columns capture groups it sits between (the
// submatch-index-shift risk design §7 flagged and task 2.3 named).
func TestCreateIndex_MethodCapture(t *testing.T) {
	cases := []struct {
		name        string
		sql         string
		wantMethod  string
		wantUnique  bool
		wantColumns []string
	}{
		{
			name:        "USING brin sets Method",
			sql:         "CREATE INDEX idx_a ON t USING brin (created_at);\n",
			wantMethod:  "brin",
			wantUnique:  false,
			wantColumns: []string{"created_at"},
		},
		{
			name:        "USING gin sets Method",
			sql:         "CREATE INDEX idx_b ON t USING gin (tags);\n",
			wantMethod:  "gin",
			wantUnique:  false,
			wantColumns: []string{"tags"},
		},
		{
			name:        "no USING clause leaves Method empty",
			sql:         "CREATE INDEX idx_c ON t (name);\n",
			wantMethod:  "",
			wantUnique:  false,
			wantColumns: []string{"name"},
		},
		{
			name:        "UNIQUE + USING together: unique group must not shift",
			sql:         "CREATE UNIQUE INDEX idx_d ON t USING btree (email);\n",
			wantMethod:  "btree",
			wantUnique:  true,
			wantColumns: []string{"email"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := sqlddl.New()
			s, err := p.ParseSchema([]providers.SourceFile{{Path: "x.sql", Content: []byte("CREATE TABLE t (id int);\n" + tc.sql)}})
			if err != nil {
				t.Fatalf("ParseSchema: %v", err)
			}
			tb := requireTable(t, s, "t")
			if len(tb.Indexes) != 1 {
				t.Fatalf("Indexes = %d, want 1", len(tb.Indexes))
			}
			idx := tb.Indexes[0]
			if idx.Method != tc.wantMethod {
				t.Errorf("Method = %q, want %q", idx.Method, tc.wantMethod)
			}
			if idx.Unique != tc.wantUnique {
				t.Errorf("Unique = %v, want %v", idx.Unique, tc.wantUnique)
			}
			if len(idx.Columns) != len(tc.wantColumns) || idx.Columns[0] != tc.wantColumns[0] {
				t.Errorf("Columns = %v, want %v", idx.Columns, tc.wantColumns)
			}
		})
	}
}

// Schema.Dialect is set by the SQL-DDL builder at its one assembly site,
// mirroring database.type's own vocabulary ("postgresql"/"mysql"/"sqlserver")
// — the signal DW-021 consults to abstain honestly on MySQL.
func TestParseSchema_SetsDialect(t *testing.T) {
	cases := []struct {
		name    string
		opt     sqlddl.Option
		want    string
	}{
		{"default (no WithDialect) is postgresql", nil, "postgresql"},
		{"explicit Postgres()", sqlddl.WithDialect(sqlddl.Postgres()), "postgresql"},
		{"MySQL()", sqlddl.WithDialect(sqlddl.MySQL()), "mysql"},
		{"SQLServer()", sqlddl.WithDialect(sqlddl.SQLServer()), "sqlserver"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p *sqlddl.Parser
			if tc.opt != nil {
				p = sqlddl.New(tc.opt)
			} else {
				p = sqlddl.New()
			}
			s, err := p.ParseSchema([]providers.SourceFile{{Path: "x.sql", Content: []byte("CREATE TABLE t (id int);\n")}})
			if err != nil {
				t.Fatalf("ParseSchema: %v", err)
			}
			if s.Dialect != tc.want {
				t.Errorf("Schema.Dialect = %q, want %q", s.Dialect, tc.want)
			}
		})
	}
}

func requireTable(t *testing.T, s *db.Schema, name string) db.Table {
	t.Helper()
	for _, tb := range s.Tables {
		if tb.Name == name {
			return tb
		}
	}
	t.Fatalf("table %q not found in parsed schema (tables: %v)", name, s.Tables)
	return db.Table{}
}
