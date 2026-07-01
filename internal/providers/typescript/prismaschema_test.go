package typescript_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// TestProviderSatisfiesSchemaParser is the type-assertion contract: the TS
// provider implements the providers.SchemaParser capability (resolved by the
// caller exactly like CoverageManifest — ADR 0014).
func TestProviderSatisfiesSchemaParser(t *testing.T) {
	var _ providers.SchemaParser = (*typescript.Provider)(nil)
}

// --- helpers ---

// parseFixture parses testdata/<name> and fails the test on error.
func parseFixture(t *testing.T, name string) *db.Schema {
	t.Helper()
	path := filepath.Join("testdata", name)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %q: %v", name, err)
	}
	schema, err := typescript.New().ParseSchema([]providers.SourceFile{{Path: name, Content: content}})
	if err != nil {
		t.Fatalf("ParseSchema(%q): unexpected error: %v", name, err)
	}
	return schema
}

func parseBlog(t *testing.T) *db.Schema { t.Helper(); return parseFixture(t, "blog.prisma") }

func tableByName(t *testing.T, s *db.Schema, name string) db.Table {
	t.Helper()
	for _, tb := range s.Tables {
		if tb.Name == name {
			return tb
		}
	}
	t.Fatalf("table %q not found (have %v)", name, tableNames(s))
	return db.Table{}
}

func tableNames(s *db.Schema) []string {
	var out []string
	for _, tb := range s.Tables {
		out = append(out, tb.Name)
	}
	return out
}

func columnNames(tb db.Table) []string {
	var out []string
	for _, c := range tb.Columns {
		out = append(out, c.Name)
	}
	return out
}

