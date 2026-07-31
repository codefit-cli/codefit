package db_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
	sdb "github.com/codefit-cli/codefit/internal/sensors/db"
)

const happySchema = `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model User {
  id    Int    @id
  email String @unique
}

model Post {
  id       Int  @id
  authorId Int
  author   User @relation(fields: [authorId], references: [id])
}

model NoKey {
  name String
}
`

// writeProject writes a schema + optional .codefit.yaml into a temp root and
// returns an AuditContext pointing at it.
func writeProject(t *testing.T, schemaRel, schema, yaml string) auditctx.AuditContext {
	t.Helper()
	root := t.TempDir()
	abs := filepath.Join(root, schemaRel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	var cfg *config.Config
	if yaml != "" {
		if err := os.WriteFile(filepath.Join(root, ".codefit.yaml"), []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}
		var err error
		cfg, err = config.Load(filepath.Join(root, ".codefit.yaml"))
		if err != nil {
			t.Fatalf("loading test .codefit.yaml: %v", err)
		}
	}
	return auditctx.AuditContext{ProjectRoot: root, Language: "typescript", Config: cfg}
}

const yamlWithSchema = `version: "1"
project:
  name: t
  language: typescript
  framework: next
database:
  orm: prisma
  type: postgresql
  paradigm: oltp
  schema_paths:
    - prisma/schema.prisma
`

func TestSensorDB_Run_MeasuresAndFinds(t *testing.T) {
	ctx := writeProject(t, "prisma/schema.prisma", happySchema, yamlWithSchema)
	r, err := sdb.New(typescript.New()).Audit(ctx)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if !r.Measured {
		t.Fatalf("Measured=false, want true; note=%q", r.Note)
	}
	// DB-050 affirmation on NoKey.
	var db050 int
	for _, f := range r.Res.Findings {
		if f.ID == "DB-050" {
			db050++
			if f.Fingerprint == "" {
				t.Error("DB-050 finding must be fingerprinted")
			}
			if f.Probabilistic || f.Confidence != 1.0 {
				t.Error("DB-050 must be an affirmation (Confidence 1.0, Probabilistic false)")
			}
		}
	}
	if db050 != 1 {
		t.Errorf("DB-050 count = %d, want 1 (NoKey)", db050)
	}
	// DB-001 surface on Post.authorId (no covering index).
	var fkNoIdx int
	for _, it := range r.Res.Surface {
		if it.Category == string(surface.CategoryDBFKNoIndex) {
			fkNoIdx++
			if it.Fingerprint == "" || it.ID == "" {
				t.Error("DB-001 surface must be fingerprinted and have a stable ID")
			}
		}
	}
	if fkNoIdx != 1 {
		t.Errorf("DB-001 surface count = %d, want 1 (Post.authorId)", fkNoIdx)
	}
	// Score reflects the single medium DB-050 (100 - 5).
	if r.Res.Score != 95 {
		t.Errorf("Score = %d, want 95", r.Res.Score)
	}
}

func TestSensorDB_Disabled_NotMeasured(t *testing.T) {
	yaml := yamlWithSchema + "sensors:\n  db:\n    enabled: false\n"
	ctx := writeProject(t, "prisma/schema.prisma", happySchema, yaml)
	r, err := sdb.New(typescript.New()).Audit(ctx)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if r.Measured || r.Note == "" {
		t.Errorf("disabled sensor must be not-measured with a note, got Measured=%v note=%q", r.Measured, r.Note)
	}
	if len(r.Res.Findings) != 0 {
		t.Error("disabled sensor must not emit findings (never a false 'clean')")
	}
}

func TestSensorDB_NoSchemaPaths_NotMeasured(t *testing.T) {
	yaml := "version: \"1\"\nproject:\n  name: t\n  language: typescript\n  framework: next\n"
	ctx := writeProject(t, "prisma/schema.prisma", happySchema, yaml)
	r, err := sdb.New(typescript.New()).Audit(ctx)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if r.Measured || r.Note == "" {
		t.Errorf("no schema_paths must be not-measured with a note, got Measured=%v note=%q", r.Measured, r.Note)
	}
}

// The "no parser for this schema type" case is now a compile-time concern of the
// adapter (the sensor depends on providers.SchemaParser directly), so it is tested
// in the mcp package (schemaParserForPaths), not here.

func TestSensorDB_MissingSchemaFile_Errors(t *testing.T) {
	// .codefit.yaml points at prisma/schema.prisma but we write the schema elsewhere.
	ctx := writeProject(t, "other.prisma", happySchema, yamlWithSchema)
	_, err := sdb.New(typescript.New()).Audit(ctx)
	if err == nil {
		t.Fatal("a configured-but-missing schema file must be an error, got nil")
	}
}

// The DB sensor satisfies the shared Sensor identity.
func TestSensorDB_Identity(t *testing.T) {
	s := sdb.New(typescript.New())
	if s.Name() != "db" || s.Dimension() != findings.DimensionDB {
		t.Errorf("identity = {%s, %s}, want {db, db}", s.Name(), s.Dimension())
	}
}

// DB-020 (Phase 2.2) must be in the unified-baseline scope (ADR 0019) like
// every other DB surface category, or its items can never be baselined/
// pruned. A locked test, not an assumption — forgetting this is "the only
// way to corrupt an existing baseline" (design §15).
func TestSensorDB_OwnedCategories_IncludesDB020(t *testing.T) {
	s := sdb.New(typescript.New())
	want := string(surface.CategoryDBViewSensitiveColumn)
	for _, c := range s.OwnedCategories() {
		if c == want {
			return
		}
	}
	t.Errorf("OwnedCategories() missing %q (have %v)", want, s.OwnedCategories())
}

// DB-011's prefix-redundant-index category (Unit E, Phase 2.2) must be in
// the unified-baseline scope (ADR 0019) too, same rationale as DB-020 above.
func TestSensorDB_OwnedCategories_IncludesPrefixRedundantIndex(t *testing.T) {
	s := sdb.New(typescript.New())
	want := string(surface.CategoryDBPrefixRedundantIndex)
	for _, c := range s.OwnedCategories() {
		if c == want {
			return
		}
	}
	t.Errorf("OwnedCategories() missing %q (have %v)", want, s.OwnedCategories())
}

// S1 asserted that the paradigm assembly left the sensor's output
// byte-identical, because dwrules.All() was empty. S2 broke that premise on
// purpose: there are five real DW rules now. What SURVIVES the premise change
// is the property that actually mattered — a schema with NO warehouse-role
// table must be completely untouched by the DW family, under EVERY valid
// paradigm config value. happySchema (User/Post/NoKey) is exactly such a
// schema, so no config value may add or remove a single item.
func TestSensorDB_ParadigmAssembly_NoDWEffectOnAnOLTPSchema(t *testing.T) {
	base := yamlWithSchema // has "paradigm: oltp"
	variants := map[string]string{
		"unset (no paradigm key)": "version: \"1\"\nproject:\n  name: t\n  language: typescript\n  framework: next\ndatabase:\n  orm: prisma\n  type: postgresql\n  schema_paths:\n    - prisma/schema.prisma\n",
		"auto":                    strings.Replace(base, "paradigm: oltp", "paradigm: auto", 1),
		"oltp":                    base,
		"olap":                    strings.Replace(base, "paradigm: oltp", "paradigm: olap", 1),
		"mixed":                   strings.Replace(base, "paradigm: oltp", "paradigm: mixed", 1),
	}

	// Baseline from the "oltp" variant, to compare every other variant against
	// — proves identical output without hardcoding a magic count that would
	// drift with every unrelated dbrules rule this schema happens to hit.
	baseCtx := writeProject(t, "prisma/schema.prisma", happySchema, variants["oltp"])
	baseR, err := sdb.New(typescript.New()).Audit(baseCtx)
	if err != nil {
		t.Fatalf("Audit (baseline): %v", err)
	}
	if !baseR.Measured || len(baseR.Res.Findings) == 0 && len(baseR.Res.Surface) == 0 {
		t.Fatalf("baseline audit produced no findings/surface to compare against: %+v", baseR)
	}

	for name, yaml := range variants {
		t.Run(name, func(t *testing.T) {
			ctx := writeProject(t, "prisma/schema.prisma", happySchema, yaml)
			r, err := sdb.New(typescript.New()).Audit(ctx)
			if err != nil {
				t.Fatalf("Audit: %v", err)
			}
			if !r.Measured {
				t.Fatalf("Measured=false, want true; note=%q", r.Note)
			}
			if len(r.Res.Findings) != len(baseR.Res.Findings) {
				t.Errorf("Findings count = %d, want %d (unchanged by paradigm assembly)", len(r.Res.Findings), len(baseR.Res.Findings))
			}
			if len(r.Res.Surface) != len(baseR.Res.Surface) {
				t.Errorf("Surface count = %d, want %d (unchanged by paradigm assembly)", len(r.Res.Surface), len(baseR.Res.Surface))
			}
			for _, it := range r.Res.Surface {
				if strings.HasPrefix(it.Category, "dw-") {
					t.Errorf("DW surface item %q emitted on a schema with no warehouse-role table", it.Category)
				}
			}
		})
	}
}

// The other half of the same premise change: on a schema that IS a star, the
// DW family must actually run through the sensor — end to end, from
// ParseSchema through paradigm.Detect to dwrules — and its items must be
// stamped with a fingerprint and a stable ID like every other surface item,
// or they can never be baselined.
func TestSensorDB_ParadigmAssembly_DWRulesRunOnAStarSchema(t *testing.T) {
	// fact_sales fans out to two dimensions (real corroboration); dim_product
	// is keyed by a natural string code (DW-002) and there is no time
	// dimension anywhere (DW-005).
	schema := `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model fact_sales {
  id           Int          @id
  customerKey  Int
  productCode  String
  amount       Float
  dim_customer dim_customer @relation(fields: [customerKey], references: [customer_key])
  dim_product  dim_product  @relation(fields: [productCode], references: [product_code])
}

model dim_customer {
  customer_key Int          @id
  name         String
  fact_sales   fact_sales[]
}

model dim_product {
  product_code String       @id
  category     String
  fact_sales   fact_sales[]
}
`
	yaml := strings.Replace(yamlWithSchema, "paradigm: oltp", "paradigm: auto", 1)
	ctx := writeProject(t, "prisma/schema.prisma", schema, yaml)
	r, err := sdb.New(typescript.New()).Audit(ctx)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if !r.Measured {
		t.Fatalf("Measured=false: %q", r.Note)
	}

	for _, want := range []string{
		string(surface.CategoryDWDimensionNoSurrogateKey), // dim_product's natural key
		string(surface.CategoryDWNoTimeDimension),         // no dim_date anywhere
	} {
		found := false
		for _, it := range r.Res.Surface {
			if it.Category != want {
				continue
			}
			found = true
			if it.Fingerprint == "" || it.ID == "" {
				t.Errorf("%s item is not stamped (fingerprint=%q id=%q) — it could never be baselined",
					want, it.Fingerprint, it.ID)
			}
		}
		if !found {
			t.Errorf("no %s item — the DW family did not run through the sensor", want)
		}
	}

	// DW-001 must NOT fire: fact_sales genuinely joins two dimensions.
	for _, it := range r.Res.Surface {
		if it.Category == string(surface.CategoryDWNoFactDimensionFK) {
			t.Errorf("DW-001 fired on a fact that joins two dimensions: %v", it.StructuralSignals)
		}
	}
}

// Every DW category must be in the sensor's baseline scope (ADR 0019), same
// rationale as the DB-020 and prefix-redundant-index locks above: an
// undeclared category can never be baselined or pruned.
func TestSensorDB_OwnedCategories_IncludesEveryDWCategory(t *testing.T) {
	owned := map[string]bool{}
	for _, c := range sdb.New(typescript.New()).OwnedCategories() {
		owned[c] = true
	}
	for _, want := range []surface.Category{
		surface.CategoryDWNoFactDimensionFK,
		surface.CategoryDWDimensionNoSurrogateKey,
		surface.CategoryDWNoTimeDimension,
		surface.CategoryDWSCD2NoCurrencyIndex,
		surface.CategoryDWMixedSCDStrategies,
		surface.CategoryDWNoColumnarIndex,
	} {
		if !owned[string(want)] {
			t.Errorf("OwnedCategories() missing %q", want)
		}
	}
}

// --- 3NF-suppression boundary tests (design §2e, §7 boundary risk #3) ---

// hasSurfaceForTable reports whether items contains a category item whose
// "table: <name>" StructuralSignal equals table.
func hasSurfaceForTable(items []findings.SurfaceItem, category, table string) bool {
	want := "table: " + table
	for _, it := range items {
		if it.Category != category {
			continue
		}
		for _, sig := range it.StructuralSignals {
			if sig == want {
				return true
			}
		}
	}
	return false
}

// auto-olap: default/"auto" config, a GENUINE multi-table star schema
// (fact_sales with real FK fan-out to two dimensions) — this is the headline
// scenario and must be asserted END-TO-END, not folded to OLTP: post-review
// fix (reliability WARNING c, S1 review ledger), a lone single-column PK is
// no longer sufficient corroboration (CRITICAL C1), so dim_product must
// classify as a dimension via REAL fan-in from fact_sales, and
// paradigm.Detect must genuinely return olap for the schema — only then is
// its denormalized (repeating category columns) DB-003 item suppressed
// (spec "Denormalized dimension table is not flagged").
func TestSensorDB_3NFSuppression_AutoOLAP_DimensionTableNotFlagged(t *testing.T) {
	schema := `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model fact_sales {
  id          Int          @id
  customer_id Int
  product_id  Int
  customer    dim_customer @relation(fields: [customer_id], references: [id])
  product     dim_product  @relation(fields: [product_id], references: [id])
}

model dim_customer {
  id Int @id
}

model dim_product {
  id        Int    @id
  category1 String
  category2 String
}
`
	yaml := `version: "1"
project:
  name: t
  language: typescript
  framework: next
database:
  orm: prisma
  type: postgresql
  schema_paths:
    - prisma/schema.prisma
`
	ctx := writeProject(t, "prisma/schema.prisma", schema, yaml)
	r, err := sdb.New(typescript.New()).Audit(ctx)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}

	// The star must genuinely auto-detect as olap, and dim_product must
	// classify via REAL fan-in from fact_sales — not merely because it has a
	// single-column PK (the vacuous corroboration this test used to,
	// wrongly, rely on before the C1 fix).
	cls := paradigm.Detect(r.Schema)
	if cls.Paradigm != paradigm.ParadigmOLAP {
		t.Fatalf("paradigm.Detect(r.Schema).Paradigm = %q, want olap (genuine multi-table star)", cls.Paradigm)
	}
	if got := cls.Roles["dim_product"]; got != paradigm.RoleDimension {
		t.Fatalf("Roles[dim_product] = %q, want dimension (real fan-in from fact_sales)", got)
	}

	if hasSurfaceForTable(r.Res.Surface, string(surface.CategoryDBRepeatingGroups), "dim_product") {
		t.Error("DB-003 fired for dim_product under auto/olap-role detection, want suppressed")
	}
}

