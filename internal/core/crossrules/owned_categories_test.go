package crossrules_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/crossrules"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// db-model-completeness-contract, dbcoverage enforcement Control D (design
// SS9): extends the OwnedCategories count-lock pattern (dwrules_test.go:
// 148-172) to crossrules — an emitted-but-undeclared category can never be
// baselined or pruned (ADR 0019), the one way to corrupt an existing
// baseline. This IS fully mechanical here: each of the two cross rules
// (DB-010, DB-013) emits exactly one category, 1:1.
func TestCrossrules_OwnedCategories_CoversEveryRule(t *testing.T) {
	want := []string{
		string(surface.CategoryDBFilteredColumnNoIndex),
		string(surface.CategoryDBNoCompositeIndex),
	}
	got := crossrules.OwnedCategories()
	if len(got) != len(crossrules.All()) {
		t.Errorf("OwnedCategories() has %d entries but All() has %d rules — one category per rule",
			len(got), len(crossrules.All()))
	}
	sorted := append([]string(nil), got...)
	sort.Strings(sorted)
	sort.Strings(want)
	if !reflect.DeepEqual(sorted, want) {
		t.Errorf("OwnedCategories() = %v, want %v", sorted, want)
	}
}
