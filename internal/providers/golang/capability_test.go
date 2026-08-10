package golang_test

import (
	"testing"

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
