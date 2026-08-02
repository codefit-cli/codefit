# ADR 0044 — Source bytes become text before any tokenizer, and a source that yields nothing is DECLARED

**Status:** Accepted · **Date:** 2026-08-02 · **Phase:** 2 (RF-03 parser floor)

**Extends [ADR 0034](0034-neutral-model-completeness-contract-for-structure.md).**
0034's invariant, its per-table carriers and its measurement/diagnostics boundary
are untouched. What this ADR adds is a floor one level ABOVE everything 0034 and
[ADR 0043](0043-table-shaped-head-floor-and-withheld-session-scoped-tables.md)
reason about: both of them describe what happens INSIDE a file codefit
successfully read. Neither has anything to say about a file whose bytes never
became statements at all.

## Context

A schema dump produced by `pg_dump` under PowerShell is **UTF-16LE with a
byte-order mark**. That is not exotic; it is what the tool writes when its
output is redirected on Windows. Run verbatim through the real
`internal/sensors/db.Sensor.Audit`, a dump declaring **9 tables, 9 primary keys
and 11 foreign keys** measured:

```
Measured: true    Score: 100    Note: <empty>
0 tables, 0 views, 0 procedures, 0 triggers
Schema.Unreduced: 0   Schema.Withheld: 0
FINDINGS (0)   SURFACE ITEMS (0)
```

**No floor fired, because nothing was dropped.** ADR 0043's table-shaped-head
net never saw a `CREATE … TABLE` head, because in those bytes there is no such
head — there is `C\x00R\x00E\x00A\x00T\x00E\x00`. The reducer's `default:`
branch was never even a factor. Every carrier the completeness contract built is
empty, and `Measured`, `Score` and the finding count are IDENTICAL to a genuinely
clean scan. `sensors/db/db.go:110` names this exact scenario as the thing codefit
exists to catch, and codefit was producing it.

Measured across four encodings of the same real dump, before any change (the
same probe, before and after, is quoted in the PR):

| input | tables |
|---|---|
| UTF-16LE **with** BOM (the file as it sits on disk) | **0** |
| UTF-16LE, BOM stripped | **0** |
| UTF-8 with BOM | 18 |
| UTF-8, no BOM | 18 |

The cause is one line: `readSchemaSources` handed `os.ReadFile`'s raw bytes
straight to a byte-oriented tokenizer. Probing the other readers found the same
line in two more places (§2.5).

## Decision

### 2.1 Bytes become TEXT at the filesystem boundary, not in the parser

`internal/core/sourcetext` (a leaf, standard library only) decodes the three
BOM-marked encodings — UTF-8 `EF BB BF`, UTF-16LE `FF FE`, UTF-16BE `FE FF` —
and is called by the layer that opens the file.

That layer choice follows ADR 0014's own division rather than convenience: the
parser is FILESYSTEM-FREE and receives sources, while a byte-order mark is a
property of the FILE, not of the SQL in it. It also keeps
`providers.SourceFile.Content` a text contract for every provider at once,
instead of asking each new parser to remember a decode step — the fail-closed
reasoning of ADR 0034 §2.3, applied to a different obligation.

No new dependency was taken. `golang.org/x/text` is pure Go and would have been
admissible under the `CGO_ENABLED=0` constraint, but it is not currently a
dependency (absent from `go.mod` AND `go.sum`, checked rather than assumed), and
`unicode/utf16` in the standard library does the whole job in fourteen lines.

### 2.2 A BOM-LESS file is NEVER sniffed — it is DECLARED unreadable

This is the load-bearing half of the decoding decision.

Detecting BOM-less UTF-16 means inferring an encoding from NUL bytes at regular
offsets. The inference is unfalsifiable at the point of use, and a WRONG one
silently rewrites the content of a Latin-1 file, a file in a codepage nobody
here has heard of, or anything binary-ish that happens to fit the pattern. That
is a corruption strictly WORSE than the silence it would replace, because no
later layer could detect it: the tokenizer would receive plausible-looking text
and reduce it confidently.

So `Decode` returns a mark-less file BYTE-IDENTICAL, and the honest half of the
case is served by a separate, POSITIVE observation: `ContainsNUL` reports that a
NUL byte survived decoding. NUL is not legal content in any schema or source
file codefit reads. That lets the caller say "codefit could not read this as
text" — a checkable claim about the bytes in hand — without ever asserting what
the file actually was. **Declaring beats guessing**, and the declaration is what
keeps the refusal from becoming the very silence this ADR removes.

