package providers_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/golang"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// TestCapability_SurfaceMustBeSubsetOfProviderCategories is C2: every category
// a Capability declares in Surface must be a member of
// surface.ProviderCategories — the vocabulary D1b just locked to the const
// block. This is what makes a provider's "N of 4" claim DERIVED
// (len(surface.ProviderCategories) is the denominator), never a literal a
// provider could drift from.
func TestCapability_SurfaceMustBeSubsetOfProviderCategories(t *testing.T) {
	valid := providers.Capability{
		Security:  providers.RuleSet{Declared: []string{"SEC-001"}, Enumerable: false},
		Practices: providers.RuleSet{Declared: []string{"PRAC-001"}, Enumerable: false},
		Surface:   []surface.Category{surface.CategoryAuthz},
	}
	if !valid.ValidSurface() {
		t.Error("a Capability whose Surface is a real subset of ProviderCategories must be ValidSurface() == true")
	}

	invalid := providers.Capability{Surface: []surface.Category{surface.Category("not-a-real-category")}}
	if invalid.ValidSurface() {
		t.Error("a Capability whose Surface contains a category outside ProviderCategories must be ValidSurface() == false (C2)")
	}
}

// isZeroCapability reports whether c carries no declaration at all — C1's
// completeness check. A zero Capability means a provider forgot to implement
// Capability() beyond the interface's compile-time requirement (a bare
// `return providers.Capability{}` still satisfies the interface).
func isZeroCapability(c providers.Capability) bool {
	return len(c.Security.Declared) == 0 && len(c.Practices.Declared) == 0 &&
		len(c.Surface) == 0 && !c.CoverageManifest
}

// TestCapability_EveryRegisteredProviderDeclaresNonZero is C1, driven through
// the interface: every REAL registered provider (constructed the same way
// production does, never a hand-built struct) must declare a non-zero
// Capability, and that Capability's Surface must satisfy C2.
func TestCapability_EveryRegisteredProviderDeclaresNonZero(t *testing.T) {
	registered := []providers.LanguageProvider{golang.New(), typescript.New()}
	for _, p := range registered {
		t.Run(p.Language(), func(t *testing.T) {
			cap := p.Capability()
			if isZeroCapability(cap) {
				t.Errorf("%s.Capability() is the zero value — every registered provider must declare a non-zero Capability (C1)", p.Language())
			}
			if !cap.ValidSurface() {
				t.Errorf("%s.Capability().Surface is not a subset of surface.ProviderCategories (C2)", p.Language())
			}
			if !cap.Security.ValidExclusions() {
				t.Errorf("%s.Capability().Security has a rule id in both Declared and Excluded (C6)", p.Language())
			}
			if !cap.Practices.ValidExclusions() {
				t.Errorf("%s.Capability().Practices has a rule id in both Declared and Excluded (C6)", p.Language())
			}
			if ok, phantom := cap.Security.ValidExclusionSource(); !ok {
				t.Errorf("%s.Capability().Security has excluded ids that don't match the real rule-id shape (C7): %v", p.Language(), phantom)
			}
			if ok, phantom := cap.Practices.ValidExclusionSource(); !ok {
				t.Errorf("%s.Capability().Practices has excluded ids that don't match the real rule-id shape (C7): %v", p.Language(), phantom)
			}
		})
	}
}

// TestRuleSet_ExcludedCannotAlsoBeDeclared is C6: a rule id in Excluded must
// never also appear in Declared — Declared says a rule IS covered, Excluded
// says it permanently is NOT, and a rule id claiming both is a self-defeating
// declaration, not a real fact about the provider.
func TestRuleSet_ExcludedCannotAlsoBeDeclared(t *testing.T) {
	disjoint := providers.RuleSet{
		Declared: []string{"PRAC-001", "PRAC-002"},
		Excluded: []providers.ExcludedRule{{ID: "PRAC-004", Reason: "dropped, ADR 0056"}},
	}
	if !disjoint.ValidExclusions() {
		t.Error("a RuleSet whose Excluded ids are disjoint from Declared must be ValidExclusions() == true")
	}

	overlapping := providers.RuleSet{
		Declared: []string{"PRAC-004"},
		Excluded: []providers.ExcludedRule{{ID: "PRAC-004", Reason: "dropped, ADR 0056"}},
	}
	if overlapping.ValidExclusions() {
		t.Error("a RuleSet whose Excluded id also appears in Declared must be ValidExclusions() == false (C6)")
	}
}