// WARNING (risk R2), S1 review ledger: 3NF-suppression must not be silent —
// "siempre se informan las consecuencias" (CLAUDE.md). When suppress3NF
// withholds items, the sensor's Result.Note carries a factual trace of what
// was withheld and how to see it (database.paradigm: oltp). The note must
// stay empty (never spammed) when nothing was suppressed.
func TestSensorDB_3NFSuppression_AuditTraceNote(t *testing.T) {
	t.Run("note appears when items are suppressed", func(t *testing.T) {
		schema := `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model fact_sales {
  id          Int          @id
  customer_id Int
  product_id  Int
  customer    dim_customer @relation(fields: [customer_id], references: [id])
  product     dim_product  @relation(fields: [product_id], references: [id])
}

model dim_customer {
  id Int @id
}

model dim_product {
  id        Int    @id
  category1 String
  category2 String
}
`
		yaml := `version: "1"
project:
  name: t
  language: typescript
  framework: next
database:
  orm: prisma
  type: postgresql
  schema_paths:
    - prisma/schema.prisma
`
		ctx := writeProject(t, "prisma/schema.prisma", schema, yaml)
		r, err := sdb.New(typescript.New()).Audit(ctx)
		if err != nil {
			t.Fatalf("Audit: %v", err)
		}
		if r.Note == "" {
			t.Fatal("Note is empty, want a factual trace of what 3NF-suppression withheld")
		}
		if !strings.Contains(r.Note, "1") {
			t.Errorf("Note = %q, want it to mention the suppressed count (1)", r.Note)
		}
		if !strings.Contains(r.Note, "database.paradigm") {
			t.Errorf("Note = %q, want it to point at database.paradigm: oltp as the escape hatch", r.Note)
		}
	})

	t.Run("note is empty when nothing is suppressed", func(t *testing.T) {
		// yamlWithSchema has paradigm: oltp and happySchema has no
		// fact_/dim_ tables — suppress3NF never drops anything here.
		ctx := writeProject(t, "prisma/schema.prisma", happySchema, yamlWithSchema)
		r, err := sdb.New(typescript.New()).Audit(ctx)
		if err != nil {
			t.Fatalf("Audit: %v", err)
		}
		if r.Note != "" {
			t.Errorf("Note = %q, want empty when nothing was suppressed (never spam)", r.Note)
		}
	})

	t.Run("note pluralizes items and tables when more than one is withheld", func(t *testing.T) {
		schema := `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model fact_sales {
  id          Int          @id
  customer_id Int
  product_id  Int
  customer    dim_customer @relation(fields: [customer_id], references: [id])
  product     dim_product  @relation(fields: [product_id], references: [id])
}

model dim_customer {
  id     Int    @id
  phone1 String
  phone2 String
}

model dim_product {
  id        Int    @id
  category1 String
  category2 String
}
`
		yaml := `version: "1"
project:
  name: t
  language: typescript
  framework: next
database:
  orm: prisma
  type: postgresql
  schema_paths:
    - prisma/schema.prisma
`
		ctx := writeProject(t, "prisma/schema.prisma", schema, yaml)
		r, err := sdb.New(typescript.New()).Audit(ctx)
		if err != nil {
			t.Fatalf("Audit: %v", err)
		}
		if r.Note == "" {
			t.Fatal("Note is empty, want a factual trace of what 3NF-suppression withheld")
		}
		if !strings.Contains(r.Note, "2 1NF surface items") {
			t.Errorf("Note = %q, want it to pluralize \"items\" for a count of 2", r.Note)
		}
		if !strings.Contains(r.Note, "2 OLAP-classified tables") {
			t.Errorf("Note = %q, want it to pluralize \"tables\" for a count of 2", r.Note)
		}
	})
}

