package mcp_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/namematch"
	"github.com/codefit-cli/codefit/internal/core/valueshape"
	"github.com/codefit-cli/codefit/internal/mcp"
)

// The SEC-001 value veto (ADR 0086) is applied by BOTH providers: Go in its own
// go/ast code, TypeScript through the rule-engine operator
// metavariable-value-veto (ADR 0087, issue #148). Both are coverage claims, and
// both must reach an agent through the real codefit-coverage handler — not only
// through COVERAGE.md, which no agent reads. A limit declared in a provider but
// never rendered is as silent, from the agent's side, as no limit at all; a
// claim declared only in the human mirror is the mirror-ahead-of-source defect
// this project's doc map exists to prevent.
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
	// The forms are not checked here by substring: "urn" is satisfied by "a urn:
	// URN" and "url" by "a URL", so a form dropped from the list would still
	// pass. valueshape's own test parses DeclaredLimit's form list as a set and
	// compares it with Forms(); what this handler test owes is that the served
	// detail carries that constant verbatim. Checked beside the independent
	// markers above, never instead of them: an emptied constant would satisfy
	// this line alone.
	if valueshape.DeclaredLimit == "" || !strings.Contains(detail, valueshape.DeclaredLimit) {
		t.Errorf("SEC-001's served detail does not carry valueshape.DeclaredLimit verbatim:\n%s", detail)
	}
}

// TestCoverage_TypeScriptSEC001AppliesTheValueVeto locks the TypeScript side in
// the SOURCE the handler serves (the hand-written CoverageManifest,
// ts.hardcoded-secrets): an agent asking about TypeScript's SEC-001 must learn
// that the value veto applies there, through which operator, with which limits,
// and that the divergence the previous text declared is gone. A stale "does NOT
// apply" sentence left beside the new one would tell the agent both things.
func TestCoverage_TypeScriptSEC001AppliesTheValueVeto(t *testing.T) {
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
		"applies the value veto",
		"metavariable-value-veto",
		"ADR 0086",
		"ADR 0087",
		"internal/core/valueshape",
		"is not reported at all",
		"NOT REPORTED",
		"STILL REPORTED",
		"concatenation",
	} {
		if !strings.Contains(detail, marker) {
			t.Errorf("%s does not declare the TypeScript value veto (missing %q):\n%s", id, marker, detail)
		}
	}
	for _, form := range valueshape.Forms() {
		if !strings.Contains(detail, string(form)) {
			t.Errorf("%s does not name the vetoed form %q:\n%s", id, form, detail)
		}
	}
	// The limit text is the shared constant, cited and not retyped. Checked
	// beside the independent markers above, never instead of them: an emptied
	// constant would satisfy this line alone.
	if valueshape.DeclaredLimit == "" || !strings.Contains(detail, valueshape.DeclaredLimit) {
		t.Errorf("%s does not carry valueshape.DeclaredLimit verbatim:\n%s", id, detail)
	}
	for _, stale := range []string{
		"does NOT apply the value veto",
		"DIVERGES from Go",
		"divergence is temporary",
	} {
		if strings.Contains(detail, stale) {
			t.Errorf("%s still carries the pre-#148 divergence text %q, which is now false:\n%s", id, stale, detail)
		}
	}
}
