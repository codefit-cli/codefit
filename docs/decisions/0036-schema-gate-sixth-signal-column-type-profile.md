# ADR 0036 — Schema gate, sixth signal: the column-type profile split, measured over 26 public corpora

**Status:** Accepted · **Date:** 2026-07-31 · **Phase:** 2 (RF-03 OLAP closure)

Extends ADR 0035 (schema gate, stage 1). It does **not** rewrite it: 0035's five
signals, its inertness decision and its vendored-corpus measurement all stand.
This ADR adds a sixth signal, and — more importantly — replaces 0035's
five-corpus reading with a 26-corpus one that changes what the gate's numbers
mean.

**Superseded in scope (2026-08-01) by
[ADR 0040](0040-delimited-type-names-resolve-at-the-canonical-form.md).**
The DECISION below — the sixth signal, its thresholds, and the 26-corpus
selection measurement — is untouched and still holds. What is no longer true is a
PARSER FACT this ADR named as an open second gap, and this ADR is not rewritten —
read the following together with 0040:

- **The "Measured cause, found while building this" paragraph describes a defect
  that is FIXED.** A delimited type name is now unwrapped at the canonical form
  before the `TypeMap` lookup, so the two AdventureWorksDW corpora no longer parse
  entirely unclassified: the vendored excerpt goes from 74 of 74 to **0 of 74**,
  and the full upstream install script from 359 of 359 to **6 of 359** (the 6 are
  `[sysname]` and `[xml]`, genuinely outside the T-SQL vocabulary).
- **The prediction in the first bullet after it came true, and is now spent.** It
  read: fixing `ALTER TABLE ... ADD CONSTRAINT` "will not move this signal's
  AdventureWorksDW row; the bracketed-type gap is separate and must be fixed too".
  Correct on both counts, and the second fix has now landed.
