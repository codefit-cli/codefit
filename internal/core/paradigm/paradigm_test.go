package paradigm_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
)

// TestDetect locks the spec's three Detection scenarios verbatim (spec
// "Paradigm and Table-Role Detection (S1)").
func TestDetect(t *testing.T) {
	t.Run("fact/dim prefixes with surrogate keys and FK fan-out classify as olap", func(t *testing.T) {
		schema := &db.Schema{
			Tables: []db.Table{
				{
					Name:       "fact_sales",
					PrimaryKey: []string{"id"},
					ForeignKeys: []db.ForeignKey{
						{Columns: []string{"customer_id"}, RefTable: "dim_customer", RefColumns: []string{"id"}},
						{Columns: []string{"date_id"}, RefTable: "dim_date", RefColumns: []string{"id"}},
						{Columns: []string{"product_id"}, RefTable: "dim_product", RefColumns: []string{"id"}},
					},
				},
				{Name: "dim_customer", PrimaryKey: []string{"id"}},
				{Name: "dim_date", PrimaryKey: []string{"id"}},
				{Name: "dim_product", PrimaryKey: []string{"id"}},
			},
		}

		cls := paradigm.Detect(schema)

		if cls.Paradigm != paradigm.ParadigmOLAP {
			t.Errorf("Paradigm = %q, want olap", cls.Paradigm)
		}
		if got := cls.Roles["fact_sales"]; got != paradigm.RoleFact {
			t.Errorf("Roles[fact_sales] = %q, want fact", got)
		}
		if got := cls.Roles["dim_customer"]; got != paradigm.RoleDimension {
			t.Errorf("Roles[dim_customer] = %q, want dimension", got)
		}
		if got := cls.Roles["dim_date"]; got != paradigm.RoleDimension {
			t.Errorf("Roles[dim_date] = %q, want dimension", got)
		}
	})

	t.Run("table with no prefix and no structural signal is unclassified", func(t *testing.T) {
		schema := &db.Schema{
			Tables: []db.Table{
				{
					Name:       "order_items",
					PrimaryKey: []string{"order_id", "product_id"}, // composite, not surrogate
					// no foreign keys: low FK fan-out
				},
			},
		}

		cls := paradigm.Detect(schema)

		if got := cls.Roles["order_items"]; got != paradigm.RoleUnclassified {
			t.Errorf("Roles[order_items] = %q, want unclassified", got)
		}
	})

	t.Run("star-shaped tables mixed with unrelated OLTP tables classify as mixed", func(t *testing.T) {
		schema := &db.Schema{
			Tables: []db.Table{
				{
					Name:       "fact_orders",
					PrimaryKey: []string{"id"},
					ForeignKeys: []db.ForeignKey{
						{Columns: []string{"customer_id"}, RefTable: "dim_customer", RefColumns: []string{"id"}},
						{Columns: []string{"product_id"}, RefTable: "dim_product", RefColumns: []string{"id"}},
					},
				},
				{Name: "dim_customer", PrimaryKey: []string{"id"}},
				{Name: "dim_product", PrimaryKey: []string{"id"}},
				// unrelated normalized OLTP tables, no prefix, composite/natural keys
				{Name: "employees", PrimaryKey: []string{"employee_id", "department_id"}},
				{Name: "departments", PrimaryKey: []string{"department_id", "company_id"}},
			},
		}

		cls := paradigm.Detect(schema)

		if cls.Paradigm != paradigm.ParadigmMixed {
			t.Errorf("Paradigm = %q, want mixed", cls.Paradigm)
		}
	})
}

// TestDetect_NilSchema locks the honest fallback: a nil schema classifies as
// oltp with no roles, mirroring dbrules.Run/crossrules.RunWith's nil guard.
func TestDetect_NilSchema(t *testing.T) {
	cls := paradigm.Detect(nil)
	if cls.Paradigm != paradigm.ParadigmOLTP {
		t.Errorf("Paradigm = %q, want oltp for a nil schema", cls.Paradigm)
	}
	if len(cls.Roles) != 0 {
		t.Errorf("Roles = %v, want empty for a nil schema", cls.Roles)
	}
}

