# ADR 0040 — A delimited type name resolves at the canonical form, not per dialect

**Status:** Accepted · **Date:** 2026-08-01 · **Phase:** 2 (RF-03 OLAP closure)

**Supersedes in scope [ADR 0036](0036-schema-gate-sixth-signal-column-type-profile.md)
§Context ("Measured cause, found while building this") and its row for the two
AdventureWorksDW corpora.** 0036's decision — the sixth signal, its thresholds and
its measured 26-corpus selection — is untouched and still holds. What changes is a
PARSER FACT that ADR named as an open second gap and predicted would have to be
fixed separately. It has been.

## Context

`typeBase` (`internal/providers/sqlddl/types.go`) reduced a column's declared type
expression to a `TypeMap` lookup key by stripping a trailing length/precision
`(...)` and a trailing `[]` array marker. It did not remove the identifier
delimiters that WRAP a type name, and the dialect type maps are keyed on the bare
word — so every delimited type missed the lookup and fell back to `db.TypeUnknown`.

T-SQL writes types that way by default. Microsoft's own generated install scripts
delimit the type of *every* column:

```sql
[CustomerKey] [int] IDENTITY(1,1) NOT NULL,
[CustomerAlternateKey] [nvarchar](15) NOT NULL,
```

**Two measured consequences, both live on `main` before this change:**

1. **A false positive, not merely a silence.** DW-002 fired on `DimCustomer` and
   `DimDate` claiming neither is keyed by a surrogate, over DDL where `CustomerKey`
   and `DateKey` are single-column `[int]` primary keys — the exact shape DW-002's
   own doc comment cites as what must **not** fire. Across the 26-corpus survey,
   **18 of 26** `dw-dimension-no-surrogate-key` items were this artifact.
2. **A signal that could not reach two corpora.** `type_profile_split` (ADR 0036)
   fails closed above `maxUnclassifiedPct`, so it abstained on both AdventureWorksDW
   corpora — 74 of 74 columns unclassified on the vendored excerpt, 359 of 359 on
   the full upstream script.

## Decision

**Unwrap the CANONICAL identifier delimiter at the column-type lookup site.**

### 1. The fix is dialect-free, and a bracket strip would have been wrong

`split()` is the sole owner of quoting (ADR 0022) and re-emits every dialect-quoted
identifier canonicalized to ANSI `"…"` before `reduce.go` ever runs. A type name
occupies an identifier position, so by the time the lookup happens **`[int]`,
`` `int` `` and `"int"` have already collapsed onto the single form `"int"`** — the
reducer never sees a bracket at all.

That is why one unwrap at the canonical form closes **all three dialects at once**,
with no per-dialect branch and no new `Dialect` datum. MySQL backtick-quoted and
ANSI double-quoted type names were **probed on `main`** (`` `int` `` → unknown,
`"int4"` → unknown) and carry the identical defect; they are not a speculative
widening, they are the same code path with the same canonical input.

A fix written as "strip `[` and `]`" would have been both dialect-specific and
**dead code**, since no bracket survives tokenization.

### 2. The unwrap lives at the type lookup, NOT in `typeBase`

`typeBase` has a second caller — `isInlineKeyIndexForm` (`reduce.go`) — which asks a
different question of the same token: after a bare, unquoted leading `KEY`/`INDEX`,
is the next token a TYPE (so this is a column named `key`) or an index NAME?

There the delimiters are **the discriminator, not noise**: an author who delimits a
token in that position is naming an index precisely because the name collides with a
keyword, and MySQL — the dialect that has this inline form — spells its types bare.

Putting the unwrap into `typeBase` was tried as a **mutation** and it fabricates:
`CREATE TABLE t (a int, KEY \`int\` (a))` loses its index and gains a phantom column
named `KEY` of type `int`. So the composition is `typeLookupKey = unquoteTypeIdent ∘
typeBase`, used only by `applyColumn`, and the anti-corruption case is test-locked.

### 3. PostgreSQL's array marker cannot be disturbed

The trailing `[]` is a SUFFIX with a different meaning in the one dialect where `[`
is not an identifier quote at all, so it reaches `typeBase` verbatim. `typeBase`
strips it first; `unquoteTypeIdent` then removes only a matched pair of the canonical
`"` that WRAPS what remains. Neither step can see the other's marker. `"text"[]` —
both at once — is locked, and reversing the order was mutated and fails.

### 4. `db.TypeUnknown` stays the honest fallback

This maps a delimited **spelling** onto the same lookup key as the bare word. It does
not widen any vocabulary. An unrecognized keyword still lands on `TypeUnknown`, and
so does a form that is not exactly ONE quoted identifier — a schema-qualified user
type (`[dbo].[MyType]` → `"dbo"."MyType"`) is left untouched rather than
half-stripped into a fragment.

