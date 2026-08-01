package paradigm_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
)

// This file locks the role-name VOCABULARY widening: the recognized spellings
// of a fact/dimension candidate. It deliberately does NOT touch structural
// corroboration (locked decision A5, ADR 0033 decision 2) — every table below
// still carries the real fan-out/fan-in its role requires, so a regression in
// corroboration fails these tests too rather than hiding behind a wider name.
//
// The widening is driven by an empirical yield measurement over 22 real public
// corpora: the DW family measured near-zero not because its logic is wrong but
// because prefixRole matched a four-entry, snake_case, case-SENSITIVE
// vocabulary that real warehouses do not use.

// starWith builds a minimal star: one fact-candidate table with FK fan-out to
// every dimension candidate (satisfying factFanOutMin), and each dimension
// therefore carrying fan-in >= 1. Corroboration is real, never assumed.
//
// It also opens the SCHEMA GATE (ADR 0037), and that is load-bearing in BOTH
// directions for this file. The vocabulary tests come in two kinds — "this
// spelling IS a warehouse token" and "this spelling is NOT one" — and a closed
// gate would break the first kind loudly and the second kind SILENTLY, since
// every table in a non-qualifying schema is unclassified regardless of how it
// is spelled. TestVocabulary_BareWordPrefixIsNotAToken, the guard against
// wrongly promoting factory_settings, would have gone on passing while checking
// nothing at all.
//
// gateOpeningTables (schemagate_verdict_test.go) contributes a declared
// calendar and one plain table, neither of which holds a role, so the star's own
// role map is exactly what these tests assert about.
func starWith(fact string, dims ...string) *db.Schema {
	f := db.Table{Name: fact, PrimaryKey: []string{"id"}}
	tables := []db.Table{}
	for _, d := range dims {
		f.ForeignKeys = append(f.ForeignKeys, db.ForeignKey{
			Columns: []string{d + "_id"}, RefTable: d, RefColumns: []string{"id"},
		})
		tables = append(tables, db.Table{Name: d, PrimaryKey: []string{"id"}})
	}
	return withGateOpen(&db.Schema{Tables: append([]db.Table{f}, tables...)})
}

// TestVocabulary_CaseInsensitive locks the single highest-leverage finding of
// the yield measurement: 4 of 9 real warehouse corpora use exactly the Kimball
// convention the rule already knows, only capitalized. prefixRole compared
// raw bytes, so every one of them classified as unclassified and produced
// zero DW items.
func TestVocabulary_CaseInsensitive(t *testing.T) {
	cls := paradigm.Detect(starWith("Fact_Sales", "Dim_Customer", "Dim_Date"))

	if got := cls.Roles["Fact_Sales"]; got != paradigm.RoleFact {
		t.Errorf("Roles[Fact_Sales] = %q, want fact", got)
	}
	if got := cls.Roles["Dim_Customer"]; got != paradigm.RoleDimension {
		t.Errorf("Roles[Dim_Customer] = %q, want dimension", got)
	}
	if got := cls.Roles["Dim_Date"]; got != paradigm.RoleDimension {
		t.Errorf("Roles[Dim_Date] = %q, want dimension", got)
	}
}

// TestVocabulary_DbtFactPrefix locks fct_, dbt's own widely-used convention
// for a fact model.
func TestVocabulary_DbtFactPrefix(t *testing.T) {
	cls := paradigm.Detect(starWith("fct_orders", "dim_customer", "dim_product"))

	if got := cls.Roles["fct_orders"]; got != paradigm.RoleFact {
		t.Errorf("Roles[fct_orders] = %q, want fact", got)
	}
}

// TestVocabulary_Suffixes locks the SUFFIX forms. prefixRole only ever read
// prefixes, so TPC-DS's date_dim and dw-supermarket's *_fact were invisible.
func TestVocabulary_Suffixes(t *testing.T) {
	cls := paradigm.Detect(starWith("sales_fact", "date_dim", "item_dim"))

	if got := cls.Roles["sales_fact"]; got != paradigm.RoleFact {
		t.Errorf("Roles[sales_fact] = %q, want fact", got)
	}
	if got := cls.Roles["date_dim"]; got != paradigm.RoleDimension {
		t.Errorf("Roles[date_dim] = %q, want dimension", got)
	}
	if got := cls.Roles["item_dim"]; got != paradigm.RoleDimension {
		t.Errorf("Roles[item_dim] = %q, want dimension", got)
	}
}