- **The `✗` rows for the two AdventureWorksDW corpora in the measurement table no
  longer abstain.** `type_profile_split` FIRES on the full install script (joining
  `calendar_table` in its deciding set, with the gate's verdict unchanged), and on
  the vendored 3-table excerpt it is evaluated and returns **false** on the
  arithmetic — one text-dominated table against `textPoleMinTables = 2` — rather
  than failing closed. **No signal's per-corpus verdict changed anywhere else, and
  the selection measurement that decided `decidingSignals` is unaffected.**
- **The test this ADR cites,
  `TestSchemaGate_TypeProfileSplit_AbstainsOnBracketedTSQLTypes`, no longer
  exists.** It was replaced by
  `TestSchemaGate_TypeProfileSplit_UnclassifiedBudget`, which locks the same
  fail-closed budget with genuinely unclassified types — see ADR 0040
  §Consequences for why the original could not fail under mutation.

**Re-measurement (2026-08-02) — the detection figure improved, and the precision
claim overstated its evidence base.** This ADR is not rewritten; read the whole
of the following together with the body. The DECISION — the sixth signal, its
thresholds, and "select the zero-false-positive signals rather than count all
six" — is untouched and comes out of the re-measurement better supported, not
worse. What moved is the measurement, and the reading of it.

Re-run on `main` at `4f81e85` through the same method the body describes: the
real providers and the real gate (`paradigm.WarehouseSignals`, `paradigm.Detect`)
driven by a throwaway harness over `internal/sensors/db.Sensor.Audit`, `auto`
config throughout, over the same 26 corpora at the same pinned commits.
**Positive control first**, for the same reason the body gives: four constructed
fixtures through the same harness, one per deciding signal plus an OLTP negative
— `calendar_table`, `surrogate_key_names` and `type_profile_split` each fire
**alone** on their own fixture, and the OLTP fixture fires none. No zero below is
a harness artifact.

**1. Four rows differ from the measurement table, and only ONE of them is new.**
Measured at `955e69d` — the very commit that last revised this ADR — and at
`4f81e85`:

| Corpus | table above | at `955e69d` | at `4f81e85` |
|---|---|---|---|
| `dw-kenap` | 1/1, no signal | 1/1, no signal | **7/7, `calendar_table`+`no_audit_timestamps`, gate OPEN, `olap`** |
| `awdw-full` | 31/2, `cal` + `✗`type | 31/31, `cal`+`aud`+`star`+`type` | 31/31, `cal`+`aud`+`star`+`type` |
| `vendored-awdw` | 3/0, `cal` + `✗`type | 3/3, `cal`+`aud` | 3/3, `cal`+`aud` |
| `pagila` | 92/70 | 92/69 | 92/69 |

Only `dw-kenap` moved after this ADR was written, and its cause is
[ADR 0041](0041-run-on-statement-separation-at-the-create-table-tail.md): its
`CREATE_DW.sql` declares 7 tables with no `;` between them, and before run-on
separation the reducer read one and discarded six. It now parses 7/7 proven,
fires `calendar_table` on `Dim_Date`, opens the gate and classifies `olap` with
6 dimensions and 1 fact. **The other three rows were already stale when this ADR
was last edited.** Two of them the ADR corrects in its own prose (the two
AdventureWorksDW rows, in the block above and in §Consequences); `pagila`'s
`92/70` is corrected nowhere and is simply wrong — it measures 92/69 at both
trees. Its verdict is unaffected in every run: no signal fires, the gate stays
shut.

**2. The corrected per-signal totals over the 26.** Changes in bold:

| Signal | fires on W | fires on O |
|---|---:|---:|
| `calendar_table` | **9** | 0 |
| `surrogate_key_names` | 3 | 0 |
| `type_profile_split` | **4** | 0 |
| `no_audit_timestamps` | **9** | 5 |
| `star_topology` | **7** | 5 |
| `bulk_load_shape` | 0 | 0 |

**3. The corrected counting table, and the argument it still makes.**

| Rule | fires on W | fires on O |
|---|---:|---:|
| ≥ 1 signal | 12 | 7 |
| ≥ 2 signals | 10 | 3 |
| ≥ 3 signals | 6 | 0 |
| ≥ 1 of {calendar, surrogate, type_profile} | **10** | **0** |
| ≥ 2 of {calendar, surrogate, type_profile} | 5 | 0 |

Selecting still beats counting, and by a wider margin than the body records:
**10 of 13 against 6 of 13** at the same zero false positives, where the body
read 9 against 5. Nothing about the decision needs revisiting.

**4. The precision claim rests on 9 corpora, not 13 — the zero is real, its base
was overstated.** Four of the 13 corpora in the transactional column parse to
**zero tables**: `vendored-sakila`, `vendored-pagila` and `vendored-aw-oltp`
vendor only views, procedures and triggers, and `jaffle-shop-dbt`'s dbt models
are `SELECT`s (a fact this ADR already records, for `jaffle_shop` alone). A
zero-table schema is below `minJudgeableTables = 3`, so `judgeable()` returns
false, `WarehouseSignals` returns empty evidence and `Qualifies()` is false **by
construction**. Those four corpora are structurally incapable of producing a
false positive; counting them as evidence of precision credits the gate with
work it never did. The correct statement is **zero false positives across the 9
transactional corpora that have parseable tables**.

**5. Two of the labels are wrong.** `tpch` is filed W. TPC-H is a
decision-support *benchmark* by purpose, but its schema is deliberately
normalized: eight tables (`NATION`, `REGION`, `PART`, `SUPPLIER`, `PARTSUPP`,
`CUSTOMER`, `ORDERS`, `LINEITEM`), an order-entry model with no date dimension,
no `_sk` vocabulary and no numeric-dominated table — verified by reading
`dss.ddl` at the pinned commit. It presents no dimensional evidence, so the gate
is **right** not to fire on it, and counting it as a miss understates the gate.
`jaffle-shop-dbt` is filed O and is dbt's canonical *analytics* demo; it is inert
either way at 0 tables.

**6. The honest restatement.** Shape-based analytic recall of **10 of 12** —
excluding `tpch`, which presents no dimensional evidence to detect — with **0
false positives across 9 evidentially non-empty transactional corpora**. Both
figures are robust to the relabelling in point 5: moving `jaffle-shop-dbt` to the
analytic column changes neither denominator, because a zero-table corpus is
excluded from both. The two real misses are `dw-barousse` and `dw-ngthao`.

**7. `dw-ngthao`'s miss is NOT a parser limit.** The 9/3 row invites the reading
that six unproven tables are what closes the gate. Tested by counterfactual:
running its **gold layer alone** (`Script/Gold/create_table.sql`) parses **3
tables, 3 proven, zero unclassified columns and 100% profiled coverage** — and
the gate still stays **shut**. It is a genuine three-way miss on evidence, not on
parsing: `fact_sales` is 5 numeric of 9 columns (55.6%, under
`numericPolePct = 60`), the schema declares no calendar table, and it uses no
`_sk` columns. The declared limit at the end of this ADR already names the first
of those three causes; what is new is that the parser is not among them.

## Context

ADR 0035 built five schema-wide signals and left them inert, to be MEASURED
before anything is wired. Two facts about that stage-1 measurement made it a poor
basis for stage 2's threshold:

1. It ran over the repository's own vendored corpora only — three-to-five-table
   OLTP excerpts plus one warehouse the parser cannot read. `bulk_load_shape`
   fired on ZERO of them, so its thresholds remained reasoned rather than
   measured, and 0035 says so.
2. All five signals read either NAMES or RELATIONAL STRUCTURE. The neutral model
   carries a third, strongly discriminating fact that no rule in codefit has ever
   asked for: `db.Column.Type` (`internal/core/db`, normalized per dialect by the
   provider, `RawType` preserved).

## Decision

### 1. A sixth signal, `type_profile_split`, over column types

Shapes first, numbers second:

- a FACT-like table is **numeric-dominated** — dimension keys plus numeric
  measures, near-zero descriptive text;
- a DIMENSION-like table is **text-dominated** — a key plus many descriptive
  attributes;
- an ordinary OLTP table is a **MIX**, and usually carries datetime/bool too.

**The signal is not per-table, and cannot be.**
`order_items(order_id int, product_id int, quantity int, price float)` has an
exact fact profile — and so do northwind's `order_details` (5 columns, 5
numeric), chinook's `invoice_line` (5/5) and 22 of pagila's payment partitions.
What a warehouse has and a transactional schema does not is the SCHEMA-LEVEL
BIMODALITY: it splits into a few numeric-dominated tables plus several
text-dominated ones. An OLTP schema does not split.

