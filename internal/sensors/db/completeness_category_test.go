package db_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
	sdb "github.com/codefit-cli/codefit/internal/sensors/db"
)

// db-model-completeness-contract, design SS5 (mandatory, easy-to-miss step):
// an emitted-but-undeclared surface category can never be baselined or
// pruned (ADR 0019) — extending OwnedCategories' count-lock pattern here,
// same rationale as the DB-020/prefix-redundant-index/DW locks above it in
// this file.
func TestSensorDB_OwnedCategories_IncludesTableStructureUnproven(t *testing.T) {
	s := sdb.New(typescript.New())
	want := string(surface.CategoryDBTableStructureUnproven)
	for _, c := range s.OwnedCategories() {
		if c == want {
			return
		}
	}
	t.Errorf("OwnedCategories() missing %q (have %v)", want, s.OwnedCategories())
}
