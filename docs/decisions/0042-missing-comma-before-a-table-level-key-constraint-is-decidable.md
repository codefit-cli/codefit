# ADR 0042 — A missing comma before a table-level key constraint is decidable, so it is recovered rather than fabricated

**Status:** Accepted · **Date:** 2026-08-02 · **Phase:** 2 (RF-03 OLAP closure)

**Closes known limit (12) of `internal/core/dbcoverage/dbcoverage.go` and
NARROWS [ADR 0034](0034-neutral-model-completeness-contract-for-structure.md)
§2.6.** 0034's invariant, dispositions and carriers are untouched. What this ADR
changes is the SIZE of the fabrication class 0034 declared out of scope: one of
its two named instances is closed, and the class is shown to be reachable on
real DDL rather than only on constructed input.

## Context

`splitTopLevelParts` splits a `CREATE TABLE` body on commas at paren depth 0. A
body item is therefore delimited by commas and by nothing else — so when a comma
is MISSING, two declarations arrive as one item, the item starts with a column
name, and everything after it reads as that column's INLINE modifiers.

Measured through the real parser, on all three dialects, both with an ordinary
`;` terminator and inside an ADR 0041 run-on:

```
### A/postgres ORDINARY ';'-terminated, MISSING comma before table-level PRIMARY KEY
  table=fact_reservation pk=[profit] cols=[car_sid profit] proven=true note="" unreduced=0

### C/postgres CONTROL: comma present
  table=fact_reservation pk=[car_sid profit] cols=[car_sid profit] proven=true note="" unreduced=0
```

The composite key the DDL declares is replaced by a single-column key named
after whichever column happened to precede the constraint, and the table still
reports `StructureProven()==true`. That is a FABRICATION of the ADR 0034 §2.6
class — the completeness contract structurally cannot see it, because nothing
was dropped: the reducer believes it succeeded.

It is PRE-EXISTING and DELIMITER-INDEPENDENT. ADR 0041 only made it REACHABLE on
a real corpus: `dw-kenap`'s `Fact_Reservation` is written this way and reported
`pk=[Profit]` where its DDL declares

```sql
PRIMARY KEY(Car_sid, Date_from, Date_to, Customer_sid, Point_sid, Category_sid)
```

Two further corpora were found by MEASUREMENT rather than by search:
`dw-salesmart` and `dw-ssis-salesmart` both write

```sql
calendar_month_name NVARCHAR(15) NOT NULL
CONSTRAINT pk_dim_date PRIMARY KEY CLUSTERED (date_key)
```

and reported `dim_date`'s primary key as `[calendar_month_name]` — a text column
— which then made `DW-002` ("dimension without a surrogate key") fire on a
dimension that declares a perfectly good integer surrogate. A fabricated key is
not an isolated wrong field: it propagates into the rules that read it.

### The sibling shapes, probed rather than assumed

Every shape below was run through the real parser under all three dialects
before any of them was designed for. All three dialects behaved identically,
which is the expected consequence of ADR 0015 (the reducer is dialect-free CODE;
the dialect supplies data).

| tail glued to `b INT` by a missing comma | before | class |
|---|---|---|
| `PRIMARY KEY (a, b)` | `pk=[b]` | fabrication |
| `PRIMARY KEY CLUSTERED (a, b)` | `pk=[b]` | fabrication |
| `CONSTRAINT n PRIMARY KEY (a, b)` | `pk=[b]` | fabrication |
| `UNIQUE (a, b)` | unique index on `[b]` | fabrication |
| `UNIQUE(a, b)` (no space) | index silently DROPPED | drop, `Complete=true` |
| `UNIQUE KEY n (a, b)` | unique index on `[b]` | fabrication |
| `CONSTRAINT n UNIQUE (a, b)` | unique index on `[b]` | fabrication |
| `FOREIGN KEY (a) REFERENCES o(x)` | FK on `[b]`, `RawType` of `b` corrupted to `"INT\nFOREIGN KEY (a)"` | fabrication |
| `CONSTRAINT n FOREIGN KEY (a) …` | FK on `[b]` | fabrication |
| `KEY n (a, b)` / `INDEX n (a, b)` | index dropped, `RawType` corrupted | drop, `Complete=true` |
| `CHECK (a > 0)` | identical to the comma-present reading | no defect |
| `CONSTRAINT n CHECK (a > 0)` | identical to the comma-present reading | no defect |
| `EXCLUDE USING gist (…)` | `RawType` corrupted; the constraint is a declared skip either way | partial |

## Decision

### 2.1 The boundary is DECIDED by the grammar, not guessed

In PostgreSQL, MySQL and T-SQL alike:

- a COLUMN constraint `PRIMARY KEY` or `UNIQUE` takes NO bare parenthesized
  column list. Every parenthesized continuation those grammars admit after an
  inline key is keyword-introduced — PostgreSQL's `index_parameters`
  (`INCLUDE (…)`, `WITH (…)`, `USING INDEX TABLESPACE …`), T-SQL's
  `WITH (…)` / `ON scheme (…)`;