// TestDetect_PrefixWithoutCorroboration_Demotes locks that a fact_/dim_
// prefix ALONE, with no structural corroboration, demotes to unclassified —
// structure corroborates a name candidate, it never substitutes for one.
func TestDetect_PrefixWithoutCorroboration_Demotes(t *testing.T) {
	t.Run("fact_ prefix, composite PK, no FK fan-out", func(t *testing.T) {
		schema := &db.Schema{Tables: []db.Table{{
			Name:       "fact_orphan",
			PrimaryKey: []string{"a", "b"}, // composite: not a surrogate
			// no ForeignKeys: zero fan-out
		}}}
		cls := paradigm.Detect(schema)
		if got := cls.Roles["fact_orphan"]; got != paradigm.RoleUnclassified {
			t.Errorf("Roles[fact_orphan] = %q, want unclassified (no corroboration)", got)
		}
	})

	t.Run("dim_ prefix, composite PK, no fan-in", func(t *testing.T) {
		schema := &db.Schema{Tables: []db.Table{{
			Name:       "dim_orphan",
			PrimaryKey: []string{"a", "b"}, // composite: not a surrogate
			// nothing else in the schema references it: zero fan-in
		}}}
		cls := paradigm.Detect(schema)
		if got := cls.Roles["dim_orphan"]; got != paradigm.RoleUnclassified {
			t.Errorf("Roles[dim_orphan] = %q, want unclassified (no corroboration)", got)
		}
	})
}

// TestDetect_SingleColumnPKAloneInsufficientCorroboration locks the CRITICAL
// C1 fix (S1 4R review, ledger sdd/db-olap-rules/review-ledger-s1): a lone
// single-column (surrogate) primary key is NOT, by itself, structural
// corroboration for fact_/dim_. Almost every ordinary OLTP table has a
// single-column surrogate PK, so accepting it alone made the corroboration
// vacuous — any fact_/dim_-prefixed table with a simple `id` PK classified as
// warehouse-role regardless of any real relational fan-out/fan-in, silently
// suppressing its DB-002/DB-003 findings under the auto default.
func TestDetect_SingleColumnPKAloneInsufficientCorroboration(t *testing.T) {
	t.Run("dim_ prefix, single-column surrogate PK, zero fan-in", func(t *testing.T) {
		schema := &db.Schema{Tables: []db.Table{{
			Name:       "dim_users",
			PrimaryKey: []string{"id"}, // single-column surrogate PK ALONE
			// nothing else in the schema references it: zero fan-in
		}}}
		cls := paradigm.Detect(schema)
		if got := cls.Roles["dim_users"]; got != paradigm.RoleUnclassified {
			t.Errorf("Roles[dim_users] = %q, want unclassified (a lone surrogate PK is not corroboration)", got)
		}
	})

	t.Run("fact_ prefix, single-column surrogate PK, zero FK fan-out", func(t *testing.T) {
		schema := &db.Schema{Tables: []db.Table{{
			Name:       "fact_x",
			PrimaryKey: []string{"id"}, // single-column surrogate PK ALONE
			// no ForeignKeys: zero fan-out
		}}}
		cls := paradigm.Detect(schema)
		if got := cls.Roles["fact_x"]; got != paradigm.RoleUnclassified {
			t.Errorf("Roles[fact_x] = %q, want unclassified (a lone surrogate PK is not corroboration)", got)
		}
	})
}

