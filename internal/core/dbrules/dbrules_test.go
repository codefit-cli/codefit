package dbrules_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// --- helpers ---

func run(s *db.Schema) ([]findings.Finding, []findings.SurfaceItem) { return dbrules.Run(s) }

func findingsWithID(fs []findings.Finding, id string) []findings.Finding {
	var out []findings.Finding
	for _, f := range fs {
		if f.ID == id {
			out = append(out, f)
		}
	}
	return out
}

func surfaceWithCategory(items []findings.SurfaceItem, cat surface.Category) []findings.SurfaceItem {
	var out []findings.SurfaceItem
	for _, it := range items {
		if it.Category == string(cat) {
			out = append(out, it)
		}
	}
	return out
}

// --- DB-050: table without a primary key (affirmation) ---

func TestDB050_FiresOnNoPK(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		{Name: "NoPK", Pos: db.Pos{File: "s.prisma", Line: 7}, Columns: []db.Column{{Name: "x", Type: db.TypeString}}},
	}}
	fs, _ := run(s)
	got := findingsWithID(fs, "DB-050")
	if len(got) != 1 {
		t.Fatalf("DB-050 findings = %d, want 1", len(got))
	}
	f := got[0]
	if f.Dimension != findings.DimensionDB || f.Confidence != 1.0 || f.Probabilistic {
		t.Errorf("DB-050 = {dim:%s conf:%v prob:%v}, want {db 1 false} (affirmation)", f.Dimension, f.Confidence, f.Probabilistic)
	}
	if f.Severity != findings.SeverityMedium {
		t.Errorf("DB-050 severity = %s, want medium", f.Severity)
	}
	if f.Line != 7 || f.File != "s.prisma" {
		t.Errorf("DB-050 anchor = %s:%d, want s.prisma:7 (Table.Pos)", f.File, f.Line)
	}
}

func TestDB050_NegativeWithPK(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{{Name: "HasPK", PrimaryKey: []string{"id"}}}}
	fs, _ := run(s)
	if got := findingsWithID(fs, "DB-050"); len(got) != 0 {
		t.Errorf("DB-050 should not fire on a table with a PK, got %d", len(got))
	}
}

// --- DB-001: foreign key with no covering index (surface) ---

func db001Table(fks []db.ForeignKey, idx []db.Index, pk []string) *db.Schema {
	return &db.Schema{Tables: []db.Table{{
		Name: "T", Pos: db.Pos{File: "s.prisma", Line: 1}, PrimaryKey: pk, ForeignKeys: fks, Indexes: idx,
	}}}
}

func TestDB001_Uncovered(t *testing.T) {
	s := db001Table(
		[]db.ForeignKey{{Columns: []string{"userId"}, RefTable: "User", RefColumns: []string{"id"}, Pos: db.Pos{File: "s.prisma", Line: 5}}},
		nil, []string{"id"})
	_, items := run(s)
	got := surfaceWithCategory(items, surface.CategoryDBFKNoIndex)
	if len(got) != 1 {
		t.Fatalf("DB-001 surface = %d, want 1", len(got))
	}
	it := got[0]
	if v, ok := it.StructuralFacts["covering_index_detected"]; !ok || v {
		t.Errorf("covering_index_detected = %v (ok=%v), want false", v, ok)
	}
	if it.Line != 5 || it.File != "s.prisma" {
		t.Errorf("DB-001 anchor = %s:%d, want s.prisma:5 (ForeignKey.Pos)", it.File, it.Line)
	}
}

func TestDB001_CoveredByPK(t *testing.T) {
	// FK on [id] where the PK is [id] → covered by the implicit PK index.
	s := db001Table(
		[]db.ForeignKey{{Columns: []string{"id"}, RefTable: "Other", RefColumns: []string{"id"}}},
		nil, []string{"id"})
	_, items := run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBFKNoIndex); len(got) != 0 {
		t.Errorf("FK that IS the PK must be covered (implicit PK index), got %d", len(got))
	}
}

