package dwrules_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dwrules"
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

// TestOwnedCategories_EmptyInS1 locks that dwrules produces NO new surface
// category yet — S2 adds DW-001/002/005/010/011 and their categories.
func TestOwnedCategories_EmptyInS1(t *testing.T) {
	if got := dwrules.OwnedCategories(); len(got) != 0 {
		t.Errorf("OwnedCategories() = %v, want empty in S1", got)
	}
}

// TestAll_EmptyInS1 locks that the S1 rule set is empty — the skeleton, not
// yet a real DW rule.
func TestAll_EmptyInS1(t *testing.T) {
	if got := dwrules.All(); len(got) != 0 {
		t.Errorf("All() = %v, want empty in S1", got)
	}
}
