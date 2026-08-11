package mcp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/scoring"
	"github.com/codefit-cli/codefit/internal/mcp"
)

const regressionTSYAMLNoDB = `version: "1"
project:
  name: t
  language: typescript
  framework: next
`

const regressionTSYAMLWithDB = `version: "1"
project:
  name: t
  language: typescript
  framework: next
database:
  type: postgresql
  schema_paths:
    - prisma/schema.prisma
`

const regressionNoPKSchema = `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model NoKey {
  name String
}
`

// stripKey unmarshals raw JSON into a generic map, deletes key, and
// re-marshals — used to compare "everything except the new field" between
// two JSON payloads with a stable (alphabetical, from encoding/json's map
// marshaling) key order on both sides.
func stripKey(t *testing.T, raw []byte, key string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	delete(m, key)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(out)
}

// TestScanAll_TypeScriptHappyPath_OnlyDiffIsAddedSecurityKey is the modified
// requirement's exact success criterion, made checkable rather than asserted:
// "every pre-existing field value and the baseline delta identical", NOT
// "byte-identical" — the response gains exactly one field (`security`) and
// nothing else about it changes, for a TypeScript project with a resolvable
// provider, with and without a configured schema.
//
// The golden is the REAL pre-change response (commit 337f158, before this
// change), captured via `git worktree add --detach` and dumped with
// json.MarshalIndent — not a re-implementation of what the old shape "should"
// have produced (see testdata/README.md).
//
// Mutation gate (design's modified-requirement note): any unconditional
// addition to the response for a resolvable language — e.g. always setting a
// note on Security even when secRan is true, or letting `scanned`'s
// construction reorder observedFrom's dedup — flips this test from pass to
// fail.
func TestScanAll_TypeScriptHappyPath_OnlyDiffIsAddedSecurityKey(t *testing.T) {
	cases := []struct {
		name   string
		golden string
		files  map[string]string
	}{
		{
			name:   "no-db",
			golden: "scanall_ts_nodb_prechange.json",
			files: map[string]string{
				".codefit.yaml":  regressionTSYAMLNoDB,
				"app/x/route.ts": "export async function GET() { return Response.json({}); }\n",
			},
		},
		{
			name:   "with-db",
			golden: "scanall_ts_withdb_prechange.json",
			files: map[string]string{
				".codefit.yaml":        regressionTSYAMLWithDB,
				"prisma/schema.prisma": regressionNoPKSchema,
				"app/x/route.ts":       "export async function GET() { return Response.json({}); }\n",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := writeProj(t, tc.files)
			resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
			if err != nil {
				t.Fatalf("HandleScanAll: %v", err)
			}

			// The new field is present and reports security as measured (no
			// caveat) — this IS the added key, asserted before it is stripped.
			if !resp.Security.Measured || resp.Security.Note != "" {
				t.Fatalf("a TypeScript project (provider resolves) must have security.measured=true and no note, got %+v",
					resp.Security)
			}

			golden, err := os.ReadFile(filepath.Join("testdata", tc.golden))
			if err != nil {
				t.Fatalf("reading pre-change golden %s: %v", tc.golden, err)
			}
			live, err := json.Marshal(resp)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			// "budget" is also excluded from the identical-fields comparison, on top
			// of "security": this test's scope is the ADR 0059 regression (the
			// response for a resolvable language gains exactly one field), not the
			// budget's own byte number. ADR 0062 recalibrated ResponseBudgetBytes
			// 60 000 -> 40 000 (roadmap P0-4, measured by bisection against a real
			// client) after this golden was captured, so `budget.bytes` and
			// `budget.note`'s embedded number now legitimately differ from the
			// golden — that recalibration has its own dedicated, mutation-proved
			// coverage in scanall_budget_test.go, including the I4 honesty lock
			// (TestScanAllBudget_HonestyPersistsWhenTheBudgetForcesWithholding).
			//
			// "summary" joins them, and the reason is worth stating precisely
			// because it is NOT the same kind of reason. budget and security are
			// excluded as out of this test's scope. summary is excluded because
			// this golden PROVED IT WRONG: the with-db capture reports every
			// summary count as zero over a db section holding 1 finding and 1
			// surface item. The response deliberately no longer agrees with it,
			// and the recorded diff (see the commit that wired summary.db) is the
			// evidence the undercount existed. Narrowing this strip-set BEFORE
			// that wiring would have recorded evidence of a rename instead.
			//
			// Summary coverage is not dropped, it is HANDED OVER — to two tests
			// that assert against this same golden and this same fixture:
			//   - TestScanAll_ThePreChangeGoldenTestifiesToTheUndercount (below):
			//     the golden's own bytes contradict themselves, and the live
			//     response reports both dimensions.
			//   - TestScanAll_TheMigrationIsExact (below): summary.security.* is
			//     verbatim what the golden's flat summary.* carried, for BOTH the
			//     no-db and with-db cases — the adapter half that the
			//     scanall_budget_test.go copy of flatten() performs for the
			//     invariant golden.
			strip := func(t *testing.T, raw []byte) string {
				t.Helper()
				return stripKey(t, []byte(stripKey(t, []byte(stripKey(t, raw, "security")), "budget")), "summary")
			}
			gotStripped := strip(t, live)
			wantStripped := strip(t, golden) // golden has no "security" key; delete is a no-op, kept for symmetry
			if gotStripped != wantStripped {
				t.Errorf("the change moved a pre-existing field for a resolvable-language project.\n"+
					"pre-change (minus security, budget, summary): %s\npost-change (minus security, budget, summary): %s",
					wantStripped, gotStripped)
			}

			// The strip above removed `summary` from the byte comparison; it does
			// NOT remove it from this test's obligations. summary.security.* must
			// still be verbatim what the golden's flat summary.* held, in the same
			// run, over the same response — otherwise "consumers read
			// summary.security.* for the old values" is a promise with no control.
			assertSummarySecurityMatchesFlatGolden(t, golden, resp)
		})
	}
}