- `FOREIGN KEY (…)` is not a column-constraint form at all. T-SQL is the only
  one of the three that admits the FOREIGN KEY keywords inline, and only as
  `FOREIGN KEY REFERENCES t (c)` — which always places `REFERENCES` and a table
  name between the keyword and the parenthesis.

So `<column definition> <head> (` has EXACTLY ONE legal reading: a missing
comma. `applyTableItem` therefore CUTS the item at the head and reduces both
halves, recursively, so a body item missing several commas recovers every
constraint in it. The residual starts AT the head, so on re-entry the head sits
at offset 0 and the rule declines — which is also why the recursion terminates.

### 2.2 RECOVER, not abstain — and what that rejected

The abstention alternative was to route such a table to the ADR 0034 floor
(`Complete=false`, `Schema.Unreduced`), turning the fabrication into a declared
unknown. It was rejected for the same reason ADR 0041 rejected declare-only:

- **it is not a guess.** §2.1 leaves one reading, so recovering it is reading the
  DDL, not repairing it. The project prefers honest abstention over guessing;
  this is not guessing.
- **the detection IS the boundary.** Anything able to declare the table unproven
  has already located the head — so abstaining pays the identical false-cut risk
  and returns nothing.
- **abstention costs the whole dimension on exactly the tables that matter.** The
  three affected corpora are warehouses, and the affected table is the FACT
  table in one of them; every DW rule abstains on an unproven table, so the
  honest-abstention reading would silence the dimension on a star schema whose
  structure is in fact fully readable.

Nothing is recovered on a guess: the residual is dispatched back through
`applyTableItem`, so a constraint whose column list cannot actually be read
still lands on `applyTableConstraint`'s pre-existing `MarkUnproven` floor.

### 2.3 The host is NOT demoted

The host column was read in full and the cut was made at a keyword that cannot
belong to it, so it is genuinely complete. Marking it unproven would be the
false demotion ADR 0034 §2.4 and ADR 0041 §2.5 both warn about.

### 2.4 The optional tokens are a CLOSED set, and that is the false-cut guard

`CLUSTERED`/`NONCLUSTERED` may precede the list of a table-level key and cannot
precede a bare `(` inline (there, `WITH` or `ON` always intervenes). A single
index NAME is admitted only after `KEY`/`INDEX`/`FOREIGN KEY`, never after a
bare `UNIQUE` — because PostgreSQL's inline `UNIQUE WITH (fillfactor = 70)` is
precisely the shape a one-identifier wildcard would mis-cut.

The two entries of `reservedHeadContinuation` a test can prove load-bearing are
`CHECK` and `DEFAULT`: they are the only legal inline continuations that are ONE
word followed directly by a parenthesis, so without them
`id INT UNIQUE KEY CHECK (other > 0)` and `id INT UNIQUE KEY DEFAULT (0)` are
cut and fabricate a unique index over the parentheses' contents. Both are locked
and mutation-proven. The remaining entries, `REFERENCES` included, are defensive
depth and are labelled as such rather than claimed as controls.

### 2.5 Three exclusions, the same three as ADR 0041 §2.3

`topLevelTokens` treats a single-quoted string, a canonical `"…"` identifier and
a parenthesized group as opaque. Each corresponds to a shape a dialect actually
writes — MySQL's `COMMENT='…'`, a column literally named `PRIMARY KEY` (which
`split()` re-emits quoted), and both dialects' `WITH (…)` — and each is locked by
a test that was mutation-proven to fabricate a key when its guard is removed. The
paren case is a deliberately adversarial input and is labelled as such in the
test, exactly as ADR 0041's was.

The walk shares `topLevelBytes` with `firstTopLevelStatementHead` (a pure
extraction, behaviour-identical) and still stays SEPARATE from
`firstTopLevelMatch`, which partitioning consults over text it must keep reading
byte-identically and which does not track identifier quoting.

### 2.6 A `CONSTRAINT <name>` preamble belongs to the residual

When a head is found, the rule walks BACKWARD over an immediately preceding
`CONSTRAINT <name>`. This is load-bearing twice: without it
`b INT CONSTRAINT pk PRIMARY KEY (a, b)` leaves the name dangling on the column,
and — much worse — the PROPERLY comma-separated item
`CONSTRAINT pk PRIMARY KEY (a, b)` is itself cut, because its own head does not
sit at offset 0. Mutation-proven: removing the walk breaks the Sakila and
AdventureWorks goldens.

### 2.7 A SECOND fabrication of the same class, closed in the same change

