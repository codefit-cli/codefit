# ADR 0045 — A sequence is not a table, and a body item is anchored on its own line

**Status:** Accepted · **Date:** 2026-08-02 · **Phase:** 2 (RF-03 parser fidelity)

**Extends [ADR 0034](0034-neutral-model-completeness-contract-for-structure.md).**
0034's invariant, its per-table carriers and its measurement/diagnostics boundary
are untouched. §2.4's DECLARED-skip rule is what decides the first half of this
ADR, and §2.6's fabrication boundary is what decides the second: both defects are
FABRICATIONS, not drops, so `db.Table.Complete` structurally could not catch
either. The reducer believed it succeeded in both cases.

**Related to [ADR 0043](0043-table-shaped-head-floor-and-withheld-session-scoped-tables.md).**
0043 taught the `CREATE TABLE` family to declare what it cannot reduce. This one
teaches four OTHER branches not to invent a table out of a name that was never a
table's — the opposite direction, and the reason 0043's limit (v) is left
standing rather than widened (§2.6 below).

Both defects were found by the FIRST run of codefit's DB dimension against a real
project's PostgreSQL dump (a Spring/Hibernate schema, `pg_dump` 17 output). They
are unrelated in mechanism and are recorded in one ADR because they were measured
in one run and share one blast-radius measurement.

## Context

### A. `CREATE SEQUENCE` produced phantom tables

`pg_dump` writes one `CREATE SEQUENCE` per serial/identity column and then, for
EVERY relation it dumps, an ownership statement spelled with `ALTER TABLE` —
because PostgreSQL's `ALTER TABLE` legally accepts every relation kind for the
ownership actions:

```sql
CREATE SEQUENCE public.users_id_seq ...;
ALTER TABLE public.users_id_seq OWNER TO postgres;
ALTER SEQUENCE public.users_id_seq OWNED BY public.users.id;
```

`reduce.go` had no branch for `CREATE SEQUENCE` at all, so when the `ALTER TABLE`
arrived the name was unknown and `getTable` MATERIALIZED A TABLE from it.

Measured through the real `Sensor.Audit` over the real dump, before any change:

```
NOTE=codefit could not prove the structure of 9 table(s) complete — no CREATE
TABLE statement was ever seen for this table: _user_id_seq, batch_id_seq,
batch_log_id_seq, diagnosis_id_seq, inventory_item_id_seq (+4 more). ...
TABLES=18
   _user(cols=6,proven=true)
   _user_id_seq(cols=0,proven=false)
   ...
SURFACE=23
  db-table-structure-unproven db/dump.sql:54 "ALTER TABLE public._user_id_seq OWNER TO postgres;"
  ... (9 of these)
```

Nine sequences, nine phantom tables, **9 of the run's 23 surface items**. Each
one asks the agent whether a SEQUENCE declares a primary key, which a sequence
cannot have, and the note describes them as "9 table(s)" codefit could not read.
It scales one-to-one with sequence count: roughly one junk item per table on any
Hibernate schema.

The same mechanism runs for VIEWS. Measured on the vendored Pagila corpus, 21 of
its 23 unproven tables were not tables: 13 sequences, 7 views, and one
materialized view — the last of these materialized not by `ALTER TABLE` but by
`CREATE UNIQUE INDEX rental_category ON public.rental_by_category`.

### B. Body items were anchored one line early

```go
line := st.line + strings.Count(st.text[:innerStart+p.off], "\n")
```

`p.off` is the COMMA BOUNDARY — `splitTopLevelParts` starts each part at the byte
after the separating `,` — which sits BEFORE the newline that precedes the item's
own text. Every `CREATE TABLE` body item was therefore anchored one line early
(including the first, whose part begins right after the `(`).

Measured on the real dump: DB-053 reported `column: password` at line 33, whose
content is `lastname character varying(255),`. Reproduced on a minimal probe:

```
column id           reported line=1 -> that line's content: "CREATE TABLE public.users ("
column firstname    reported line=2 -> that line's content: "id bigint NOT NULL,"
column lastname     reported line=3 -> that line's content: "firstname character varying(255),"
column password     reported line=4 -> that line's content: "lastname character varying(255),"
```

The `ALTER TABLE` action loop shares the same offset convention and the same
defect. It is invisible on `pg_dump` output because `pg_dump` writes ONE action
per statement and `reAlterTable`'s own `\s+(.*)$` has already consumed the
newline before the first action — so only the SECOND and later actions of a
multi-action statement are affected. Confirmed by execution rather than assumed
(`line_anchor_test.go`, red run) before the second call site was touched.

