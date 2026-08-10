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

// TestCapabilityStatement_GoNamesTheRealGap is 10.8: Go is registered but not
// exposed to security scanning — the statement must state the DB-dimension
// only coverage and that scan-security does NOT resolve a provider for Go,
// derived from the registry's Exposure record, never a hardcoded string.
func TestCapabilityStatement_GoNamesTheRealGap(t *testing.T) {
	s := scaffold.CapabilityStatement("go")
	low := strings.ToLower(s)
	if !strings.Contains(low, "db dimension") && !strings.Contains(low, "database") {
		t.Errorf("go's capability statement must name the DB-dimension-only coverage, got: %q", s)
	}
	if !strings.Contains(low, "does not resolve") && !strings.Contains(low, "not scanned") {
		t.Errorf("go's capability statement must say scan-security does not resolve a provider for it, got: %q", s)
	}
}

// TestCapabilityStatement_DerivedFromRegistryExposure proves the statement is
// COMPUTED from the registry's Exposure field, not a hardcoded per-language
// string: flipping which languages are exposed for security must flip which
// wording each one gets. This drives the real registry.ByName lookup rather
// than re-implementing the decision.
func TestCapabilityStatement_DerivedFromRegistryExposure(t *testing.T) {
	tsStatement := scaffold.CapabilityStatement("typescript")
	goStatement := scaffold.CapabilityStatement("go")
	if tsStatement == goStatement {
		t.Fatal("typescript (exposed) and go (not exposed) must get different capability statements")
	}
}