// --- the pre-change summary adapter (forced duplicate) -------------------------

// flatSummary mirrors the summary shape the pre-change goldens RECORDED: four
// unqualified counts, all of them security. The goldens' bytes are the only
// committed evidence the undercount existed, and testdata/README.md forbids
// regenerating them, so the per-dimension renest is absorbed by an adapter on
// this side and never by an edit on that one.
//
// This is a deliberate, exact copy of the adapter in scanall_budget_test.go.
// That file is package `mcp` (it drives the unexported buildScanAll and
// handleScanAllBudgeted); this file is package `mcp_test` (it drives the public
// HandleScanAll). Go offers the two packages no way to share an unexported test
// helper, so the duplication is FORCED by the package split, not chosen. Keep
// the two copies textually parallel: if one changes and the other does not, one
// of them is wrong.
type flatSummary struct {
	Endpoints             int `json:"endpoints"`
	DeterministicFindings int `json:"deterministic_findings"`
	SurfaceItems          int `json:"surface_items"`
	CertainConcerns       int `json:"certain_concerns"`
}

// preChangeInvariant is the conclusion block in the pre-change summary shape —
// the same struct scanall_budget_test.go reads its 79e34b0 golden into.
type preChangeInvariant struct {
	Summary  flatSummary          `json:"summary"`
	Scope    mcp.ScopeBlock       `json:"scope"`
	Score    scoring.ScoreSummary `json:"score"`
	Baseline mcp.BaselineDelta    `json:"baseline"`
}

// flatten projects a post-change response back onto the pre-change shape by
// reading summary.security. The projection is the ASSERTION, not a convenience:
// the removed requirement's migration promise is "consumers read
// summary.security.* for the old values", and this is what makes that promise
// checkable against a real pre-change capture.
func flatten(r mcp.ScanAllResponse) preChangeInvariant {
	var s flatSummary
	if r.Summary.Security != nil {
		s = flatSummary{
			Endpoints:             r.Summary.Security.Endpoints,
			DeterministicFindings: r.Summary.Security.DeterministicFindings,
			SurfaceItems:          r.Summary.Security.SurfaceItems,
			CertainConcerns:       r.Summary.Security.CertainConcerns,
		}
	}
	return preChangeInvariant{Summary: s, Scope: r.Scope, Score: r.Score, Baseline: r.Baseline}
}

// assertSummarySecurityMatchesFlatGolden is the migration control: the four
// counts under summary.security must be VERBATIM the four the pre-change golden
// carried at summary.*.
//
// Note on strength: over a golden whose flat summary is all zeros this
// comparison is weak (it proves security stayed zero, nothing more). The
// non-vacuous instance is
// TestScanAll_ScoreWeights_Absent_ByteIdenticalToPreChange, whose golden
// recorded {endpoints:1, deterministic_findings:1, surface_items:3,
// certain_concerns:4} — and which guards explicitly against that golden going
// empty.
func assertSummarySecurityMatchesFlatGolden(t *testing.T, golden []byte, resp mcp.ScanAllResponse) {
	t.Helper()
	var g struct {
		Summary flatSummary `json:"summary"`
	}
	if err := json.Unmarshal(golden, &g); err != nil {
		t.Fatalf("parsing the pre-change golden's flat summary: %v", err)
	}
	if got := flatten(resp).Summary; got != g.Summary {
		t.Errorf("summary.security.* must be verbatim the pre-change flat summary.* (the migration the "+
			"removed requirement promises).\npre-change summary.*:  %+v\npost-change summary.security.*: %+v",
			g.Summary, got)
	}
}

