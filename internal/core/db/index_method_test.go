package db

import "testing"

// TestIndex_MethodField locks db.Index.Method as a first-class field of the
// neutral model (index-method-capture slice). The RED state for this test IS
// a compile failure: before the field exists, the struct literal below does
// not compile at all — the same "compile-red" discipline
// architecture/db-olap-rules-tasks-s3 (obs #1293) already used for this exact
// field. GREEN is simply the field existing and round-tripping the value
// unchanged.
func TestIndex_MethodField(t *testing.T) {
	ix := Index{Columns: []string{"c"}, Method: "brin"}
	if ix.Method != "brin" {
		t.Errorf("Method = %q, want %q", ix.Method, "brin")
	}
}

// TestIndex_MethodField_EmptyMeansNone locks the empty-means-none convention
// (same as Table.DBName, Trigger.ExecutesFunction): a zero-value Index has no
// declared access method, never a guessed default like "btree".
func TestIndex_MethodField_EmptyMeansNone(t *testing.T) {
	var ix Index
	if ix.Method != "" {
		t.Errorf("zero-value Index.Method = %q, want empty", ix.Method)
	}
}
