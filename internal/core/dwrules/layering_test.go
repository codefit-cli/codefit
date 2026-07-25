package dwrules_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestCoreDWRules_NeverImportProviderOrSensor is a CONTRACT test, not a
// convention (cloned from crossrules/layering_test.go, ADR 0029): both
// internal/core/dwrules and internal/core/paradigm must NEVER import
// anything under internal/providers OR internal/sensors. Re-asserting
// paradigm here is belt-and-suspenders (design §2b wording: "core/paradigm +
// core/dwrules must never reach internal/providers or internal/sensors") —
// paradigm already has its own standalone contract test in its own package.
func TestCoreDWRules_NeverImportProviderOrSensor(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to `go list`; skipped in -short")
	}
	forbidden := []string{"internal/providers", "internal/sensors"}
	for _, pkg := range []string{
		"github.com/codefit-cli/codefit/internal/core/paradigm",
		"github.com/codefit-cli/codefit/internal/core/dwrules",
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
