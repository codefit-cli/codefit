package scaffold_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/scaffold"
)

// TestCapabilityStatement_TypeScriptStatesSecurityScanning is the exposed
// case: TypeScript is admitted by the security resolver, so its statement
// must name that codefit-scan-security/scan-all can audit its code.
func TestCapabilityStatement_TypeScriptStatesSecurityScanning(t *testing.T) {
	s := scaffold.CapabilityStatement("typescript")
	if !strings.Contains(strings.ToLower(s), "scan-security") && !strings.Contains(strings.ToLower(s), "scan-all") {
		t.Errorf("typescript's capability statement must name security scanning, got: %q", s)
	}
}

// TestCapabilityStatement_GoNamesItsPartialReach is R4
// (docs/specs/declared-partial-language-exposure.md): Go is now exposed for
// security scanning (roadmap P4-1), but exposure is not a parity claim — the
// statement must name the ACTUAL narrow reach (6 declared security rules, 1
// of 4 surface categories) rather than implying full coverage the way a bare
// "can audit go code" sentence would. This is the "boundary moved, not
// deleted" replacement for the old not-exposed statement Go used to get.
func TestCapabilityStatement_GoNamesItsPartialReach(t *testing.T) {
	s := scaffold.CapabilityStatement("go")
	low := strings.ToLower(s)
	if !strings.Contains(low, "scan-security") && !strings.Contains(low, "scan-all") {
		t.Errorf("go's capability statement must name security scanning is now available, got: %q", s)
	}
	if !strings.Contains(low, "6") {
		t.Errorf("go's capability statement must name its 6 declared security rules, got: %q", s)
	}
	if !strings.Contains(low, "1 of 4") {
		t.Errorf("go's capability statement must name its 1-of-4 surface category reach, got: %q", s)
	}
	for _, notMapped := range []string{"idor", "overfetch", "nplus1"} {
		if !strings.Contains(low, notMapped) {
			t.Errorf("go's capability statement must name the unmapped surface category %q, got: %q", notMapped, s)
		}
	}
}

// TestCapabilityStatement_DerivedFromRegistryExposure proves the statement is
// COMPUTED from the registry's Exposure/Capability, not a hardcoded
// per-language string: typescript and go, though both now exposed, declare
// DIFFERENT capabilities (9 rules/4 categories vs 6 rules/1 category), so
// their statements must differ. This drives the real registry.ByName lookup
// rather than re-implementing the decision.
func TestCapabilityStatement_DerivedFromRegistryExposure(t *testing.T) {
	tsStatement := scaffold.CapabilityStatement("typescript")
	goStatement := scaffold.CapabilityStatement("go")
	if tsStatement == goStatement {
		t.Fatal("typescript and go declare different capabilities and must get different capability statements")
	}
}

// TestCapabilityStatementForEntry_UnexposedNamesTheGap is R3's kept
// guarantee, exercised directly against the pure per-entry function (no
// currently-registered language is unexposed for security scanning, so this
// drives a hand-built Entry rather than a real registry lookup — the only
// way to keep this branch under test now that Go crossed the boundary): a
// FUTURE registered-but-unexposed language must still get the honest
// not-scanned statement, never silently upgraded to "can audit".
func TestCapabilityStatementForEntry_UnexposedNamesTheGap(t *testing.T) {
	s := scaffold.CapabilityStatementForExposure("cobol", false)
	low := strings.ToLower(s)
	if !strings.Contains(low, "does not resolve") && !strings.Contains(low, "not scanned") {
		t.Errorf("an unexposed language's capability statement must say scan-security does not resolve a provider for it, got: %q", s)
	}
	if !strings.Contains(low, "db dimension") && !strings.Contains(low, "database") {
		t.Errorf("an unexposed language's capability statement must name the DB-dimension-only coverage, got: %q", s)
	}
}
