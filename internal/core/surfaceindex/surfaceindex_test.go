package surfaceindex_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surfaceindex"
)

// TestIndex_LightFieldsOnly is spec scenario "light fields only": N surface
// items in, every index entry carries exactly id/category/file/line/
// fingerprint/structural_facts, and none of the heavy fields (snippet,
// structural_signals, reason_to_review, indirect_call).
func TestIndex_LightFieldsOnly(t *testing.T) {
	items := []findings.SurfaceItem{
		{
			ID:                "abc123",
			Category:          "db-no-timestamps",
			File:              "prisma/schema.prisma",
			Line:              6,
			Snippet:           "model NoKey {",
			StructuralSignals: []string{"table: NoKey", "columns: name"},
			StructuralFacts:   map[string]bool{"has_created_at": false},
			ReasonToReview:    "This table has no audit timestamp...",
			IndirectCall:      "someHelper",
			Fingerprint:       "fp1",
		},
		{
			ID:              "def456",
			Category:        "idor",
			File:            "app/x/route.ts",
			Line:            3,
			Snippet:         "prisma.thing.findUnique(...)",
			ReasonToReview:  "does this endpoint check ownership?",
			StructuralFacts: map[string]bool{"local_access_detected": true},
			Fingerprint:     "fp2",
		},
	}

	entries, count := surfaceindex.Index(items)
	if count != len(items) {
		t.Fatalf("count = %d, want %d", count, len(items))
	}
	if len(entries) != len(items) {
		t.Fatalf("len(entries) = %d, want %d", len(entries), len(items))
	}
	for i, e := range entries {
		want := items[i]
		if e.ID != want.ID {
			t.Errorf("entry[%d].ID = %q, want %q", i, e.ID, want.ID)
		}
		if e.Category != want.Category {
			t.Errorf("entry[%d].Category = %q, want %q", i, e.Category, want.Category)
		}
		if e.File != want.File {
			t.Errorf("entry[%d].File = %q, want %q", i, e.File, want.File)
		}
		if e.Line != want.Line {
			t.Errorf("entry[%d].Line = %d, want %d", i, e.Line, want.Line)
		}
		if e.Fingerprint != want.Fingerprint {
			t.Errorf("entry[%d].Fingerprint = %q, want %q", i, e.Fingerprint, want.Fingerprint)
		}
		if len(e.StructuralFacts) != len(want.StructuralFacts) {
			t.Errorf("entry[%d].StructuralFacts = %v, want %v", i, e.StructuralFacts, want.StructuralFacts)
		}
	}

	// The heavy fields must never leak: marshal and check the raw bytes carry
	// none of their keys. A field-by-field struct comparison could not catch a
	// leak through an accidental embedded/extra field; this checks the wire.
	rawBytes, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	raw := string(rawBytes)
	for _, forbidden := range []string{`"snippet"`, `"structural_signals"`, `"reason_to_review"`, `"indirect_call"`} {
		if strings.Contains(raw, forbidden) {
			t.Errorf("index entry leaks a heavy field %s into the wire: %s", forbidden, raw)
		}
	}
}

// TestIndex_ZeroItems is spec scenario "zero items": no items in, Index
// returns an empty slice and a zero count.
func TestIndex_ZeroItems(t *testing.T) {
	entries, count := surfaceindex.Index(nil)
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
	if len(entries) != 0 {
		t.Errorf("len(entries) = %d, want 0", len(entries))
	}
}

// TestResolve_ReturnsFullItemsAndNamesMisses is spec scenario "detail equals
// old shape" plus "unrecognized id named": a requested id that exists comes
// back as the FULL SurfaceItem, field for field; a requested id that matches
// nothing is named in unrecognized rather than silently dropped.
func TestResolve_ReturnsFullItemsAndNamesMisses(t *testing.T) {
	items := []findings.SurfaceItem{
		{
			ID:                "abc123",
			Category:          "db-no-timestamps",
			File:              "prisma/schema.prisma",
			Line:              6,
			Snippet:           "model NoKey {",
			StructuralSignals: []string{"table: NoKey"},
			StructuralFacts:   map[string]bool{"has_created_at": false},
			ReasonToReview:    "This table has no audit timestamp...",
			Fingerprint:       "fp1",
		},
	}

	found, unrecognized := surfaceindex.Resolve(items, []string{"abc123", "nope"})
	if len(found) != 1 {
		t.Fatalf("len(found) = %d, want 1", len(found))
	}
	if !reflect.DeepEqual(found[0], items[0]) {
		t.Errorf("Resolve did not return the full item byte for byte:\ngot:  %+v\nwant: %+v", found[0], items[0])
	}
	if len(unrecognized) != 1 || unrecognized[0] != "nope" {
		t.Errorf("unrecognized = %v, want [\"nope\"] — an unmatched id must be NAMED, never silently dropped", unrecognized)
	}
}
