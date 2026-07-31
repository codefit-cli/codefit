package dwrules_test

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dwrules"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// TestRunWith_SeamGate proves the merge mechanism BEFORE any real DW rule
// exists (crossrules.go's own precedent, see crossrules.RunWith), so the
// first S2 rule lands without touching this seam: an empty rule set over a
// real schema returns (nil, nil).
func TestRunWith_SeamGate(t *testing.T) {
	schema := &db.Schema{
		Tables: []db.Table{{Name: "fact_sales"}},
	}
	cls := &paradigm.Classification{
		Paradigm: paradigm.ParadigmOLAP,
		Roles:    map[string]paradigm.Role{"fact_sales": paradigm.RoleFact},
	}

	fs, surf := dwrules.RunWith(schema, cls, nil)

	if fs != nil {
		t.Errorf("Findings = %v, want nil", fs)
	}
	if surf != nil {
		t.Errorf("Surface = %v, want nil", surf)
	}
}

// TestRunWith_NilSchema mirrors dbrules.Run/crossrules.RunWith's guard: a nil
// schema yields nothing regardless of the rule set.
func TestRunWith_NilSchema(t *testing.T) {
	fs, surf := dwrules.RunWith(nil, nil, dwrules.All())
	if fs != nil || surf != nil {
		t.Errorf("RunWith(nil, ...) = (%v, %v), want (nil, nil)", fs, surf)
	}
}

// fakeRule is a minimal Rule implementation used only to exercise RunWith's
// merge loop and Run's All()-delegation in isolation, independent of any real
// rule's behavior (dwrules.All() now holds the S2 family — see
// TestAll_RegistersTheS2Family below).
type fakeRule struct{ id string }

func (r fakeRule) ID() string { return r.id }

func (fakeRule) Check(*db.Schema, *paradigm.Classification) ([]findings.Finding, []findings.SurfaceItem) {
	return []findings.Finding{{ID: "FAKE-1"}}, []findings.SurfaceItem{{Category: "fake"}}
}

// TestRunWith_MergesRuleOutput exercises RunWith's merge loop over an
// injected rule set, isolated from dwrules.All() so it proves the merge
// mechanism itself appends a rule's output, independent of any real rule.
func TestRunWith_MergesRuleOutput(t *testing.T) {
	schema := &db.Schema{Tables: []db.Table{{Name: "t"}}}
	cls := &paradigm.Classification{}

	fs, surf := dwrules.RunWith(schema, cls, []dwrules.Rule{fakeRule{id: "FAKE-1"}})

	if len(fs) != 1 || fs[0].ID != "FAKE-1" {
		t.Errorf("Findings = %v, want one FAKE-1 finding", fs)
	}
	if len(surf) != 1 || surf[0].Category != "fake" {
		t.Errorf("Surface = %v, want one fake-category item", surf)
	}
}

// TestRunWith_NilClassification mirrors TestRunWith_NilSchema: a nil
// classification yields nothing regardless of the rule set — even a
// non-empty one that would otherwise run and return output (SUGGESTION,
// resilience, S1 review ledger). Unreachable via the production sensor
// today (it always builds a real Classification), but forward-safety for
// S2, where every real rule dereferences cls.
func TestRunWith_NilClassification(t *testing.T) {
	schema := &db.Schema{Tables: []db.Table{{Name: "t"}}}
	fs, surf := dwrules.RunWith(schema, nil, []dwrules.Rule{fakeRule{id: "FAKE-1"}})
	if fs != nil || surf != nil {
		t.Errorf("RunWith(schema, nil, ...) = (%v, %v), want (nil, nil)", fs, surf)
	}
}

