# 0058 — A declared limit can go stale, and nobody re-verifies it

**Status:** accepted · **Date:** 2026-08-05 · **Phase:** 3, priority P0-2
(`docs/roadmap.md`) · **Spec:** `sql-ddl-phantom-index` (SDD change, Engram) ·
**Builds on [ADR 0034](0034-neutral-model-completeness-contract-for-structure.md) and
[ADR 0022](0022-per-dialect-data-descriptor-for-sql-ddl-parsing.md)**

## Context

`internal/providers/sqlddl`'s inline-index discriminator (`isInlineKeyIndexForm`,
`reduce.go`) decides whether an item like `fulltext tsvector NOT NULL` inside a `CREATE
TABLE` body is a COLUMN or MySQL's inline secondary-index shorthand (`KEY name (cols)`).
Before this change, it looked only at whether the token after the leading keyword
(`KEY`/`INDEX`/`FULLTEXT`/`SPATIAL`) was a type the dialect's `TypeMap` recognized. An
unmapped type — PostgreSQL's own `tsvector`, which real Pagila uses for its `film.fulltext`
column — was read as "not a type I know", and the whole item was routed to the index
dispatch regardless of whether a parenthesized column list followed it at all.

### What the manifest said, and for how long it was wrong in two different ways

`dbcoverage.go` limit (5) / `COVERAGE.md` declared this as a **fabrication**: "the column is
silently dropped and a phantom zero-column index is fabricated in its place instead...not
yet fixed." That sentence was accurate once. It stopped being accurate on **2026-07-31**,
four days before this change, when `tsql-alter-add-constraint` landed the FABRICATION GUARD
in `applyTableConstraint` (`cols := parenCols(c); if len(cols) == 0 { t.MarkUnproven(...);
return }`) for an unrelated reason — closing a T-SQL `ALTER TABLE ... ADD CONSTRAINT`
fabrication class. That guard runs on **every** path into `applyTableConstraint`, including
this one, so the moment it landed, this bug's behavior silently changed from *fabricate a
phantom index* to *drop the column and abstain honestly* (`Complete=false`). Nobody
re-verified the manifest's specific claim against the new code, so it kept describing a
fabrication that had already stopped happening — in the **safer** direction (an abstention
misdescribed as a fabrication overstates the danger, not understates it), but wrong
nonetheless, and wrong in a way that would have sent an agent looking for the wrong symptom.

`dbcoverage.go`'s DW-002 documentation (line 39) separately claimed a specific consequence
was "reachable, not hypothetical": a warehouse-paradigm dimension shaped
`(id integer NOT NULL, fulltext tsvector NOT NULL, PRIMARY KEY (fulltext))` would have its
`fulltext` column dropped by limit (5) while the table-level `PRIMARY KEY (fulltext)`
constraint (a separate statement, with its own parenthesized column list) survived into the
model — so DW-002's surrogate-key check would see a one-column, non-composite primary key
and (correctly) judge `tsvector` not a provable integer surrogate. **That claim was never
measured before it was written.** `dw002.go` abstains on `!t.StructureProven()` *before* it
ever reaches the composite/integer surrogate test — and the *same* drop that removed
`fulltext` also demoted the table itself to `Complete=false`. Built and run in a `git
worktree` of the pre-fix tip: DW-002 does **not** fire on this exact shape. The citation
described a path that was never actually reachable, and the manifest asserted it anyway.

### The corpus was shrunk to dodge the bug, so the bug was unmeasurable

`internal/providers/sqlddl/testdata/pagila_excerpt.sql` omitted the `film` table entirely —
not trimmed, the *whole table* — with an inline comment citing this exact bug as the reason.
`pagila_test.go` excluded it from expected tables to match. The corpus was made to avoid
exercising its own known limit, which means no test in the tree could ever have caught either
staleness above: a fixture built to dodge a defect cannot measure that defect going stale.

## Decision

### 1. Close the drop: a positive column test, not paren-presence

`isInlineKeyIndexForm` now splits off the type expression's trailing modifiers
(`splitTypeAndMods`, the same split a column definition itself already uses) and asks
whether exactly one bare token remains with no `(` in it. Two simpler alternatives were
tried and rejected:

- **Bare paren-presence** (`no '(' → column`) regresses T-SQL's paren-less inline index
  (`INDEX ix CLUSTERED COLUMNSTORE`) from honest abstention into a fabricated column named
  `index`.
