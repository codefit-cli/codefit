package db_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
	sdb "github.com/codefit-cli/codefit/internal/sensors/db"
)

// db-model-completeness-contract, dbcoverage enforcement Control D (design
// SS9). A full mechanical len(OwnedCategories())==len(dbrules.All()) lock is
// NOT achievable here (unlike crossrules/dwrules, where it's 1 category per
// rule): dbrules is not 1:1 — DB-050/DB-011/DB-011prefix do not map cleanly
// to exactly one owned category each, and "db" (the dimension name) is an
// extra entry. This test is therefore a HAND-MAINTAINED correspondence
// check, same convention as the pre-existing TestSensorDB_OwnedCategories_
// IncludesDB020/IncludesPrefixRedundantIndex/IncludesEveryDWCategory tests
// in db_test.go — consolidated here into ONE list covering every category
// any dbrules rule is known to emit, so a future addition has one place to
// extend rather than a new one-off test per category (ADR 0019: an
// emitted-but-undeclared category can never be baselined or pruned).
func TestSensorDB_OwnedCategories_IncludesEveryDBRulesCategory(t *testing.T) {
	owned := map[string]bool{}
	for _, c := range sdb.New(typescript.New()).OwnedCategories() {
		owned[c] = true
	}
	for _, want := range []surface.Category{
		surface.CategoryDBFKNoIndex,
		surface.CategoryDBDupIndex,
		surface.CategoryDBMultivalued,
		surface.CategoryDBFKTextType,
		surface.CategoryDBNoTimestamps,
		surface.CategoryDBSensitiveUnencrypted,
		surface.CategoryDBRepeatingGroups,
		surface.CategoryDBViewSensitiveColumn,
		surface.CategoryDBPrefixRedundantIndex,
		surface.CategoryDBTableStructureUnproven,
		// The routine-body rule family (DB-030/031/040/041, 0.2.3) — verified
		// via grep to be the ONLY four dbrules-emitted surface categories with
		// no corresponding entry in OwnedCategories() before this commit.
		surface.CategoryDBRoutineNoExceptionHandling,
		surface.CategoryDBTriggerCrossTableCascade,
		surface.CategoryDBTriggerExternalCall,
		surface.CategoryDBDynamicSQLInRoutine,
	} {
		if !owned[string(want)] {
			t.Errorf("OwnedCategories() missing %q — emitted by a dbrules rule but not declared, "+
				"the one way to corrupt an existing baseline (ADR 0019)", want)
		}
	}
}