Per table (`profileOf`), each column falls in exactly one bucket:
`int|float` → numeric, `string|text` → descriptive, `bool|datetime|json|bytes|
enum` → classified-but-neither (counted in the denominator only), **anything
else → unclassified**.

| Threshold | Value | Where the value comes from |
|---|---|---|
| numeric-pole share of columns | ≥ 60% | measured identically at 50/60/70/80%; only at 90% do real warehouse facts drop out — 60 is the middle of the flat region |
| numeric-pole descriptive cap | ≤ 15% | allows exactly one degenerate-dimension text column in eight; at 5% two real warehouses' facts drop out; 10–30% measured identically |
| numeric-pole width | ≥ 8 columns | **the load-bearing one.** At 7 or below pagila fires; at 4 sakila fires too; at 8 no transactional corpus fires. Real facts measured 9–34 columns wide |
| text-pole descriptive share | ≥ 50% | the least "dominated" can honestly mean; measured real dimensions run 55–85% |
| text-pole width | ≥ 4 columns | keeps a 2-column (id, name) lookup from being descriptive by arithmetic. Measured identically at 2, 3 and 4 — **reasoned, not measured** |
| numeric-pole tables | ≥ 1, and ≥ 10% of profiled tables | synapse has ONE wide numeric table (`room_stats_current`, an aggregate) among 133 profiled = 0.75%; the lowest real warehouse this catches is 20%. 10% has a 13x margin below the false positive |
| text-pole tables | ≥ 2 | "a few numeric-dominated tables plus SEVERAL text-dominated ones"; one text table is a lookup table and every schema has one |
| unclassified budget per table | ≤ 20% | fail-closed; measured identically 0–99% — **reasoned, not measured** |
| profiled coverage of the schema | ≥ 50% of tables | no-vacuous-truths, applied to a distributional claim; measured identically 0–90% — **reasoned, not measured** |

