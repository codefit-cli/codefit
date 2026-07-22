package typescript

import (
	"reflect"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/query"
	"github.com/codefit-cli/codefit/internal/providers"
)

func extract(t *testing.T, code string) []query.QueryFilter {
	t.Helper()
	fs, err := (&Provider{}).ExtractQueryFilters(providers.SourceFile{Path: "app.ts", Content: []byte(code)})
	if err != nil {
		t.Fatalf("ExtractQueryFilters: %v", err)
	}
	return fs
}

// one asserts a single filter with the given model/columns/composite (Pos ignored).
func one(t *testing.T, fs []query.QueryFilter, model string, cols []string, composite bool) {
	t.Helper()
	if len(fs) != 1 {
		t.Fatalf("want 1 filter, got %d: %+v", len(fs), fs)
	}
	f := fs[0]
	if f.Model != model || !reflect.DeepEqual(f.Columns, cols) || f.Composite != composite {
		t.Errorf("filter = {Model:%q Columns:%v Composite:%v}, want {Model:%q Columns:%v Composite:%v}",
			f.Model, f.Columns, f.Composite, model, cols, composite)
	}
}

func TestExtract_SingleColumnWhere(t *testing.T) {
	fs := extract(t, `const u = await prisma.user.findMany({ where: { email: input } })`)
	one(t, fs, "User", []string{"email"}, false)
}

func TestExtract_CompositeWhere(t *testing.T) {
	fs := extract(t, `prisma.user.findMany({ where: { tenantId: t, status: s } })`)
	one(t, fs, "User", []string{"tenantId", "status"}, true)
}

func TestExtract_ShorthandWhere(t *testing.T) {
	// findUnique({ where: { id } }) — id is shorthand for id: id, a filter by id.
	fs := extract(t, `prisma.user.findUnique({ where: { id } })`)
	one(t, fs, "User", []string{"id"}, false)
}

func TestExtract_QuotedKey(t *testing.T) {
	fs := extract(t, `prisma.user.findFirst({ where: { "email": input } })`)
	one(t, fs, "User", []string{"email"}, false)
}

func TestExtract_AliasedClientAndModelNormalization(t *testing.T) {
	// Aliased client (db, not prisma) + a multi-word model: the accessor userProfile
	// normalizes to the schema name UserProfile (ADR 0029).
	fs := extract(t, `db.userProfile.findFirst({ where: { slug: s } })`)
	one(t, fs, "UserProfile", []string{"slug"}, false)
}

func TestExtract_AndMerges(t *testing.T) {
	// AND: [ {…}, {…} ] merges into one composite column group.
	fs := extract(t, `prisma.user.findMany({ where: { AND: [ { tenantId: t }, { status: s } ] } })`)
	one(t, fs, "User", []string{"tenantId", "status"}, true)
}

func TestExtract_AndObjectForm(t *testing.T) {
	fs := extract(t, `prisma.user.findMany({ where: { AND: { tenantId: t, status: s } } })`)
	one(t, fs, "User", []string{"tenantId", "status"}, true)
}

// THE TRAP: a create with data:{email} — keys that LOOK like columns but are NOT a
// filter — must emit ZERO filters. Only where filters. Same lineage as the
// vocab traps of 0.2.3.
func TestExtract_DataIsNotWhere(t *testing.T) {
	if fs := extract(t, `prisma.user.create({ data: { email: x, name: y } })`); len(fs) != 0 {
		t.Errorf("create with data:{…} must emit no filter (data is not where), got %+v", fs)
	}
}

func TestExtract_UpdateReadsWhereNotData(t *testing.T) {
	// update({ where: {id}, data: {email} }) — only the where (id) is a filter.
	fs := extract(t, `prisma.user.update({ where: { id }, data: { email: x } })`)
	one(t, fs, "User", []string{"id"}, false)
}

func TestExtract_SelectIncludeOrderByAreNotWhere(t *testing.T) {
	// select/include/orderBy objects have column-looking keys but do not filter.
	fs := extract(t, `prisma.user.findMany({
		select: { email: true, name: true },
		orderBy: { createdAt: "asc" },
		where: { id: input },
	})`)
	one(t, fs, "User", []string{"id"}, false)
}

