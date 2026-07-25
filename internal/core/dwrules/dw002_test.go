package dwrules

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

func run002(s *db.Schema, c *paradigm.Classification) []findings.SurfaceItem {
	fs, surf := dw002{}.Check(s, c)
	if len(fs) != 0 {
		panic("DW-002 must be SURFACE-only (ADR 0017), never an affirmation")
	}
	return surf
}

// dimTable builds a dimension-shaped table with the given primary key.
func dimTable(name string, pk []string, cols ...db.Column) db.Table {
	tb := dwtbl(name, cols...)
	tb.PrimaryKey = pk
	return tb
}

// onlyDim is the one-dimension schema + classification used by most DW-002
// cases (DW-002 is per-table; it needs no surrounding star).
func onlyDim(tb db.Table) (*db.Schema, *paradigm.Classification) {
	return &db.Schema{Tables: []db.Table{tb}},
		cls(paradigm.ParadigmOLAP, map[string]paradigm.Role{tb.Name: paradigm.RoleDimension})
}

// A dimension keyed by its NATURAL business key — the source system's own
// identifier, here a string code — is the classic warehouse mistake: the
// dimension can no longer track history, and every fact row is bound to a key
// the source may reuse or change.
func TestDW002_DimensionWithNaturalStringKey_Fires(t *testing.T) {
	s, c := onlyDim(dimTable("dim_customer", []string{"customer_code"},
		dwcol("customer_code", db.TypeString),
		dwcol("name", db.TypeString),
	))

	got := itemsOfCategory(run002(s, c), surface.CategoryDWDimensionNoSurrogateKey)
	if len(got) != 1 {
		t.Fatalf("DW-002 items = %d, want 1 (natural string business key)", len(got))
	}
	if pk := signalValue(got[0], "primary_key"); pk != "customer_code" {
		t.Errorf("primary_key signal = %q, want %q", pk, "customer_code")
	}
	if got[0].StructuralFacts["composite_primary_key"] {
		t.Error("composite_primary_key must be false for a single-column PK")
	}
	if got[0].StructuralFacts["integer_primary_key"] {
		t.Error("integer_primary_key must be false for a string PK")
	}
}

// THE TRAP: an integer surrogate key — the shape every well-modelled warehouse
// uses (AdventureWorksDW's DimCustomer.CustomerKey is exactly this) — must NOT
// fire. A rule that flagged it would be unusable.
func TestDW002_DimensionWithIntegerSurrogateKey_DoesNotFire(t *testing.T) {
	s, c := onlyDim(dimTable("dim_customer", []string{"customer_key"},
		dwcol("customer_key", db.TypeInt),
		dwcol("customer_code", db.TypeString),
	))

	if got := itemsOfCategory(run002(s, c), surface.CategoryDWDimensionNoSurrogateKey); len(got) != 0 {
		t.Errorf("DW-002 items = %d, want 0 (integer surrogate key)", len(got))
	}
}

// A COMPOSITE primary key is a business key by construction — a surrogate is
// one column by definition.
func TestDW002_DimensionWithCompositePK_Fires(t *testing.T) {
	s, c := onlyDim(dimTable("dim_product", []string{"product_code", "revision"},
		dwcol("product_code", db.TypeString),
		dwcol("revision", db.TypeInt),
	))

	got := itemsOfCategory(run002(s, c), surface.CategoryDWDimensionNoSurrogateKey)
	if len(got) != 1 {
		t.Fatalf("DW-002 items = %d, want 1 (composite PK)", len(got))
	}
	if !got[0].StructuralFacts["composite_primary_key"] {
		t.Error("composite_primary_key must be true for a two-column PK")
	}
	if pk := signalValue(got[0], "primary_key"); pk != "product_code, revision" {
		t.Errorf("primary_key signal = %q, want both columns", pk)
	}
}

// EDGE: a dimension with NO primary key at all must ABSTAIN. DB-050 already
// affirms "table without a primary key" deterministically; firing DW-002 too
// would double-report one defect under two IDs and add pure noise.
func TestDW002_DimensionWithNoPrimaryKey_Abstains(t *testing.T) {
	s, c := onlyDim(dimTable("dim_customer", nil, dwcol("name", db.TypeString)))

	if got := itemsOfCategory(run002(s, c), surface.CategoryDWDimensionNoSurrogateKey); len(got) != 0 {
		t.Errorf("DW-002 items = %d, want 0 — DB-050 owns the no-primary-key case", len(got))
	}
}

// Only DIMENSION-role tables are evaluated: a fact, a staging table, or an
// ordinary OLTP table with a natural key is none of DW-002's business.
func TestDW002_NonDimensionRoles_EmitNothing(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{
		dimTable("fact_sales", []string{"order_no"}, dwcol("order_no", db.TypeString)),
		dimTable("stg_import", []string{"code"}, dwcol("code", db.TypeString)),
		dimTable("users", []string{"email"}, dwcol("email", db.TypeString)),
	}}
	c := cls(paradigm.ParadigmMixed, map[string]paradigm.Role{
		"fact_sales": paradigm.RoleFact,
		"stg_import": paradigm.RoleStaging,
		"users":      paradigm.RoleUnclassified,
	})

	if got := run002(s, c); len(got) != 0 {
		t.Errorf("DW-002 items = %d, want 0 (no dimension-role table)", len(got))
	}
}

// EDGE: a single-column PK whose column is not declared on the table at all
// (a schema the parser reconstructed incompletely) must not crash, and must be
// treated as NOT-proven-integer rather than silently assumed fine.
func TestDW002_PrimaryKeyColumnNotDeclared_Fires(t *testing.T) {
	s, c := onlyDim(dimTable("dim_customer", []string{"ghost_key"}, dwcol("name", db.TypeString)))

	got := itemsOfCategory(run002(s, c), surface.CategoryDWDimensionNoSurrogateKey)
	if len(got) != 1 {
		t.Fatalf("DW-002 items = %d, want 1 (PK column absent from the model — not provably a surrogate)", len(got))
	}
	if got[0].StructuralFacts["primary_key_column_resolved"] {
		t.Error("primary_key_column_resolved must be false when the PK column is not in the table")
	}
}

func TestDW002_ID(t *testing.T) {
	if got := (dw002{}).ID(); got != "DW-002" {
		t.Errorf("ID() = %q, want %q", got, "DW-002")
	}
}

func TestDW002_RegisteredInAll(t *testing.T) {
	if !registeredIn("DW-002") {
		t.Error("DW-002 is not registered in dwrules.All() — the sensor would never run it")
	}
}

// A primary key column with no RawType (a parser that classified the neutral
// type but recovered no verbatim type text) must still render a usable
// primary_key_type signal, falling back to the neutral type rather than
// emitting an empty value.
func TestDW002_PrimaryKeyColumnWithoutRawType_FallsBackToNeutralType(t *testing.T) {
	tb := dimTable("dim_customer", []string{"code"}, db.Column{Name: "code", Type: db.TypeString})
	s, c := onlyDim(tb)

	got := itemsOfCategory(run002(s, c), surface.CategoryDWDimensionNoSurrogateKey)
	if len(got) != 1 {
		t.Fatalf("DW-002 items = %d, want 1", len(got))
	}
	if v := signalValue(got[0], "primary_key_type"); v != "string" {
		t.Errorf("primary_key_type signal = %q, want the neutral type %q", v, "string")
	}
}
