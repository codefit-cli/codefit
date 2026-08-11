package mcp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/mcp"
)

// scoreWeightsFixtureFiles is one project shape shared by every test in this
// file: a security-triggering endpoint (SQL built by string concatenation,
// SEC-010/high) plus a PK-less table (DB-050/medium) — two dimensions with
// DIFFERENT per-dimension scores, so a weight change is visible in Global.
func scoreWeightsFixtureFiles(codefitYAML string) map[string]string {
	return map[string]string{
		".codefit.yaml": codefitYAML,
		"prisma/schema.prisma": `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model NoKey {
  name String
}
`,
		"app/x/route.ts": `export async function GET(req: Request) {
  const { searchParams } = new URL(req.url);
  db.query("SELECT * FROM t WHERE x = " + searchParams.get('x'));
  return Response.json(await prisma.thing.findMany());
}
`,
	}
}

const scoreWeightsYAMLAbsent = `version: "1"
project:
  name: t
  language: typescript
  framework: next
database:
  type: postgresql
  schema_paths:
    - prisma/schema.prisma
`

const scoreWeightsYAMLCustom = `version: "1"
project:
  name: t
  language: typescript
  framework: next
database:
  type: postgresql
  schema_paths:
    - prisma/schema.prisma
report:
  score_weights:
    security: 10
    db: 90
`

const scoreWeightsYAMLPartial = `version: "1"
project:
  name: t
  language: typescript
  framework: next
database:
  type: postgresql
  schema_paths:
    - prisma/schema.prisma
report:
  score_weights:
    security: 100
`

// TestScanAll_ScoreWeights_CustomMapDrivesGlobal is the load-bearing test for
// P1-2: it does NOT call scoring.ResolveWeights directly (that would only
// prove the resolver echoes its input) — it drives the REAL MCP handler with
// a real .codefit.yaml and proves the number in the RESPONSE moves when the
// user's map moves, by recomputing the expected weighted average from the
// dimension scores the response itself reports.
func TestScanAll_ScoreWeights_CustomMapDrivesGlobal(t *testing.T) {
	rootDefault := writeProj(t, scoreWeightsFixtureFiles(scoreWeightsYAMLAbsent))
	respDefault, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: rootDefault, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanAll (absent score_weights): %v", err)
	}
	secScore := respDefault.Score.ByDimension[findings.DimensionSecurity]
	dbScore := respDefault.Score.ByDimension[findings.DimensionDB]
	if secScore == nil || dbScore == nil {
		t.Fatalf("both security and db must be measured, got %+v", respDefault.Score.ByDimension)
	}
	if *secScore == *dbScore {
		t.Fatalf("fixture must produce DIFFERENT per-dimension scores to discriminate a weight change, got security=db=%d", *secScore)
	}
	wantDefaultGlobal := (*secScore*35 + *dbScore*20) / 55 // DefaultWeights(): security 35, db 20
	if respDefault.Score.Global != wantDefaultGlobal {
		t.Fatalf("sanity check on the default formula failed: global=%d, want %d (security=%d*35 + db=%d*20)/55",
			respDefault.Score.Global, wantDefaultGlobal, *secScore, *dbScore)
	}

	rootCustom := writeProj(t, scoreWeightsFixtureFiles(scoreWeightsYAMLCustom))
	respCustom, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: rootCustom, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanAll (custom score_weights): %v", err)
	}

	// The per-dimension scores themselves (DimensionScore over the same
	// findings) must be unchanged — only the WEIGHTED AVERAGE moves.
	if custSec := respCustom.Score.ByDimension[findings.DimensionSecurity]; custSec == nil || *custSec != *secScore {
		t.Errorf("by_dimension.security must be unaffected by score_weights, got %v want %d", custSec, *secScore)
	}
	if custDB := respCustom.Score.ByDimension[findings.DimensionDB]; custDB == nil || *custDB != *dbScore {
		t.Errorf("by_dimension.db must be unaffected by score_weights, got %v want %d", custDB, *dbScore)
	}

	wantCustomGlobal := (*secScore*10 + *dbScore*90) / 100 // score_weights: security 10, db 90
	if respCustom.Score.Global != wantCustomGlobal {
		t.Errorf("global with custom score_weights = %d, want %d ((security=%d*10 + db=%d*90)/100) — "+
			"the user's map was not actually used by scoring.Compute",
			respCustom.Score.Global, wantCustomGlobal, *secScore, *dbScore)
	}
	if respCustom.Score.Global == respDefault.Score.Global {
		t.Errorf("custom score_weights (10/90) must produce a DIFFERENT global than defaults (35/20), got %d for both",
			respCustom.Score.Global)
	}
}