// TestVocabulary_SingleLetterPrefixes locks d_/f_, the convention dw-jobtech
// uses (d_date, d_country). The measurement recorded these BOTH as misses and
// as the cause of DW-001/DW-005 FALSE POSITIVES: codefit reported "the fact
// reaches no dimension" while the dimensions were sitting right there,
// unrecognized.
func TestVocabulary_SingleLetterPrefixes(t *testing.T) {
	cls := paradigm.Detect(starWith("f_jobs", "d_date", "d_country"))

	if got := cls.Roles["f_jobs"]; got != paradigm.RoleFact {
		t.Errorf("Roles[f_jobs] = %q, want fact", got)
	}
	if got := cls.Roles["d_date"]; got != paradigm.RoleDimension {
		t.Errorf("Roles[d_date] = %q, want dimension", got)
	}
}

// TestVocabulary_PascalCaseKimball locks the no-separator PascalCase form
// (FactInternetSales / DimCustomer) that Microsoft's own AdventureWorksDW
// uses — the corpus THIS REPOSITORY vendors, and the naming half of the two
// limits that used to keep that corpus silent. Both are closed now (the
// parser half by the T-SQL ALTER TABLE ... ADD CONSTRAINT reducer), so
// dbcoverage.go no longer declares it a blind spot; it records the reach
// instead. What dbcoverage.go still declares as a blind spot on this corpus
// is a DIFFERENT limit one layer down — the bracketed T-SQL type name
// ([int]) reading as db.TypeUnknown, which is a type-vocabulary gap, not a
// naming one.
//
// Lowercasing alone cannot reach it: "factinternetsales" does not carry the
// "fact_" prefix. The discriminator is PascalCase's own signal — the
// character after the token is UPPERCASE — which is exactly what separates it
// from the ordinary English words the negative test below protects.
func TestVocabulary_PascalCaseKimball(t *testing.T) {
	cls := paradigm.Detect(starWith("FactInternetSales", "DimCustomer", "DimDate"))

	if got := cls.Roles["FactInternetSales"]; got != paradigm.RoleFact {
		t.Errorf("Roles[FactInternetSales] = %q, want fact", got)
	}
	if got := cls.Roles["DimCustomer"]; got != paradigm.RoleDimension {
		t.Errorf("Roles[DimCustomer] = %q, want dimension", got)
	}
}

// TestVocabulary_BareWordPrefixIsNotAToken is the GUARD on the widening, and
// the reason PascalCase is matched on the uppercase boundary rather than on a
// bare "fact"/"dim" prefix.
//
// Both tables below carry REAL corroboration (fan-out >= 2 / fan-in >= 1), so
// if the vocabulary wrongly accepted them they WOULD promote — and promotion
// silences their DB-002/DB-003 1NF findings through the sensor's suppression
// pass (db.go:356). This test is the lock against re-opening that false
// negative, the same class the S1 4R caught as CRITICAL-1.
func TestVocabulary_BareWordPrefixIsNotAToken(t *testing.T) {
	s := starWith("factory_settings", "dimension_config", "d_lookup")
	// Give dimension_config real fan-in from a second referrer too, so its
	// demotion can only come from the vocabulary, never from thin structure.
	s.Tables = append(s.Tables, db.Table{
		Name:       "audit_log",
		PrimaryKey: []string{"id"},
		ForeignKeys: []db.ForeignKey{
			{Columns: []string{"cfg_id"}, RefTable: "dimension_config", RefColumns: []string{"id"}},
		},
	})

	cls := paradigm.Detect(s)

	if got := cls.Roles["factory_settings"]; got != paradigm.RoleUnclassified {
		t.Errorf("Roles[factory_settings] = %q, want unclassified — "+
			"a bare word starting with \"fact\" is not a warehouse token", got)
	}
	if got := cls.Roles["dimension_config"]; got != paradigm.RoleUnclassified {
		t.Errorf("Roles[dimension_config] = %q, want unclassified — "+
			"a bare word starting with \"dim\" is not a warehouse token", got)
	}
}