`applyColumn` scanned a column's raw modifier tail with `strings.Contains`, so
`b INT COMMENT 'PRIMARY KEY'` declared a key on `b` and
`b INT COMMENT 'NOT NULL'` reported `b` as non-nullable. The scans now read the
tail MASKED to top level (`maskNonTopLevel`), so a keyword inside a string
literal, a quoted identifier or a parenthesized expression is DATA, not syntax;
`REFERENCES` keeps reading the ORIGINAL text (its referenced columns live inside
the parentheses the mask blanks) and only requires the keyword itself to sit at
top level.

This is scope the missing-comma brief did not ask for, and it is included
because it is the same class — a fabricated key at `Complete=true` — it was
proven live by the guard tests written for the cut, and it is measurably free:
it changed nothing on any of the 29 corpora.

### 2.8 What is NOT covered, and why each is a decision

Each is locked as a characterization test (ADR 0034 §2.7), so the limit is
machine-visible rather than prose. Invert the case, do not delete it, when the
reducer learns the shape.

- **MySQL's bare `KEY`/`INDEX`/`FULLTEXT`/`SPATIAL` shorthand.** A bare `KEY` is
  ALSO a legal inline column modifier (it means `PRIMARY KEY`), so the head is a
  single word that can equally be a column or type NAME. Adding it was measured:
  it recovers the index correctly on the constructed shape, and it introduces a
  new fabrication on `b key(10)` — a column whose user-defined type is named
  `key` — which the existing `isInlineKeyIndexForm` discriminator also misreads.
  Zero of the 29 corpora carry the shape, so the trade buys nothing and costs a
  fabrication. Declared instead.
- **`CHECK` and `EXCLUDE`.** `CHECK` has a legal inline reading, and neither
  declares a key, index or column — so both readings reduce to the same model.
  Not a defect to fix.
- **`PRIMARY KEY USING BTREE (…)`, `UNIQUE NULLS NOT DISTINCT (…)`.** Extra
  tokens the closed optional set deliberately does not admit (§2.4).
- **A missing comma between two PLAIN column definitions.** No keyword marks the
  boundary at all; any rule that guessed one would be inventing a column name.

## Alternatives rejected

- **Abstain instead of recovering** — §2.2.
- **A general "cut at any constraint-ish keyword" vocabulary.** `CHECK`,
  `CONSTRAINT`, `KEY` and `DEFAULT (` are all legal inline, so widening the head
  set buys false cuts. Same shape as ADR 0041 §2.2's rejection of `WITH`/`SET`.
- **Allowing one identifier after a bare `PRIMARY KEY`/`UNIQUE`** (to catch
  `PRIMARY KEY USING BTREE (…)`). Rejected: it mis-cuts PostgreSQL's inline
  `PRIMARY KEY WITH (fillfactor = 70)` and T-SQL's
  `PRIMARY KEY CLUSTERED WITH (…)`, fabricating an index over storage
  parameters.
- **Cutting in `splitTopLevelParts`.** Rejected: that function is consulted by
  several callers over text with no grammar attached; the rule belongs where the
  item's KIND is already being decided.
- **Regex-based head matching.** Rejected: RE2 has no lookahead, so the
  `REFERENCES` and one-identifier conditions cannot be written safely, and the
  three opacity exclusions are clearer as one token walk than as three
  post-filters.

## Consequences

- **`dw-kenap`'s `Fact_Reservation`: `pk=[Profit]` → the six columns its DDL
  declares.** One false `DB-001` (`fk-no-index`) disappears with it: `Car_sid`
  is the recovered key's LEADING column, so its foreign key is covered by the
  primary key's prefix. The other five foreign keys still fire, correctly — they
  are in the key but not at its head.
- **`dw-salesmart` / `dw-ssis-salesmart`'s `dim_date`:
  `pk=[calendar_month_name]` → `[date_key]` / `[date_sk]`**, removing one false
  `DW-002` each.
- The measurement set is 29 jobs: the 26-corpus external survey ADR 0041 used —
  which already includes verbatim copies of the vendored fixtures — plus three
  jobs covering every `.sql` corpus under this repository's own `testdata`.
- **The other 26 of the 29 measured corpora are byte-identical** on tables,
  structure-proven
  count, columns, indexes, foreign keys, views, procedures, triggers, column
  `RawType`s, paradigm, every emitted item and the scan note. The measurement's
  own sensitivity was proven by a positive control: with the `(` requirement
  removed from the head rule, ALL 29 corpora change.
- **No golden was regenerated.** The repo's 18 fixtures carry no missing-comma
  body item under any dialect, and the cut is a no-op on every one of them.
- One new authored fixture,
  `testdata/tsql/constructed_missing_comma_table_constraint.sql`, transcribing
  the real shape with `;` terminators so the defect's delimiter-independence
  stays visible; measured into `gateCorpusExpectations` like every other corpus.

## Related

ADR 0015 (rules and reducer stay dialect-free code), 0018 (the declared subset),
0022 (per-dialect data), 0028 (coverage honesty), 0034 (the completeness
contract whose §2.6 boundary this narrows), 0041 (the run-on separation that
made this reachable, and whose tail-cut discipline this mirrors).