// TestScanAll_ThePreChangeGoldenTestifiesToTheUndercount turns the golden from
// a comparison target into a WITNESS.
//
// scanall_ts_withdb_prechange.json is a real response codefit produced at
// 337f158. It contradicts itself inside its own bytes: every summary count is
// zero, while the db section it carries holds a deterministic finding and a
// surface item, and its baseline delta says codefit OBSERVED two items this
// pass. An agent reading only the summary of that response would have concluded
// the project was clean.
//
// The assertions below read the golden's bytes directly — no live handler is
// involved in establishing the defect — and then run the live handler over the
// same fixture to show the same project now reports both dimensions. If someone
// ever regenerates the golden from a fixed build (the mutation this control
// exists to catch), the first half fails immediately: a regenerated file has a
// non-zero summary and stops testifying to anything.
func TestScanAll_ThePreChangeGoldenTestifiesToTheUndercount(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "scanall_ts_withdb_prechange.json"))
	if err != nil {
		t.Fatalf("reading the pre-change golden: %v", err)
	}
	var captured struct {
		Summary  flatSummary `json:"summary"`
		Baseline struct {
			New int `json:"new"`
		} `json:"baseline"`
		DB struct {
			Measured bool             `json:"measured"`
			Findings []map[string]any `json:"findings"`
			Surface  []map[string]any `json:"surface"`
		} `json:"db"`
	}
	if err := json.Unmarshal(raw, &captured); err != nil {
		t.Fatalf("parsing the pre-change golden: %v", err)
	}

	// The captured response's OWN evidence that it looked at a database.
	if !captured.DB.Measured {
		t.Fatal("the golden's db section reports measured=false — it cannot testify to an undercount of a " +
			"dimension it never measured; the capture is not the one this control needs")
	}
	if len(captured.DB.Findings) < 1 || len(captured.DB.Surface) < 1 {
		t.Fatalf("the golden must hold at least one db finding AND one db surface item to witness the "+
			"undercount, got %d finding(s) / %d surface item(s)",
			len(captured.DB.Findings), len(captured.DB.Surface))
	}
	// A second, independent contradiction in the same bytes: the baseline delta
	// counts the items codefit OBSERVED, and it says two.
	if captured.Baseline.New != 2 {
		t.Fatalf("the golden's baseline.new must be 2 (the two db items codefit observed), got %d — this "+
			"control reads a capture it does not recognise", captured.Baseline.New)
	}
	// And the verdict it published over that evidence: nothing, anywhere.
	if captured.Summary != (flatSummary{}) {
		t.Fatalf("the golden's summary must be ALL ZERO — that is the defect it testifies to. Got %+v. "+
			"If this golden was regenerated from a fixed build, the only committed evidence the undercount "+
			"existed has been destroyed; restore it from 337f158 (see testdata/README.md).",
			captured.Summary)
	}

	// Same project, current build: both dimensions are counted, and each count
	// declares which dimension it counted.
	root := writeProj(t, map[string]string{
		".codefit.yaml":        regressionTSYAMLWithDB,
		"prisma/schema.prisma": regressionNoPKSchema,
		"app/x/route.ts":       "export async function GET() { return Response.json({}); }\n",
	})
	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanAll: %v", err)
	}
	if resp.Summary.DB == nil {
		t.Fatal("summary.db is null over the very project whose db section holds a finding and a surface " +
			"item — the summary is still blind to the dimension")
	}
	if resp.Summary.DB.DeterministicFindings != len(captured.DB.Findings) {
		t.Errorf("summary.db.deterministic_findings = %d, want %d (the db findings this project produces)",
			resp.Summary.DB.DeterministicFindings, len(captured.DB.Findings))
	}
	if resp.Summary.DB.SurfaceItems != len(captured.DB.Surface) {
		t.Errorf("summary.db.surface_items = %d, want %d (the db surface items this project produces)",
			resp.Summary.DB.SurfaceItems, len(captured.DB.Surface))
	}
	if resp.Summary.DB.SchemaSources != 1 {
		t.Errorf("summary.db.schema_sources = %d, want 1 (the single configured schema this pass read)",
			resp.Summary.DB.SchemaSources)
	}
	// The roll-up the agent actually skims must not read as zero open questions.
	if resp.Summary.Totals.SurfaceItems == 0 {
		t.Error("summary.totals.surface_items is 0 over a project with a mapped db surface item — this is " +
			"the exact reading the defect produced")
	}
	if resp.Summary.Totals.SurfaceItems != resp.Summary.DB.SurfaceItems+resp.Summary.Security.SurfaceItems {
		t.Errorf("summary.totals.surface_items = %d, want %d (db %d + security %d) — totals is not derived "+
			"from the sub-blocks", resp.Summary.Totals.SurfaceItems,
			resp.Summary.DB.SurfaceItems+resp.Summary.Security.SurfaceItems,
			resp.Summary.DB.SurfaceItems, resp.Summary.Security.SurfaceItems)
	}
}
