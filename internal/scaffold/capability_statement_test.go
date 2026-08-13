package scaffold_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/providers/registry"
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

// TestCapabilityStatement_Undetected is the third distinguishable declaration:
// detected+exposed states its real reach, registered-but-unexposed states its
// gap, and no-detection-at-all states THIS. None of the three may read as a
// pass.
//
// It must never fall through to the "is not a registered language" branch —
// that sentence, rendered for the sentinel, would say "undetected is not a
// registered language", which is codefit talking to itself about its own
// vocabulary instead of telling the developer what happened.
func TestCapabilityStatement_Undetected(t *testing.T) {
	s := scaffold.CapabilityStatement(config.LanguageUndetected)
	if s != scaffold.UndetectedStatement() {
		t.Errorf("CapabilityStatement(%q) must be the undetected statement, got: %q", config.LanguageUndetected, s)
	}
	low := strings.ToLower(s)

	if strings.Contains(low, "not a registered language") {
		t.Errorf("the sentinel must not render the unregistered-language fallback, got: %q", s)
	}
	// The sentinel is codefit's internal spelling, not a word to show a human.
	if strings.Contains(s, config.LanguageUndetected+" is") {
		t.Errorf("the statement must not present the sentinel as if it were a language name, got: %q", s)
	}

	// It names every marker that can actually resolve a provider — derived,
	// so the list cannot drift from the registry the way the deleted refusal
	// message did.
	markers := registry.InitDetectMarkerFiles()
	if len(markers) == 0 {
		t.Fatal("no InitDetect markers registered; the loop below would pass vacuously")
	}
	for _, m := range markers {
		if !strings.Contains(s, m) {
			t.Errorf("the statement must name the marker %q codefit looks for, got: %q", m, s)
		}
	}
	// And it names no manifest that cannot help — the original defect.
	for _, cannotHelp := range []string{"pyproject.toml", "requirements.txt", "pom.xml", "build.gradle"} {
		if strings.Contains(s, cannotHelp) {
			t.Errorf("the statement names %q, which resolves no provider — that is the original defect, got: %q", cannotHelp, s)
		}
	}

	// The DB dimension still applies, and code scanning does not.
	if !strings.Contains(s, "schema_paths") {
		t.Errorf("the statement must say the DB dimension still audits the schema when schema_paths is configured, got: %q", s)
	}
	if !strings.Contains(low, "scan-security") {
		t.Errorf("the statement must say code-level security scanning does not run here, got: %q", s)
	}
}

// TestCapabilityStatement_UndetectedReusesTheDBOnlyVocabulary is D4 made
// mechanical instead of promised: the undetected statement and the
// registered-but-unexposed statement must share ONE db-only fragment, not two
// sentences that say the same thing and drift apart. Asserting the fragment
// BYTE-FOR-BYTE in both outputs is the check; asserting only that each mentions
// "database" would pass on two divergent wordings.
func TestCapabilityStatement_UndetectedReusesTheDBOnlyVocabulary(t *testing.T) {
	clause := scaffold.DBOnlyClauseForTest()
	if clause == "" {
		t.Fatal("the shared db-only clause is empty; both assertions below would pass vacuously")
	}
	if !strings.Contains(clause, "schema_paths") {
		t.Fatalf("the shared clause must name the config key that turns the DB dimension on, got: %q", clause)
	}

	undetected := scaffold.UndetectedStatement()
	unexposed := scaffold.CapabilityStatementForExposure("cobol", false)
	if !strings.Contains(undetected, clause) {
		t.Errorf("the undetected statement must interpolate the shared clause %q, got: %q", clause, undetected)
	}
	if !strings.Contains(unexposed, clause) {
		t.Errorf("the unexposed statement must interpolate the shared clause %q, got: %q", clause, unexposed)
	}
}

// TestCapabilityStatement_ThreeCasesAreDistinguishable pins that the three
// declarations are three, not two dressed up as three.
func TestCapabilityStatement_ThreeCasesAreDistinguishable(t *testing.T) {
	exposed := scaffold.CapabilityStatement("typescript")
	unexposed := scaffold.CapabilityStatementForExposure("cobol", false)
	undetected := scaffold.CapabilityStatement(config.LanguageUndetected)
	for _, pair := range []struct{ a, b, name string }{
		{exposed, unexposed, "exposed vs unexposed"},
		{exposed, undetected, "exposed vs undetected"},
		{unexposed, undetected, "unexposed vs undetected"},
	} {
		if pair.a == pair.b {
			t.Errorf("%s must be distinguishable statements, both are: %q", pair.name, pair.a)
		}
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