// TestRuleSet_ValidExclusionSource_Enumerable_CatchesPhantomID is C7 — the
// phantom-exclusion gap sdd-verify found by mutation: renaming Go's real
// PRAC-004 exclusion to "PRAC-999-NEVER-EXISTED" left every existing test
// green, because ValidExclusions/C6 only checks Declared/Excluded
// disjointness, never whether an excluded id ever corresponded to a real
// rule. This fixture builds an Enumerable:true RuleSet (Go's real Practices
// is Enumerable:false; this is a fixture made enumerable ONLY for this test,
// per the mutation sdd-verify ran) whose Declared ids share the exact
// "PRAC-<3 digits>" shape Control A proves accurate for a real Enumerable
// family. ValidExclusionSource must reject an excluded id that does not
// match that shape (the exact fabricated-marker defect) and accept one that
// does.
func TestRuleSet_ValidExclusionSource_Enumerable_CatchesPhantomID(t *testing.T) {
	base := providers.RuleSet{
		Declared:   []string{"PRAC-001", "PRAC-002", "PRAC-003", "PRAC-005"},
		Enumerable: true, // fixture-only — see doc comment above
	}

	phantom := base
	phantom.Excluded = []providers.ExcludedRule{{ID: "PRAC-999-NEVER-EXISTED", Reason: "fabricated for test"}}
	ok, bad := phantom.ValidExclusionSource()
	if ok {
		t.Fatal("an Enumerable RuleSet must reject an Excluded id that does not match the family's real rule-id shape (C7)")
	}
	if len(bad) != 1 || bad[0] != "PRAC-999-NEVER-EXISTED" {
		t.Fatalf("phantom = %v, want exactly [PRAC-999-NEVER-EXISTED]", bad)
	}

	real := base
	real.Excluded = []providers.ExcludedRule{{ID: "PRAC-004", Reason: "dropped, ADR 0056"}}
	ok, bad = real.ValidExclusionSource()
	if !ok || len(bad) != 0 {
		t.Errorf("an Excluded id matching the family's real rule-id shape must pass C7, got ok=%v bad=%v", ok, bad)
	}
}

// TestRuleSet_ValidExclusionSource_NonEnumerable_NotApplicable proves the
// non-enumerable path is DOCUMENTED, not silently skipped: fed the exact same
// fabricated id as the Enumerable test above, but with Enumerable:false (Go's
// real shape — no All()/ID() loader exists to ground Declared's own shape
// against, so even a shape-only claim would over-promise), the same phantom
// id is explicitly NOT caught. This is the honest declared gap (ADR 00NN,
// mirrors dbcoverage's Control C): the check does not silently pass by
// accident, it deliberately declines to make a claim it cannot back.
func TestRuleSet_ValidExclusionSource_NonEnumerable_NotApplicable(t *testing.T) {
	nonEnumerable := providers.RuleSet{
		Declared:   []string{"PRAC-001", "PRAC-002", "PRAC-003", "PRAC-005"},
		Enumerable: false,
		Excluded:   []providers.ExcludedRule{{ID: "PRAC-999-NEVER-EXISTED", Reason: "fabricated for test"}},
	}
	ok, bad := nonEnumerable.ValidExclusionSource()
	if !ok || len(bad) != 0 {
		t.Errorf("Enumerable:false must return (true, nil) — 'not applicable', no claim made — got ok=%v bad=%v", ok, bad)
	}
}

// TestRuleSet_ValidLimits is the INVERSE of ValidExclusions, and the inversion
// is the whole point. An excluded id must NOT be in Declared: it names a rule
// that will never exist. A limit id MUST be in Declared: it qualifies a rule
// that DOES exist and is claimed as covered. A limit attached to an id the
// provider does not implement is a caveat about nothing, which reads to an
// agent as a caveat about something.
func TestRuleSet_ValidLimits(t *testing.T) {
	grounded := providers.RuleSet{
		Declared: []string{"SEC-001", "SEC-010"},
		Limits:   []providers.RuleLimit{{ID: "SEC-001", Limit: "matches by name component, not substring"}},
	}
	if ok, orphan := grounded.ValidLimits(); !ok {
		t.Errorf("a limit on a Declared id must be valid, got orphans %v", orphan)
	}

	floating := providers.RuleSet{
		Declared: []string{"SEC-001", "SEC-010"},
		Limits:   []providers.RuleLimit{{ID: "SEC-999", Limit: "a caveat about a rule that is not covered"}},
	}
	ok, orphan := floating.ValidLimits()
	if ok {
		t.Error("a limit whose id is not Declared must be invalid — it qualifies a claim nobody made")
	}
	if len(orphan) != 1 || orphan[0] != "SEC-999" {
		t.Errorf("ValidLimits should name the orphan, got %v", orphan)
	}

	// An empty Limits list is valid: most rules have no declared limit.
	if ok, _ := (providers.RuleSet{Declared: []string{"SEC-001"}}.ValidLimits()); !ok {
		t.Error("a RuleSet with no limits must be valid")
	}
}

// TestEveryRegisteredProviderHasGroundedLimits runs the same check over the
// REAL providers, constructed the way production does, so a provider cannot
// ship a limit attached to a rule it does not declare.
func TestEveryRegisteredProviderHasGroundedLimits(t *testing.T) {
	registered := []providers.LanguageProvider{golang.New(), typescript.New()}
	var totalLimits int
	for _, p := range registered {
		cap := p.Capability()
		totalLimits += len(cap.Security.Limits) + len(cap.Practices.Limits)
		if ok, orphan := cap.Security.ValidLimits(); !ok {
			t.Errorf("%s: security limits reference undeclared rule ids %v", p.Language(), orphan)
		}
		if ok, orphan := cap.Practices.ValidLimits(); !ok {
			t.Errorf("%s: practices limits reference undeclared rule ids %v", p.Language(), orphan)
		}
	}
	// Vacuum guard: with no limits declared anywhere, the loop above asserts
	// nothing and would stay green after the mechanism was deleted.
	if totalLimits == 0 {
		t.Fatal("vacuum: no provider declares any limit, so the grounding check verified nothing")
	}
}