**This is not cosmetic.** The baseline fingerprint is
`sha256(category ‖ file ‖ normalized content of the line at the anchor)`
(`sensors/db.stampFingerprints` → `findings.Fingerprint`), so a finding's
COMMITTED IDENTITY was bound to an unrelated column's text, and a surface item's
snippet quoted the wrong declaration back to the agent.

## Decision

### 2.1 Non-table relations are REMEMBERED, never modeled

`builder` gains `nonTableRelations map[string]bool`, filled by a new
`reCreateSequence` branch and by the existing `reView` branch. It is
reducer-internal only and adds NO model surface — the same discipline
`partSchemeFunc`/`partFuncStrategy` already use for T-SQL's partitioning
vocabulary, and the precedent this file already documents for "a statement worth
reading but not worth modeling".

A `CREATE SEQUENCE` is a DECLARED, RECOGNIZED skip in ADR 0034 §2.4's sense: it
can never declare a column, key or index of any table, so recording it as
incompleteness would be a false demotion. It is reported through NEITHER carrier:

- not `Schema.Unreduced` — that channel means "codefit could not read this", and
  codefit read it perfectly;
- not `Schema.Withheld` — that channel means "codefit read a TABLE and decided it
  does not belong in the persistent schema", and a sequence is not a table. Using
  it would need a new `WithheldReason` for a thing the model never had a place
  for.

### 2.2 One predicate, at every site that creates a table from a REFERENCE

`isKnownNonTable(name)` is consulted by the four branches that can materialize a
table from a name they never saw declared: `applyAlterTable`, `applyCreateIndex`,
`applyCreateColumnstoreIndex` and `markUnrecognizedIndexShape`. Sharing one
predicate is what keeps the four consistent rather than merely similar — the same
argument `markUnrecognizedIndexShape` itself already makes for sharing a floor.