func TestDB001_CoveredByUnique(t *testing.T) {
	s := db001Table(
		[]db.ForeignKey{{Columns: []string{"email"}, RefTable: "User", RefColumns: []string{"email"}}},
		[]db.Index{{Columns: []string{"email"}, Unique: true}}, []string{"id"})
	_, items := run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBFKNoIndex); len(got) != 0 {
		t.Errorf("FK covered by a @unique index must not fire, got %d", len(got))
	}
}

func TestDB001_CoveredByCompositePrefix(t *testing.T) {
	// FK [a] is the leading prefix of index [a,b] → covered.
	s := db001Table(
		[]db.ForeignKey{{Columns: []string{"a"}, RefTable: "X", RefColumns: []string{"id"}}},
		[]db.Index{{Columns: []string{"a", "b"}}}, []string{"id"})
	_, items := run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBFKNoIndex); len(got) != 0 {
		t.Errorf("FK as leading prefix of a composite index must be covered, got %d", len(got))
	}
}

func TestDB001_NonLeadingNotCovered(t *testing.T) {
	// FK [b] is a NON-leading member of index [a,b] → NOT covered.
	s := db001Table(
		[]db.ForeignKey{{Columns: []string{"b"}, RefTable: "X", RefColumns: []string{"id"}}},
		[]db.Index{{Columns: []string{"a", "b"}}}, []string{"id"})
	_, items := run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBFKNoIndex); len(got) != 1 {
		t.Errorf("FK as a non-leading index member must NOT be covered (1 surface), got %d", len(got))
	}
}

// --- DB-011: duplicate index (surface, exact only this slice) ---

func TestDB011_ExactDuplicateReportsHigherLine(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{{
		Name: "T", PrimaryKey: []string{"id"},
		Indexes: []db.Index{
			{Columns: []string{"x"}, Unique: false, Pos: db.Pos{File: "s.prisma", Line: 10}},
			{Columns: []string{"x"}, Unique: false, Pos: db.Pos{File: "s.prisma", Line: 20}},
		},
	}}}
	_, items := run(s)
	got := surfaceWithCategory(items, surface.CategoryDBDupIndex)
	if len(got) != 1 {
		t.Fatalf("DB-011 surface = %d, want 1 (exact duplicate)", len(got))
	}
	if got[0].Line != 20 {
		t.Errorf("DB-011 must report the duplicate at the higher line, got line %d, want 20", got[0].Line)
	}
}

func TestDB011_NegativeDistinctIndexes(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{{
		Name: "T",
		Indexes: []db.Index{
			{Columns: []string{"x"}, Unique: false, Pos: db.Pos{Line: 10}},
			{Columns: []string{"x"}, Unique: true, Pos: db.Pos{Line: 20}},  // different uniqueness
			{Columns: []string{"y"}, Unique: false, Pos: db.Pos{Line: 30}}, // different column
		},
	}}}
	_, items := run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBDupIndex); len(got) != 0 {
		t.Errorf("distinct indexes must not be duplicates, got %d", len(got))
	}
}

func TestDB011_PrefixRedundancyIsNotThisRulesCategory(t *testing.T) {
	// [a] subsumed by [a,b] is prefix-redundancy — that is db011prefix's
	// domain (CategoryDBPrefixRedundantIndex, Unit E / Phase 2.2,
	// db011prefix.go), not db011's own EXACT-duplicate category. Must NOT
	// fire CategoryDBDupIndex here (the converse is locked in
	// db011prefix_test.go's TestDB011Prefix_PrefixPairNeverFiresExactDuplicateCategory).
	s := &db.Schema{Tables: []db.Table{{
		Name: "T",
		Indexes: []db.Index{
			{Columns: []string{"a"}, Pos: db.Pos{Line: 10}},
			{Columns: []string{"a", "b"}, Pos: db.Pos{Line: 20}},
		},
	}}}
	_, items := run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBDupIndex); len(got) != 0 {
		t.Errorf("prefix-redundancy is db011prefix's own category, must not fire DB-011's exact-duplicate category, got %d", len(got))
	}
}

