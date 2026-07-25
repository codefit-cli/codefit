package dwrules_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dwrules"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
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
// merge loop and Run's All()-delegation, since dwrules.All() is itself empty
// in S1 (no real rule exists yet to exercise these paths otherwise).
type fakeRule struct{ id string }

func (r fakeRule) ID() string { return r.id }

func (fakeRule) Check(*db.Schema, *paradigm.Classification) ([]findings.Finding, []findings.SurfaceItem) {
	return []findings.Finding{{ID: "FAKE-1"}}, []findings.SurfaceItem{{Category: "fake"}}
}

// TestRunWith_MergesRuleOutput exercises RunWith's merge loop over a
// non-empty rule set — dwrules.All() is empty in S1, so this is the only way
// to prove the merge mechanism actually appends a rule's output.
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
