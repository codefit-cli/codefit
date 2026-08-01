package mcp_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/mcp"
	dbsensor "github.com/codefit-cli/codefit/internal/sensors/db"
)

// dw020ScanAllSchema is a two-dimension PostgreSQL star with ONE partitioned
// fact table and ONE unpartitioned one — the same mixed shape the rule's
// real-parser unit tests use, restated here because this test proves a
// different thing: that the item reaches the PRODUCTION scan-all DB section,
// stamped and inside the baseline scope.
//
// The keys are spelled _sk so the ADR 0037 schema gate opens
// (surrogate_key_names); without it no table holds a warehouse role and the
// DW family evaluates nothing at all.
const dw020ScanAllSchema = `
CREATE TABLE dim_customer (customer_sk integer PRIMARY KEY);

CREATE TABLE dim_product (product_sk integer PRIMARY KEY);

CREATE TABLE fact_sales (
    sale_sk integer NOT NULL,
    customer_sk integer NOT NULL,
    product_sk integer NOT NULL,
    sale_date date NOT NULL,
    amount numeric(12,2)
) PARTITION BY RANGE (sale_date);
ALTER TABLE fact_sales ADD CONSTRAINT fs_c FOREIGN KEY (customer_sk) REFERENCES dim_customer(customer_sk);
ALTER TABLE fact_sales ADD CONSTRAINT fs_p FOREIGN KEY (product_sk) REFERENCES dim_product(product_sk);

CREATE TABLE fact_sales_2024_01 PARTITION OF fact_sales FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');
ALTER TABLE fact_sales_2024_01 ADD CONSTRAINT p1c FOREIGN KEY (customer_sk) REFERENCES dim_customer(customer_sk);
ALTER TABLE fact_sales_2024_01 ADD CONSTRAINT p1p FOREIGN KEY (product_sk) REFERENCES dim_product(product_sk);

CREATE TABLE fact_returns (
    return_sk integer PRIMARY KEY,
    customer_sk integer NOT NULL,
    product_sk integer NOT NULL,
    amount numeric(12,2)
);
ALTER TABLE fact_returns ADD CONSTRAINT fr_c FOREIGN KEY (customer_sk) REFERENCES dim_customer(customer_sk);
ALTER TABLE fact_returns ADD CONSTRAINT fr_p FOREIGN KEY (product_sk) REFERENCES dim_product(product_sk);
`

// TestScanAll_DW020_ReachesTheDBSection is DW-020's ADR 0016 Definition-of-Done
// proof: a dimension is not finished until scan-all runs it. It drives the
// REAL mcp.HandleScanAll — not runDBForScanAll, not the sensor — so every
// layer between the rule and the agent is exercised: parser resolution from
// database.type, the DB sensor, the DW runner, fingerprint stamping and the
// unified baseline filter.
//
// It also closes the ADR 0019 hazard in the only way that actually proves it:
// the emitted category must be inside dbsensor.OwnedCategories(). A category
// that is emitted but not owned can never be baselined or pruned, which is
// the single way to corrupt a committed baseline — and this repository has
// been bitten by it before.
func TestScanAll_DW020_ReachesTheDBSection(t *testing.T) {
	root := writeProj(t, map[string]string{
		".codefit.yaml":  tsYAMLWithSQLDDL,
		"db/schema.sql":  dw020ScanAllSchema,
		"app/x/route.ts": "export async function GET() { return Response.json({}); }\n",
	})
	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanAll: %v", err)
	}
	if resp.DB == nil || !resp.DB.Measured {
		t.Fatalf("DB section must be measured for this SQL-DDL project, got %+v", resp.DB)
	}

	want := string(surface.CategoryDWFactsNotPartitioned)
	var found int
	for _, it := range resp.DB.Surface {
		if it.Category != want {
			continue
		}
		found++
		if it.Fingerprint == "" {
			t.Error("DW-020 surface item has an empty fingerprint — the sensor did not stamp it, so it could never be baselined")
		}
		if it.ID == "" {
			t.Error("DW-020 surface item has an empty stable id")
		}
		if it.File != "db/schema.sql" {
			t.Errorf("DW-020 item anchored at %q, want db/schema.sql", it.File)
		}
	}
	if found != 1 {
		t.Fatalf("DW-020 items in the production scan-all db bucket = %d, want exactly 1 — the rule did not reach scan-all", found)
	}

	owned := false
	for _, c := range dbsensor.New(nil).OwnedCategories() {
		if c == want {
			owned = true
		}
	}
	if !owned {
		t.Errorf("%s is not in dbsensor.OwnedCategories() — it would fall outside the unified baseline scope (ADR 0019)", want)
	}
}
