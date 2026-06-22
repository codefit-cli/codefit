package typescript_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/syntax"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

func parseFile(t *testing.T, name string) syntax.Node {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	root, err := typescript.New().Parse(providers.SourceFile{Path: name, Content: content})
	if err != nil {
		t.Fatalf("Parse(%s): %v", name, err)
	}
	return root
}

// find returns the first node of the given type in the tree (DFS over named
// children), or nil.
func find(n syntax.Node, typ string) syntax.Node {
	if n.Type() == typ {
		return n
	}
	for i := 0; i < n.NamedChildCount(); i++ {
		if got := find(n.NamedChild(i), typ); got != nil {
			return got
		}
	}
	return nil
}

func TestIdentity(t *testing.T) {
	p := typescript.New()
	if p.Language() != "typescript" {
		t.Errorf("Language = %q", p.Language())
	}
	exts := p.FileExtensions()
	if len(exts) != 2 || exts[0] != ".ts" || exts[1] != ".tsx" {
		t.Errorf("FileExtensions = %v, want [.ts .tsx]", exts)
	}
	if len(p.Frameworks()) == 0 {
		t.Error("Frameworks should not be empty")
	}
	pc := p.DefaultPathCriticality()
	if len(pc.Production) == 0 || len(pc.Test) == 0 || len(pc.Example) == 0 {
		t.Errorf("DefaultPathCriticality incomplete: %+v", pc)
	}
}

func TestParseTypeScriptNavigable(t *testing.T) {
	root := parseFile(t, "example.ts")
	if root.HasError() {
		t.Fatal("example.ts parsed with errors")
	}
	fn := find(root, "function_declaration")
	if fn == nil {
		t.Fatal("no function_declaration found")
	}
	// Navigate by field name — what the rule engine will rely on.
	name := fn.ChildByField("name")
	if name == nil || string(name.Text()) != "loadUser" {
		t.Errorf("function name node = %v", name)
	}
	params := fn.ChildByField("parameters")
	if params == nil || params.NamedChildCount() != 2 {
		t.Errorf("expected 2 parameters, got %v", params)
	}
}

func TestParseTSXNavigable(t *testing.T) {
	root := parseFile(t, "example.tsx")
	if root.HasError() {
		t.Fatal("example.tsx parsed with errors")
	}
	if find(root, "jsx_element") == nil {
		t.Error("no jsx_element found in TSX")
	}
	if find(root, "function_declaration") == nil {
		t.Error("no function_declaration found in TSX")
	}
}

func TestParseDetectsTSXByExtension(t *testing.T) {
	// JSX in a .tsx must parse; the same JSX in a .ts is not valid TS and the
	// provider must select the grammar by extension.
	tsx := []byte("export const C = () => <div>{1}</div>\n")
	root, err := typescript.New().Parse(providers.SourceFile{Path: "c.tsx", Content: tsx})
	if err != nil {
		t.Fatal(err)
	}
	if root.HasError() || find(root, "jsx_element") == nil {
		t.Error("TSX content in a .tsx file should parse with jsx")
	}
}

func TestParseStartLine(t *testing.T) {
	root := parseFile(t, "example.ts")
	fn := find(root, "function_declaration")
	if fn == nil {
		t.Fatal("no function")
	}
	// loadUser is declared on line 3 of example.ts.
	if fn.StartLine() != 3 {
		t.Errorf("function StartLine = %d, want 3", fn.StartLine())
	}
}