func columnByName(t *testing.T, tb db.Table, name string) db.Column {
	t.Helper()
	for _, c := range tb.Columns {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("column %q not found in table %q (have %v)", name, tb.Name, columnNames(tb))
	return db.Column{}
}

func hasIndex(tb db.Table, unique bool, cols ...string) bool {
	for _, ix := range tb.Indexes {
		if ix.Unique == unique && equalStrings(ix.Columns, cols) {
			return true
		}
	}
	return false
}

func equalStrings(a, b []string) bool {
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

// lineOf returns the 1-based line number of the first fixture line containing
// substr, so Pos assertions survive fixture edits (no hardcoded line numbers).
func lineOf(t *testing.T, name, substr string) int {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture %q: %v", name, err)
	}
	for i, line := range strings.Split(string(content), "\n") {
		if strings.Contains(line, substr) {
			return i + 1
		}
	}
	t.Fatalf("substring %q not found in fixture %q", substr, name)
	return 0
}

// --- tests ---

// #1
func TestParseSchema_Tables(t *testing.T) {
	s := parseBlog(t)
	got := tableNames(s)
	want := []string{"User", "Post", "Membership"}
	if !equalStrings(got, want) {
		t.Fatalf("tables = %v, want %v", got, want)
	}
}

// #2
func TestParseSchema_Columns(t *testing.T) {
	s := parseBlog(t)
	cases := map[string][]string{
		"User":       {"id", "email", "name", "bio", "tags"}, // posts (relation) excluded
		"Post":       {"id", "title", "authorId"},            // author (relation) excluded
		"Membership": {"userId", "groupId", "role"},
	}
	for table, want := range cases {
		got := columnNames(tableByName(t, s, table))
		if !equalStrings(got, want) {
			t.Errorf("%s columns = %v, want %v", table, got, want)
		}
	}
}

// #3
func TestParseSchema_SimplePK(t *testing.T) {
	if got := tableByName(t, parseBlog(t), "User").PrimaryKey; !equalStrings(got, []string{"id"}) {
		t.Fatalf("User.PrimaryKey = %v, want [id]", got)
	}
}

// #4
func TestParseSchema_CompositePK(t *testing.T) {
	if got := tableByName(t, parseBlog(t), "Membership").PrimaryKey; !equalStrings(got, []string{"userId", "groupId"}) {
		t.Fatalf("Membership.PrimaryKey = %v, want [userId groupId]", got)
	}
}

// #5
func TestParseSchema_Nullable(t *testing.T) {
	u := tableByName(t, parseBlog(t), "User")
	if !columnByName(t, u, "bio").Nullable {
		t.Error("User.bio should be nullable")
	}
	if columnByName(t, u, "name").Nullable {
		t.Error("User.name should NOT be nullable")
	}
}

// #6
func TestParseSchema_List(t *testing.T) {
	if !columnByName(t, tableByName(t, parseBlog(t), "User"), "tags").List {
		t.Error("User.tags should be a list")
	}
}

// #7
func TestParseSchema_Types(t *testing.T) {
	u := tableByName(t, parseBlog(t), "User")
	email := columnByName(t, u, "email")
	if email.Type != db.TypeString || email.RawType != "String" {
		t.Errorf("User.email = {%s, %q}, want {string, String}", email.Type, email.RawType)
	}
	authorID := columnByName(t, tableByName(t, parseBlog(t), "Post"), "authorId")
	if authorID.Type != db.TypeInt {
		t.Errorf("Post.authorId.Type = %s, want int", authorID.Type)
	}
}

// #8
func TestParseSchema_UniqueSingle(t *testing.T) {
	if !hasIndex(tableByName(t, parseBlog(t), "User"), true, "email") {
		t.Error("User should have a single unique index on [email]")
	}
}

// #9
func TestParseSchema_CompositeUnique(t *testing.T) {
	if !hasIndex(tableByName(t, parseBlog(t), "Membership"), true, "groupId", "role") {
		t.Error("Membership should have a composite unique index on [groupId, role]")
	}
}

// #10
func TestParseSchema_CompositeIndex(t *testing.T) {
	if !hasIndex(tableByName(t, parseBlog(t), "Post"), false, "authorId", "title") {
		t.Error("Post should have a composite non-unique index on [authorId, title]")
	}
}

// #11 (+ AJUSTE 2: phantom back-relation FK)
func TestParseSchema_ForeignKey(t *testing.T) {
	s := parseBlog(t)
	post := tableByName(t, s, "Post")
	if len(post.ForeignKeys) != 1 {
		t.Fatalf("Post.ForeignKeys = %d, want 1", len(post.ForeignKeys))
	}
	fk := post.ForeignKeys[0]
	if !equalStrings(fk.Columns, []string{"authorId"}) || fk.RefTable != "User" || !equalStrings(fk.RefColumns, []string{"id"}) {
		t.Errorf("Post FK = %+v, want {[authorId] User [id]}", fk)
	}
	// AJUSTE 2: the back-relation (posts Post[]) must NOT invent a FK on User.
	if got := tableByName(t, s, "User").ForeignKeys; len(got) != 0 {
		t.Errorf("User.ForeignKeys = %+v, want empty (back-relation must not create a FK)", got)
	}
}

// #12
func TestParseSchema_Positions(t *testing.T) {
	s := parseBlog(t)
	user := tableByName(t, s, "User")
	if got, want := user.Pos.Line, lineOf(t, "blog.prisma", "model User {"); got != want {
		t.Errorf("User.Pos.Line = %d, want %d", got, want)
	}
	if user.Pos.File != "blog.prisma" {
		t.Errorf("User.Pos.File = %q, want blog.prisma", user.Pos.File)
	}
	if got, want := columnByName(t, user, "email").Pos.Line, lineOf(t, "blog.prisma", "email String"); got != want {
		t.Errorf("User.email.Pos.Line = %d, want %d", got, want)
	}
	post := tableByName(t, s, "Post")
	for _, ix := range post.Indexes {
		if equalStrings(ix.Columns, []string{"authorId", "title"}) {
			if got, want := ix.Pos.Line, lineOf(t, "blog.prisma", "@@index([authorId, title])"); got != want {
				t.Errorf("composite index Pos.Line = %d, want %d", got, want)
			}
		}
	}
}

// #13
func TestParseSchema_OLTPSurfaceEmpty(t *testing.T) {
	s := parseBlog(t)
	if len(s.Views) != 0 || len(s.Procedures) != 0 || len(s.Triggers) != 0 {
		t.Errorf("Prisma parse should leave Views/Procedures/Triggers empty, got %d/%d/%d",
			len(s.Views), len(s.Procedures), len(s.Triggers))
	}
}

// #15 comments ignored + malformed errors
func TestParseSchema_CommentsIgnored(t *testing.T) {
	src := `// a leading comment
/// a doc comment
model Widget {
  id   Int    @id // trailing comment
  name String      // another
}`
	s, err := typescript.New().ParseSchema([]providers.SourceFile{{Path: "widget.prisma", Content: []byte(src)}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	w := tableByName(t, s, "Widget")
	if got := columnNames(w); !equalStrings(got, []string{"id", "name"}) {
		t.Errorf("Widget columns = %v, want [id name]", got)
	}
}

func TestParseSchema_MalformedErrors(t *testing.T) {
	src := "model Broken {\n  id Int @id\n" // unclosed block
	if _, err := typescript.New().ParseSchema([]providers.SourceFile{{Path: "broken.prisma", Content: []byte(src)}}); err == nil {
		t.Fatal("expected an error for an unclosed block, got nil")
	}
}

// #16 enum column (two-pass): role is a real column, not a relation
func TestParseSchema_EnumColumn(t *testing.T) {
	s := parseBlog(t)
	m := tableByName(t, s, "Membership")
	role := columnByName(t, m, "role")
	if role.Type != db.TypeEnum || role.RawType != "Role" {
		t.Errorf("Membership.role = {%s, %q}, want {enum, Role}", role.Type, role.RawType)
	}
	// negative: Role must not be parsed as a table (it is an enum, not a model).
	for _, tb := range s.Tables {
		if tb.Name == "Role" {
			t.Error("enum Role must not appear as a table")
		}
	}
}

// AJUSTE 3: a view block parses OK and leaves Views empty (skip-and-ignore).
func TestParseSchema_ViewSkipped(t *testing.T) {
	src := `view ActiveUser {
  id    Int
  email String
}

model User {
  id Int @id
}`
	s, err := typescript.New().ParseSchema([]providers.SourceFile{{Path: "v.prisma", Content: []byte(src)}})
	if err != nil {
		t.Fatalf("view block should parse without error, got: %v", err)
	}
	if len(s.Views) != 0 {
		t.Errorf("Views should stay empty (out of scope this slice), got %d", len(s.Views))
	}
	if len(s.Tables) != 1 || s.Tables[0].Name != "User" {
		t.Errorf("expected only the User table, got %v", tableNames(s))
	}
}
