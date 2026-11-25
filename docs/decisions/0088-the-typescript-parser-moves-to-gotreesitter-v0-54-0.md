# 0088 — The TypeScript parser moves to gotreesitter v0.54.0

Date: 2026-11-25
Status: accepted
Amends [ADR 0002](0002-typescript-parser-gotreesitter.md): the pinned version,
and the conclusion of its caveat 2. Issue `#189`.

## Context

The TypeScript and TSX provider parses with
`github.com/odvcencio/gotreesitter`, pinned at `v0.20.2` since ADR 0002. The
parser is error-recovering: given a form its grammar does not accept, it still
returns a tree, and on `v0.20.2` that tree's root was often an `ERROR` node. The
rules and the surface queries then walk a different tree, and everything after
the form stops being reported. Issue `#189` is the report: an ordinary line of
valid TypeScript, and the hardcoded credential below it disappeared from the
scan.

ADR 0002 accepted the parser on the condition that its safety caps "do not bite
typical project code", proven on three files. A census measured the condition
on real code instead, and it is false — not because of the caps, but because of
the grammar. Over 1,587 unique TypeScript files from four real projects, built
with the release flags (`-s -w`, the `grammar_subset` tags, linux/amd64),
`v0.20.2` parsed **219** files with an error (41 `.ts`, 178 `.tsx`), almost all
from ordinary forms:

| form (first error of the file) | `.ts` | `.tsx` |
|---|---|---|
| an arrow function parameter with a type annotation, destructured or not | 32 | 166 |
| a function-declaration parameter with a literal or call default | 2 | 8 |
| an `import("…")` type in a type argument or annotation | 3 | 1 |
| an arrow function parameter with a default value | 0 | 1 |
| a tagged template with explicit type arguments | 1 | 0 |
| array-destructured arrow function parameters | 1 | 0 |
| unclassified | 2 | 2 |
| **files** | **41** | **178** |

On `v0.20.2` the root of every one of those 219 trees was `ERROR`, and a
median of 88% of each file's bytes lay after its first error.

## Decision

Pin **`v0.54.0`**. Not the newest release (`v0.58.0`), and not `v0.20.2`.

Measured over the same 1,587 files. The error and finding counts, the binary
size, the identity differential and the residual forms were re-measured on this
change's tree; the wall time, the slowest file and the `v0.58.0` column come
from the version bisect that preceded it.

| | `v0.20.2` | `v0.54.0` | `v0.58.0` |
|---|---|---|---|
| files parsing with an error | 219 | **3** (2 `.ts`, 1 `.tsx`) | 4 (0 `.ts`, 4 `.tsx`) |
| findings | 89 | 99 | 99 |
| census wall time | 35.9 s | 28.4 s | — |
| slowest single file | 108 ms | 158 ms | 367 ms |
| stripped release binary, linux/amd64 | 16,363,682 B | 20,844,706 B (+27%) | about +38% |

Both sizes are `go build` with the Makefile's tags and `-s -w`, and both columns
build the same code, before this change added its coverage-manifest text.

`v0.58.0` was rejected because it turns three `.tsx` files that `v0.54.0`
parses clean into errors of 19 to 34 bytes, reports the same number of findings
and surface items, and costs more in size and in its slowest parse.

**Nothing was lost.** Findings and surface items were compared by identity —
rule id or surface category, file and line — between this tree built with
`v0.20.2` and with `v0.54.0`, through the real provider, over the same corpus:

| | `v0.20.2` | `v0.54.0` | kept | gained | lost |
|---|---|---|---|---|---|
| findings | 89 | 99 | 89 | 10 | **0** |
| surface items | 642 | 712 | 642 | 70 | **0** |

All 10 gained findings are `SEC-001`; the 70 gained surface items are 29
`nplus1`, 22 `authz`, 16 `idor` and 3 `overfetch`. Every gained item is in a file
`v0.20.2` parsed with an error, and none of the kept surface items changed its
structural facts. (The bisect counted 421 → 491 surface items: it passed each
file by its base name, which hides the surface that depends on the path, such as
a Next.js route. The delta, +70, is the same.)

### The residual: three forms

`v0.54.0` still parses three forms of valid TypeScript with an error. All are
declared in the coverage manifest (`ts.parser-residual-forms`) and pinned row by
row by `TestParserForms_StillAnErrorToday`; each boundary that parses clean is
pinned as a valid row. The census met (a) and (b); (c) was found afterwards,
while reducing other shapes to synthetic fixtures. Searched for afterwards, 0 of
the 614 `.tsx` files of the measured corpus contain the shape that triggers (c);
one contains its nested form, which parses clean.

