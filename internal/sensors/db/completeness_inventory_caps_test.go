package db

import (
	"strconv"
	"strings"
	"testing"

	coredb "github.com/codefit-cli/codefit/internal/core/db"
)

// completenessNote's O(1)-in-schema-size guarantee (design §7a, resolved
// decision #3: "a 200-table systematic failure is one bounded line, not
// 200") — sdd-verify W2 (obs #1279): completenessInventoryTableCap=5 /
// completenessInventoryReasonCap=3 raised to 100000 left the suite green,
// since no test exercised either cap. This is an INTERNAL test (package db,
// same precedent as paradigm_internal_test.go/flyway_test.go in this
// package) so it can call the unexported completenessNote directly with
// synthetic schemas built past both caps — real production code paths only
// ever produce 2 distinct reasons (the closed Reason* vocabulary has
// exactly two members), so the reason cap's trigger condition is NOT
// reachable through ParseSchema; it is a defensive guard, tested here
// directly against the aggregation function rather than left unreachable
// and unverified.

func unprovenSyntheticTable(name string, reason coredb.Reason) coredb.Table {
	tb := coredb.Table{Name: name}
	tb.MarkUnproven(reason, "-- synthetic, for cap testing only --", coredb.Pos{File: "x.sql", Line: 1})
	return tb
}

// Table cap: 7 tables share ONE reason -> only the first 5 names are listed,
// followed by "(+2 more)". The count in the leading sentence ("7 table(s)")
// is NOT capped — only the enumerated names are.
func TestCompletenessNote_TableCap_BoundsNamedTables(t *testing.T) {
	const reason = "synthetic-reason-for-table-cap"
	var tables []coredb.Table
	for i := 1; i <= 7; i++ {
		tables = append(tables, unprovenSyntheticTable(sprintfTable(i), reason))
	}
	note := completenessNote(&coredb.Schema{Tables: tables})

	if !strings.Contains(note, "7 table(s)") {
		t.Errorf("note = %q, want the total count (7 table(s)) stated, uncapped", note)
	}
	if !strings.Contains(note, "(+2 more)") {
		t.Errorf("note = %q, want \"(+2 more)\" — 7 names minus the 5-table cap", note)
	}
	for i := 1; i <= 5; i++ {
		if !strings.Contains(note, sprintfTable(i)) {
			t.Errorf("note = %q, want table %s (within the first 5) named", note, sprintfTable(i))
		}
	}
	for i := 6; i <= 7; i++ {
		if strings.Contains(note, sprintfTable(i)) {
			t.Errorf("note = %q, want table %s (beyond the 5-table cap) NOT named individually", note, sprintfTable(i))
		}
	}
}

// Reason cap: 4 DISTINCT reasons (synthetic — the real Reason* vocabulary
// has exactly 2 members, so this scenario is unreachable via ParseSchema;
// this test proves the aggregation function's own guard directly) -> only
// the first 3 reasons are detailed, followed by "(+1 more reasons)".
func TestCompletenessNote_ReasonCap_BoundsDistinctReasons(t *testing.T) {
	var tables []coredb.Table
	reasons := []string{"synthetic-reason-A", "synthetic-reason-B", "synthetic-reason-C", "synthetic-reason-D"}
	for i, reason := range reasons {
		tables = append(tables, unprovenSyntheticTable(sprintfTable(i+1), coredb.Reason(reason)))
	}
	note := completenessNote(&coredb.Schema{Tables: tables})

	if !strings.Contains(note, "(+1 more reasons)") {
		t.Errorf("note = %q, want \"(+1 more reasons)\" — 4 distinct reasons minus the 3-reason cap", note)
	}
	for _, reason := range reasons[:3] {
		if !strings.Contains(note, reason) {
			t.Errorf("note = %q, want reason %q (within the first 3) named", note, reason)
		}
	}
	if strings.Contains(note, reasons[3]) {
		t.Errorf("note = %q, want reason %q (the 4th, beyond the cap) NOT named individually", note, reasons[3])
	}
}

func sprintfTable(i int) string {
	return "t" + strconv.Itoa(i)
}