// TestRun_DelegatesToRunWithAll locks that Run calls RunWith(s, cls, All()).
// The schema is a single table with NO recognized role, so every registered DW
// rule abstains on it and Run returns (nil, nil) — the assertion stays valid as
// the rule set grows, and doubles as the spec's "rules do not fire on OLTP or
// unclassified tables" guarantee at the runner level.
func TestRun_DelegatesToRunWithAll(t *testing.T) {
	schema := &db.Schema{Tables: []db.Table{{Name: "t"}}}
	cls := &paradigm.Classification{Roles: map[string]paradigm.Role{"t": paradigm.RoleUnclassified}}

	fs, surf := dwrules.Run(schema, cls)

	if fs != nil || surf != nil {
		t.Errorf("Run(...) = (%v, %v), want (nil, nil) — no DW rule may touch an unclassified table", fs, surf)
	}
}

// TestAll_RuleIDsAreUniqueAndNonEmpty replaces S1's TestAll_EmptyInS1, whose
// premise ("the skeleton holds no rule") broke the moment the first real DW
// rule landed in S2. It is written to stay valid as the family grows: every
// registered rule must carry a non-empty, DW-prefixed, UNIQUE ID — a
// duplicate ID would silently collapse two rules' baseline identity.
func TestAll_RuleIDsAreUniqueAndNonEmpty(t *testing.T) {
	all := dwrules.All()
	if len(all) == 0 {
		t.Fatal("All() is empty — S2 registers the star-schema/SCD rule family")
	}
	seen := map[string]bool{}
	for _, r := range all {
		id := r.ID()
		if !strings.HasPrefix(id, "DW-") {
			t.Errorf("rule ID %q must carry the DW- prefix", id)
		}
		if seen[id] {
			t.Errorf("duplicate rule ID %q — two rules would share one baseline identity", id)
		}
		seen[id] = true
	}
}

// TestAll_RegistersTheS2AndS3Family locks the EXACT S2+S3 rule set (renamed
// from TestAll_RegistersTheS2Family when DW-021 landed). DW-020
// (partitioning, S4) is deliberately ABSENT and must stay declared-not-covered
// in dbcoverage.go/COVERAGE.md until it is actually built — a rule appearing
// here before its coverage prose, or coverage prose before the rule, is the
// exact drift this project's honesty doctrine exists to prevent.
func TestAll_RegistersTheS2AndS3Family(t *testing.T) {
	want := []string{"DW-001", "DW-002", "DW-005", "DW-010", "DW-011", "DW-021"}
	got := make([]string, 0, len(dwrules.All()))
	for _, r := range dwrules.All() {
		got = append(got, r.ID())
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("All() rule IDs = %v, want %v", got, want)
	}
}

// TestOwnedCategories_CoversEveryS2AndS3Rule replaces S1's
// TestOwnedCategories_EmptyInS1 (renamed from TestOwnedCategories_
// CoversEveryS2Rule when DW-021 landed). Every DW rule emits exactly one
// surface category, and every one of those categories MUST be declared
// owned — an undeclared category can never be baselined or pruned (ADR
// 0019), which is the single way to corrupt an existing baseline.
func TestOwnedCategories_CoversEveryS2AndS3Rule(t *testing.T) {
	want := []string{
		string(surface.CategoryDWNoFactDimensionFK),
		string(surface.CategoryDWDimensionNoSurrogateKey),
		string(surface.CategoryDWNoTimeDimension),
		string(surface.CategoryDWSCD2NoCurrencyIndex),
		string(surface.CategoryDWMixedSCDStrategies),
		string(surface.CategoryDWNoColumnarIndex),
	}
	got := dwrules.OwnedCategories()
	if len(got) != len(dwrules.All()) {
		t.Errorf("OwnedCategories() has %d entries but All() has %d rules — one category per rule",
			len(got), len(dwrules.All()))
	}
	sorted := append([]string(nil), got...)
	sort.Strings(sorted)
	sort.Strings(want)
	if !reflect.DeepEqual(sorted, want) {
		t.Errorf("OwnedCategories() = %v, want %v", sorted, want)
	}
}
