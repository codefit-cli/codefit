package dbrules_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// --- DB-051: FK typed as text vs a numeric/uuid key (surface) ---

// twoTable builds a schema: a referenced table and a referencing table with one FK.
func fkSchema(fkCol string, fkType db.Type, fkRaw string, refTable, refCol string, refType db.Type) *db.Schema {
	return &db.Schema{Tables: []db.Table{
		{Name: refTable, PrimaryKey: []string{refCol}, Columns: []db.Column{{Name: refCol, Type: refType}}},
		{Name: "Child", PrimaryKey: []string{"id"},
			Columns: []db.Column{
				{Name: "id", Type: db.TypeInt},
				{Name: fkCol, Type: fkType, RawType: fkRaw, Pos: db.Pos{File: "s.prisma", Line: 5}},
			},
			ForeignKeys: []db.ForeignKey{{Columns: []string{fkCol}, RefTable: refTable, RefColumns: []string{refCol}, Pos: db.Pos{File: "s.prisma", Line: 6}}},
		},
	}}
}

func TestDB051_MismatchFires(t *testing.T) {
	s := fkSchema("authorId", db.TypeString, "String", "User", "id", db.TypeInt)
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBFKTextType)
	if len(got) != 1 {
		t.Fatalf("DB-051 = %d, want 1 (String FK -> Int PK)", len(got))
	}
	if !got[0].StructuralFacts["type_mismatch"] {
		t.Error("type_mismatch should be true")
	}
	if got[0].Line != 6 {
		t.Errorf("anchor line = %d, want 6 (ForeignKey.Pos)", got[0].Line)
	}
}

func TestDB051_UuidMatchDoesNotFire(t *testing.T) {
	s := fkSchema("orgId", db.TypeString, "String", "Org", "id", db.TypeString)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBFKTextType); len(got) != 0 {
		t.Errorf("String FK -> String uuid PK must NOT fire, got %d", len(got))
	}
}

func TestDB051_IntToIntDoesNotFire(t *testing.T) {
	s := fkSchema("postId", db.TypeInt, "Int", "Post", "id", db.TypeInt)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBFKTextType); len(got) != 0 {
		t.Errorf("Int FK -> Int PK must NOT fire, got %d", len(got))
	}
}

func TestDB051_TextKeyFires(t *testing.T) {
	// String -> String (no mismatch) but @db.Text key.
	s := fkSchema("orgRef", db.TypeText, "String @db.Text", "Org", "id", db.TypeString)
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBFKTextType)
	if len(got) != 1 {
		t.Fatalf("DB-051 text_key = %d, want 1", len(got))
	}
	if !got[0].StructuralFacts["text_key"] || got[0].StructuralFacts["type_mismatch"] {
		t.Errorf("want text_key=true, type_mismatch=false; got facts %v", got[0].StructuralFacts)
	}
}

func TestDB051_UnresolvableDoesNotFire(t *testing.T) {
	// FK references a table not in the schema → conservative, no fire.
	s := &db.Schema{Tables: []db.Table{
		{Name: "Child", PrimaryKey: []string{"id"},
			Columns:     []db.Column{{Name: "id", Type: db.TypeInt}, {Name: "ghostId", Type: db.TypeString}},
			ForeignKeys: []db.ForeignKey{{Columns: []string{"ghostId"}, RefTable: "Ghost", RefColumns: []string{"id"}}},
		},
	}}
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBFKTextType); len(got) != 0 {
		t.Errorf("unresolvable reference must NOT fire, got %d", len(got))
	}
}

// --- DB-052: missing audit timestamps (surface) ---

func TestDB052_BothMissingFires(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{{
		Name: "T", Pos: db.Pos{File: "s.prisma", Line: 3}, PrimaryKey: []string{"id"},
		Columns: []db.Column{{Name: "id", Type: db.TypeInt}, {Name: "name", Type: db.TypeString}},
	}}}
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBNoTimestamps)
	if len(got) != 1 {
		t.Fatalf("DB-052 = %d, want 1", len(got))
	}
	if got[0].StructuralFacts["has_created_at"] || got[0].StructuralFacts["has_updated_at"] {
		t.Error("has_created_at/has_updated_at should both be false")
	}
	if got[0].StructuralFacts["looks_like_join_table"] {
		t.Error("a table with a data column is not a join table")
	}
}

