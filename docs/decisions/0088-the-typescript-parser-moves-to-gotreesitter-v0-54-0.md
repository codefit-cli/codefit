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

Both sizes are `go build` with the Makefile's tags and `-s -w`, from an export of
the tree without `.git`; a build inside the git checkout also stamps VCS data and
lands a few kilobytes away (20,836,514 B measured once). Both columns build the
same code, before this change added its coverage-manifest text; the final tree of
this change, exported the same way, measured 20,848,802 B.

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

### The residual: two forms

`v0.54.0` still parses two forms of valid TypeScript with an error. Both are
declared in the coverage manifest (`ts.parser-residual-forms`) and pinned by
`TestParserForms_StillAnErrorToday`:

- **(a) A tagged template with explicit type arguments**,
  ``await sql<[{ id: number }]>`select 1` ``, in `.ts` and `.tsx`. The parser
  leaves a `MISSING` `!` and a zero-width `ERROR` node. Two census files.
- **(b) In `.tsx`, a bare `&` followed by a name, with no closing `;`, inside a
  quoted JSX attribute value** — a query string in an `href`. The parser reads it
  as an unterminated character reference: a 13-byte error. One census file.
  `&amp;`, `& b` and the same text as a JSX child parse clean. This form is
  **new**: `v0.20.2` parsed it clean, so it is the one file that errors on
  `v0.54.0` and did not on `v0.20.2`; the identity differential above shows it
  lost nothing.

Both errors are local: on the synthetic fixtures the finding on the next line is
still reported, and the root of the tree stays `program`.

### The gate for every future bump

Upstream behaviour across versions is measured **non-monotonic**: a form fixed
in one release breaks in a later one. Among the versions bisected,
`v0.43.1`–`v0.53.0` turn clean `.tsx` files into an error under a `program`
root, `.tsx` `import()` types break from `v0.22.0` until `v0.47.0`, and `v0.21.0`
takes 79 to 121 seconds on one 25 KB file. So the version is pinned exactly, and
a bump is accepted only when the parser-form tests still pass:

- `TestParserForms_ValidFormsParseAndKeepTheLaterFinding` — one synthetic
  fixture per census form, in `.ts` and `.tsx`, each with a credential after the
  form. Each fixture was observed to fail on `v0.20.2` (the form errors and the
  credential is missing) and to pass on `v0.54.0`.
- `TestParserForms_StillAnErrorToday` — genuinely broken code (`const x = ;`,
  `if (x {`) stays an error, and the two residual forms stay errors, each with
  its reason; a parser that starts accepting a residual form fails it until the
  manifest is updated.
- `TestParserForms_ParseTimeCeiling` — every TypeScript fixture under the
  provider's `testdata`, the vendored large real files included, parses within
  30 seconds. It catches a parser that is slow everywhere. It does **not** catch
  `v0.21.0`: that version parses the set in about 0.8 s, because its slowdown
  needs an interaction inside one real file that no synthetic file reproduced,
  and that file is a foreign project's code, so it is not vendored.

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
  nothing past an ordinary line before; nothing that was reported before
  disappears.
- The binary grows by 4,481,024 bytes, about 27%, and the slowest measured parse
  goes from 108 ms to 158 ms, while the census as a whole parses faster. The
  growth is parser runtime code, not grammar data.
- A scan response does not say that a file parsed with an error. A file holding
  a residual form is audited past the error, but nothing in the response marks
  it. That is separate work; this decision does not claim it.
- The environment can now steer the parser more than before. codefit sets none
  of these variables, and an attacker who controls codefit's environment
  already controls more than its parser.

## Alternatives rejected

- **Stay on `v0.20.2`.** 219 of 1,587 real files losing everything after an
  ordinary line is the defect itself.
- **The newest release, `v0.58.0`.** More errors on `.tsx` than `v0.54.0`, the
  same finding and surface counts, a larger binary and a slower worst case.
- **Recover around the error, or switch parser.** Out of scope for this
  decision; of the 219 files, 217 parse clean after the upgrade without either.
