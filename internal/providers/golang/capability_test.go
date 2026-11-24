package golang_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/namematch"
	"github.com/codefit-cli/codefit/internal/core/valueshape"
	"github.com/codefit-cli/codefit/internal/providers/golang"
)

// TestCapability_PRAC004DeclaredPermanentlyExcluded is P1-4b's owed manifest
// entry: PRAC-004 was permanently dropped (ADR 0056 — it fired on every `go`
// statement with no synchronization detection at all) and must stay stated in
// Go's capability declaration, with its reason, rather than silently vanish
// if Practices.Declared is ever edited by someone who never reads the ADR.
func TestCapability_PRAC004DeclaredPermanentlyExcluded(t *testing.T) {
	cap := golang.New().Capability()

	var reason string
	var found bool
	for _, ex := range cap.Practices.Excluded {
		if ex.ID == "PRAC-004" {
			found = true
			reason = ex.Reason
		}
	}
	if !found {
		t.Fatal("golang.Provider.Capability().Practices.Excluded does not name PRAC-004 — " +
			"its permanent drop (ADR 0056) must be declared, not silently omitted (P1-4b)")
	}
	if reason == "" {
		t.Error("PRAC-004's ExcludedRule must carry a non-empty Reason")
	}
	if !cap.Practices.ValidExclusions() {
		t.Error("Practices.Excluded overlaps Practices.Declared — PRAC-004 must not be claimed as both covered and excluded (C6)")
	}
}

// TestCapability_SEC001DeclaresTheValueVeto keeps the value veto (ADR 0086) a
// DECLARED limit of SEC-001 rather than a silent one. A literal the veto
// silences is, from the agent's side, indistinguishable from "no credential
// here" unless the capability says the rule skips those shapes.
//
// The declaration must name every form internal/core/valueshape can veto: a
// form added there and not declared here would be exactly the undeclared
// blindness the census doctrine exists to prevent. It must also keep the name
// gate's own limit (ADR 0075) — the two qualify the same rule and neither
// replaces the other.
func TestCapability_SEC001DeclaresTheValueVeto(t *testing.T) {
	sec := golang.New().Capability().Security
	if ok, orphan := sec.ValidLimits(); !ok {
		t.Fatalf("security limits reference undeclared rule ids %v", orphan)
	}

	var limits []string
	for _, l := range sec.Limits {
		if l.ID == "SEC-001" {
			limits = append(limits, l.Limit)
		}
	}
	if len(limits) != 2 {
		t.Fatalf("SEC-001 carries %d declared limits, want 2 (name gate + value veto): %q", len(limits), limits)
	}

	var veto string
	for _, l := range limits {
		if strings.Contains(l, "internal/core/valueshape") {
			veto = l
		}
	}
	if veto == "" {
		t.Fatalf("no SEC-001 limit declares the value veto: %q", limits)
	}
	// Each form must be named AS ITSELF, in the limit's own form list. A plain
	// substring check is satisfied by prose that merely contains the letters:
	// "urn" by "a urn: URN", "url" by "a URL", so a form dropped from the list
	// would still pass. The list is parsed and compared as a set.
	declared := declaredForms(t, veto)
	for _, form := range valueshape.Forms() {
		if !declared[string(form)] {
			t.Errorf("the value-veto limit's form list does not name %q, which the provider silences: %v", form, declared)
		}
		delete(declared, string(form))
	}
	for extra := range declared {
		t.Errorf("the value-veto limit's form list names %q, which valueshape does not declare", extra)
	}
	if !slices.Contains(limits, namematch.LimitLowercaseConcatenation) {
		t.Error("declaring the value veto dropped SEC-001's name-gate limit (ADR 0075)")
	}
}

// declaredFormList is the shape the value-veto limit uses to enumerate its
// forms: "forms a, b, c" — lowercase names joined by ", ".
var declaredFormList = regexp.MustCompile(`forms ([a-z-]+(?:, [a-z-]+)*)`)

// declaredForms parses the limit's form list into a set. A limit with no such
// list, or with more than one, fails: the comparison would otherwise be made
// against nothing, or against a list chosen at random.
func declaredForms(t *testing.T, limit string) map[string]bool {
	t.Helper()
	lists := declaredFormList.FindAllStringSubmatch(limit, -1)
	if len(lists) != 1 {
		t.Fatalf("the value-veto limit carries %d form lists (\"forms a, b, …\"), want exactly 1: %q", len(lists), limit)
	}
	set := make(map[string]bool)
	for _, name := range strings.Split(lists[0][1], ", ") {
		set[name] = true
	}
	return set
}