// TestDetect_FactCorroboratedByFanOutAlone locks the FK-fan-out corroboration
// path independently of the surrogate-PK path (a composite-PK fact table
// with high fan-out still corroborates as fact).
func TestDetect_FactCorroboratedByFanOutAlone(t *testing.T) {
	schema := &db.Schema{
		Tables: []db.Table{
			{
				Name:       "fact_events",
				PrimaryKey: []string{"event_id", "occurred_at"}, // composite: not surrogate
				ForeignKeys: []db.ForeignKey{
					{RefTable: "dim_customer"},
					{RefTable: "dim_product"},
				},
			},
			{Name: "dim_customer"},
			{Name: "dim_product"},
		},
	}
	cls := paradigm.Detect(schema)
	if got := cls.Roles["fact_events"]; got != paradigm.RoleFact {
		t.Errorf("Roles[fact_events] = %q, want fact (FK fan-out corroboration)", got)
	}
}

// TestDetect_DimensionCorroboratedByFanInAlone locks the fan-in corroboration
// path independently of the surrogate-PK path.
func TestDetect_DimensionCorroboratedByFanInAlone(t *testing.T) {
	schema := &db.Schema{
		Tables: []db.Table{
			{
				Name:       "fact_sales",
				PrimaryKey: []string{"id"},
				ForeignKeys: []db.ForeignKey{
					{RefTable: "dim_region"},
				},
			},
			{
				Name:       "dim_region",
				PrimaryKey: []string{"region_code", "country_code"}, // composite: not surrogate
			},
		},
	}
	cls := paradigm.Detect(schema)
	if got := cls.Roles["dim_region"]; got != paradigm.RoleDimension {
		t.Errorf("Roles[dim_region] = %q, want dimension (fan-in corroboration)", got)
	}
}

// TestDetect_StagingAndMartPrefixesAcceptedOnNameAlone locks that stg_/mart_
// tables need no structural corroboration in S1 (design §2a: no structural
// signal specified for staging/mart tables).
func TestDetect_StagingAndMartPrefixesAcceptedOnNameAlone(t *testing.T) {
	schema := &db.Schema{Tables: []db.Table{
		{Name: "stg_raw_events"},
		{Name: "mart_customer_summary"},
	}}
	cls := paradigm.Detect(schema)
	if got := cls.Roles["stg_raw_events"]; got != paradigm.RoleStaging {
		t.Errorf("Roles[stg_raw_events] = %q, want staging", got)
	}
	if got := cls.Roles["mart_customer_summary"]; got != paradigm.RoleMart {
		t.Errorf("Roles[mart_customer_summary] = %q, want mart", got)
	}
}

// TestResolve locks the spec's "Paradigm Config Override Semantics (S1)"
// scenarios.
func TestResolve(t *testing.T) {
	detected := paradigm.Classification{
		Paradigm: paradigm.ParadigmOLAP,
		Roles:    map[string]paradigm.Role{"fact_sales": paradigm.RoleFact},
	}

	t.Run("explicit override wins over detection", func(t *testing.T) {
		got := paradigm.Resolve(detected, paradigm.ParadigmOLTP)
		if got.Paradigm != paradigm.ParadigmOLTP {
			t.Errorf("Paradigm = %q, want oltp (explicit override)", got.Paradigm)
		}
		// Roles stay detection-derived so per-table suppression still works
		// under an override that keeps a mixed reality (design §2a).
		if got.Roles["fact_sales"] != paradigm.RoleFact {
			t.Errorf("Roles[fact_sales] = %q, want fact (roles unchanged by override)", got.Roles["fact_sales"])
		}
	})

	t.Run("auto lets detection decide", func(t *testing.T) {
		got := paradigm.Resolve(detected, paradigm.ParadigmAuto)
		if got.Paradigm != paradigm.ParadigmOLAP {
			t.Errorf("Paradigm = %q, want olap (auto keeps detection)", got.Paradigm)
		}
	})

	t.Run("empty override lets detection decide", func(t *testing.T) {
		got := paradigm.Resolve(detected, "")
		if got.Paradigm != paradigm.ParadigmOLAP {
			t.Errorf("Paradigm = %q, want olap (empty keeps detection)", got.Paradigm)
		}
	})
}
