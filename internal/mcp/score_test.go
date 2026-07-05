package mcp_test

import (
	"encoding/json"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/scoring"
	"github.com/codefit-cli/codefit/internal/mcp"
)

// No-regression of VALUE: with only security measured, the global equals today's
// flat security score exactly (secScore*35/35 == secScore).
func TestScanAll_Score_SecurityOnly_NoValueRegression(t *testing.T) {
	root := writeProj(t, map[string]string{
		".codefit.yaml": tsYAMLNoDB,
		// a hardcoded secret → one critical security finding (SEC-001) so the score
		// is not a trivial 100 and the no-regression is meaningful.
		"src/keys.ts": "export const apiKey = \"sk-ant-abcdefgh12345678\";\n",
	})
	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanAll: %v", err)
	}
	// Recompute today's flat security score independently and compare.
	sec, err := mcp.HandleScanSecurity(mcp.ScanRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanSecurity: %v", err)
	}
	if resp.Score.Global != sec.Score {
		t.Errorf("global = %d, want %d (== flat security score, no value regression)", resp.Score.Global, sec.Score)
	}
	if p := resp.Score.ByDimension[findings.DimensionSecurity]; p == nil || *p != sec.Score {
		t.Errorf("by_dimension.security = %v, want %d", p, sec.Score)
	}
	if resp.Score.ByDimension[findings.DimensionDB] != nil {
		t.Error("by_dimension.db must be null when db did not run")
	}
	if resp.DB != nil {
		t.Error("DB section must still be omitted when there is no database")
	}
}

// The by_dimension shape: exactly the DefaultWeights keys, measured ones *int,
// unmeasured ones null.
func TestScanAll_Score_ByDimensionShape(t *testing.T) {
	root := writeProj(t, map[string]string{
		".codefit.yaml":  tsYAMLNoDB,
		"app/x/route.ts": "export async function GET() { return Response.json({}); }\n",
	})
	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanAll: %v", err)
	}
	for d := range scoring.DefaultWeights() {
		if _, ok := resp.Score.ByDimension[d]; !ok {
			t.Errorf("by_dimension missing key %q (must carry every weighted dimension)", d)
		}
	}
	if len(resp.Score.ByDimension) != len(scoring.DefaultWeights()) {
		t.Errorf("by_dimension has %d keys, want %d", len(resp.Score.ByDimension), len(scoring.DefaultWeights()))
	}
	// review/complexity/tests are declared not-audited → null.
	for _, d := range []findings.Dimension{findings.DimensionReview, findings.DimensionComplexity, findings.DimensionTests} {
		if resp.Score.ByDimension[d] != nil {
			t.Errorf("by_dimension.%s must be null (not audited)", d)
		}
	}
	// score is always present in the JSON.
	data, _ := json.Marshal(resp)
	if !contains(string(data), "\"score\"") || !contains(string(data), "\"by_dimension\"") {
		t.Errorf("response must always carry a score/by_dimension: %s", data)
	}
}

// security + db: the global is the weighted average, integer-divided. The expected
// values are HAND-COMPUTED literals (not the production formula), so the test pins
// the actual truncation instead of validating the code against itself:
//
//	security: one critical hardcoded-secret finding in src/** (production, no
//	          downgrade) → 100 − 20 = 80.
//	db:       the NoKey table has no primary key → DB-050 (medium) → 100 − 5 = 95.
//	global:   (80*35 + 95*20) / (35+20) = 4700 / 55 = 85.45… → 85 (truncated).
func TestScanAll_Score_WithDB_WeightedAverage(t *testing.T) {
	const wantSec, wantDB, wantGlobal = 80, 95, 85
	root := writeProj(t, map[string]string{
		".codefit.yaml":        tsYAMLWithDB,
		"prisma/schema.prisma": noPKSchema,
		"src/keys.ts":          "export const apiKey = \"sk-ant-abcdefgh12345678\";\n",
	})
	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanAll: %v", err)
	}
	// Pin the component scores so the hand calc above is grounded, not assumed.
	sec, _ := mcp.HandleScanSecurity(mcp.ScanRequest{Root: root, Language: "typescript"})
	db, _ := mcp.HandleScanDB(mcp.ScanDBRequest{Root: root, Language: "typescript"})
	if sec.Score != wantSec {
		t.Fatalf("security score = %d, want %d (the test's hand calc drifted)", sec.Score, wantSec)
	}
	if !db.Measured || db.Score != wantDB {
		t.Fatalf("db score = %d (measured=%v), want %d", db.Score, db.Measured, wantDB)
	}
	// The global is the literal 85, not a re-derivation of the production formula.
	if resp.Score.Global != wantGlobal {
		t.Errorf("global = %d, want %d (hand-computed (80*35+95*20)/55, integer truncation)", resp.Score.Global, wantGlobal)
	}
	if p := resp.Score.ByDimension[findings.DimensionDB]; p == nil || *p != wantDB {
		t.Errorf("by_dimension.db = %v, want %d", p, wantDB)
	}
}

// Surface does not penalize: a project with db surface (FK with no index) but no db
// affirmations scores db == 100.
func TestScanAll_Score_SurfaceDoesNotPenalize(t *testing.T) {
	// A schema with a PK (no DB-050 affirmation) and a foreign key with no covering
	// index (DB-001 surface). Surface must not lower the score.
	schema := `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model User {
  id Int @id
}

model Post {
  id       Int  @id
  authorId Int
  author   User @relation(fields: [authorId], references: [id])
}
`
	root := writeProj(t, map[string]string{
		".codefit.yaml":        tsYAMLWithDB,
		"prisma/schema.prisma": schema,
	})
	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanAll: %v", err)
	}
	p := resp.Score.ByDimension[findings.DimensionDB]
	if p == nil {
		t.Fatal("db must be measured")
	}
	if *p != 100 {
		t.Errorf("db score = %d, want 100 (surface must not penalize)", *p)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