func TestDB052_BothPresentDoesNotFire(t *testing.T) {
	for _, names := range [][]string{{"createdAt", "updatedAt"}, {"created_at", "updated_at"}} {
		s := &db.Schema{Tables: []db.Table{{
			Name: "T", PrimaryKey: []string{"id"},
			Columns: []db.Column{
				{Name: "id", Type: db.TypeInt},
				{Name: names[0], Type: db.TypeDateTime},
				{Name: names[1], Type: db.TypeDateTime},
			},
		}}}
		_, items := dbrules.Run(s)
		if got := surfaceWithCategory(items, surface.CategoryDBNoTimestamps); len(got) != 0 {
			t.Errorf("both timestamps present (%v) must NOT fire, got %d", names, len(got))
		}
	}
}

func TestDB052_OnlyOneMissingDoesNotFire(t *testing.T) {
	// Deferred candidate (DB-052b): "one of the pair missing" is not decided yet.
	// This locks TODAY's behavior (no fire), not a permanent decision.
	s := &db.Schema{Tables: []db.Table{{
		Name: "T", PrimaryKey: []string{"id"},
		Columns: []db.Column{{Name: "id", Type: db.TypeInt}, {Name: "createdAt", Type: db.TypeDateTime}},
	}}}
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBNoTimestamps); len(got) != 0 {
		t.Errorf("only createdAt present must NOT fire this slice, got %d", len(got))
	}
}

func TestDB052_JoinTableFactSet(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{{
		Name: "Link", PrimaryKey: []string{"aId", "bId"},
		Columns: []db.Column{{Name: "aId", Type: db.TypeInt}, {Name: "bId", Type: db.TypeInt}},
	}}}
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBNoTimestamps)
	if len(got) != 1 {
		t.Fatalf("DB-052 = %d, want 1 (join table still fires)", len(got))
	}
	if !got[0].StructuralFacts["looks_like_join_table"] {
		t.Error("a composite-PK, all-key-columns table should set looks_like_join_table=true")
	}
}

// --- DB-053: sensitive column stored in the clear (surface, ALWAYS emits) ---

func col(name string, typ db.Type) db.Column {
	return db.Column{Name: name, Type: typ, Pos: db.Pos{File: "s.prisma", Line: 4}}
}

func db053Schema(cols ...db.Column) *db.Schema {
	return &db.Schema{Tables: []db.Table{{Name: "T", PrimaryKey: []string{"id"}, Columns: cols}}}
}

func TestDB053_PlaintextFires(t *testing.T) {
	s := db053Schema(col("password", db.TypeString))
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBSensitiveUnencrypted)
	if len(got) != 1 {
		t.Fatalf("DB-053 = %d, want 1 (password String)", len(got))
	}
	if got[0].StructuralFacts["encryption_hint_in_name"] {
		t.Error("plain 'password' has no encryption hint")
	}
}

func TestDB053_HashHintEmitsWithFact(t *testing.T) {
	// Adjustment 1: passwordHash EMITS (not suppressed), with the hint as a FACT.
	s := db053Schema(col("passwordHash", db.TypeString))
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBSensitiveUnencrypted)
	if len(got) != 1 {
		t.Fatalf("passwordHash must still EMIT (fact, not suppression), got %d", len(got))
	}
	if !got[0].StructuralFacts["encryption_hint_in_name"] {
		t.Error("passwordHash must set encryption_hint_in_name=true")
	}
}

func TestDB053_WrongTypeDoesNotFire(t *testing.T) {
	for _, c := range []db.Column{col("passwordChangedAt", db.TypeDateTime), col("passwordResetCount", db.TypeInt), col("isPublic", db.TypeBool)} {
		_, items := dbrules.Run(db053Schema(c))
		if got := surfaceWithCategory(items, surface.CategoryDBSensitiveUnencrypted); len(got) != 0 {
			t.Errorf("%s (%s) must NOT fire (type/token filter), got %d", c.Name, c.Type, len(got))
		}
	}
}

func TestDB053_MultiWordTokensFire(t *testing.T) {
	for _, name := range []string{"apiKey", "refreshToken", "creditCard"} {
		_, items := dbrules.Run(db053Schema(col(name, db.TypeString)))
		if got := surfaceWithCategory(items, surface.CategoryDBSensitiveUnencrypted); len(got) != 1 {
			t.Errorf("%s String must fire, got %d", name, len(got))
		}
	}
}

