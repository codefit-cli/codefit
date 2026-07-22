package db

import "testing"

// TestCoveredByOrderedPrefix locks the leftmost-prefix semantics that DB-001/DB-010/DB-013
// all depend on — a composite index covers a lookup on its leading columns, never a
// trailing one. This is the compound-index trap made mechanical.
func TestCoveredByOrderedPrefix(t *testing.T) {
	tests := []struct {
		name     string
		coverers [][]string
		cols     []string
		want     bool
	}{
		{"single index exact", [][]string{{"email"}}, []string{"email"}, true},
		{"composite leftmost column IS covered", [][]string{{"a", "b"}}, []string{"a"}, true},
		{"composite trailing column NOT covered", [][]string{{"a", "b"}}, []string{"b"}, false},
		{"composite exact pair covered", [][]string{{"a", "b"}}, []string{"a", "b"}, true},
		{"no coverer", nil, []string{"email"}, false},
		{"unrelated index", [][]string{{"name"}}, []string{"email"}, false},
		{"empty cols never covered", [][]string{{"a"}}, nil, false},
		{"one of several coverers matches", [][]string{{"x"}, {"email", "y"}}, []string{"email"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CoveredByOrderedPrefix(tt.coverers, tt.cols); got != tt.want {
				t.Errorf("CoveredByOrderedPrefix(%v, %v) = %v, want %v", tt.coverers, tt.cols, got, tt.want)
			}
		})
	}
}

// TestIndexLike includes the primary key as an implicit index and preserves each
// index's column order.
func TestIndexLike(t *testing.T) {
	tb := Table{
		PrimaryKey: []string{"id"},
		Indexes:    []Index{{Columns: []string{"email"}}, {Columns: []string{"tenantId", "status"}}},
	}
	got := IndexLike(tb)
	// indexes first (in order), then the PK as an implicit index.
	want := [][]string{{"email"}, {"tenantId", "status"}, {"id"}}
	if len(got) != len(want) {
		t.Fatalf("IndexLike len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if len(got[i]) != len(want[i]) {
			t.Fatalf("IndexLike[%d] = %v, want %v", i, got[i], want[i])
		}
		for j := range want[i] {
			if got[i][j] != want[i][j] {
				t.Errorf("IndexLike[%d] = %v, want %v", i, got[i], want[i])
			}
		}
	}
}

// TestIndexLike_NoPK omits the PK entry when there is none.
func TestIndexLike_NoPK(t *testing.T) {
	if got := IndexLike(Table{Indexes: []Index{{Columns: []string{"a"}}}}); len(got) != 1 {
		t.Errorf("IndexLike with no PK = %v, want 1 entry", got)
	}
}

// TestCoveredBySetPrefix locks the ORDER-INSENSITIVE leading-set coverage DB-013
// uses: same leading set covers (any order), a gap in the prefix does not, a shorter
// index cannot. This is the composite-filter trap made mechanical.
func TestCoveredBySetPrefix(t *testing.T) {
	tests := []struct {
		name     string
		coverers [][]string
		cols     []string
		want     bool
	}{
		{"exact pair in order", [][]string{{"a", "b"}}, []string{"a", "b"}, true},
		{"same leading set, reversed order", [][]string{{"b", "a"}}, []string{"a", "b"}, true},
		{"leading prefix of a wider index", [][]string{{"a", "b", "c"}}, []string{"a", "b"}, true},
		{"gap in the prefix (a,c vs [a,b,c])", [][]string{{"a", "b", "c"}}, []string{"a", "c"}, false},
		{"index shorter than the set", [][]string{{"a"}}, []string{"a", "b"}, false},
		{"no coverer", nil, []string{"a", "b"}, false},
		{"two single-column indexes do not compose", [][]string{{"a"}, {"b"}}, []string{"a", "b"}, false},
		{"empty cols never covered", [][]string{{"a", "b"}}, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CoveredBySetPrefix(tt.coverers, tt.cols); got != tt.want {
				t.Errorf("CoveredBySetPrefix(%v, %v) = %v, want %v", tt.coverers, tt.cols, got, tt.want)
			}
		})
	}
}
