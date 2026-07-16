package db_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/findings"
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