The shared 3-table floor (`minJudgeableTables`) governs it like the other five,
and is also the floor on PROFILED tables.

### 2. Fail closed on `TypeUnknown` — and it is not hypothetical

A profile computed over types the parser did not classify is a guess, so a table
over the 20% unclassified budget is not profiled at all. The `unclassified`
branch is the type switch's **default**, so both `db.TypeUnknown` and the zero
value `""` (what a `db.Column` nobody typed carries) land there, as does any
`db.Type` added to the core later — the safe direction.

**Measured cause, found while building this:** the real AdventureWorksDW install
script parses with **359 of 359 columns unclassified**, and the vendored excerpt
with **74 of 74**. The reason is a second, independent parser gap: that DDL
brackets its type names (`[int]`, `[nvarchar](50)`), `typeBase`
(`internal/providers/sqlddl/types.go`) strips only a trailing `(...)` and `[]`,
and the dialect type map's keys are unbracketed — so every lookup misses and
returns `TypeUnknown`. Verified by direct probe (a two-table fixture, bracketed
vs unbracketed, same dialect) and locked with real DDL in
`TestSchemaGate_TypeProfileSplit_AbstainsOnBracketedTSQLTypes`.

Two consequences worth stating plainly:

- fixing the `ALTER TABLE ... ADD CONSTRAINT` gap (the prerequisite ADR 0035
  named for stage 2) will **not** move this signal's AdventureWorksDW row; the
  bracketed-type gap is separate and must be fixed too;
- this is NOT a T-SQL-wide blindness. Five of the seven T-SQL corpora measured
  here (`dw-salesmart`, `dw-ssis-salesmart`, `dw-ngthao`, `dw-kantor`,
  `dw-gravity`) declare unbracketed types and parse with **zero** unclassified
  columns; one of them, `dw-kantor`, is one of the three corpora the new signal
  fires on. Only the two AdventureWorksDW corpora are affected.

### 3. A completeness lock on the inertness lock

The stage-1 structural lock enumerates gate symbols by hand. Adding a sixth
signal is exactly when such a lock silently stops covering what it names, so
`TestSchemaGate_EveryGateSymbolIsEnumerated` now parses `schemagate.go` and
requires every declaration in it to appear in that list. It found three symbols
the list had never named — `Has`, `Count` and `allSpokesAreLeaves` — on its first
run.

Stage 1 remains INERT. All three locks pass, and the mutation runs proving each
of them can fail are recorded in the change that introduced this ADR.

## The measurement

26 corpora — 13 analytic, 13 transactional — parsed through the real providers
and the real gate (`paradigm.WarehouseSignals`), via a throwaway harness driving
`internal/sensors/db.Sensor.Audit`. Twenty-two are public clones, pinned by
commit below; four are verbatim copies of this repository's OWN testdata
(the `vendored-*` rows), measured through the same harness as a cross-check
against `schemagate_corpus_test.go`. **Nothing was vendored INTO the
repository** — the clones live outside the tree and this change adds no corpus
file.

**Positive control first**, because a zero from a broken harness is worse than no
measurement. Six constructed fixtures, one per signal, through the same harness:
every signal fires on its own fixture — `calendar_table`, `surrogate_key_names`,
`bulk_load_shape`, `no_audit_timestamps`, `star_topology`, `type_profile_split`
(the last also fires `bulk_load_shape`, its columns being `*_key` with no FKs).
No signal's zero below is a harness artifact.

Legend: `cal` calendar_table · `sk` surrogate_key_names · `bulk` bulk_load_shape ·
`aud` no_audit_timestamps · `star` star_topology · `type` type_profile_split.