// TestScanAll_ScoreWeights_PartialMapMissingMeasuredDimension_ActionableError
// is the reachable-consequence half of P1-2: a map that validates (sums to
// 100) but names only ONE of the two dimensions this scan measures must now
// surface scoring.MissingWeights as a real, actionable error — naming the
// dimension the user forgot and pointing at score_weights, not codefit's own
// wiring.
func TestScanAll_ScoreWeights_PartialMapMissingMeasuredDimension_ActionableError(t *testing.T) {
	root := writeProj(t, scoreWeightsFixtureFiles(scoreWeightsYAMLPartial))
	_, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err == nil {
		t.Fatal("a score_weights map naming only 'security' on a scan that also measures db must error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "db") {
		t.Errorf("error must name the missing dimension %q, got: %v", "db", err)
	}
	if !strings.Contains(msg, "score_weights") {
		t.Errorf("error must be actionable and point at score_weights, got: %v", err)
	}
	if strings.Contains(msg, "codefit internal") {
		t.Errorf("a user-supplied partial map is a CONFIG error, not a codefit wiring bug — must not read 'codefit internal', got: %v", err)
	}
}

// TestScanAll_ScoreWeights_Absent_ByteIdenticalToPreChange locks the no-value-
// regression half of P1-2: a project with NO score_weights key must produce
// exactly the response main produced before this change. The golden was
// captured with `git worktree add --detach cfd1ad7` (this branch's base) and
// `go run` over internal/mcp.HandleScanAll on this exact fixture — not
// re-implemented by hand.
//
// "security" is excluded from the comparison (same stripKey idiom as
// scanall_regression_test.go): roadmap P4-1
// (docs/specs/declared-partial-language-exposure.md) added
// security.surface_coverage to EVERY resolvable-language response, TypeScript
// included, as the R2 not-covered statement — a real, declared, user-visible
// field addition, not a regression this golden should catch. Everything else
// this test's scope actually covers (score_weights' effect on the score) is
// still asserted byte-for-byte.
func TestScanAll_ScoreWeights_Absent_ByteIdenticalToPreChange(t *testing.T) {
	root := writeProj(t, scoreWeightsFixtureFiles(scoreWeightsYAMLAbsent))
	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanAll: %v", err)
	}
	live, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	golden, err := os.ReadFile(filepath.Join("testdata", "scanall_scoreweights_absent_prechange.json"))
	if err != nil {
		t.Fatalf("reading pre-change golden: %v", err)
	}
	// "summary" is excluded on top of "security", for a different reason. security
	// was excluded as a declared field addition outside this test's scope. summary
	// is excluded because this golden was PROVED WRONG: it recorded
	// {endpoints:1, deterministic_findings:1, surface_items:3, certain_concerns:4}
	// for a project that also produces 1 db finding and 1 db surface item, so its
	// deterministic count was short by one and its surface count short by one.
	// The response deliberately no longer agrees with it. The bytes stay: they are
	// the second independent capture testifying to the undercount, and unlike
	// scanall_ts_withdb_prechange.json this one shows it WITHOUT having to read
	// another block — the numbers are simply wrong on their face.
	// Summary coverage for this fixture is taken over below, in the same run.
	gotStripped := stripKey(t, []byte(stripKey(t, live, "security")), "summary")
	wantStripped := stripKey(t, []byte(stripKey(t, golden, "security")), "summary")
	if gotStripped != wantStripped {
		t.Errorf("absent score_weights must be byte-identical to the pre-change (cfd1ad7) response (minus security, summary).\nlive:\n%s\ngolden:\n%s",
			gotStripped, wantStripped)
	}

	// The NON-VACUOUS migration control (scanall_regression_test.go's goldens
	// have all-zero flat summaries; this one does not). summary.security.* must
	// be verbatim what the golden's flat summary.* carried.
	var g struct {
		Summary flatSummary `json:"summary"`
	}
	if err := json.Unmarshal(golden, &g); err != nil {
		t.Fatalf("parsing the pre-change golden's flat summary: %v", err)
	}
	if g.Summary == (flatSummary{}) {
		t.Fatal("this golden's flat summary is all zero — it is the one capture whose non-zero counts make " +
			"the migration assertion below mean something; a zeroed one would pass over anything")
	}
	assertSummarySecurityMatchesFlatGolden(t, golden, resp)

	// And what the golden got WRONG, stated as the numbers: the db dimension it
	// measured (its own db section holds a finding and a surface item, and
	// by_dimension.db is 95) contributed nothing to any count it published.
	if resp.Summary.DB == nil {
		t.Fatal("summary.db is null over a project whose schema was measured")
	}
	if resp.Summary.Totals.DeterministicFindings != g.Summary.DeterministicFindings+resp.Summary.DB.DeterministicFindings {
		t.Errorf("totals.deterministic_findings = %d, want %d (the golden's %d security + %d db)",
			resp.Summary.Totals.DeterministicFindings,
			g.Summary.DeterministicFindings+resp.Summary.DB.DeterministicFindings,
			g.Summary.DeterministicFindings, resp.Summary.DB.DeterministicFindings)
	}
	if resp.Summary.Totals.SurfaceItems <= g.Summary.SurfaceItems {
		t.Errorf("totals.surface_items = %d, which is not MORE than the pre-change flat count of %d — the "+
			"db surface item is still uncounted", resp.Summary.Totals.SurfaceItems, g.Summary.SurfaceItems)
	}
}
