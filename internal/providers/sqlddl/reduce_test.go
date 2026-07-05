package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// compile-time: the SQL-DDL parser is a providers.SchemaParser (and nothing more).
var _ providers.SchemaParser = (*sqlddl.Parser)(nil)

func parse(t *testing.T, files ...string) *db.Schema {
	t.Helper()
	srcs := make([]providers.SourceFile, len(files))
	for i, f := range files {
		srcs[i] = providers.SourceFile{Path: "V" + itoa(i) + "__m.sql", Content: []byte(f)}
	}
	s, err := sqlddl.New().ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	return s
}

func itoa(n int) string { return string(rune('0' + n)) }

func table(t *testing.T, s *db.Schema, name string) db.Table {
	t.Helper()
	for _, tb := range s.Tables {
		if tb.Name == name {
			return tb
		}
	}
	t.Fatalf("table %q not found; have %v", name, tableNames(s))
	return db.Table{}
}

func tableNames(s *db.Schema) []string {
	var out []string
	for _, tb := range s.Tables {
		out = append(out, tb.Name)
	}
	return out
}

func colNames(tb db.Table) []string {
	var out []string
	for _, c := range tb.Columns {
		out = append(out, c.Name)
	}
	return out
}

func hasCol(tb db.Table, name string) bool {
	for _, c := range tb.Columns {
		if c.Name == name {
			return true
		}
	}
	return false
}

