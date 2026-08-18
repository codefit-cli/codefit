package mcp_test

import (
	"encoding/json"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/surfaceindex"
	"github.com/codefit-cli/codefit/internal/mcp"
)

// TestScanDB_MeasuredIndexBytesPerItem_Pagila is not a correctness lock — it
// is the measurement CHANGELOG.md's before/after figures are drawn from,
// logged at runtime rather than frozen in a comment (the coverage-chain
// lesson: a published figure drifts the moment a field is edited elsewhere).
// Re-run with `-v` to get the current numbers before quoting them anywhere.
func TestScanDB_MeasuredIndexBytesPerItem_Pagila(t *testing.T) {
	root := writeProj(t, map[string]string{
		".codefit.yaml": tsYAMLWithSQLDDL,
		"db/schema.sql": pagilaExcerptSQL(t),
	})

	full, err := mcp.HandleScanDB(mcp.ScanDBRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanDB (index): %v", err)
	}
	if full.Count == 0 {
		t.Fatal("the Pagila fixture produced no db surface items — this measurement would be meaningless")
	}
	ids := make([]string, 0, len(full.Surface))
	for _, e := range full.Surface {
		ids = append(ids, e.ID)
	}
	withDetail, err := mcp.HandleScanDB(mcp.ScanDBRequest{Root: root, Language: "typescript", Detail: ids})
	if err != nil {
		t.Fatalf("HandleScanDB (detail): %v", err)
	}

	detailRaw, err := json.Marshal(withDetail.Detail)
	if err != nil {
		t.Fatalf("marshal detail: %v", err)
	}
	indexRaw, err := json.Marshal(full.Surface)
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}

	n := float64(full.Count)
	t.Logf("MEASURED (Pagila excerpt, %d items, %d categories): full detail %d B (%.1f B/item) · light index %d B (%.1f B/item) · ratio %.2fx",
		full.Count, countCategories(full.Surface), len(detailRaw), float64(len(detailRaw))/n,
		len(indexRaw), float64(len(indexRaw))/n, float64(len(detailRaw))/float64(len(indexRaw)))
	t.Log("SAMPLE CAVEAT: this is one corpus; the ratio is structural (field-size driven) but the count is small.")
}

func countCategories(entries []surfaceindex.Entry) int {
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.Category] = true
	}
	return len(seen)
}
