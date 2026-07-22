package crossrules

import (
	"reflect"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/query"
)

func schema(tables ...db.Table) *db.Schema { return &db.Schema{Tables: tables} }

func table(name string, cols ...string) db.Table {
	t := db.Table{Name: name}
	for _, c := range cols {
		t.Columns = append(t.Columns, db.Column{Name: c})
	}
	return t
}

// TestReconcile is the certainty gate: it resolves only on an exact single table
// match with at least one real column, and abstains otherwise. Schemas are built
// by hand (neutral), no parsing.
func TestReconcile(t *testing.T) {
	s := schema(
		table("User", "id", "email", "tenantId", "status"),
		table("Post", "id", "title"),
	)
	tests := []struct {
		name     string
		filter   query.QueryFilter
		wantOK   bool
		wantCols []string
	}{
		{"exact single match, column exists",
			query.QueryFilter{Model: "User", Columns: []string{"email"}}, true, []string{"email"}},
		{"composite, all columns exist",
			query.QueryFilter{Model: "User", Columns: []string{"tenantId", "status"}, Composite: true}, true, []string{"tenantId", "status"}},
		{"no table by this name — abstain",
			query.QueryFilter{Model: "Comment", Columns: []string{"id"}}, false, nil},
		{"empty model — abstain",
			query.QueryFilter{Model: "", Columns: []string{"id"}}, false, nil},
		{"column does not exist — abstain",
			query.QueryFilter{Model: "Post", Columns: []string{"authorId"}}, false, nil},
		{"relation-nested filter (not a column) — abstain",
			query.QueryFilter{Model: "User", Columns: []string{"profile"}}, false, nil},
		{"partial: keep the real column, drop the phantom",
			query.QueryFilter{Model: "User", Columns: []string{"email", "ghost"}}, true, []string{"email"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, cols, ok := reconcile(s, tt.filter)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !reflect.DeepEqual(cols, tt.wantCols) {
				t.Errorf("cols = %v, want %v", cols, tt.wantCols)
			}
		})
	}
}

// TestReconcile_MultipleMatchAbstains locks the ambiguity floor: two tables sharing
// a Name (a merge artifact, two files defining the same model) must ABSTAIN, never
// silently pick one.
func TestReconcile_MultipleMatchAbstains(t *testing.T) {
	s := schema(table("User", "id", "email"), table("User", "id", "name"))
	if _, _, ok := reconcile(s, query.QueryFilter{Model: "User", Columns: []string{"id"}}); ok {
		t.Error("reconcile must abstain when more than one table matches the model")
	}
}

// TestReconcile_ResolvedTableIsTheRightOne guards that reconcile returns the table
// it matched (a later rule reads its Indexes/PrimaryKey).
func TestReconcile_ResolvedTableIsTheRightOne(t *testing.T) {
	s := schema(table("User", "id", "email"), table("Post", "id", "title"))
	got, _, ok := reconcile(s, query.QueryFilter{Model: "Post", Columns: []string{"title"}})
	if !ok || got == nil || got.Name != "Post" {
		t.Fatalf("resolved table = %v (ok=%v), want Post", got, ok)
	}
}

// TestReconcile_MappedFieldMatchesOnFieldName locks that @map does NOT break the
// Prisma cross (ADR 0029): the code filters by the FIELD name (email), and the
// Prisma parser puts the field name in Column.Name — the @map physical name goes
// to DBName, which reconcile never reads. So they match. The schema shape here is
// exactly what the real parser produces (verified by
// typescript.TestPrismaMapNamingSpace).
func TestReconcile_MappedFieldMatchesOnFieldName(t *testing.T) {
	s := schema(db.Table{Name: "User", Columns: []db.Column{
		{Name: "id"},
		{Name: "email", DBName: "user_email"}, // @map("user_email")
	}})
	_, cols, ok := reconcile(s, query.QueryFilter{Model: "User", Columns: []string{"email"}})
	if !ok || !reflect.DeepEqual(cols, []string{"email"}) {
		t.Fatalf("a @map'd field must match on the FIELD name (email), got cols=%v ok=%v", cols, ok)
	}
}

// TestReconcile_PhysicalNameSchemaAbstains is the DECLARED LIMIT (ADR 0029): when
// the schema is in PHYSICAL-column-name space (a SQL-DDL parse, where Column.Name
// is the DB column "user_email") but the code speaks the LOGICAL field name
// (email), the two names live in different spaces and reconcile ABSTAINS — it never
// fuzzy-matches across the field↔column boundary. Cross-naming-space (Prisma-field
// code × physical-name schema) is out of scope until a non-Prisma extractor
// resolves the field→column mapping in its own provider. Same honesty floor as
// OR/NOT/relation-nested.
func TestReconcile_PhysicalNameSchemaAbstains(t *testing.T) {
	s := schema(db.Table{Name: "User", Columns: []db.Column{
		{Name: "user_email"}, // SQL-DDL: only the physical column name exists
	}})
	if _, _, ok := reconcile(s, query.QueryFilter{Model: "User", Columns: []string{"email"}}); ok {
		t.Error("a physical-name schema crossed with a logical-field query must abstain (declared limit)")
	}
}

// TestRunWith_EmptyRuleSet is the merge-mechanism gate at the core: RunWith with an
// EXPLICITLY empty rule set yields nothing even for a filter that WOULD reconcile —
// independent of what All() holds. This is the injectable property the seam gate
// relies on, and it must stay true forever as All() grows.
func TestRunWith_EmptyRuleSet(t *testing.T) {
	s := schema(table("User", "id", "email"))
	f, surf := RunWith(s, []query.QueryFilter{{Model: "User", Columns: []string{"email"}}}, nil)
	if f != nil || surf != nil {
		t.Errorf("RunWith an empty rule set must yield nothing, got findings=%v surface=%v", f, surf)
	}
}

// TestRun_NilSchema — no database, nothing to cross.
func TestRun_NilSchema(t *testing.T) {
	if f, surf := Run(nil, nil); f != nil || surf != nil {
		t.Error("Run(nil, …) must yield nothing")
	}
}