func eqStr(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- incremental reducer ---

func TestReducer_IncrementalTableAcrossMigrations(t *testing.T) {
	// A table born in one "migration", altered in later ones — the final state accumulates.
	v1 := `CREATE TABLE plant (
  id BIGSERIAL PRIMARY KEY,
  organization_id BIGINT NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  plant_code VARCHAR(30) NOT NULL,
  CONSTRAINT uq_plant_code UNIQUE (plant_code)
);
CREATE INDEX idx_plant_org ON plant(organization_id);`
	v2 := `ALTER TABLE plant ADD COLUMN current_stage VARCHAR(20) NOT NULL DEFAULT 'GERMINATION';`
	v3 := `ALTER TABLE plant ADD COLUMN sede_id BIGINT REFERENCES sede(id);
CREATE INDEX idx_plant_sede ON plant(sede_id);`
	s := parse(t, v1, v2, v3)
	plant := table(t, s, "plant")
	if got := colNames(plant); !eqStr(got, []string{"id", "organization_id", "plant_code", "current_stage", "sede_id"}) {
		t.Errorf("plant columns = %v, want accumulated [id organization_id plant_code current_stage sede_id]", got)
	}
	if !eqStr(plant.PrimaryKey, []string{"id"}) {
		t.Errorf("plant PK = %v, want [id]", plant.PrimaryKey)
	}
	if len(plant.ForeignKeys) != 2 {
		t.Errorf("plant FKs = %d, want 2 (organization, sede)", len(plant.ForeignKeys))
	}
	if len(plant.Indexes) < 3 { // uq_plant_code (unique) + idx_plant_org + idx_plant_sede
		t.Errorf("plant indexes = %d, want >= 3", len(plant.Indexes))
	}
}

func TestReducer_DropColumn(t *testing.T) {
	s := parse(t, "CREATE TABLE t (id int, tmp int);", "ALTER TABLE t DROP COLUMN tmp;")
	if hasCol(table(t, s, "t"), "tmp") {
		t.Error("DROP COLUMN tmp should have removed it")
	}
}

// AJUSTE 1 — IF NOT EXISTS skips the WHOLE statement (first wins, no merge).
func TestReducer_IfNotExists_SkipsWholeStatement(t *testing.T) {
	first := `CREATE TABLE t (id int, only_first int);`
	second := `CREATE TABLE IF NOT EXISTS t (id int, only_second int);`
	s := parse(t, first, second)
	tb := table(t, s, "t")
	if !hasCol(tb, "only_first") {
		t.Error("first CREATE TABLE's columns must survive")
	}
	if hasCol(tb, "only_second") {
		t.Error("IF NOT EXISTS on an existing table must skip the WHOLE statement (no Frankenstein merge)")
	}
	if got := colNames(tb); !eqStr(got, []string{"id", "only_first"}) {
		t.Errorf("columns = %v, want only the first body [id only_first]", got)
	}
}

// --- DML / non-DDL skipped without crashing ---

func TestReducer_SkipsDMLAndProcedural(t *testing.T) {
	src := `CREATE TABLE t (id int);
INSERT INTO t (id) VALUES (1);
UPDATE t SET id = 2 WHERE id = 1;
GRANT SELECT ON t TO someone;
DO $$ BEGIN PERFORM 1; END $$;
COMMENT ON TABLE t IS 'hi';
ALTER TABLE t ADD COLUMN name varchar(10);`
	s := parse(t, src)
	tb := table(t, s, "t")
	if !eqStr(colNames(tb), []string{"id", "name"}) {
		t.Errorf("columns = %v, want [id name] (DML/DO/GRANT skipped, DDL applied)", colNames(tb))
	}
}

// --- type mapping ---

func TestReducer_TypeMapping(t *testing.T) {
	s := parse(t, `CREATE TABLE t (
  a BIGSERIAL,
  b VARCHAR(30),
  c TEXT,
  d TIMESTAMP,
  e BOOLEAN,
  f JSONB,
  g NUMERIC(10,2),
  h tag[] );`)
	want := map[string]db.Type{"a": db.TypeInt, "b": db.TypeString, "c": db.TypeText, "d": db.TypeDateTime, "e": db.TypeBool, "f": db.TypeJSON, "g": db.TypeFloat}
	tb := table(t, s, "t")
	for _, col := range tb.Columns {
		if w, ok := want[col.Name]; ok && col.Type != w {
			t.Errorf("col %s type = %s, want %s (raw %q)", col.Name, col.Type, w, col.RawType)
		}
	}
	for _, col := range tb.Columns {
		if col.Name == "h" && !col.List {
			t.Error("tag[] should set List=true")
		}
	}
}

// --- declared limits: parse the structural part, ignore the rest, never crash ---

func TestReducer_DeclaredLimits_NoCrash(t *testing.T) {
	src := `CREATE TYPE mood AS ENUM ('happy','sad');
CREATE TABLE t (
  id int,
  status varchar(10),
  CONSTRAINT chk_status CHECK (status IN ('A','B'))
);
CREATE INDEX idx_partial ON t (status) WHERE status = 'A';
ALTER TABLE t ALTER COLUMN status SET NOT NULL;`
	s := parse(t, src)
	tb := table(t, s, "t")
	if !hasCol(tb, "status") {
		t.Error("the structural part (columns) must still parse despite CHECK/partial/ALTER COLUMN")
	}
	// the partial index is still recorded (base columns), the WHERE is ignored
	if len(tb.Indexes) != 1 {
		t.Errorf("partial index should be recorded by its base columns, got %d indexes", len(tb.Indexes))
	}
}

// --- views / procedures / triggers populated (bodies NOT captured) ---

func TestReducer_ViewProcTrigger_Populated(t *testing.T) {
	src := `CREATE VIEW active_v AS SELECT id FROM t WHERE ok;
CREATE MATERIALIZED VIEW mv AS SELECT 1;
CREATE FUNCTION f() RETURNS int AS $$ BEGIN RETURN 1; END; $$ LANGUAGE plpgsql;
CREATE TRIGGER trg BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION f();`
	s := parse(t, src)
	if len(s.Views) != 2 {
		t.Errorf("views = %d, want 2 (view + materialized)", len(s.Views))
	}
	if len(s.Procedures) != 1 {
		t.Errorf("procedures = %d, want 1", len(s.Procedures))
	}
	if len(s.Triggers) != 1 {
		t.Fatalf("triggers = %d, want 1", len(s.Triggers))
	}
	if s.Triggers[0].Table != "t" {
		t.Errorf("trigger.Table = %q, want t (parsed from ON)", s.Triggers[0].Table)
	}
}