Boundaries, all deliberate and all tested: a UTF-16 file whose final code unit
is cut in half is decoded as far as it goes (returning `""` for a truncated file
would reintroduce the defect); unpaired surrogates become U+FFFD; a UTF-32LE
file begins `FF FE 00 00`, so its BOM reads as UTF-16LE and it decodes to text
full of U+0000 — reported by `ContainsNUL`, not silently misread.

### 2.3 THE DURABLE HALF: a configured source that yields NOTHING is reported

Decoding closes the cases that are known. The defect is the CLASS: some other
encoding, a truncated file, a format nobody has written a branch for, all
produce the same `Measured=true, Note="", zero of everything` state.

`sensors/db.unreadSources` therefore judges the OUTCOME, not the input — the
same shape-not-list reasoning ADR 0043 §2.1 gives for `reTableShapedHead`. A
configured schema source that contributed NO POSITION to the neutral model is
declared, whatever the cause.

"Contributed" is measured against the model's own `Pos` fields, INCLUDING its
honest-failure carriers (`Schema.Unreduced`, `Schema.Withheld`,
`Table.Unreduced`). This boundary is the sharpest one in the change and it runs
in the opposite direction from the rest: a file codefit read and DECLARED it
could not reduce is the OPPOSITE of an unread one. ADR 0034 and 0043 exist to
produce exactly that record; reporting it as "codefit read nothing here" would
undo their work, telling an agent the parser was blind at the precise point
where it spoke up.

Reading the MODEL rather than asking the parser is also what makes the floor
provider-agnostic: a future `SchemaParser` cannot forget to implement it,
because there is nothing for it to implement.

### 2.4 Total blindness is `Measured=false`; partial blindness is not

`Result{Measured,Note}` already carried this doctrine — "Measured=false with a
Note is the honest 'not audited' state ... distinct from 'audited, 0 findings'"
— and it had simply never been REACHABLE from a file codefit failed to read.
This ADR makes it reachable, and nothing about the vocabulary is new.

- **Every** configured source unread for a defect reason → `Measured=false`.
  The consequence is not cosmetic: `scanall.go` drops an unmeasured dimension
  from the weighted score, so the schema that produced a fake **100** now
  produces no db score at all. That is the correct answer to "how healthy is a
  schema codefit never read".
- **Some** sources unread → `Measured=true`, with the note naming the ones that
  were not. codefit DID audit what it could read, and reporting the whole scan
  as unmeasured would throw away real findings over one bad file.

A fourth trace joins `Result.Note` FIRST, ahead of the completeness inventory,
the withholding trace and the schema-gate verdict — the ordering rule
`joinTraces` already states, applied to a trace that qualifies all three: an
inventory of unproven tables says nothing useful about a file that never reached
the parser. It obeys the same three disciplines (aggregate by reason, state the
fact and its CONSEQUENCE and never a parser diagnosis, stay empty when there is
nothing to say), and its consequence sentence is the point of the whole change:
without it a reader sees "0 tables, 0 findings" and cannot tell whether codefit
read the schema and found it clean, or never read it at all.

### 2.5 An EMPTY file is legitimate — and is still reported

A genuinely empty or comment-only file is not a defect, and it does not change
`Measured`. It is nevertheless RECORDED, in its own wording, and that choice is
deliberate.