- **(a) A tagged template whose explicit type argument is an array type**
  (``sql<Row[]>`…` ``), **an object type, alone or inside a tuple**
  (``sql<[{ id: number }]>`…` ``), **a generic type** (``sql<Array<Row>>`…` ``)
  **or two type arguments** (``sql<A, B>`…` ``), in `.ts` and `.tsx`. After
  `await` the parser leaves a `MISSING` `!` and no `ERROR` node; without `await`,
  an `ERROR` node. A single type name (`sql<Row>`), a tuple of type names
  (`sql<[A, B]>`) and a union (`sql<A | B>`) parse clean. The generic-type
  spelling parsed clean on `v0.20.2`. Two census files. Local: the finding on the
  next line is still reported.
- **(b) In `.tsx`, a bare `&` followed by a name, with no closing `;`, inside a
  quoted JSX attribute value** — a query string in an `href`. The parser reads it
  as an unterminated character reference and leaves an `ERROR` node over it, as
  wide as that text. `&amp;`, `& b` and the same text as a JSX child parse clean.
  New relative to `v0.20.2`, which parsed it clean (`v0.53.0` rejects it too): it
  is the one census file that errors on `v0.54.0` and did not on `v0.20.2`, and
  the identity differential above shows it lost nothing. Local.
- **(c) In `.tsx`, JSX text that starts with `(` right after an opening tag with
  no attribute, on the same line, when that element is not nested inside another
  JSX element or fragment** — where an expression stands: after `return`, `=` or
  `=>`, in a call argument, or inside a `{…}` container
  (`return <span>(optional)</span>;`, `<div>{on && <span>(optional)</span>}</div>`).
  Nested directly inside another element or fragment
  (`return <div><span>(optional)</span></div>;`), the same text parses clean.
  New relative to `v0.20.2`, which parsed it clean; `v0.53.0` and `v0.58.0`
  reject it too. **Not local:** the `ERROR` node runs into the next statement,
  and on the synthetic fixtures the finding on the next line is lost; inside a
  `{…}` container, or with a lone `(` as the text, the `ERROR` node spans the
  whole fixture file. After an attribute
  (`<span className="hint">(optional)</span>`) or on its own line, the same text
  parses clean.

Form (c) is the one place where `v0.54.0` can lose a finding `v0.20.2` reported.
No file of the measured corpus triggers it, and `v0.20.2` loses far more (219
files). The decision stands, under the release condition in the consequences
below; the cost is declared rather than assumed away.

The measured corpus holds 1,587 unique-content files: 973 `.ts` and 614 `.tsx`.
Of the `.tsx` files, 83 sit under hidden `.claude/` directories (agent worktree
copies of the same projects that differ in content, so deduplication by content
kept them) and 531 outside any hidden directory. Every count in this ADR is over
all 614.

### A gate for future bumps: necessary, not sufficient

Upstream behaviour across versions is measured **non-monotonic**: a form fixed
in one release breaks in a later one. Among the versions bisected,
`v0.43.1`–`v0.53.0` turn clean `.tsx` files into an error under a `program`
root, `.tsx` `import()` types break from `v0.22.0` until `v0.47.0`, and `v0.21.0`
takes 79 to 121 seconds on one 25 KB file. So the version is pinned exactly.

The parser-form tests reject a bump that fails a row. Passing them does not
accept one: they hold only the forms reduced to a synthetic fixture. **Accepting a
bump also requires re-running the census over real TypeScript projects and
comparing findings and surface items by identity, as this ADR did; that census is
not automated in this repository.**

- `TestParserForms_ValidFormsParseAndKeepTheLaterFinding` — one synthetic fixture
  per form, each with a credential after the form; each row names the versions it
  was observed to fail on. It catches:
  - the census forms (typed arrow parameter, return-type arrow, literal default,
    arrow default, `import("…")` type, array-destructured arrow parameters):
    observed failing on `v0.20.2`, in `.ts` and `.tsx`;
  - an `=` in JSX text (`<p>total = gross - tax</p>`), the shape behind the
    whole-file `.tsx` errors: observed failing, with the later credential lost,
    on `v0.43.0`, `v0.43.1`, `v0.47.0`, `v0.50.0` and `v0.53.0`;
  - the clean boundaries of residuals (a) and (c), so a declared residual cannot
    silently grow.
- `TestParserForms_StillAnErrorToday` — genuinely broken code (`const x = ;`,
  `if (x {`) stays an error, and every residual row stays an error, with whether
  its later finding is kept pinned in both directions. A version that starts
  accepting a residual row fails it; that alone does not make the version
  acceptable.
- `TestParserForms_ParseTimeCeiling` — every TypeScript fixture under the
  provider's `testdata`, the vendored large real files included, parses within
  30 seconds. It catches a parser that is slow everywhere.

