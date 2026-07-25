package paradigm_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestCoreParadigm_NeverImportProviderOrSensor is a CONTRACT test, not a
// convention (mirrors crossrules/layering_test.go, ADR 0029/0033):
// internal/core/paradigm must NEVER import anything under internal/providers
// OR internal/sensors. Its whole point is that the paradigm/role fact is a
// pure function of db.Schema, so the core reasons over it without a
// core→provider or core→sensor edge.
func TestCoreParadigm_NeverImportProviderOrSensor(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to `go list`; skipped in -short")
	}
	forbidden := []string{"internal/providers", "internal/sensors"}
	pkg := "github.com/codefit-cli/codefit/internal/core/paradigm"
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
