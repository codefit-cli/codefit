package paradigm_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
)

// D6 (design SS6) — paradigm.roleFor demotes to RoleUnclassified exactly as
// before (byte-identical on a proven schema), but Classification now also
// records WHICH demotions are suspect: a table carrying a recognized prefix
// (fact_/dim_) but demoted for lack of structural corroboration, where that
// lack MIGHT be a dropped statement rather than a genuine absence.

// unprovenFactPrefixed returns a fact_-prefixed table with NO foreign keys
// (below factFanOutMin) whose structure is unproven — the demotion below the
// corroboration threshold might be caused by a dropped FK statement.
func unprovenFactPrefixed(name string) db.Table {
	tb := db.Table{Name: name}
	tb.MarkUnproven(db.ReasonUnreducedTableStatement, "ALTER TABLE "+name+" ...;", db.Pos{File: "x.sql", Line: 1})
	return tb
}

func TestParadigm_Unprovable_DemotedFactPrefixed_UnprovenTable_IsMarked(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{unprovenFactPrefixed("fact_sales")}}
	cls := paradigm.Detect(s)
	if cls.Roles["fact_sales"] != paradigm.RoleUnclassified {
		t.Fatalf("role = %q, want unclassified (fan-out below threshold)", cls.Roles["fact_sales"])
	}
	if !cls.Unprovable["fact_sales"] {
		t.Error("Unprovable[fact_sales] = false, want true — a recognized prefix demoted while its own structure is unproven")
	}
}

func TestParadigm_Unprovable_DemotedFactPrefixed_ProvenTable_IsNotMarked(t *testing.T) {
	tb := db.Table{Name: "fact_sales", Complete: true}
	s := &db.Schema{Tables: []db.Table{tb}}
	cls := paradigm.Detect(s)
	if cls.Roles["fact_sales"] != paradigm.RoleUnclassified {
		t.Fatalf("role = %q, want unclassified (fan-out below threshold)", cls.Roles["fact_sales"])
	}
	if cls.Unprovable["fact_sales"] {
		t.Error("Unprovable[fact_sales] = true, want false — this demotion is genuine, the model is proven complete")
	}
}

// A NO-PREFIX table demoted (never promoted in the first place) is an
// ordinary "no recognized prefix" unclassified — never Unprovable, even when
// unproven, because the demotion cause is the missing prefix, not structure.
func TestParadigm_Unprovable_NoPrefixTable_NeverMarked(t *testing.T) {
	tb := db.Table{Name: "orders"}
	tb.MarkUnproven(db.ReasonUnreducedTableStatement, "...", db.Pos{File: "x.sql", Line: 1})
	s := &db.Schema{Tables: []db.Table{tb}}
	cls := paradigm.Detect(s)
	if cls.Unprovable["orders"] {
		t.Error("Unprovable[orders] = true, want false — no recognized prefix means the demotion cause is vocabulary, not structure")
	}
}

// Promotion is unconditionally safe: a fact_-prefixed table that CLEARS the
// fan-out threshold is genuinely a fact regardless of completeness elsewhere
// (a dropped FK can only UNDERcount fan-out, never overcount it).
func TestParadigm_Unprovable_PromotedTable_NeverMarked_EvenIfUnproven(t *testing.T) {
	tb := db.Table{Name: "fact_sales"}
	tb.ForeignKeys = []db.ForeignKey{{RefTable: "dim_a"}, {RefTable: "dim_b"}}
	tb.MarkUnproven(db.ReasonUnreducedTableStatement, "...", db.Pos{File: "x.sql", Line: 1})
	s := &db.Schema{Tables: []db.Table{tb, {Name: "dim_a"}, {Name: "dim_b"}}}
	cls := paradigm.Detect(s)
	if cls.Roles["fact_sales"] != paradigm.RoleFact {
		t.Fatalf("role = %q, want fact (fan-out clears the threshold)", cls.Roles["fact_sales"])
	}
	if cls.Unprovable["fact_sales"] {
		t.Error("Unprovable[fact_sales] = true, want false — promotion is unconditionally safe")
	}
}

// A dim_-prefixed table with a genuine fan-in of zero (nothing references
// it) demoted while UNPROVEN elsewhere in the schema — a dropped FK on
// SOME OTHER table could be the one that would have referenced it, so
// completeness is schema-wide for the fan-in corroboration.
func TestParadigm_Unprovable_DimPrefixed_UnprovenElsewhereInSchema_IsMarked(t *testing.T) {
	dim := db.Table{Name: "dim_customer", Complete: true}
	other := unprovenFactPrefixed("fact_sales") // unproven, but irrelevant to dim_customer's own FKs
	s := &db.Schema{Tables: []db.Table{dim, other}}
	cls := paradigm.Detect(s)
	if cls.Roles["dim_customer"] != paradigm.RoleUnclassified {
		t.Fatalf("role = %q, want unclassified (fan-in is zero)", cls.Roles["dim_customer"])
	}
	if !cls.Unprovable["dim_customer"] {
		t.Error("Unprovable[dim_customer] = false, want true — a lost FK ANYWHERE in the schema could have starved this table's fan-in")
	}
}