What the tests do **not** catch, measured:

- **`v0.58.0`'s `.tsx` errors.** Three census files that `v0.54.0` parses clean
  error on `v0.58.0` (18 to 34 bytes). None was reduced to a synthetic fixture:
  line-level reduction keeps a structure of more than a hundred lines, and
  renaming its identifiers changes how `v0.54.0` parses it. On `v0.58.0` the
  tests fail only on the residual (a) rows, because `v0.58.0` accepts them; the
  reason `v0.58.0` was rejected is pinned nowhere in the test suite, only in this
  ADR's census.
- **`v0.21.0`'s slowdown.** That version parses the fixture set in about 1 s;
  its slowdown needs an interaction inside one real file that no synthetic file
  reproduced, and that file is a foreign project's code, so it is not vendored.

### The finding cache

A cached result from a binary with the old parser is never served by the new
one. The cache key starts with the analyzer identity, which is the SHA-256 of
the running executable (ADR 0050), and every binary `go build` links embeds its
module build list, parser version included. A codefit built with another parser
version therefore has other bytes, another identity and other keys.
`TestCache_ParserVersionIsPartOfTheAnalyzerIdentity` builds codefit with the
release flags and checks each link of that chain. No change to the key was
needed.

### Dependency checklist, for the new version

- **Transitive tree.** `go list -deps ./cmd/codefit` with the release tags: four
  new packages, all inside the same module
  (`grammars/internal/standaloneregistry`, `grammars/runtime`,
  `internal/luapattern`, `internal/parsercorephase0`); no new module and no new
  standard-library package. `go.sum` swaps the two `v0.20.2` lines for two
  `v0.54.0` lines. The module's own `go.mod` is unchanged from `v0.20.2`: it
  declares `go 1.22.0` and the same requirements, and codefit's build list stays
  at 26 modules.
- **Integrity.** `go mod verify`: all modules verified; `GOSUMDB` is
  `sum.golang.org`.
- **Sensitive code**, in the packages that compile into codefit. No package
  imports `net`, `os/exec` or `syscall`, as in `v0.20.2`. `unsafe` is imported by
  two packages instead of one; the new one uses `unsafe.Sizeof` for memory
  accounting. `reflect` is new, used to copy a language and to key external
  scanner state by type. One file write is new: a diagnostic transcript appended
  to a path only when both `GTS_DISPATCHER_TRANSCRIPT=1` and
  `GTS_DISPATCHER_TRANSCRIPT_OUT` are set; another writer
  (`writeOutlineCensusJSON`) is compiled in but called only from upstream tests.
  The grammar-blob override directory (`GOTREESITTER_GRAMMARGEN_BLOB_DIR`)
  existed in `v0.20.2` too. The root package reads at least 44 distinct
  environment variables, against 18 in `v0.20.2`; one of them,
  `GOT_PARSE_MEMORY_BUDGET_MB`, changes parse results.
- **Known vulnerabilities.** Not re-run here: the repository's
  `dependency vulnerabilities` check runs `govulncheck` on the pull request.

## Consequences

- Files that used to stop being read are read to the end. A user upgrading
  codefit will see **new** findings and surface items in files that reported
  nothing past an ordinary line before. Over the census nothing that was reported
  before disappeared; residual form (c) can hide a finding `v0.20.2` reported, in
  `.tsx` code the census did not contain.
- The binary grows by 4,481,024 bytes, about 27%, and the slowest measured parse
  goes from 108 ms to 158 ms, while the census as a whole parses faster. The
  growth is parser runtime code, not grammar data.
- A scan response does not say that a file parsed with an error. A file holding
  residual form (a) or (b) is audited past the error, one holding form (c) may
  not be, and nothing in the response marks either. That is separate work; this
  decision does not claim it.
- **Release condition, set by the owner when accepting this decision:** no
  release is cut from this line until parse-error detection lands (the follow-up
  work that reports a file whose parse is incomplete), so that in every published
  version a file holding a residual form is reported as not fully audited. Until
  then this upgrade lives only on the development line.
- The environment can now steer the parser more than before. codefit sets none
  of these variables, and an attacker who controls codefit's environment
  already controls more than its parser.

## Alternatives rejected

- **Stay on `v0.20.2`.** 219 of 1,587 real files losing everything after an
  ordinary line is the defect itself.
- **The newest release, `v0.58.0`.** More errors on `.tsx` than `v0.54.0` in the
  census, the same finding and surface counts, a larger binary and a slower worst
  case. It accepts residual (a) but not (b) or (c).
- **Recover around the error, or switch parser.** Out of scope for this
  decision; of the 219 files, 217 parse clean after the upgrade without either.