The whitespace/comment test is a UNION over the schema languages codefit reads
(SQL's `--` and `/* */`, MySQL's `#`, Prisma's `//`), not a per-dialect
tokenizer — a `--` inside a string literal reads as a comment opener here.
Reporting the benign case is what makes that imprecision SAFE: a misjudged file
still lands in the note, merely under the wrong one of two reported buckets.
Skipping the benign case would have converted every imprecision in that function
back into silence, which is the failure this ADR exists to end.

The over-report direction is declared rather than hidden: a migration whose only
content is `GRANT`, `INSERT` or `SET` statements contributes no position and IS
reported. The sentence is true — codefit read nothing structural from it — and
the alternative direction is the silence this ADR removes.

### 2.6 The other readers: probed, not assumed

`rg` found five user-facing `os.ReadFile` sites. Each was MEASURED through its
real caller, and they did not all behave the same way:

| reader | UTF-8 | UTF-8+BOM | UTF-16LE+BOM | UTF-16LE no BOM |
|---|---|---|---|---|
| `sensors/db` (schema) | 18 tables | 18 tables | **0 tables** | **0 tables** |
| `sensors/security` (source walk) | 1 finding, score 90 | 1 finding, score 90 | **0, score 100** | **0, score 100** |
| `mcp/scanall` cross extractor | 1 filter | 1 filter | **0 filters** | **0 filters** |
| `config.Load` (`.codefit.yaml`) | loads | loads | **loads** | hard ERROR |
| `scaffold/detect` (prisma provider) | detects | detects | falls back to "" | falls back to "" |

The first three are the same defect class and take the same decode. **Config is
NOT in the class and is not touched**: `yaml.v3` implements the YAML spec's own
byte-order-mark handling, so a UTF-16 config with a mark loads correctly and one
without fails LOUDLY (`control characters are not allowed`) — either outcome is
honest. `scaffold/detect` degrades to a default rather than to an all-clear.
`baseline`, `cache` and `cve/manifest` read artifacts codefit itself wrote.

The floor of §2.3 is applied to the DB dimension ONLY, and the asymmetry is
declared rather than accidental: schema sources are a small CONFIGURED list
where "this file reached no rule" is a fact about the developer's own
configuration, whereas the security sensor and the cross extractor walk a whole
repository, where a file yielding nothing is the ordinary case and a note per
such file would be noise, not signal. **Declared residual:** a BOM-less UTF-16
SOURCE file therefore remains silently unread for the security dimension and the
cross.

## Alternatives rejected

- **Sniff BOM-less UTF-16 from NUL positions.** Rejected, §2.2: a wrong guess
  corrupts silently and undetectably, which is worse than the silence.
- **Decode inside each parser.** Rejected, §2.1: it re-opens the obligation for
  every future provider and contradicts ADR 0014's filesystem-free parser.
- **Take `golang.org/x/text`.** Rejected: pure Go and admissible, but not
  currently a dependency, and `unicode/utf16` covers the whole need.
- **Carry the unread fact on `db.Schema` as a new core field.** Rejected: no
  rule reads it, and ADR 0034 §2.9's "no third channel invented" applies — the
  fact reaches the agent on `Result.Note`, the channel that already carries the
  sensor's own verdicts (schema gate, 3NF suppression).
- **Ask the parser "did you recognize anything in this file".** Rejected, §2.3:
  it is an interface change (ADR 0015) AND it fails open — a provider that
  forgets to answer reads as "everything fine".
- **Skip the empty/comment-only case instead of reporting it.** Rejected, §2.5:
  it turns every imprecision in the comment union back into silence.
- **`Measured=false` on a PARTIAL failure.** Rejected, §2.4: it discards real
  findings from the sources codefit did read.
- **Replicate the floor in the security sensor.** Rejected, §2.6: one note per
  file that yielded no finding, across a whole repository walk, is noise.

## Consequences

- The real dump, audited WHERE IT SITS with no conversion: 18 tables (9 real,
  all `StructureProven` with a primary key, 115 columns, 11 foreign keys, plus 9
  `*_id_seq` phantoms that are a separate, pre-existing defect), 23 surface
  items. Previously zero of everything.
- **28 vendored corpora, ZERO delta** — tables, proven counts, columns, primary
  and foreign keys, indexes, views, procedures, triggers, `Unreduced`,
  `Withheld`, every emitted item and the whole scan note are identical before
  and after. `Decode` is the identity on a mark-less file, which is what every
  correctly encoded file in the repository is. The measurement was proven
  sensitive by positive control (a sentinel row diffs).
- **No corpus could have caught a regression here**, which is its own finding:
  all 28 were UTF-8 with no byte-order mark. Three authored
  fixtures are the only control — `internal/sensors/db/testdata/twin_utf8.sql`
  and its two UTF-16LE twins — and they live under the SENSOR, not the parser,
  because that is where the decode is.
- `Result.Note` now has FOUR independent producers on one channel.
- A scan whose every schema file is unreadable no longer contributes a db score
  to the weighted global. A project that was silently getting 100 for its
  database dimension will see the dimension disappear and a note explaining why
  — a visible change, and the honest one.

## Related

ADR 0014 (the filesystem-free parser this decode respects), 0015, 0018 (the
declared subset), 0028 (coverage honesty), 0034 (the completeness contract this
extends), 0041 and 0043 (the two floors INSIDE a file that this one sits above).
