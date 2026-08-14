package mcp_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/mcp"
)

// WHY THIS TEST IS AT THIS LAYER AND NOT ONE MORE ASSERTION IN THE SENSOR.
//
// The sensor's own controls prove the floor engages. They cannot prove the
// verdict SURVIVES the adapter — and the verdict is worth nothing if it does
// not, because `score.by_dimension.db` and the `summary.db` block are what an
// agent actually reads. A second assertion beside the first would have proved
// the same fact twice; this one proves the fact travels.
//
// The shape it locks is the one ADR 0072 changed: a configured schema path that
// resolved to no schema file used to reach scan-all as a measured db dimension
// scoring 100, and now reaches it as `by_dimension.db: null` with `summary.db`
// absent and the section's note naming the path.

const zeroResolutionScanAllYAML = `version: "1"
project:
  name: t
  language: typescript
  framework: next
database:
  type: postgresql
  schema_paths:
    - db/migrations
`

// migrationsEmbedGo is the ordinary golang-migrate embed package: the directory
// holds real content, and not one file of it is a schema file the resolver takes.
const migrationsEmbedGo = `package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
`

// A route with nothing interesting in it — present only so the SECURITY
// dimension measures normally. It is the contrast that makes the assertion
// specific: this is not a scan that failed, it is a scan in which one dimension
// honestly did not measure.
const zeroResolutionRoute = `export async function GET() {
  return Response.json({ ok: true });
}
`

func writeZeroResolutionScanAllProj(t *testing.T) string {
	t.Helper()
	return writeProj(t, map[string]string{
		".codefit.yaml":          zeroResolutionScanAllYAML,
		"db/migrations/embed.go": migrationsEmbedGo,
		"app/health/route.ts":    zeroResolutionRoute,
	})
}

// C4. Through the real handler: the db dimension of a project whose only
// configured schema path resolved to nothing is NOT MEASURED, and that verdict
// is visible in every place scan-all publishes it.
func TestHandleScanAll_DBZeroResolution_IsNotMeasuredEndToEnd(t *testing.T) {
	root := writeZeroResolutionScanAllProj(t)

	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanAll over a project whose schema path resolves to nothing must not error: %v", err)
	}

	// Anti-vacuity: the db dimension must have been ATTEMPTED. Without this, a
	// project where the section is simply absent would satisfy every assertion
	// below while proving nothing about the floor.
	if resp.DB == nil {
		t.Fatal("no db section at all — the dimension never ran, so this test says nothing about the floor")
	}
	if resp.DB.Measured {
		t.Fatalf("db reports measured=true over a configured path that resolved to no schema file; note: %q", resp.DB.Note)
	}
	if !strings.Contains(resp.DB.Note, "db/migrations") {
		t.Errorf("db.note must name the configured path that resolved to nothing, got %q", resp.DB.Note)
	}

	if got, present := resp.Score.ByDimension[findings.DimensionDB]; !present {
		t.Error("by_dimension must carry the db key even when unmeasured (the declared-null shape)")
	} else if got != nil {
		t.Errorf("by_dimension.db must be null for an unmeasured dimension, got %d", *got)
	}
	if resp.Summary.DB != nil {
		t.Errorf("summary.db must be ABSENT when the db dimension did not measure, got %+v", resp.Summary.DB)
	}

	// The declared-null shape asserted on the BYTES the agent receives, not only
	// on the Go value: `"db": null` present inside by_dimension, and no "db" key
	// in summary at all.
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshaling the response: %v", err)
	}
	if !strings.Contains(string(raw), `"db":null`) {
		t.Errorf("the serialized response must carry by_dimension's `\"db\":null`, got:\n%s", raw)
	}

	// The contrast. Security measured normally on the same pass, so the db null
	// is a statement about the db dimension and not about a scan that fell over.
	if !resp.Security.Measured {
		t.Errorf("security must still measure normally on this project — otherwise the db verdict is not "+
			"specific to db; note: %q", resp.Security.Note)
	}
}