- **Ignoring modifiers** misreads a column whose `DEFAULT`/`GENERATED` tail carries parens
  (`fulltext tsvector NOT NULL DEFAULT to_tsvector('')`) as the index form, because the
  parens are inside the tail, not the type token itself.

Grammar shape stays in the dialect-free reducer (consistent with ADR 0022's descriptor
boundary: `TypeMap`/`Modifiers` are per-dialect *vocabularies*, not grammar), consuming only
data the descriptor already exposes.

### 2. The corpus is restored before the fix, so the fix is measured on real DDL

`film` is restored to `pagila_excerpt.sql`, verbatim, fetched directly from upstream Pagila
(commit `5ba5a57`, the commit the fixture already cites) — not hand-typed, not from memory.
This lands in its own commit, before the parser fix, so the red is visible: 13 of 14 columns,
`StructureProven()==false`. Regenerating the golden at that point would have baked the lossy
read in and erased the red the whole change exists to show.

### 3. A residual is declared, not silently left standing

`<kw> <unmapped-type>(args)` (`fulltext tsvector(10)`, `spatial geometry(Point,4326)`) is
structurally identical to a named inline index (`KEY idx(a)`) and stays undecidable without
reserved-word knowledge, which this discriminator deliberately does not carry (adding it
would trade one narrow bug for an unbounded per-dialect reserved-word maintenance surface).
It still fabricates an index from the type's own arguments. Locked as a characterization
test (`internal/providers/sqlddl/limits_test.go`,
`TestResidualParenType_StillFabricatesAnIndex_DeclaredLimit`) so it cannot silently get worse.

### 4. Both stale manifest claims are corrected in the same change, not just the code

`dbcoverage.go` limit (5) now states the actual sequence — narrowed, not newly fixed from
nothing; the fabrication had already stopped four days before anyone said so — and the L39
DW-002 sentence is rewritten from what was actually measured (never reachable pre-fix; newly
reachable, and correctly diagnosed, post-fix), not from what seemed plausible when it was
written. `COVERAGE.md` mirrors both, verified against the source afterward per this
project's fuente-antes-que-espejo discipline.

## Consequences

- **The declared-limit-ages lesson, stated for reuse.** A declared limit is not
  self-maintaining. This project is disciplined about *declaring* what it does not cover,
  but nothing re-verifies that a declared limit is *still true* once the code beneath it
  changes for an unrelated reason. The FABRICATION GUARD closed this bug's worse half as a
  side effect and nobody noticed for four days — not because anyone was careless, but because
  no mechanism asks the question. This ADR does not build that mechanism (a general
  "re-verify every declared limit on every change" control is its own, larger project); it
  records the pattern so the next declared-limit audit knows to look for it.
- **A fixture shrunk to dodge a defect is complicit in hiding it.** `pagila_excerpt.sql`'s
  missing `film` table is the second instance of this exact pattern found in this project
  (see the `sql-ddl-phantom-index` proposal's addendum). Worth searching the rest of the
  declared-limit inventory for the same shape: a corpus that avoids exercising what it
  documents as a limit cannot be used to tell whether that limit is still accurate.
- **`db.go`'s `Complete` field doc comment needed no edit.** Verified: it does not name
  `film.fulltext` and its "DROPS, not FABRICATIONS" boundary was already accurate for both
  the drop this change closes and the residual fabrication it declares. Locked byte-identical
  by `internal/core/db/complete_doccomment_test.go` (a go/ast hash lock, same discipline as
  `internal/core/crossrules/structureproven_debt_test.go`) rather than edited to match —
  editing an already-correct comment to "match" a change would be exactly the kind of
  unverified touch this ADR is about.
- **DB-011b gained a test it should already have had.** `isStrictPrefix`
  (`internal/core/dbrules/db011prefix.go`) already guarded against a zero-column index
  (`len(short) == 0`), unlike DB-011a, which had one before this change
  (`TestDB011_TwoZeroColumnIndexes_NeverFlaggedAsDuplicates`). Mirrored here
  (`TestDB011Prefix_ZeroColumnIndexNeverSubsumesOrIsSubsumed`) — a lock on existing correct
  behavior, not a fix, found only because the proposal phase re-verified a claim ("this guard
  doesn't exist") against the code rather than trusting it.
