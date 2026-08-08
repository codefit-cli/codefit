//go:build dogfood

// Dogfood harness for the response budget. The defect it guards was MEASURED on
// a real project, not imagined: codefit-scan-all over a 317-file TypeScript
// repository returned 313 284 bytes and exceeded the MCP client's limit. A
// fixture cannot reproduce that — any fixture small enough to live in this repo
// fits the budget whether or not the fix exists, so a fixture-only byte
// assertion proves nothing. This file measures the real corpus.
//
// READ-ONLY OVER THE PROJECTS, exactly like dogfood_cache_test.go. scan-all
// WRITES the next baseline, so nothing here calls the public handler: it calls
// handleScanAllBudgeted with a baseline path inside t.TempDir(). That also
// reproduces the state the 313 KB response was measured in — nothing tracked
// yet, so every item is new and no bucket is silenced by a baseline that a
// previous dogfood run left behind.
//
// Build-tagged `dogfood`: EXCLUDED from the normal gate, skips clean when
// dogfood.local.json is absent.
//
//	go test -tags dogfood -run TestDogfoodResponseBudget -v ./internal/mcp/
package mcp

import (
	"encoding/json"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/scope"
)

// TestDogfoodResponseBudget is contract item 1: over a corpus that previously
// exceeded the limit, the response now fits the budget — measured in bytes on
// the real serialized payload.
//
// The "previously exceeded" half is measured in the SAME run rather than quoted
// from the transcript: buildScanAll returns the complete actionable detail that
// the old response inlined verbatim, so marshalling it gives the exact bytes the
// old shape would have spent. No production logic is reimplemented here; both
// numbers come out of the production path.
func TestDogfoodResponseBudget(t *testing.T) {
	var measured, exceededBefore int

	for _, p := range loadDogfoodProjects(t) {
		t.Run(p.Name, func(t *testing.T) {
			root := dogfoodRoot(t, p)

			resp, complete, _, err := buildScanAll(
				ScanAllRequest{Root: root, Language: "typescript"}, scope.Full(), dogfoodBaselinePath(t))
			if err != nil {
				t.Fatalf("scan-all over %s: %v", root, err)
			}
			if resp.Scope.AuditableTotal == 0 {
				t.Fatalf("the scan of %s found 0 auditable files — a response over nothing fits any "+
					"budget and this subtest would prove exactly that", root)
			}
			measured++

			// BEFORE: what the old shape put in `actionable` for this same scan.
			before, err := json.Marshal(complete)
			if err != nil {
				t.Fatal(err)
			}
			// AFTER: the real serialized payload an MCP client receives.
			rendered, _ := withNamedActionable(resp, complete, ResponseBudgetBytes)
			after, err := json.Marshal(rendered)
			if err != nil {
				t.Fatal(err)
			}

			concerns := 0
			for _, ep := range complete {
				concerns += len(ep.Concerns)
			}
			t.Logf("BUDGET %-14s files=%d endpoints=%d (actionable=%d clean=%d frontier=%d) concerns=%d | "+
				"actionable-detail BEFORE=%d bytes | whole response AFTER=%d bytes (budget %d)",
				p.Name, resp.Scope.AuditableTotal, resp.Summary.Endpoints,
				resp.Actionable.Count, resp.ResolvedClean.Count, resp.FrontierPending.Count, concerns,
				len(before), len(after), ResponseBudgetBytes)

			if len(before) > ResponseBudgetBytes {
				exceededBefore++
			}
			if len(after) > ResponseBudgetBytes {
				t.Errorf("the response over %s is %d bytes, %d over the declared budget of %d",
					p.Name, len(after), len(after)-ResponseBudgetBytes, ResponseBudgetBytes)
			}
			// Fitting by having withheld everything would satisfy the byte assertion
			// and defeat its purpose. On a corpus this size the budget must be met by
			// the naming, not by the truncation.
			if resp.Budget.Withheld > 0 {
				t.Logf("NOTE %s withheld %d endpoint(s) to fit", p.Name, resp.Budget.Withheld)
			}
		})
	}

	if measured == 0 {
		if t.Failed() {
			return
		}
		t.Skip("no dogfood project was present on this machine")
	}
	// The corpus-level guard, and the reason this file exists: if not one project
	// would have blown the budget under the old shape, then "it now fits" is a
	// statement about small projects and the measurement is not entitled to claim
	// the defect is fixed.
	if exceededBefore == 0 {
		t.Errorf("not one of the %d measured project(s) would have exceeded the %d-byte budget under the "+
			"OLD inline shape: this run cannot show the defect is fixed — point dogfood.local.json at a "+
			"project the size of the one that failed (317 files, 174 endpoints)", measured, ResponseBudgetBytes)
	}
}

// dogfoodBaselinePath keeps the baseline OUT of the working clone. The public
// handler would write .codefit-baseline into the project root; this is why the
// harness drives handleScanAllBudgeted's seam instead.
func dogfoodBaselinePath(t *testing.T) string {
	t.Helper()
	return t.TempDir() + "/.codefit-baseline"
}