// explicit-olap-override: database.paradigm: olap set explicitly, ANY table
// (even one with no fact_/dim_/stg_/mart_ prefix) has its DB-003 item
// dropped SCHEMA-WIDE — the dev asserts the whole schema is a warehouse.
func TestSensorDB_3NFSuppression_ExplicitOLAPOverride_DropsSchemaWide(t *testing.T) {
	schema := `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model widgets {
  id     Int    @id
  color1 String
  color2 String
}
`
	yaml := `version: "1"
project:
  name: t
  language: typescript
  framework: next
database:
  orm: prisma
  type: postgresql
  paradigm: olap
  schema_paths:
    - prisma/schema.prisma
`
	ctx := writeProject(t, "prisma/schema.prisma", schema, yaml)
	r, err := sdb.New(typescript.New()).Audit(ctx)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if hasSurfaceForTable(r.Res.Surface, string(surface.CategoryDBRepeatingGroups), "widgets") {
		t.Error("DB-003 fired for widgets (unclassified role) under explicit olap override, want schema-wide suppression")
	}
}

// mixed-partial: mixed paradigm (explicit override here), a dimension-role
// table's DB-003 item is dropped WHILE an unrelated unclassified-role
// table's DB-003 item in the SAME schema still fires — proves NO
// over-suppression on mixed.
func TestSensorDB_3NFSuppression_Mixed_OnlyOLAPRoleSuppressed(t *testing.T) {
	schema := `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model fact_sales {
  id          Int          @id
  customer_id Int
  customer    dim_customer @relation(fields: [customer_id], references: [id])
}

model dim_customer {
  id     Int    @id
  phone1 String
  phone2 String
}

model orders {
  id     Int    @id
  color1 String
  color2 String
}
`
	yaml := `version: "1"
project:
  name: t
  language: typescript
  framework: next
database:
  orm: prisma
  type: postgresql
  paradigm: mixed
  schema_paths:
    - prisma/schema.prisma
`
	ctx := writeProject(t, "prisma/schema.prisma", schema, yaml)
	r, err := sdb.New(typescript.New()).Audit(ctx)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if hasSurfaceForTable(r.Res.Surface, string(surface.CategoryDBRepeatingGroups), "dim_customer") {
		t.Error("DB-003 fired for dim_customer (dimension role) under mixed, want suppressed")
	}
	if !hasSurfaceForTable(r.Res.Surface, string(surface.CategoryDBRepeatingGroups), "orders") {
		t.Error("DB-003 did not fire for orders (unclassified role) under mixed, want NOT suppressed (over-suppression)")
	}
}

