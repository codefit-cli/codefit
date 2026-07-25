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