func TestDB053_NonSensitiveDoesNotFire(t *testing.T) {
	_, items := dbrules.Run(db053Schema(col("bio", db.TypeString), col("keyboard", db.TypeString)))
	if got := surfaceWithCategory(items, surface.CategoryDBSensitiveUnencrypted); len(got) != 0 {
		t.Errorf("non-sensitive names must NOT fire, got %d", len(got))
	}
}

// --- DB-003: repeating groups (surface) ---

func TestDB003_GroupFires(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{{
		Name: "Contact", PrimaryKey: []string{"id"},
		Columns: []db.Column{
			{Name: "id", Type: db.TypeInt},
			{Name: "phone1", Type: db.TypeString, Pos: db.Pos{Line: 10}},
			{Name: "phone2", Type: db.TypeString, Pos: db.Pos{Line: 11}},
			{Name: "phone3", Type: db.TypeString, Pos: db.Pos{Line: 12}},
		},
	}}}
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBRepeatingGroups)
	if len(got) != 1 {
		t.Fatalf("DB-003 = %d, want 1 group", len(got))
	}
	if !got[0].StructuralFacts["uniform_type"] {
		t.Error("phone1/2/3 are all String → uniform_type=true")
	}
	if got[0].Line != 10 {
		t.Errorf("anchor line = %d, want 10 (lowest-line member)", got[0].Line)
	}
}

func TestDB003_SingleDoesNotFire(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{{
		Name: "T", PrimaryKey: []string{"id"},
		Columns: []db.Column{{Name: "id", Type: db.TypeInt}, {Name: "phone1", Type: db.TypeString}},
	}}}
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBRepeatingGroups); len(got) != 0 {
		t.Errorf("a single suffixed column is not a group, got %d", len(got))
	}
}

func TestDB003_MixedTypeDoesNotFire(t *testing.T) {
	s := &db.Schema{Tables: []db.Table{{
		Name: "T", PrimaryKey: []string{"id"},
		Columns: []db.Column{
			{Name: "id", Type: db.TypeInt},
			{Name: "code1", Type: db.TypeString},
			{Name: "code2", Type: db.TypeInt},
		},
	}}}
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBRepeatingGroups); len(got) != 0 {
		t.Errorf("a non-uniform-type base is not a repeating group this slice, got %d", len(got))
	}
}

// --- All() now enumerates eight rules ---

func TestAll_EightRules(t *testing.T) {
	ids := map[string]bool{}
	for _, r := range dbrules.All() {
		ids[r.ID()] = true
	}
	for _, want := range []string{"DB-050", "DB-001", "DB-011", "DB-002", "DB-051", "DB-052", "DB-053", "DB-003"} {
		if !ids[want] {
			t.Errorf("dbrules.All() missing %s (have %v)", want, ids)
		}
	}
}

// --- End-to-end over authored fixtures (parser -> rules) ---

func parseFixtureDB(t *testing.T, name string) *db.Schema {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := typescript.New().ParseSchema([]providers.SourceFile{{Path: name, Content: content}})
	if err != nil {
		t.Fatalf("ParseSchema(%s): %v", name, err)
	}
	return schema
}

func TestFixtures_CategoryCounts(t *testing.T) {
	cases := []struct {
		file string
		cat  surface.Category
		want int
	}{
		{"db051_fk_types.prisma", surface.CategoryDBFKTextType, 2},            // Post.authorId (mismatch), Doc.orgRef (text_key)
		{"db052_timestamps.prisma", surface.CategoryDBNoTimestamps, 2},        // MissingBoth, LinkTable
		{"db053_sensitive.prisma", surface.CategoryDBSensitiveUnencrypted, 4}, // password, passwordHash, apiKey, refreshToken
		{"db003_repeating.prisma", surface.CategoryDBRepeatingGroups, 2},      // phone group, addr group
	}
	for _, c := range cases {
		_, items := dbrules.Run(parseFixtureDB(t, c.file))
		if got := len(surfaceWithCategory(items, c.cat)); got != c.want {
			t.Errorf("%s: %s = %d, want %d", c.file, c.cat, got, c.want)
		}
	}
}