| Corpus | Kind | tables/proven | cal | sk | bulk | aud | star | type |
|---|---|---|:-:|:-:|:-:|:-:|:-:|:-:|
| tpcds | W | 25/25 | ● | ● | | ● | ● | ● |
| tpch | W | 8/8 | | | | ● | | |
| dw-kantor | W | 4/4 | ● | | | ● | ● | ● |
| dw-gamerec | W | 5/5 | ● | | | ● | | ● |
| dw-p4pa | W | 17/17 | ● | | | ● | ● | |
| dw-salesmart | W | 7/7 | ● | | | | ● | |
| dw-ssis-salesmart | W | 5/5 | ● | ● | | | ● | |
| dw-gravity | W | 4/4 | | ● | | ● | | |
| dw-barousse | W | 17/16 | | | | | ● | |
| dw-ngthao | W | 9/3 | | | | | | |
| dw-kenap | W | 1/1 | | | | | | |
| awdw-full | W | 31/2 | ● | | | | | ✗ |
| vendored-awdw | W | 3/0 | ● | | | | | ✗ |
| northwind_psql | O | 14/14 | | | | ● | ● | |
| synapse | O | 134/134 | | | | ● | ● | |
| test_db (employees) | O | 6/6 | | | | ● | ● | |
| chinook | O | 11/11 | | | | ● | | |
| sakila-full | O | 16/16 | | | | ● | | |
| dub (Prisma) | O | 83/83 | | | | | ● | |
| formbricks (Prisma) | O | 53/53 | | | | | ● | |
| pagila | O | 92/70 | | | | | | |
| adventureworks-oltp-pg | O | 70/0 | | | | | | |
| jaffle-shop-dbt | O | 0/0 | | | | | | |
| vendored-sakila / -pagila / -aw-oltp | O | 0/0 | | | | | | |

`✗` = ABSTAINED because its column types are unclassified (the bracketed-type
gap above), which is a different fact from "did not fire".

Per-signal totals over the 26:

| Signal | fires on W | fires on O |
|---|---:|---:|
| calendar_table | 8 | 0 |
| surrogate_key_names | 3 | 0 |
| type_profile_split | 3 | 0 |
| no_audit_timestamps | 6 | 5 |
| star_topology | 6 | 5 |
| bulk_load_shape | 0 | 0 |

### What the numbers say about a stage-2 threshold

Counting signals is the wrong shape. Over these 26 corpora:

| Rule | fires on W | fires on O |
|---|---:|---:|
| ≥ 1 signal | 10 | 8 |
| ≥ 2 signals | 7 | 3 |
| ≥ 3 signals | 5 | 0 |
| ≥ 1 of {calendar, surrogate, type_profile} | **9** | **0** |
| ≥ 2 of {calendar, surrogate, type_profile} | 4 | 0 |

A count-based ">= 3 of any" reaches 5 of 13 warehouses. **Selecting the three
zero-false-positive signals and requiring ONE of them reaches 9 of 13, still
with zero transactional corpora** — better recall AND the same precision, from a
simpler rule. The two noisy signals (`no_audit_timestamps`, `star_topology`) fire
5 times each on transactional schemas and carry almost no information about
warehouse-ness; including them in a count is what forces the threshold up to 3
and costs the recall.

### Redundancy and independence

- `bulk_load_shape` fires on NOTHING in 26 corpora. Empirically it is inert; its
  thresholds remain unmeasured, as 0035 already declared.
- `no_audit_timestamps` and `star_topology` are the noise pair (Jaccard 0.38,
  5 transactional fires each). They are not redundant with each other by fire
  set, but they are equally uninformative.
- `calendar_table` is the single strongest signal (8/0).
- `surrogate_key_names` adds one corpus (`dw-gravity`) that `calendar_table`
  misses — genuinely independent work.
- `type_profile_split` fires on `{tpcds, dw-kantor, dw-gamerec}`, which is a
  strict SUBSET of `calendar_table`'s fire set. **On this corpus set it adds no
  new positive**; it adds corroboration, and it reads a completely different
  input (types, not names), so it survives the failure mode `calendar_table`
  cannot: a warehouse whose calendar is spelled `date_dimension` or
  `dim_fiscal_date` — the declared limit 0035 already carries. Its independence
  is mechanical, not yet demonstrated by a corpus, and this ADR does not claim
  otherwise.
- **Does it beat `bulk_load_shape`?** Yes, unambiguously: 3 correct fires versus
  0 of anything, and every threshold in it except three (text-pole width,
  unclassified budget, coverage floor — all labelled above) was set by
  measurement, including one that was set by an actual false positive.