The `b.tables` check comes FIRST and is load-bearing, not defensive: `normalizeName`
strips the schema qualifier, so a view `public.x` and a table `other.x` collapse
to one name. When they do, the TABLE wins, because the guard can only ever decline
to CREATE an entry and never touches one already in the model. Locked by
`TestSQLDDL_ViewAndTableCollideOnName_TheTableStillWins`, mutation-proven (dropping
the check loses the table's declared primary key outright).

### 2.3 REJECTED: "`OWNER TO` never creates a table"

The simpler rule — refuse to materialize a table when every action of an
`ALTER TABLE` is a recognized skip — fixes sequences, views and any future
relation kind with no registry at all. It was rejected because it is the wrong
FACT: it would also delete a genuinely declared table whose `CREATE TABLE` this
scan did not read, which ADR 0034's `ReasonTableNeverDeclared` exists to surface
and which is test-locked behavior.

The chosen rule is driven by POSITIVE EVIDENCE — codefit itself read the
`CREATE SEQUENCE`/`CREATE VIEW` — so it never guesses from the shape of an action
or the shape of a name. `TestSQLDDL_UnknownRelationOwnerTo_StillMaterializesNeverDeclared`
locks the rejected design out: `ALTER TABLE public.ghost_table OWNER TO postgres`
alone still materializes with `ReasonTableNeverDeclared`.

A NAME-driven variant (`strings.HasSuffix(name, "_seq")`) is locked out by the
same discipline, through
`TestSQLDDL_AlterBeforeCreateSequence_StillMaterializes` — mutation-proven: adding
the suffix heuristic makes that test fail, because the reducer is an incremental
fold over statements IN ORDER and codefit reports what it read, in the order it
read it.

### 2.4 An index over a view is DROPPED, not re-homed

`db.View` carries no index field, because the DB dimension's rules are about
tables. Registering a table to hold a materialized view's index would be exactly
the FABRICATION class ADR 0034 §2.6 says `Complete` cannot catch — strictly worse
than declaring the loss. An index FORM the reducer cannot read over a view still
reaches `Schema.Unreduced`, because that statement genuinely was not read; only
the fully-read one is dropped, and only because there is nowhere honest to put it.

### 2.5 A body item's anchor is its first non-whitespace byte

`part.textOff()` advances `off` past the part's leading whitespace, and both
`splitTopLevelParts` consumers count newlines to it. A part that is entirely
whitespace returns `off` unchanged rather than the offset past its end — there is
no item text to anchor on, and advancing across a blank fragment would only push
the anchor onto an unrelated line.

One interaction was found by the existing suite rather than by reasoning:
`applyTableItem`'s missing-comma residual (ADR 0042) counted its own newlines from
the RAW item start, which after this change double-counts the leading whitespace.
`TestSQLDDL_MissingCommaCut_ReportsTheConstraintsOwnLine` caught it; the residual
now counts from the item's text.

The correction is locked against the SOURCE, not against itself:
`TestSQLDDL_EveryVendoredCorpus_ColumnsAnchorOnTheirOwnSourceLine` parses every
`.sql` corpus under `testdata/` with the REAL parser and requires each column's
anchored line to CONTAIN that column's name — 195 anchors across 22 corpora. A
golden regenerated by the same defective code it is meant to guard proves nothing;
this does.

### 2.6 BOUNDARY: the registry is NOT extended to withheld temporary tables

ADR 0043's limit (v) — a later `ALTER TABLE`/`CREATE INDEX` naming a withheld
temporary table still materializes a phantom — stands UNCHANGED, and that is a
decision:

- a temporary table IS a table, so an absence-based question about one is not
  nonsense the way "does this sequence declare a primary key" is;
- PostgreSQL keeps tables, sequences and views in ONE relation namespace per
  schema, so a table/sequence/view name collision cannot legitimately happen —
  which is what makes §2.2's exclusion SOUND rather than heuristic. A temporary
  table and a persistent one, by contrast, routinely share a name across
  `pg_temp` and `public`, and this reducer's schema-qualifier stripping would
  collapse them. Extending the registry there would trade a phantom for a
  DELETED real table.

## Consequences

### Measured over 26 external corpora, both directions

Exactly TWO corpora move. **23 items removed, ZERO added.**

| corpus | removed | what they were |
|---|---|---|
| `pagila` | 21 | 13 sequences, 7 views, 1 materialized view |
| `adventureworks-oltp-pg` | 2 | 2 views |

Tables, structure-proven counts, columns, foreign keys, indexes, views,
procedures, triggers and paradigm are identical on the other 24 and unchanged
except for the removed phantoms on these two.

The zero was proven SENSITIVE with a positive control, because a zero is also
what a broken harness produces: a build with `isKnownNonTable` forced to `false`,
run through the same harness and the same 26 jobs, reproduces all 23 items
exactly (1130 items, matching the pre-change baseline; 1107 after).

Separately, 24 item groups MOVE LINE (§2.5) — every one of them onto the line the
item is written on, verified against the source. A move is not always exactly one
line, and the exception is measured rather than theoretical: a body item preceded
by a LINE COMMENT moves two, because `split()` removes the comment's text while
keeping its newline (`dw-barousse`'s flat mart, `skills_and_types` from 32 to 34).

### The three schema goldens

Regenerated in the line-anchor commit only. **64 changed values, every one a
`Pos.Line` going N → N+1, and no other field anywhere** — verified by extracting
every `[-+]` line of the diff and confirming zero of them is anything but
`"Line": <n>`, and that every removed/added pair differs by exactly one. No golden
moved for the non-table-relation change, because `testdata/` contained ZERO
`CREATE SEQUENCE` statements before this ADR's own fixture was authored
(`rg -c -i --glob '*.sql' '^\s*create\s+sequence' internal/providers/sqlddl/testdata/`
→ exit 1, no output) and no `ALTER TABLE <view> OWNER TO`.

### Baseline fingerprints — a real, user-visible break

`findings.Fingerprint` hashes the CONTENT of the line at the anchor. Correcting
the anchor therefore changes the fingerprint of every DB finding and surface item
anchored on a `CREATE TABLE` body item. **A committed baseline entry for one of
those stops matching, and the finding reappears as new until it is re-accepted.**

The blast radius is bounded and was measured, not estimated, on the real dump:

| item kind | anchor | fingerprint |
|---|---|---|
| DB-001 `db-fk-no-index` (×10) | single-action `ALTER TABLE` | unchanged |
| DB-052 `db-no-timestamps` (×3) | the table's `CREATE TABLE` line | unchanged |
| DB-053 `db-sensitive-unencrypted` (×1) | a `CREATE TABLE` body item | `4f416a0643e5` → `e5ffe4eced03` |

13 of 14 surviving fingerprints are byte-identical. Only body-item-anchored items
move — but for a project whose baseline holds DB-002/DB-003/DB-051/DB-053 entries
(all column-anchored), that is a breaking change to committed state, and it is
declared as such in `CHANGELOG.md` rather than left for a user to discover. codefit
is pre-1.0 and its baseline format carries no version negotiation, so the honest
disclosure IS the mitigation; nothing in the repo needs a schema-version bump,
because the fingerprint is not versioned data — it is derived, on every scan, from
the source it points at.

### On the real dump

23 surface items → 14. The nine phantom sequence items are gone, the per-scan note
is now EMPTY (it previously described 9 sequences as unreadable tables), and
DB-053 anchors on line 34, `password character varying(255),` — its own
declaration.

## Related

ADR 0018 (declared subset), 0034 (completeness contract — §2.4 and §2.6 both
decide here), 0041 (found residual), 0042 (missing comma; its residual anchoring
interacts with §2.5), 0043 (table-shaped floor and withholding; its limit (v) is
deliberately left standing by §2.6).
