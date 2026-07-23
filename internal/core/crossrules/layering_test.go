package crossrules_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestCoreCrossrules_NeverImportProviderOrSensor is a CONTRACT test, not a
// convention (ADR 0029): internal/core/query and internal/core/crossrules must
// NEVER import anything under internal/providers OR internal/sensors. The whole
// point of the code↔schema cross is that BOTH its inputs are neutral — db.Schema
// (from a SchemaParser) and query.QueryFilter (from a QueryExtractor) — so the
// core reasons over them without a core→provider edge.
//
// internal/sensors is forbidden too, not just providers: the DB-010 stamping fix
// exposed dbsensor.StampSurface / Result.SchemaContent, and the stamping is called
// from the WIRING (internal/mcp), never the core (ADR 0029/0030). Were a future
// contributor to reach for the stamping (or any sensor) from a cross rule, the
// core would depend on a sensor — a layering inversion the old provider-only check
// would NOT have caught. This locks the invariant "the core knows neither
// providers nor sensors". Cross-family analogue of
// dbrules.TestCoreDB_DBRules_NeverImportTypeScriptProvider (which stays untouched).
func TestCoreCrossrules_NeverImportProviderOrSensor(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to `go list`; skipped in -short")
	}
	forbidden := []string{"internal/providers", "internal/sensors"}
	for _, pkg := range []string{
		"github.com/codefit-cli/codefit/internal/core/query",
		"github.com/codefit-cli/codefit/internal/core/crossrules",
	} {
		out, err := exec.Command("go", "list", "-deps", pkg).CombinedOutput()
		if err != nil {
			t.Fatalf("go list -deps %s: %v\n%s", pkg, err, out)
		}
		for _, bad := range forbidden {
			if strings.Contains(string(out), bad) {
				t.Errorf("%s must never import %s (core→infra layering violation), deps:\n%s", pkg, bad, out)
			}
		}
	}
}