// TestVocabulary_PascalCaseGuardIsNotVacuous proves the uppercase-boundary
// rule discriminates rather than accepting everything PascalCase: an ordinary
// PascalCase table whose leading token is NOT a warehouse word stays
// unclassified, with real corroboration present.
func TestVocabulary_PascalCaseGuardIsNotVacuous(t *testing.T) {
	cls := paradigm.Detect(starWith("FactoryOrder", "CustomerAccount", "ProductLine"))

	if got := cls.Roles["FactoryOrder"]; got != paradigm.RoleUnclassified {
		t.Errorf("Roles[FactoryOrder] = %q, want unclassified — "+
			"\"Factory\" is not the token \"Fact\" followed by a word boundary", got)
	}
}

// TestVocabulary_AllCapsIsNotPascalCase locks the edge found while designing
// the PascalCase matcher, before any of it was written: matching on "an
// uppercase character follows the token" ALONE accepts FACTORY_SETTINGS, since
// its 'O' is uppercase too — promoting an ordinary all-caps OLTP table (the
// spelling Oracle and DB2 DDL routinely use) straight into a warehouse role,
// and silencing its 1NF findings.
//
// PascalCase's real signal is an uppercase character followed by a LOWERCASE
// one — "Fact" + "In", which "FACT" + "OR" does not satisfy. An all-caps name
// is genuinely ambiguous, so it stays unclassified rather than being guessed.
func TestVocabulary_AllCapsIsNotPascalCase(t *testing.T) {
	cls := paradigm.Detect(starWith("FACTORY_SETTINGS", "DIMENSION_CONFIG", "dim_real"))

	if got := cls.Roles["FACTORY_SETTINGS"]; got != paradigm.RoleUnclassified {
		t.Errorf("Roles[FACTORY_SETTINGS] = %q, want unclassified — "+
			"all-caps is not PascalCase; \"FACT\"+\"OR\" is not the token \"Fact\"+\"In\"", got)
	}
	if got := cls.Roles["DIMENSION_CONFIG"]; got != paradigm.RoleUnclassified {
		t.Errorf("Roles[DIMENSION_CONFIG] = %q, want unclassified", got)
	}
	// Control: the same schema's genuinely-tokenized dimension DOES classify,
	// so this test cannot pass by the vocabulary simply recognizing nothing.
	if got := cls.Roles["dim_real"]; got != paradigm.RoleDimension {
		t.Errorf("Roles[dim_real] = %q, want dimension — control", got)
	}
}

// TestVocabulary_CorroborationStillRequired locks decision A5 as UNCHANGED by
// this slice: a widened NAME never substitutes for structure. Each table below
// carries a newly-recognized spelling and NO corroboration, and must still be
// demoted.
func TestVocabulary_CorroborationStillRequired(t *testing.T) {
	// date_dim opens the gate on its own (a declared calendar), so this fixture
	// needs no extra evidence — and the assertion below stays a statement about
	// CORROBORATION rather than about the gate.
	s := &db.Schema{Tables: []db.Table{
		{Name: "Fact_Sales", PrimaryKey: []string{"id"}}, // no FK fan-out
		{Name: "date_dim", PrimaryKey: []string{"id"}},   // no fan-in
		{Name: "FactInternetSales", PrimaryKey: []string{"id"}},
	}}

	cls := paradigm.Detect(s)

	for _, name := range []string{"Fact_Sales", "date_dim", "FactInternetSales"} {
		if got := cls.Roles[name]; got != paradigm.RoleUnclassified {
			t.Errorf("Roles[%s] = %q, want unclassified — a wider vocabulary must not "+
				"substitute for the structural corroboration ADR 0033 decision 2 requires", name, got)
		}
	}
}
