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
	if strings.TrimRight(string(live), "\n") != strings.TrimRight(string(golden), "\n") {
		t.Errorf("absent score_weights must be byte-identical to the pre-change (cfd1ad7) response.\nlive:\n%s\ngolden:\n%s", live, golden)
	}
}
