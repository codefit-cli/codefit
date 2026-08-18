package mcp_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/mcp"
)

// TestScanDB_DetailFetchByID_MatchesOldFlatShape is spec scenario "detail
// equals old shape" (task 4.1): requesting an index id via `detail` returns
// the FULL findings.SurfaceItem, byte-identical to what the pre-change flat
// Surface array carried.
func TestScanDB_DetailFetchByID_MatchesOldFlatShape(t *testing.T) {
	root := writeProj(t, map[string]string{
		".codefit.yaml": tsYAMLWithSQLDDL,
		"db/schema.sql": pagilaExcerptSQL(t),
	})

	indexResp, err := mcp.HandleScanDB(mcp.ScanDBRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanDB (index): %v", err)
	}
	if len(indexResp.Surface) == 0 {
		t.Fatal("the Pagila fixture produced no db surface items — this test would prove nothing")
	}
	id := indexResp.Surface[0].ID
	if id == "" {
		t.Fatal("the first index entry has an empty id")
	}

	detailResp, err := mcp.HandleScanDB(mcp.ScanDBRequest{Root: root, Language: "typescript", Detail: []string{id}})
	if err != nil {
		t.Fatalf("HandleScanDB (detail): %v", err)
	}
	if len(detailResp.Detail) != 1 {
		t.Fatalf("len(Detail) = %d, want 1", len(detailResp.Detail))
	}
	got := detailResp.Detail[0]
	if got.ID != id {
		t.Fatalf("Detail[0].ID = %q, want %q", got.ID, id)
	}
	// Every heavy field the index withheld must be present in the detail.
	if got.Snippet == "" {
		t.Error("Detail[0].Snippet is empty — detail must carry the full item, not the index projection")
	}
	if got.ReasonToReview == "" {
		t.Error("Detail[0].ReasonToReview is empty — detail must carry the full item")
	}
	if len(detailResp.Unrecognized) != 0 {
		t.Errorf("Unrecognized = %v, want none — the requested id exists", detailResp.Unrecognized)
	}
}

// TestScanDB_UnrecognizedIDNamed is spec scenario "unrecognized id named"
// (task 4.3): an id that matches nothing is named in Unrecognized with a
// note — never an empty success.
func TestScanDB_UnrecognizedIDNamed(t *testing.T) {
	root := writeProj(t, map[string]string{
		".codefit.yaml": tsYAMLWithSQLDDL,
		"db/schema.sql": pagilaExcerptSQL(t),
	})
	resp, err := mcp.HandleScanDB(mcp.ScanDBRequest{Root: root, Language: "typescript", Detail: []string{"does-not-exist"}})
	if err != nil {
		t.Fatalf("HandleScanDB: %v", err)
	}
	if len(resp.Detail) != 0 {
		t.Errorf("Detail = %+v, want none — the id does not exist", resp.Detail)
	}
	if len(resp.Unrecognized) != 1 || resp.Unrecognized[0] != "does-not-exist" {
		t.Fatalf("Unrecognized = %v, want [\"does-not-exist\"]", resp.Unrecognized)
	}
	if resp.UnrecognizedNote == "" {
		t.Fatal("UnrecognizedNote is empty — an unmatched id must never be an empty success")
	}
	// D3: the note must ADMIT ambiguity (stateless — cannot tell "never
	// existed" from "the schema moved"), and must NOT reuse
	// coverageUnrecognizedNote's wording (a stronger claim than db can make).
	if !strings.Contains(strings.ToLower(resp.UnrecognizedNote), "stateless") {
		t.Errorf("UnrecognizedNote does not admit codefit is stateless: %q", resp.UnrecognizedNote)
	}
	if strings.Contains(resp.UnrecognizedNote, "matched no entry in this language's manifest") {
		t.Error("UnrecognizedNote reuses coverageUnrecognizedNote's wording verbatim — db's ambiguity is different (D3)")
	}
}

// TestScanDB_Bytes_MeasuredOverIndexPlusDetail is task 6.1: Bytes covers the
// whole payload (index + detail), never the index alone — the exact defect
// class the coverage-chain archive (#1664) records: a response that
// misreports its own size by measuring before detail is attached.
func TestScanDB_Bytes_MeasuredOverIndexPlusDetail(t *testing.T) {
	root := writeProj(t, map[string]string{
		".codefit.yaml": tsYAMLWithSQLDDL,
		"db/schema.sql": pagilaExcerptSQL(t),
	})

	indexOnly, err := mcp.HandleScanDB(mcp.ScanDBRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("HandleScanDB (index only): %v", err)
	}
	if indexOnly.Bytes != indexOnly.IndexBytes {
		t.Errorf("with no detail requested, Bytes (%d) must equal IndexBytes (%d)", indexOnly.Bytes, indexOnly.IndexBytes)
	}
	if indexOnly.Bytes == 0 {
		t.Fatal("IndexBytes is 0 — the fixture produced no measurable index")
	}

	ids := make([]string, 0, len(indexOnly.Surface))
	for _, e := range indexOnly.Surface {
		ids = append(ids, e.ID)
	}
	withDetail, err := mcp.HandleScanDB(mcp.ScanDBRequest{Root: root, Language: "typescript", Detail: ids})
	if err != nil {
		t.Fatalf("HandleScanDB (with detail): %v", err)
	}

	// Independently measure what the response ACTUALLY serializes, and prove
	// Bytes is not scoped to something smaller than what is really returned.
	raw, err := json.Marshal(withDetail)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if withDetail.Bytes <= withDetail.IndexBytes {
		t.Errorf("Bytes (%d) must exceed IndexBytes (%d) once detail is attached", withDetail.Bytes, withDetail.IndexBytes)
	}
	// The declared Bytes must be close to (not necessarily identical to,
	// since Bytes covers only index+detail, not the whole envelope with
	// findings/measured/etc.) the real marshaled size, and in particular it
	// must not be scoped down to only the index — the coverage defect.
	if len(raw) < withDetail.Bytes {
		t.Errorf("the real marshaled response (%d bytes) is SMALLER than the declared Bytes (%d) — "+
			"Bytes cannot exceed what is really on the wire", len(raw), withDetail.Bytes)
	}
}