// DB-002 (CategoryDBMultivalued) boundary tests (CRITICAL C2, S1 review
// ledger): suppress3NF treats DB-002 and DB-003 identically (db.go
// suppressedCategory), but until this fix ONLY DB-003 was ever exercised by
// a test — a visible contract of one of the slice's two rules went
// unverified. These lock the same auto-olap/mixed-partial boundaries
// already locked for DB-003, above, for DB-002.
func TestSensorDB_3NFSuppression_DB002_MultivaluedColumn(t *testing.T) {
	t.Run("auto-olap: multivalued column on a dimension table is suppressed", func(t *testing.T) {
		schema := `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model fact_sales {
  id          Int          @id
  customer_id Int
  product_id  Int
  customer    dim_customer @relation(fields: [customer_id], references: [id])
  product     dim_product  @relation(fields: [product_id], references: [id])
}

model dim_customer {
  id Int @id
}

model dim_product {
  id     Int      @id
  labels String[]
}
`
		yaml := `version: "1"
project:
  name: t
  language: typescript
  framework: next
database:
  orm: prisma
  type: postgresql
  schema_paths:
    - prisma/schema.prisma
`
		ctx := writeProject(t, "prisma/schema.prisma", schema, yaml)
		r, err := sdb.New(typescript.New()).Audit(ctx)
		if err != nil {
			t.Fatalf("Audit: %v", err)
		}
		if hasSurfaceForTable(r.Res.Surface, string(surface.CategoryDBMultivalued), "dim_product") {
			t.Error("DB-002 fired for dim_product under auto/olap-role detection, want suppressed")
		}
	})

	t.Run("mixed: dimension suppressed, unclassified table NOT over-suppressed", func(t *testing.T) {
		schema := `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model fact_sales {
  id          Int          @id
  customer_id Int
  customer    dim_customer @relation(fields: [customer_id], references: [id])
}

model dim_customer {
  id   Int      @id
  tags String[]
}

model widgets {
  id   Int      @id
  tags String[]
}
`
		yaml := `version: "1"
project:
  name: t
  language: typescript
  framework: next
database:
  orm: prisma
  type: postgresql
  paradigm: mixed
  schema_paths:
    - prisma/schema.prisma
`
		ctx := writeProject(t, "prisma/schema.prisma", schema, yaml)
		r, err := sdb.New(typescript.New()).Audit(ctx)
		if err != nil {
			t.Fatalf("Audit: %v", err)
		}
		if hasSurfaceForTable(r.Res.Surface, string(surface.CategoryDBMultivalued), "dim_customer") {
			t.Error("DB-002 fired for dim_customer (dimension role) under mixed, want suppressed")
		}
		if !hasSurfaceForTable(r.Res.Surface, string(surface.CategoryDBMultivalued), "widgets") {
			t.Error("DB-002 did not fire for widgets (unclassified role) under mixed, want NOT suppressed (over-suppression)")
		}
	})
}

