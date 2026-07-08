package sqlddl_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// --- I8: core-no-enrichment verification ---
//
// Design §6 concluded ZERO core enrichment is needed for MySQL/T-SQL: every
// DB rule (DB-050, DB-001, DB-011, DB-002 — internal/core/dbrules/rules.go)
// reads ONLY neutral db.Schema/db.Table/db.Column fields that already existed
// before this change (PrimaryKey, ForeignKeys, Indexes, Columns[].List). This
// test is the closing PROOF: it runs those 4 rules against the real MySQL
// Sakila and T-SQL AdventureWorks golden fixtures (parsed through the MySQL()
// and SQLServer() dialects added by this SDD change) and asserts the exact,
// hand-verified structural result — never touching a dialect-specific field,
// because dbrules never imports sqlddl or knows a dialect exists.
func TestDBRules_NoCoreEnrichment_MySQLSakila(t *testing.T) {
	s := parseFixture(t, filepath.Join("mysql", "sakila_excerpt.sql"), sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())))
	fs, surf := dbrules.Run(s)

	if got := findingsWithIDLocal(fs, "DB-050"); len(got) != 0 {
		t.Errorf("DB-050 (no PK) = %d, want 0 — every Sakila table in this excerpt declares a PRIMARY KEY", len(got))
	}
	if got := surfaceWithCategoryLocal(surf, surface.CategoryDBFKNoIndex); len(got) != 0 {
		t.Errorf("DB-001 (uncovered FK) = %d, want 0 — film_actor's two FKs are covered by its composite PK and idx_fk_film_id", len(got))
	}
	if got := surfaceWithCategoryLocal(surf, surface.CategoryDBDupIndex); len(got) != 0 {
		t.Errorf("DB-011 (duplicate index) = %d, want 0 — no two indexes share the same columns+uniqueness", len(got))
	}
	if got := surfaceWithCategoryLocal(surf, surface.CategoryDBMultivalued); len(got) != 0 {
		t.Errorf("DB-002 (multivalued column) = %d, want 0 — MySQL ENUM/SET never set Column.List (no native array)", len(got))
	}
}

func TestDBRules_NoCoreEnrichment_TSQLAdventureWorks(t *testing.T) {
	s := parseFixture(t, filepath.Join("tsql", "adventureworks_excerpt.sql"), sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())))
	fs, surf := dbrules.Run(s)

	if got := findingsWithIDLocal(fs, "DB-050"); len(got) != 0 {
		t.Errorf("DB-050 (no PK) = %d, want 0 — every AdventureWorks table in this excerpt declares a PRIMARY KEY", len(got))
	}
	fkItems := surfaceWithCategoryLocal(surf, surface.CategoryDBFKNoIndex)
	if len(fkItems) != 1 {
		t.Fatalf("DB-001 (uncovered FK) = %d, want 1 — SalesOrderHeader.CustomerId has no covering index (idx_customer_account "+
			"only covers AccountNumber; SalesOrderDetail's FK IS covered by its composite PK's leading column)", len(fkItems))
	}
	if got := surfaceWithCategoryLocal(surf, surface.CategoryDBDupIndex); len(got) != 0 {
		t.Errorf("DB-011 (duplicate index) = %d, want 0", len(got))
	}
	if got := surfaceWithCategoryLocal(surf, surface.CategoryDBMultivalued); len(got) != 0 {
		t.Errorf("DB-002 (multivalued column) = %d, want 0 — T-SQL has no native array type", len(got))
	}
}

func parseFixture(t *testing.T, path string, p *sqlddl.Parser) *db.Schema {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	s, err := p.ParseSchema([]providers.SourceFile{{Path: path, Content: content}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	return s
}

func findingsWithIDLocal(fs []findings.Finding, id string) []findings.Finding {
	var out []findings.Finding
	for _, f := range fs {
		if f.ID == id {
			out = append(out, f)
		}
	}
	return out
}

func surfaceWithCategoryLocal(items []findings.SurfaceItem, cat surface.Category) []findings.SurfaceItem {
	var out []findings.SurfaceItem
	for _, it := range items {
		if it.Category == string(cat) {
			out = append(out, it)
		}
	}
	return out
}