## Blast radius, measured over 26 public corpora (the ADR 0036 set)

Every consumer of `db.Column.Type` was enumerated from the code, not assumed:
DW-002, DW-005 (`timeDimensionByGrain`), DB-051, DB-053 (`storableSecretType`),
DB-003 (`uniformType`), the schema gate's `profileOf`, and `crossrules` DB-010
(`isLowCardinalityType`).

**Only DW-002's output changed anywhere: 26 items → 8. Zero items added by any
rule, on any corpus.** All 18 removals belong to the two AdventureWorksDW corpora;
the 8 survivors are text/varchar-keyed dimensions the rule should fire on.

| | before | after |
|---|---|---|
| vendored AdventureWorksDW excerpt, unclassified columns | 74/74 | **0/74** |
| full upstream `instawdbdw.sql`, unclassified columns | 359/359 | **6/359** |
| `dw-dimension-no-surrogate-key` items, all 26 corpora | 26 | **8** |
| all other rule item counts, all 26 corpora | — | unchanged |
| golden files | — | unchanged |

The 6 residual unclassified columns are the honest fallback still working:
`[sysname]` ×5 and `[xml]` ×1, real T-SQL types deliberately outside
`sqlserverTypeMap`.

**Positive controls, because a zero is not evidence on its own.** DB-051 and DB-053
were silent on AdventureWorksDW both before and after — but before the fix their path
was *dead* on delimited types (nothing can be a secret-holding type when every type is
unknown). A constructed T-SQL probe shows both now fire on delimited columns
(`db-sensitive-unencrypted` on `[Password] [nvarchar](200)`, `db-fk-text-type` on a
`[nvarchar]` FK referencing an `[int]` key), so the zero on the real corpus is a real
zero — confirmed independently by a `rg` sweep (exit 1, against a control pattern
returning 5 hits in the same file) showing that corpus declares no sensitive-token
column name at all.

**Schema gate.** `type_profile_split` newly REACHES both AdventureWorksDW corpora
instead of failing closed. On the full script it fires and joins `calendar_table` in
the deciding set. On the vendored 3-table excerpt it is evaluated and returns
**false**, on the arithmetic rather than on an abstention: one numeric pole
(`FactInternetSales`, 20 of 26 numeric) but only ONE text-dominated table
(`DimCustomer`; `DimDate` is 12 of 19 numeric and qualifies as neither), against
`textPoleMinTables = 2`. **No gate verdict changed on any corpus** — the full script
was already open on `calendar_table`, and its paradigm stays `mixed`.

**Directional-only consumers**, unchanged on these corpora and safe by direction:
DW-005's structural-grain test can now recognize a `[date]`-keyed calendar, which can
only make DW-005 quieter (fewer false "no time dimension" claims); `crossrules`
DB-010 can now see a `[bit]` column as low-cardinality and skip it, which is the
behaviour ADR 0032 FIX 2 specified. `crossrules` runs only in `scan-all` with a code
side and was not exercised by the corpus harness — recorded rather than claimed.

## Consequences

- **SQL-DDL known limit (8) is CLOSED**, in `dbcoverage.go` and mirrored in
  `COVERAGE.md`. The entry is kept, not deleted, because it names what the parser now
  does.
- **A test that locked the bug is gone, by its own instruction.**
  `TestSchemaGate_TypeProfileSplit_AbstainsOnBracketedTSQLTypes` asserted that a
  delimited fact table is 100% unclassified and told its successor to re-measure if
  the type map ever learned those names. It has. Its replacement,
  `TestSchemaGate_TypeProfileSplit_UnclassifiedBudget`, locks the same fail-closed
  budget with a fixture that produces *genuinely* unclassified types — and is
  two-sided (3 of 12 unclassified abstains, 2 of 12 fires), because the original
  could not fail when the budget was deleted and was therefore an ornament with
  respect to the mechanism it claimed to protect. That was proven by a surviving
  mutation before the rewrite.
- **Two AdventureWorksDW corpora become usable evidence** for any future rule that
  reasons over column types. Before this, the canonical reference warehouse was typed
  entirely `unknown`.

## Alternatives rejected

- **A `Dialect` datum listing each dialect's type delimiters.** Redundant: the
  tokenizer has already erased the distinction by the time the lookup runs, so the
  datum would carry information no consumer could use.
- **Adding bracketed keys to `sqlserverTypeMap`** (`"[int]"`, `"[nvarchar]"`, …).
  Doubles every dialect's vocabulary, encodes a spelling in a semantic table, and
  still misses the MySQL and ANSI forms.
- **`strings.Trim(s, "\"")` instead of a matched-pair check.** Strips an unbalanced
  delimiter and half-strips a dotted name into a fragment. Mutated; the unit test
  fails on `"unterminated`, `trailing"` and `"`.