### Corpora, pinned

Analytic: `github.com/gregrahn/tpcds-kit@5a3a817` ·
`github.com/electrum/tpch-dbgen@32f1c1b` ·
`github.com/KamilCoolas/KantorDWHProject@f6f34b9` ·
`github.com/guimatheus92/Game-Recommendation-System@7595d0c` ·
`github.com/pagopa/p4pa-db@517e34a` ·
`github.com/Al-Moatasem/sales-data-mart@33f44a1` ·
`github.com/3amory99/Building-Sales-Data-Mart-Using-ETL-SSIS@fbd259c` ·
`github.com/3amory99/Gravity-Books-Sales-End-to-End-Project@b8d0e51` ·
`github.com/lukebarousse/SQL_Data_Engineering_Course@d3001e1` ·
`github.com/ngthaonguyen0110/SQL-DataWarehouse@01188e5` ·
`github.com/Krzy-Doma/DataWarehouses@f3a4cbe` ·
`github.com/microsoft/sql-server-samples@b47eadc`
(`samples/databases/adventure-works/data-warehouse-install-script/instawdbdw.sql`)

Transactional: `github.com/pthom/northwind_psql@cd0ef28` ·
`github.com/element-hq/synapse@f61950d` ·
`github.com/datacharmer/test_db@e324b56` ·
`github.com/lerocha/chinook-database@7f67772` ·
`github.com/jOOQ/sakila@e089a5b` · `github.com/devrimgunduz/pagila@4792d22` ·
`github.com/lorint/AdventureWorks-for-Postgres@b474991` ·
`github.com/dubinc/dub@142b0c1` · `github.com/formbricks/formbricks@9b3100b` ·
`github.com/dbt-labs/jaffle_shop@fd7bfac`

`jaffle_shop` is listed to record a NEGATIVE result about method, not about a
schema: a dbt project's models are `SELECT`s, not DDL, so it parses to zero
tables and no dbt project can be measured this way at all.

## Consequences

- Nothing codefit detects or reports changes. `internal/core/dbcoverage` and
  `COVERAGE.md` stay untouched, for the reason 0035 gave: announcing inert
  machinery as coverage is over-promising.
- Vendored rows were expected to MOVE once the parallel T-SQL
  `ALTER TABLE ... ADD CONSTRAINT` fix landed, and they did — measured rather
  than predicted, since that fix (PR #82) is now on `main`:
  `tsql/adventureworksdw_real_objects.sql` parses 3/3 proven with its real
  primary keys and 8 foreign keys, and the row that actually moved is
  **`no_audit_timestamps`**, which stopped abstaining on unproven structure and
  now affirms. `star_topology` and `bulk_load_shape` kept the same ANSWER but
  changed their REASON, which is the more interesting half: both used to abstain
  for want of proof, and now conclude on evidence — eight declared foreign keys
  falsify `bulk_load_shape`'s no-FKs premise, and six of those eight reference
  dimension tables this three-table excerpt does not vendor, so `star_topology`
  cannot establish that its spokes are leaves. As stated here, the corpus's
  `type_profile_split` answer did NOT move: the bracketed-type gap is untouched
  and still open.
- Stage 2 inherits a concrete recommendation with counts behind it: select
  signals by measured precision, do not count them.

## Declared limits carried by this signal

- A fact table whose date keys are spelled as `datetime` rather than integer keys
  reads as mixed and is not a pole (`dw-ngthao`'s `fact_sales` — 9 columns, 5
  numeric, 3 datetime — is exactly this).
- A dimension carrying many integer codes alongside its text attributes is not
  text-dominated (`dw-salesmart`'s `dim_product`, 14 columns, 6 numeric / 6
  descriptive). Loosening the text pole to 40% would have caught two more
  warehouses with still zero false positives; it was NOT done, because at 40%
  the word "dominated" stops being true and the text pole is the half that does
  not discriminate anyway.
- A warehouse whose fact pole is a small fraction of a mostly-staging schema is
  missed by the 10% share floor (`dw-barousse`, 1 fact-shaped table in 16).
- Three thresholds are reasoned rather than measured, and are labelled as such in
  both the table above and the source.
