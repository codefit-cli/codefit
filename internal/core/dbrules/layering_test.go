package dbrules_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/dbrules"
)

// TestCoreDB_DBRules_NeverImportTypeScriptProvider is a CONTRACT test, not a
// convention: internal/core/db and internal/core/dbrules must never import
// internal/providers/typescript. N+1 detection logic (a TS-AST fact) belongs
// entirely in the TypeScript provider (Unit F2) — it does NOT go in dbrules/
// core/db, which stay schema-only and inherited by every future provider
// (ADR 0014/0015). A future contributor adding "N+1 as a dbrules rule over
// db.Schema" would violate this and this test catches it.
func TestCoreDB_DBRules_NeverImportTypeScriptProvider(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to `go list`; skipped in -short")
	}
	for _, pkg := range []string{
		"github.com/codefit-cli/codefit/internal/core/db",
		"github.com/codefit-cli/codefit/internal/core/dbrules",
	} {
		out, err := exec.Command("go", "list", "-deps", pkg).CombinedOutput()
		if err != nil {
			t.Fatalf("go list -deps %s: %v\n%s", pkg, err, out)
		}
		if strings.Contains(string(out), "internal/providers/typescript") {
			t.Errorf("%s must never import internal/providers/typescript (layering violation), deps:\n%s", pkg, out)
		}
	}
}

// TestDbrulesAll_GainsNoNPlus1Entry locks that N+1 is never added to
// dbrules.All() — N+1 is code-derived (a TS AST fact about a loop), and
// dbrules.Rule.Check(*db.Schema) sees only the schema, never code. It must stay
// a surface.Query in the TypeScript provider (Unit F2), forever.
func TestDbrulesAll_GainsNoNPlus1Entry(t *testing.T) {
	for _, r := range dbrules.All() {
		id := strings.ToLower(r.ID())
		if strings.Contains(id, "nplus1") || r.ID() == "DB-201" {
			t.Errorf("dbrules.All() must never contain an N+1 rule (found %s) — N+1 is code-derived, it belongs in the TS provider only", r.ID())
		}
	}
}