func TestDB011_SubCasesHaveDistinctSuffixedIDs(t *testing.T) {
	// DB-011a (exact duplicate, db011/rules.go) and DB-011b (prefix-
	// redundant, db011prefix/db011prefix.go) share ONE PRD number, DB-011
	// (docs/PRD-codefit-v1.4.md:371, "duplicados/redundantes"), but are two
	// separate Go types/categories — same sub-case letter-suffix convention
	// as DB-052b (rules_names.go:77). The bare, unsuffixed "DB-011" must no
	// longer be emitted by either.
	ids := map[string]bool{}
	for _, r := range dbrules.All() {
		ids[r.ID()] = true
	}
	if !ids["DB-011a"] {
		t.Errorf("dbrules.All() missing DB-011a (exact duplicate), have %v", ids)
	}
	if !ids["DB-011b"] {
		t.Errorf("dbrules.All() missing DB-011b (prefix-redundant), have %v", ids)
	}
	if ids["DB-011"] {
		t.Errorf("bare unsuffixed DB-011 must not be emitted by any rule, have %v", ids)
	}
}

// --- DB-002: multivalued column (surface) ---

func TestDB002_Multivalued(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{{
		Name: "T", PrimaryKey: []string{"id"},
		Columns: []db.Column{
			{Name: "id", Type: db.TypeString},
			{Name: "tags", Type: db.TypeString, List: true, Pos: db.Pos{File: "s.prisma", Line: 9}},
		},
	}}}
	_, items := run(s)
	got := surfaceWithCategory(items, surface.CategoryDBMultivalued)
	if len(got) != 1 {
		t.Fatalf("DB-002 surface = %d, want 1", len(got))
	}
	if got[0].Line != 9 || got[0].File != "s.prisma" {
		t.Errorf("DB-002 anchor = %s:%d, want s.prisma:9 (Column.Pos)", got[0].File, got[0].Line)
	}
}

// DB-002 must carry a "table: <name>" StructuralSignal so a consumer (the
// sensor's paradigm-aware 3NF-suppression pass) can resolve the item's table
// deterministically, instead of fragile File+Line geometry (design §2e).
func TestDB002_StructuralSignals_IncludesTable(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{{
		Name: "dim_product", PrimaryKey: []string{"id"},
		Columns: []db.Column{
			{Name: "id", Type: db.TypeString},
			{Name: "tags", Type: db.TypeString, List: true, Pos: db.Pos{File: "s.prisma", Line: 9}},
		},
	}}}
	_, items := run(s)
	got := surfaceWithCategory(items, surface.CategoryDBMultivalued)
	if len(got) != 1 {
		t.Fatalf("DB-002 surface = %d, want 1", len(got))
	}
	want := "table: dim_product"
	found := false
	for _, sig := range got[0].StructuralSignals {
		if sig == want {
			found = true
		}
	}
	if !found {
		t.Errorf("DB-002 StructuralSignals = %v, want to contain %q", got[0].StructuralSignals, want)
	}
}

func TestDB002_NegativeScalar(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{{Name: "T", Columns: []db.Column{{Name: "id", Type: db.TypeString}}}}}
	_, items := run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBMultivalued); len(got) != 0 {
		t.Errorf("scalar column must not fire DB-002, got %d", len(got))
	}
}

// All() enumerates the four rules of this slice.
func TestAll_FourRules(t *testing.T) {
	ids := map[string]bool{}
	for _, r := range dbrules.All() {
		ids[r.ID()] = true
	}
	for _, want := range []string{"DB-050", "DB-001", "DB-011a", "DB-002"} {
		if !ids[want] {
			t.Errorf("dbrules.All() missing %s (have %v)", want, ids)
		}
	}
}