// oltp-negative: an explicit oltp override on a dim_-prefixed schema with the
// identical shape as the auto-olap case MUST still fire — explicit config
// always wins, detection MUST NOT suppress. A detected-oltp schema (no
// override, no recognized prefix) with the identical repeating-group shape
// also fires unchanged.
func TestSensorDB_3NFSuppression_OLTPNegative_StillFlagged(t *testing.T) {
	t.Run("explicit oltp override on a dim_-prefixed table", func(t *testing.T) {
		schema := `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model dim_product {
  id        Int    @id
  category1 String
  category2 String
}
`
		yaml := `version: "1"
project:
  name: t
  language: typescript
  framework: next
database:
  orm: prisma
  type: postgresql
  paradigm: oltp
  schema_paths:
    - prisma/schema.prisma
`
		ctx := writeProject(t, "prisma/schema.prisma", schema, yaml)
		r, err := sdb.New(typescript.New()).Audit(ctx)
		if err != nil {
			t.Fatalf("Audit: %v", err)
		}
		if !hasSurfaceForTable(r.Res.Surface, string(surface.CategoryDBRepeatingGroups), "dim_product") {
			t.Error("DB-003 did not fire for dim_product under explicit oltp override, want NOT suppressed (explicit config always wins)")
		}
	})

	t.Run("explicit oltp override on a fact_-prefixed table", func(t *testing.T) {
		// reliability WARNING (d), S1 review ledger: the doc names BOTH
		// fact_/dim_ prefixes for this explicit-oltp-always-wins guarantee,
		// but only dim_ had ever been exercised. Also covers DB-002
		// (multivalued) alongside DB-003 (repeating groups) on the same
		// table.
		schema := `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model fact_sales {
  id      Int      @id
  amount1 Int
  amount2 Int
  tags    String[]
}
`
		yaml := `version: "1"
project:
  name: t
  language: typescript
  framework: next
database:
  orm: prisma
  type: postgresql
  paradigm: oltp
  schema_paths:
    - prisma/schema.prisma
`
		ctx := writeProject(t, "prisma/schema.prisma", schema, yaml)
		r, err := sdb.New(typescript.New()).Audit(ctx)
		if err != nil {
			t.Fatalf("Audit: %v", err)
		}
		if !hasSurfaceForTable(r.Res.Surface, string(surface.CategoryDBRepeatingGroups), "fact_sales") {
			t.Error("DB-003 did not fire for fact_sales under explicit oltp override, want NOT suppressed (explicit config always wins)")
		}
		if !hasSurfaceForTable(r.Res.Surface, string(surface.CategoryDBMultivalued), "fact_sales") {
			t.Error("DB-002 did not fire for fact_sales under explicit oltp override, want NOT suppressed (explicit config always wins)")
		}
	})

	t.Run("detected-oltp, no prefix, same shape", func(t *testing.T) {
		schema := `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model widgets {
  id     Int    @id
  color1 String
  color2 String
}
`
		yaml := `version: "1"
project:
  name: t
  language: typescript
  framework: next
database:
  orm: prisma
  type: postgresql
  schema_paths:
    - prisma/schema.prisma
`
		ctx := writeProject(t, "prisma/schema.prisma", schema, yaml)
		r, err := sdb.New(typescript.New()).Audit(ctx)
		if err != nil {
			t.Fatalf("Audit: %v", err)
		}
		if !hasSurfaceForTable(r.Res.Surface, string(surface.CategoryDBRepeatingGroups), "widgets") {
			t.Error("DB-003 did not fire for widgets under detected-oltp (no prefix), want NOT suppressed")
		}
	})
}