func TestExtract_NoWhereNoFilter(t *testing.T) {
	if fs := extract(t, `prisma.user.findMany()`); len(fs) != 0 {
		t.Errorf("a bare findMany() must emit no filter, got %+v", fs)
	}
	if fs := extract(t, `prisma.user.findMany({ orderBy: { createdAt: "asc" } })`); len(fs) != 0 {
		t.Errorf("a findMany with no where must emit no filter, got %+v", fs)
	}
}

// DECLARED LIMIT: OR/NOT are skipped this slice. A where whose only top-level keys
// are OR/NOT reduces to no indexable column set → no filter.
func TestExtract_OrIsSkipped(t *testing.T) {
	if fs := extract(t, `prisma.user.findMany({ where: { OR: [ { email: e }, { name: n } ] } })`); len(fs) != 0 {
		t.Errorf("OR is a declared limit — must emit no filter, got %+v", fs)
	}
}

func TestExtract_NotIsSkipped(t *testing.T) {
	if fs := extract(t, `prisma.user.findMany({ where: { NOT: { deleted: true } } })`); len(fs) != 0 {
		t.Errorf("NOT is a declared limit — must emit no filter, got %+v", fs)
	}
}

// A real scalar column filtered alongside an OR: only the scalar column is emitted;
// the OR branch is skipped (declared limit), never guessed.
func TestExtract_ScalarBesideOr(t *testing.T) {
	fs := extract(t, `prisma.user.findMany({ where: { tenantId: t, OR: [ { a: 1 }, { b: 2 } ] } })`)
	one(t, fs, "User", []string{"tenantId"}, false)
}

func TestExtract_MultipleCallSites(t *testing.T) {
	fs := extract(t, `
		async function h() {
			await prisma.user.findUnique({ where: { id } })
			await prisma.post.findMany({ where: { authorId: a } })
		}`)
	if len(fs) != 2 {
		t.Fatalf("want 2 filters, got %d: %+v", len(fs), fs)
	}
	if fs[0].Model != "User" || fs[1].Model != "Post" {
		t.Errorf("models = %q,%q want User,Post", fs[0].Model, fs[1].Model)
	}
}

// TestPrismaMapNamingSpace locks the naming space the cross RELIES ON, verified
// against the real Prisma parser: a @map'd field keeps its FIELD name in
// Column.Name (the @map physical name goes to DBName), and @unique/@@index record
// FIELD names too — never the physical column. So the whole Prisma world (a code
// query's where field, Column.Name, Index.Columns) is one consistent field-name
// space, which is why @map does NOT break the cross (ADR 0029). If a future parser
// change moved the physical name into Column.Name, this fails loudly and the
// reconcile-in-logical-space assumption would need revisiting.
func TestPrismaMapNamingSpace(t *testing.T) {
	src := `model User {
  id    Int    @id
  email String @unique @map("user_email")
  @@index([email])
}`
	s, err := (&Provider{}).ParseSchema([]providers.SourceFile{{Path: "schema.prisma", Content: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	var email *db.Column
	for i := range s.Tables[0].Columns {
		if s.Tables[0].Columns[i].Name == "email" {
			email = &s.Tables[0].Columns[i]
		}
	}
	if email == nil {
		t.Fatal("email column not found by its FIELD name — parser moved it out of Column.Name")
	}
	if email.DBName != "user_email" {
		t.Errorf("@map physical name = %q, want it in DBName as user_email", email.DBName)
	}
	for _, idx := range s.Tables[0].Indexes {
		for _, c := range idx.Columns {
			if c == "user_email" {
				t.Errorf("index records the PHYSICAL name %q; the cross assumes field names", c)
			}
		}
	}
}

func TestModelToSchemaName(t *testing.T) {
	for in, want := range map[string]string{
		"user": "User", "userProfile": "UserProfile", "": "", "User": "User",
	} {
		if got := modelToSchemaName(in); got != want {
			t.Errorf("modelToSchemaName(%q) = %q, want %q", in, got, want)
		}
	}
}
