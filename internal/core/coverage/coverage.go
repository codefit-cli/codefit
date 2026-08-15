package coverage

// Entry is one declared unit of coverage, authored ONCE. The index the agent
// receives is a projection of this value and the detail it can ask for is a
// lookup in the same value, so a claim and its prose cannot drift apart: there
// is no second array to keep in step.
type Entry struct {
	// ID is stable and append-only. Where a typed source already exists (a rule
	// id, a surface category const, a typed exclusion) it is derived from that
	// source rather than hand-typed; hand-written prose carries a namespaced
	// slug. Renaming or removing an id is a breaking change and needs an ADR.
	ID string `json:"id"`
	// Claim is the one-line answer, at most MaxClaimBytes. It is what the agent
	// reads without asking for anything else, so it must stand alone.
	Claim string `json:"claim"`
	// Detail is the full prose, served only when the agent names this entry's id.
	// An entry whose claim already says everything carries no detail, and the
	// index reports that as a fact rather than sending the agent after nothing.
	Detail string `json:"detail,omitempty"`
}

// Status is the answer class an entry belongs to. It is NOT a field on Entry: it
// is derived from the bucket the entry was authored in, so an entry cannot
// carry a status that disagrees with where it lives. The status is still
// authored once — by choosing the bucket.
type Status string

const (
	StatusDeterministic      Status = "deterministic"
	StatusReasoning          Status = "reasoning"
	StatusNotCovered         Status = "not_covered"
	StatusDeliveredElsewhere Status = "delivered_elsewhere"
)

// MaxClaimBytes caps one claim. It is DERIVED, not chosen, and it is an
// authoring-time lint rather than a runtime budget: breaching it never changes
// what a caller receives, it turns a test red so an author shortens a claim.
//
// Derivation from two numbers this repo already owns: the response budget
// (40,000 bytes, ADR 0062) times 0.85 for framing, divided by the entry count is
// the ceiling a claim could take. RE-RUN against the entry count the per-rule
// split actually produced, which is 68 and not the ~70 the first pass assumed:
// 40,000 × 0.85 / 68 ≈ 500, still rounded down to 400 for headroom. Arithmetic
// check: 68 × (400 + ~60 of JSON framing) ≈ 31 KB, under 40,000.
//
// STATED TENSION: cap × count is not automatically under budget. Past roughly 85
// entries the worst case crosses 40,000 and the derivation has to be re-run. The
// entry count is visible in the committed ids golden, which makes that re-check
// mechanical rather than a thing someone has to remember.
//
// HOW MUCH ROOM IS LEFT is deliberately NOT frozen in this comment: a measured
// figure written here is a number that drifts the moment a claim is edited, and
// this repo has already been bitten by exactly that (the published headroom said
// 9 bytes against a measured 8). TestCoverage_IndexCarriesNoPayloadSizedString
// in internal/mcp LOGS the longest claim and the remaining headroom on every
// run, so the current number is one `go test -v` away and cannot go stale.
const MaxClaimBytes = 400

// IndexEntry is the projection of an Entry that every caller receives, always,
// for every entry the manifest holds.
type IndexEntry struct {
	ID        string `json:"id"`
	Claim     string `json:"claim"`
	Status    Status `json:"status"`
	HasDetail bool   `json:"has_detail"`
}

// Manifest declares, for one language, which classes of problems codefit covers
// and how — and, honestly, which it does not (PRD section 10).
type Manifest struct {
	Language string
	// Deterministic holds the classes codefit detects with coded patterns
	// (certainty 1.0): hardcoded secrets, SQL/command injection, weak crypto, ...
	Deterministic []Entry
	// Reasoning holds the classes codefit maps as surface for the agent to
	// reason about: IDOR, broken authz, over-fetching, ...
	Reasoning []Entry
	// NotCovered holds, explicitly, what codefit does not audit: race
	// conditions, architectural design flaws, business-logic correctness, ...
	NotCovered []Entry
	// DeliveredElsewhere holds capabilities the PRD promises under one identifier
	// that codefit delivers under ANOTHER — the capability exists, but not as a
	// rule carrying the promised id (N+1 is promised as DB-201 and shipped as the
	// provider's nplus1 surface category). It is a third answer, not a variant of
	// the other two (ADR 0057).
	DeliveredElsewhere []Entry
}

// Index projects every entry the manifest holds. It withholds nothing under any
// condition: the response budget authorizes withholding for scan-all, and for
// coverage it authorizes nothing at all.
func (m Manifest) Index() []IndexEntry {
	idx := make([]IndexEntry, 0, m.count())
	for _, b := range m.buckets() {
		for _, e := range b.entries {
			idx = append(idx, IndexEntry{
				ID:        e.ID,
				Claim:     e.Claim,
				Status:    b.status,
				HasDetail: e.Detail != "",
			})
		}
	}
	return idx
}

// ProseOf is the one rule for turning an entry back into the prose line it
// replaces: the detail if there is one, otherwise the claim. An entry short
// enough to say everything in its claim carries no detail, and duplicating the
// claim into the detail would make has_detail meaningless.
func ProseOf(e Entry) string {
	if e.Detail != "" {
		return e.Detail
	}
	return e.Claim
}

type bucket struct {
	status  Status
	entries []Entry
}

// buckets is the ONE place that pairs a slice with its status, so Index and
// Resolve cannot disagree about which entries exist or what class they are in.
func (m Manifest) buckets() []bucket {
	return []bucket{
		{StatusDeterministic, m.Deterministic},
		{StatusReasoning, m.Reasoning},
		{StatusNotCovered, m.NotCovered},
		{StatusDeliveredElsewhere, m.DeliveredElsewhere},
	}
}

func (m Manifest) count() int {
	n := 0
	for _, b := range m.buckets() {
		n += len(b.entries)
	}
	return n
}

// Resolve returns the full entries for the named ids, plus the ids it did not
// recognize. An unrecognized id is NAMED rather than silently dropped: an empty
// success would tell the agent the entry has nothing to say, which is a
// different and false answer.
func (m Manifest) Resolve(ids []string) (found []Entry, unrecognized []string) {
	byID := make(map[string]Entry, m.count())
	for _, b := range m.buckets() {
		for _, e := range b.entries {
			byID[e.ID] = e
		}
	}
	for _, id := range ids {
		if e, ok := byID[id]; ok {
			found = append(found, e)
			continue
		}
		unrecognized = append(unrecognized, id)
	}
	return found, unrecognized
}
