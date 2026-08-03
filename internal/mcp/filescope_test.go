package mcp

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/scope"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// twoQueryProject writes two handlers that each filter a DIFFERENT column, so
// which filters come back names exactly which files the cross walk opened. The
// filters are produced by the REAL typescript extractor, never hand-built.
func twoQueryProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("src/touched.ts", "export async function GET() {\n  return await prisma.account.findMany({ where: { status: \"x\" } })\n}\n")
	write("src/untouched.ts", "export async function GET() {\n  return await prisma.account.findMany({ where: { nickname: \"y\" } })\n}\n")
	return root
}

func columnsOf(t *testing.T, root string, scp scope.Scope) []string {
	t.Helper()
	var p providers.LanguageProvider = typescript.New()
	ex, ok := p.(providers.QueryExtractor)
	if !ok {
		t.Fatal("the typescript provider must implement QueryExtractor")
	}
	var cols []string
	for _, f := range collectQueryFilters(root, p.FileExtensions(), ex, scp) {
		cols = append(cols, f.Columns...)
	}
	sort.Strings(cols)
	return cols
}

// Walker B (the code x schema cross walk) consults the same layer-0 scope the
// security walk does: a narrowed pass extracts query filters only from the files
// in scope.
func TestCollectQueryFilters_NarrowedScope_OnlyScopedFiles(t *testing.T) {
	root := twoQueryProject(t)

	got := columnsOf(t, root, scope.Of([]string{"src/touched.ts"}))

	if want := []string{"status"}; !reflect.DeepEqual(got, want) {
		t.Errorf("filters from %v, want only %v — the cross walk opened out-of-scope files", got, want)
	}
}

// The same fail-safe as the security walk: an unset scope collects everything.
func TestCollectQueryFilters_UnsetScope_CollectsEverything(t *testing.T) {
	root := twoQueryProject(t)
	var unset scope.Scope

	got := columnsOf(t, root, unset)

	if want := []string{"nickname", "status"}; !reflect.DeepEqual(got, want) {
		t.Errorf("unset scope collected %v, want every filter %v", got, want)
	}
}

func TestCollectQueryFilters_FullScope_CollectsEverything(t *testing.T) {
	root := twoQueryProject(t)

	got := columnsOf(t, root, scope.Full())

	if want := []string{"nickname", "status"}; !reflect.DeepEqual(got, want) {
		t.Errorf("full scope collected %v, want every filter %v", got, want)
	}
}

// R4: the DB dimension's inputs are the CONFIGURED schema paths, not a repo
// walk. Whether it runs at all under a partial scope is therefore a question
// about those paths — answered here, so a scope holding none of them leaves the
// dimension unmeasured rather than scoring it 100 on a schema nobody touched.
func TestDBInputsInScope(t *testing.T) {
	paths := []string{"prisma/schema.prisma", "db/migrations"}
	cases := []struct {
		name  string
		scope scope.Scope
		want  bool
	}{
		{"full scope always includes the db inputs", scope.Full(), true},
		{"unset scope is not a narrowing", scope.Scope{}, true},
		{"the configured schema file itself", scope.Of([]string{"prisma/schema.prisma"}), true},
		{"the schema file spelled with a Windows separator", scope.Of([]string{`prisma\schema.prisma`}), true},
		{"a file INSIDE a configured schema directory", scope.Of([]string{"db/migrations/V2__add_index.sql"}), true},
		{"a configured schema directory named directly", scope.Of([]string{"db/migrations"}), true},
		{"only code files", scope.Of([]string{"src/handler.ts"}), false},
		{"a path that merely shares a prefix", scope.Of([]string{"db/migrations-old/V1__x.sql"}), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := dbInputsInScope(paths, c.scope); got != c.want {
				t.Errorf("dbInputsInScope = %v, want %v", got, c.want)
			}
		})
	}
}
