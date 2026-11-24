package mcp_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/namematch"
	"github.com/codefit-cli/codefit/internal/core/valueshape"
	"github.com/codefit-cli/codefit/internal/mcp"
)

// The SEC-001 value veto (ADR 0086) is applied by the Go provider and NOT yet by
// the TypeScript one (issue #148). Both halves of that sentence are coverage
// claims, and both must reach an agent through the real codefit-coverage
// handler — not only through COVERAGE.md, which no agent reads. A limit declared
// in a provider but never rendered is as silent, from the agent's side, as no
// limit at all; a divergence declared only in the human mirror is the
// mirror-ahead-of-source defect this project's doc map exists to prevent.
//
// The assertions use markers written independently of the providers' constants
// (the ADR number, the issue number, the valueshape form names) rather than
// reading the expected text back out of the provider. Reading it back would make
// the test pass when the declaration is removed from both places at once.

// TestCoverage_GoSEC001ServesBothDeclaredLimits locks that the Go derived floor
// renders SEC-001 with BOTH of its declared limits welded to its own detail: the
// name gate's all-lowercase-concatenation limit (ADR 0075) and the value veto
// (ADR 0086). Neither replaces the other.
func TestCoverage_GoSEC001ServesBothDeclaredLimits(t *testing.T) {
	resp := coverageOf(t, mcp.CoverageRequest{Language: "go", Detail: []string{"SEC-001"}})
	if len(resp.Unrecognized) != 0 || len(resp.Detail) != 1 || resp.Detail[0].ID != "SEC-001" {
		t.Fatalf("SEC-001 did not resolve on go: detail=%+v unrecognized=%v", resp.Detail, resp.Unrecognized)
	}
	detail := resp.Detail[0].Detail

	if got := strings.Count(detail, "— declared limit: "); got != 2 {
		t.Errorf("SEC-001's served detail carries %d declared limits, want 2 (name gate + value veto):\n%s", got, detail)
	}
	if !strings.Contains(detail, namematch.LimitLowercaseConcatenation) {
		t.Errorf("SEC-001's served detail lost the name-gate limit (ADR 0075):\n%s", detail)
	}
	for _, marker := range []string{"ADR 0086", "internal/core/valueshape", "is not reported at all"} {
		if !strings.Contains(detail, marker) {
			t.Errorf("SEC-001's served detail does not carry the value-veto limit (missing %q):\n%s", marker, detail)
		}
	}
	for _, form := range valueshape.Forms() {
		if !strings.Contains(detail, string(form)) {
			t.Errorf("SEC-001's served detail does not name the vetoed form %q:\n%s", form, detail)
		}
	}
}

// TestCoverage_TypeScriptSEC001DeclaresItDoesNotApplyTheValueVeto locks the
// TypeScript side of the same divergence in the SOURCE the handler serves (the
// hand-written CoverageManifest, ts.hardcoded-secrets): an agent asking about
// TypeScript's SEC-001 must learn that a credential-named binding holding a URL,
// a URN, an environment-variable name or a file path is STILL reported there,
// unlike in Go, and that this is temporary and tracked.
func TestCoverage_TypeScriptSEC001DeclaresItDoesNotApplyTheValueVeto(t *testing.T) {
	const id = "ts.hardcoded-secrets"
	resp := coverageOf(t, mcp.CoverageRequest{Language: "typescript", Detail: []string{id}})
	if resp.Derived {
		t.Fatal("typescript serves a hand-written manifest; a derived answer would not be the source under test")
	}
	if len(resp.Unrecognized) != 0 || len(resp.Detail) != 1 || resp.Detail[0].ID != id {
		t.Fatalf("%s did not resolve on typescript: detail=%+v unrecognized=%v", id, resp.Detail, resp.Unrecognized)
	}
	detail := resp.Detail[0].Detail
	if !strings.Contains(detail, "SEC-001") {
		t.Errorf("%s does not name the rule it describes (SEC-001):\n%s", id, detail)
	}

	for _, marker := range []string{
		"does NOT apply the value veto",
		"ADR 0086",
		"STILL REPORTED",
		"temporary",
		"issue #148",
	} {
		if !strings.Contains(detail, marker) {
			t.Errorf("%s does not declare the Go/TypeScript value-veto divergence (missing %q):\n%s", id, marker, detail)
		}
	}
	for _, form := range valueshape.Forms() {
		if !strings.Contains(detail, string(form)) {
			t.Errorf("%s does not name the form %q that Go vetoes and TypeScript still reports:\n%s", id, form, detail)
		}
	}
}
