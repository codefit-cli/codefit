# 0080 — `db.surface` is an index; the question is served by a different tool

- Status: accepted
- Date: 2026-08-17
- Supersedes: nothing. Reuses the shape ADR 0008/0054 (endpoints) and ADR
  0076/0078 (coverage) already established; extends it to the db dimension.

## Context

`db.surface`, in both `codefit-scan-all`'s `db` bucket and standalone
`codefit-scan-db`, carried the FULL question for every item: `snippet`,
`structural_signals`, `reason_to_review`, `indirect_call`, alongside the
light identity fields (`id`, `category`, `file`, `line`, `fingerprint`).

Measured over a real corpus (Pagila excerpt, 5 items, 4 categories, driven
through the real db sensor via `HandleScanDB`, not a hand-assembled struct):
the full shape serializes to 609.6 B/item, and **84.6% of that is the prose
half** — `reason_to_review` alone is 43% of the total. This is the identical
shape of defect the coverage-chain archive records for `codefit-coverage`
("the manifest serialized what it should summarize" — obs #1664): most of
the payload was never the thing an agent reads first.

The reproduction that files this as urgent already existed in this repo:
`TestScanAllBudget_ZeroEndpointsOverBudget_DoesNotClaimItFit` builds 200
db-heavy surface items and proves a zero-endpoint, db-only response exceeds
the 40,000-byte response budget with **nothing the budget mechanism can
withhold** — `maxDroppable` (`scanall_budget.go`) sums only endpoint
buckets; `db.surface` is not one of them and has never been droppable.

## Decision

**1. The projection is a pure leaf, `internal/core/surfaceindex`, not a
method on `internal/mcp` or on `internal/core/findings` (D1).** Two
precedents already put an equivalent projection in the core, never the
adapter: `report.NameActionable` for endpoints, `coverage.Manifest.Index()`/
`Resolve()` for coverage. A dedicated package mirrors
`internal/core/coverage` exactly: it imports only `internal/core/findings`,
and nothing outside `internal/mcp` imports it (locked by an import-boundary
census test, the same shape `internal/schemasource/layering_test.go` uses
for its own R11 lock). `findings` itself was rejected as the projection's
home — it is the package with the widest import fan-in in the repo (every
sensor and provider), and a response-shaping concern does not belong there.

**2. Every item is indexed; nothing is withheld, and the reason is NOT
coverage's reason (D4).** `codefit-coverage` withholds nothing because its
content is a fixed, authored manifest and the response budget authorizes no
withholding there at all. `db.surface` withholds nothing for a narrower,
more honest reason: **there is no ranking axis** — no severity field, no
common ordering across 18 disjoint surface categories — so there is nothing
to withhold BY. This is a stated absence of a mechanism, not a design
principle, and the two `WithheldNote` strings are deliberately different
sentences so a reader cannot infer "coverage's reason" from db's silence.
`maxDroppable` / `withheldBy` / `fitToBudget` are untouched: db stays
entirely outside scan-all's binary search over endpoint buckets.

**3. Detail is a stateless re-run, never a store (reuses D2 of the
`codefit-scan-endpoint` precedent).** `HandleScanDB` already re-runs the
full db audit unconditionally on every call. With `detail: [ids]` it runs
the identical audit, projects the complete surface into the index (always
returned, always complete), and filters the complete surface by id.
Nothing is stored between calls; ids are recomputed by the unchanged
`surface.StableID(file, line, category)` that `dbsensor.StampSurface`
already stamps on every run.

**4. A vanished id is named, and the note admits what codefit cannot know
(D3).** An id can miss for two different reasons — it never existed, or the
schema changed between calls and the item is genuinely gone — and codefit is
stateless: it cannot distinguish them. `unrecognized_note` says so rather
than picking one, which makes it strictly WEAKER than
`coverageUnrecognizedNote` (a static manifest, where "no such entry" is the
whole truth) — so the wording is not copied from it.

**5. Budget-awareness on standalone `codefit-scan-db` ships WITH `detail`,
not before it (D5).** Serving the index alone makes no size claim on its
own — a response that never grows past its light fields does not need one
yet, and shipping `over_budget: false` unconditionally would be an
assertion codefit never computed. The moment `detail` exists, though, the
response can grow by exactly the shape that made `CoverageResponse`
under-declare its own size before ADR 0078: a `182,440`-byte response
declaring `bytes: 21,951` and `over_budget: false`. Coverage's fix is
reused verbatim: `bytes` is measured LAST, over index + detail combined,
never the index alone; `index_bytes` reports the index's own share; and two
distinct notes — index-over (an authoring-shaped problem for coverage, but
for db.surface a fact about the schema's size, not an authoring mistake) vs.
detail-over ("ask for fewer ids per call") — say different, correct things.

## What this does NOT do

- It does not close roadmap P0-4's remaining half: a structural per-bucket
  cap/ranking for `db.surface`. There is still no common axis across its 18
  disjoint categories to rank or withhold by. At the measured 182.6 B/item,
  a 200-item db-only response is still ~36 KB — under the reproduction
  fixture's old fully-verbose 127 KB, but not under the 40,000-byte budget
  either, and `scan-all`'s budget note says so rather than implying the
  problem is solved.
- It does not make response size project-size-independent. 40,000 bytes is
  ~219 items of pure light index; a schema with more entries than that
  still needs the ranking this change does not build.
- It does not change `db.surface`'s baseline semantics, `StableID`, or any
  finding/fingerprint. Locked by a control matched against a pre-change
  golden's own `id` field (`scanall_regression_test.go`,
  `scanall_scoreweights_test.go`).

## Alternatives rejected

**Extend `maxDroppable`/`fitToBudget` to cover db.surface.** This needs a
ranking, which does not exist, and building one arbitrarily (e.g. by
category name, or insertion order) would be exactly the "clipped response
that reads like a complete one" defect ADR 0054/0048 forbid — an agent
reading a truncated db.surface with no declared ranking cannot tell what it
lost or why.

**Keep `db.surface` flat and add a separate `codefit-scan-db-detail`
tool.** Rejected on the same grounds ADR 0054 already settled for
endpoints: a second tool duplicates the fetch idiom `codefit-scan-endpoint`
and `codefit-coverage`'s `detail` parameter already established, for no
behavioral gain.

**Ship `over_budget` unconditionally, even before `detail` exists.** A
zero-value `false` the code never measured is an assertion, not a
declaration — the identical failure `codefit-coverage` had before ADR 0078
fixed it. Deferred to the slice that actually computes it.
