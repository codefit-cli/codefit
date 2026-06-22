package typescript_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// TestStress is the safety-cap exit criterion (ADR 0002 caveat 2): the pure-Go
// runtime has iteration/stack/node caps that could yield HasError on large or
// deeply nested files. Each fixture is REAL or representative project code, not
// a toy. If a fixture of TYPICAL project code parses with errors, the runtime's
// caps bite real code — STOP and reconsider (plan B: WASM). See testdata/README.
func TestStress(t *testing.T) {
	cases := []struct {
		file string
		why  string
	}{
		{"real_vscode_strings.ts", "large real .ts (size/iteration cap) — microsoft/vscode"},
		{"real_excalidraw_actions.tsx", "large real React component (size cap + JSX/hooks) — excalidraw"},
		{"deeply_nested.tsx", "~80 levels of nested ternary/JSX (stack-depth cap)"},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join("testdata", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			root, err := typescript.New().Parse(providers.SourceFile{Path: tc.file, Content: content})
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if root.HasError() {
				t.Fatalf("EXIT CRITERION HIT: %s parsed WITH errors — the runtime caps bite typical project code (%s)", tc.file, tc.why)
			}
		})
	}
}
