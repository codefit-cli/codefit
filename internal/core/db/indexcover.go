package db

// IndexLike returns every index-like leading-column list of a table: each index's
// columns, plus the primary key treated as an implicit index. It is the neutral
// notion of "what a lookup can be served by", shared by every rule that reasons
// about index coverage — DB-001 (FK without index, dbrules), DB-010 (filtered
// column without index, crossrules), and DB-013 (missing composite index) — so the
// leftmost-prefix semantics are defined ONCE and never drift (ADR 0015, ADR 0029).
func IndexLike(t Table) [][]string {
	out := make([][]string, 0, len(t.Indexes)+1)
	for _, ix := range t.Indexes {
		out = append(out, ix.Columns)
	}
	if len(t.PrimaryKey) > 0 {
		out = append(out, t.PrimaryKey)
	}
	return out
}

// CoveredByOrderedPrefix reports whether some coverer has cols as a LEADING prefix
// in EXACTLY THAT ORDER. A B-tree serves a lookup on the leading columns of an index
// in order, so an index [a,b] covers a lookup on [a] and on [a,b], but NOT on [b]
// alone, and NOT on [b,a] — order matters.
//
// Question it answers: "is this ORDERED column sequence a leading prefix of some
// index-like list?" Used TODAY by DB-001 (FK coverage — the FK's declared column
// order is meaningful) and DB-010 (a single filtered column — order is trivial for
// one column). Do NOT use it for a multi-column EQUALITY filter, where the WHERE
// order is irrelevant and [b,a] must cover (a,b): that is CoveredBySetPrefix.
func CoveredByOrderedPrefix(coverers [][]string, cols []string) bool {
	for _, c := range coverers {
		if hasLeadingPrefix(c, cols) {
			return true
		}
	}
	return false
}

func hasLeadingPrefix(index, prefix []string) bool {
	if len(prefix) == 0 || len(index) < len(prefix) {
		return false
	}
	for i := range prefix {
		if index[i] != prefix[i] {
			return false
		}
	}
	return true
}

// CoveredBySetPrefix reports whether some coverer's LEADING columns, taken as a
// SET, equal cols as a set. A WHERE a=? AND b=? is served by a composite index on
// (a,b) OR (b,a) — both have {a,b} as their leading set — but NOT by (a,c,b), where
// c breaks the leading run, nor by an index shorter than the set.
//
// Question it answers: "do some index's leading columns, IGNORING ORDER, equal this
// column set?" Used TODAY by DB-013 (a multi-column, all-equality filter). Do NOT
// use it where order matters — a FK's declared order (DB-001) or a positional
// prefix: that is CoveredByOrderedPrefix. It is order-insensitive precisely because
// an equality set has no order; codefit does not capture equality-vs-range (a
// declared limit, ADR 0031), so it takes the columns as an unordered set and leaves
// the order/range judgment to the agent.
func CoveredBySetPrefix(coverers [][]string, cols []string) bool {
	if len(cols) == 0 {
		return false
	}
	want := make(map[string]bool, len(cols))
	for _, c := range cols {
		want[c] = true
	}
	for _, cov := range coverers {
		if len(cov) < len(cols) {
			continue
		}
		if sameSet(cov[:len(cols)], want) {
			return true
		}
	}
	return false
}

// sameSet reports whether the leading slice's elements are exactly the keys of want
// (same size already implied by the caller slicing to len(want) — but a duplicate
// in the slice would shrink the effective set, so it is checked explicitly).
func sameSet(leading []string, want map[string]bool) bool {
	got := make(map[string]bool, len(leading))
	for _, c := range leading {
		if !want[c] {
			return false
		}
		got[c] = true
	}
	return len(got) == len(want)
}

// UniqueKeys returns every column list that UNIQUELY identifies a row of the table:
// the primary key (if any), plus the columns of each UNIQUE index/constraint. It is
// the neutral notion of "what makes a row addressable to at most one", shared by the
// cross rules (ADR 0031/0032).
func UniqueKeys(t Table) [][]string {
	var out [][]string
	if len(t.PrimaryKey) > 0 {
		out = append(out, t.PrimaryKey)
	}
	for _, ix := range t.Indexes {
		if ix.Unique {
			out = append(out, ix.Columns)
		}
	}
	return out
}

// CoveredByUniqueSubset reports whether some unique key's column set is a SUBSET of
// the filtered column set — meaning the filter CONSTRAINS a unique key and therefore
// resolves to at most one row. When that holds, NO additional index helps (the
// lookup is already a single-row seek), so the rule must not flag a missing index.
//
// This covers @id (PK), a single @unique column, and a composite @@unique in one
// rule: e.g. filter (id, salonId) with id the PK is covered (id ⊆ {id, salonId}),
// but filter (a, c) with a @@unique([a,b]) is NOT ({a,b} ⊄ {a,c}) — do not over-kill.
// It is ROBUST to the equality-vs-range limit (ADR 0031): even a range on the PK
// still uses the PK index for a bounded seek, so the short-circuit introduces no new
// false negative. Used by DB-010 and DB-013 (ADR 0032).
func CoveredByUniqueSubset(uniqueKeys [][]string, cols []string) bool {
	if len(cols) == 0 {
		return false
	}
	have := make(map[string]bool, len(cols))
	for _, c := range cols {
		have[c] = true
	}
	for _, uk := range uniqueKeys {
		if len(uk) == 0 {
			continue
		}
		if isSubset(uk, have) {
			return true
		}
	}
	return false
}

// isSubset reports whether every element of sub is a key of have.
func isSubset(sub []string, have map[string]bool) bool {
	for _, c := range sub {
		if !have[c] {
			return false
		}
	}
	return true
}