// A directory schema_path is expanded to its *.sql files in Flyway version order.
// V2 creates the table, V10 alters it — lexical order (V10 < V2) would break the
// reduction; the numeric Flyway sort must apply V2 first.
func TestSensorDB_FlywayDirectory_OrderedIncremental(t *testing.T) {
	root := t.TempDir()
	mig := filepath.Join(root, "db", "migration")
	if err := os.MkdirAll(mig, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mig, "V2__create.sql"), []byte("CREATE TABLE t (id int);"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mig, "V10__alter.sql"), []byte("ALTER TABLE t ADD COLUMN name varchar(10);"), 0o644); err != nil {
		t.Fatal(err)
	}
	yaml := "version: \"1\"\nproject:\n  name: t\n  language: java\n  framework: spring\ndatabase:\n  type: postgresql\n  schema_paths:\n    - db/migration\n"
	if err := os.WriteFile(filepath.Join(root, ".codefit.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(root, ".codefit.yaml"))
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	ctx := auditctx.AuditContext{ProjectRoot: root, Language: "java", Config: cfg}
	r, err := sdb.New(sqlddl.New()).Audit(ctx)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if !r.Measured {
		t.Fatalf("Measured=false: %q", r.Note)
	}
	// The table was reconstructed from the .sql directory (DB-050 fires: no PK).
	var db050 int
	for _, f := range r.Res.Findings {
		if f.ID == "DB-050" {
			db050++
		}
	}
	if db050 != 1 {
		t.Errorf("expected the .sql directory reconstructed exactly one table (1 DB-050), got %d", db050)
	}
}
