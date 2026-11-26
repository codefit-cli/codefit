# 0089 — A file the parser did not read whole is recorded, and cached as such

Date: 2026-11-25
Status: accepted
Amends [ADR 0050](0050-the-cache-key-is-the-analyzers-own-bytes.md) (the cache key gains a
settings segment). Applies the provenance rule of
[ADR 0053](0053-a-cache-entry-names-its-own-key-and-a-hit-must-prove-it.md) — a hit must prove
itself — and extends it to one more field, the parse status (D3); ADR 0053 itself is unchanged
and carries no back-reference to this ADR yet. Follows
[ADR 0088](0088-the-typescript-parser-moves-to-gotreesitter-v0-54-0.md).
Issue `#189`.

## Context

The TypeScript provider parses with `github.com/odvcencio/gotreesitter`, an
error-recovering parser. Given a form its grammar does not accept, it never fails: it
returns a tree with the damage marked in it — an `ERROR` node, a zero-width `MISSING` node,
or a stop before the end of the input — and every rule and surface query then walks that
tree. ADR 0088 moved the parser to `v0.54.0`, which removed almost every ordinary form that
broke a file, and declared the residual: three forms still parse with an error, and one of
them (residual (c)) loses the finding on the next line.

Nothing read the marks. `Parse` kept only the root and dropped the tree; `AnalyzeSecurity`
and `AnalyzeSurface` each parsed the file again; no code asked whether either tree was
whole. The finding cache stored findings and surface only. So a file the parser did not
read to the end produced fewer findings, and fewer findings is indistinguishable from a
cleaner file. A cached copy of that result stayed that way.

There was a second hole, found while mapping the design. The cache key is the hash of the
running binary, the file's path and its content (ADR 0050). That covers everything *in* the
binary, but the parser also reads environment variables at run time.
`GOT_PARSE_MEMORY_BUDGET_MB` makes it stop large files early. A scan run once with it set
cached the stopped, truncated analysis, and the next scan of the same binary without it was
served that entry as its own — observed, before the fix, as "0 of 2 files re-analysed" with
the large file still reported stopped.

## Decision

The repository owner approved the design on 2026-11-25. The question, verbatim: "¿Apruebas
el diseño con las dos recomendaciones (una sola llamada y huella de `GOT_*` en el caché)?"
The answer: "si".

### D1 — One parse, one truth

The parse status comes from the SAME tree that produced the findings and the surface. A new
optional interface, `providers.FileAnalyzer`, returns all three from one call; it is
resolved by type assertion, like `QueryExtractor` and `SchemaParser`. The TypeScript
provider implements it with one parse per file. `Parse`, `AnalyzeSecurity` and
`AnalyzeSurface` keep their signatures and behaviour for their other callers, and a test
pins that the combined call returns exactly what the two calls return, in the same order,
over every TypeScript fixture of the provider's testdata and one inline route handler, with
a provider that registers a project authz helper. Its positive controls require the inputs
to fire every rule id the real loader loads, to produce every surface category the
provider declares, and to show the registered helper flipping `known_authz_detected`, so a
combined call that dropped a rule, a surface query or the project's helpers fails it.

A provider that does not implement it is analysed through the two calls as before, and its
files carry the status **not reported** — never complete. The status type,
`findings.ParseStatus`, has three states (not reported, complete, incomplete with a reason
and a first line), and its zero value is "not reported", so a status nobody set cannot pass
for a clean read.

The security sensor records the status per file. `SensorResult` gains
`ParseIncomplete` (path, reason, first line, sorted by path) and `ParseStatusReported`,
which is false unless every analysed file had a status — so an empty list from a provider
that reports nothing says "not checked", not "every file was read whole".

### The reasons, and their order

The TypeScript provider checks, in a fixed order; the first that holds is the reason:

1. `stopped:<reason>` — the parse stopped before accepting the input (memory budget, node
   or iteration limit, timeout, …). First, because the stop is the cause and the `ERROR`
   root it leaves is the symptom.
2. `error_root` — the root itself is an `ERROR` node.
3. `error_node` — an `ERROR` node under the root, reported at the first in document order.
4. `missing_node` — no `ERROR` node, but a `MISSING` node.
5. `error_flag` — the tree is flagged as holding an error and neither kind of node is found.

An `ERROR` node outranks a `MISSING` node regardless of position: it can swallow text, a
`MISSING` node only stands in for a token.

### D2 — Parser settings are part of the cache identity

A new optional interface, `providers.SettingsFingerprinter`, returns an opaque fingerprint
of the settings outside the binary that change what a provider reports. The TypeScript
provider fingerprints every environment variable whose name starts with `GOT_`,
`GOTREESITTER_` or `GTS_` (case-insensitively, as Windows treats names), as name=value pairs,
sorted, hashed. The security sensor binds it into the cache after opening it, and the key
becomes `sha256(analyzer ‖ settings ‖ path ‖ content)`. The `GOT_` name never reaches
`internal/core`, which sees only the opaque string. The settings are in the key and
deliberately NOT in the generation directory: a run under another setting misses, it does
not prune the entries the usual setting wrote.

Over-inclusion is the safe direction. An extra miss is acceptable; a false hit is not. The
owner approved `GOT_*`; measuring the parser's sources showed two more prefixes it reads
(`GOTREESITTER_`, `GTS_`), and they are included for the same reason.

### D3 — An entry must prove itself

A cache entry with no parse status is a miss, and `Set` refuses to write one. This is the
provenance rule of ADR 0053 extended to one more field: an entry that cannot say how much of
its file produced its findings is not evidence of anything.

### D4 — A stop that may not recur is reported, never cached

The owner was asked on 2026-11-26: "¿Excluyo del caché las paradas no deterministas?" The
answer: "si". The reason is D2's: a warm run must equal a cold run. A stop that depends on the
state of the process rather than on the bytes and the settings can fall elsewhere, or not at
all, on the next run over the same file; caching it would freeze a transient result until the
file or the analyzer changed.

`findings.ParseStatus` gains `Reproducible`: the provider's word that the same bytes, binary
and settings give the same analysis. Every constructor sets it except
`ParseIncompleteNotReproducible`; its zero value is false, so a status nobody vouched for is not
cached. `ParseNotReported` is the zero value with `Reproducible` set, so a provider that reports
no status (Go) is cached exactly as before. The security sensor reports a not-reproducible file
in its run like any other — listed with its `stopped:` reason and line, findings and surface
returned — and does not write it to the cache. The core names no parser, reason or variable; the
TypeScript provider classifies.

It classifies every stop reason `ParseStoppedEarly` reports (gotreesitter `v0.54.0`, `tree.go`)
by what upstream documents. The evidence is the "DETERMINISM CONTRACT (issue #454)" beside
`runtimeMemoryBudgetStopReasonNow` (`parser_memory_budget_runtime.go`): the arena and scratch
budgets "and the node and stack-depth limits. Those stop the same input at the same place every
time", while readings of the process heap make the tree "a function of GC timing rather than of
the input". The guard behind a `memory_budget` stop is read from
`ParseRuntime().MemoryBudgetStopSource`, so the hard ceiling and the soft budget, which report
the same stop reason, are told apart.

| Stop reason | Guard | Cached | Evidence |
|---|---|---|---|
| `memory_budget` | `arena`, `scratch` | yes | documented deterministic; arena measured (the 5 MB stop, every run) |
| `memory_budget` | `hard_ceiling` | **no** | process heap; measured varying (below) |
| `memory_budget` | `runtime_heap`, `runtime_sys`, none named | **no** | documented as GC-timing bound; unnamed cannot be told apart |
| `node_limit`, `stack_depth_limit` | — | yes | documented deterministic; not measured here |
| `timeout`, `cancelled` | — | **no** | wall clock, caller |
| `iteration_limit`, `reuse_budget`, `token_source_eof`, `invariant_violation` | — | **no** | no determinism documented |
| any other or future reason | — | **no** | not classified |

A parse with no reported stop is not classified: complete, `error_root`, `error_node`,
`missing_node` and `error_flag` are cached as before. `error_flag`'s shape ends in
`no_stacks_alive`, which is not a reported stop; whether it is reproducible is **unknown**.

The basis for treating the hard ceiling as not reproducible is upstream's contract above, not a
measurement here. One machine's measurement, the one this change was written on, through the
security sensor (a child process; `real_vscode_strings.ts`, 92,023 bytes, beside a small file;
10 cold-then-warm pairs per value): with the default garbage collector,
`GOT_PARSE_MEMORY_HARD_CEILING_MB` at 1 or 2 stopped the large file on 9 of 10 cold runs and at
4 on 8 of 10, the rest parsing it whole — the same bytes, the same settings. The independent
verifier's 20 child runs without `GOGC=off` all stopped, so how often a run escapes the ceiling
varies by machine and moment; that it can is what the contract says. With `GOGC=off`, on the
same one machine, 1 to 4 MB stopped it on 10 of 10 runs (also under `-race`), at 8 MB on 0 of 10
(2 of 10 under `-race`). `TestParseStatus_TransientStopIsReportedButNotCached` runs at 1 MB with `GOGC=off`: the
file is listed `stopped:memory_budget` on both runs, the second run re-parses that file and only
that file, and the cache holds the small file alone. Caching it anyway fails it, and so does
classifying the hard ceiling as reproducible. Classifying the arena guard as not reproducible
fails `TestParseStatus_ParserSettingsArePartOfTheCacheIdentity`, which needs the soft-budget stop
cached; `TestParseStatus_WarmRunFromCacheStillReportsTheStatus` fails if every incomplete result
stops being cached.

`GOGC` and `GOMEMLIMIT` set the collector's pacing, so they move when the hard ceiling fires;
they remain outside the settings fingerprint (D2), which no longer matters for caching: a
hard-ceiling stop is never stored.

## What was measured (gotreesitter v0.54.0, linux/amd64)

- **Every TypeScript fixture under `internal/providers/typescript/testdata`**, 64 files:
  every valid row and every real or representative file parses with a `program` root, no
  error flag, stop reason `accepted`. Every still-an-error row also has a `program` root: an
  `ERROR` node under it (`error_node`) or, for the tagged-template residual after `await`
  and an unclosed condition, a `MISSING` node and no `ERROR` node (`missing_node`). The
  repository commits 68 `.ts`/`.tsx` files in all; the other 4 live under
  `internal/providers/registry/testdata` and `internal/scaffold/testdata`, and classify as
  complete under the default settings (measured in the correction batch below).
- **No `ERROR` root without a stop, under the default settings.** No committed fixture and
  none of 68 synthetic inputs tried for it (stray punctuation and operators, lone brackets
  and keywords, unterminated strings, control bytes, a byte-order mark — 34 inputs, each as
  `.ts` and `.tsx`) parses to an `ERROR` root. The only `ERROR` root measured under the
  default settings is a parse stopped by the memory budget, which `stopped:` outranks. With
  `GOT_C_RECOVERY=0` (the parser's older, resync-based recovery) two residual (c) rows do
  parse to an `ERROR` root without a stop; that is measured, not pinned. Removing the
  `error_root` arm falls through to `error_node`, still incomplete; no test pins the arm.
- **The `error_flag` arm, reproduced with a non-default setting.** gotreesitter `v0.54.0`
  documents the shape beside `markTruncatedTreeHasError` (`parser_api.go`): the GLR
  frontier dies mid-file (stop reason `no_stacks_alive`, which `ParseStoppedEarly` does not
  report) after the stack reduced to a clean root, so the root ends before the file, holds
  no `ERROR` or `MISSING` node, and carries only the error flag. In the correction batch of
  2026-11-26 no input reached it under the default settings — the 68 committed files, 91
  generated inputs (mostly garbage tails after valid statements, in `.ts` and `.tsx`), and a
  TypeScript `lib.dom.d.ts` — nor, over the committed files and `lib.dom.d.ts`, with
  `GOT_GLR_MAX_STACKS` at 1 or 2, `GOT_GLR_MAX_MERGE_PER_KEY=1`, or ten values of nine other
  parser switches tried one at a time. With
  `GOT_C_RECOVERY=0` it is reached: two valid statements, then a line holding a backslash
  and the text `x01`, then a credential — stop reason `no_stacks_alive`, the root ends at
  byte 25 of 56 (the end of line 2), error flag set, no `ERROR` or `MISSING` node, the
  credential lost; the same on 30 of 30 runs and under `-race`.
  `TestAnalyzeFile_CutShortCleanPrefixReportsErrorFlag` runs that input through the real
  provider in a child process with the setting and pins `error_flag`, line 0; making the
  arm return complete fails it. What is pinned is the classification of that shape; what is
  not is that the default settings ever produce it.
- **The memory budget**, through the security sensor, with and without `-race` (identical):
  at 8 and 7 MB nothing stops; at 6 and 5 MB the 92 KB vendored real file stops
  (`stopped:memory_budget`, a one-node `ERROR` root, error flag set) while a small file in
  the same run parses whole (measured with a 194-byte fixture, and pinned with a one-line
  file that also keeps its finding); at 4 MB and below the embedded rule
  patterns themselves no longer compile and the scan **fails** with an error. The soft
  budget counts the parse's own arena allocations, not the process heap, so it is
  deterministic: the test at 5 MB passed `-count=10` with and without `-race`.
- **`root.EndByte()` against the content length** was measured as a further signal and
  rejected. Under the default settings, on every fixture, every still-an-error row and every
  deterministic budget stop the root ends exactly at the end of the content; the only short
  root measured there came from the heap hard ceiling (`GOT_PARSE_MEMORY_HARD_CEILING_MB`),
  which is reported as a stop anyway. With `GOT_C_RECOVERY=0` two more short roots were
  measured: the `error_flag` shape, which check 5 already reports, and the lone-backslash
  inputs under "Not covered" below, which read complete. Every other input that read complete
  under that setting had a full-span root, so a span check would not reach them (below).
- **A BOM-less UTF-16 file** (the declared residual of ADR 0044) now gets `error_node` on
  line 1: it is listed as not parsed whole. It is still not analysed.
- **Environment names read by the parser's non-test sources:** 57 under `GOT_`, 10 under
  `GOTREESITTER_`, 17 under `GTS_`. Two more, `ISSUE454_CPUPROFILE` and
  `ISSUE454_FULLPROFILE`, only write a profile and are left out.
- **Results unchanged.** The security sensor over the repository's TypeScript testdata, on
  the base and on this change, compared by identity (rule id or surface category, file,
  line, fingerprint, facts): 39 findings and 114 surface items, identical.
- **Cost, one machine's measurement.** The same scratch harness (the security sensor over
  those 64 files), 20 runs, three rounds each, on the one linux/amd64 machine the change was
  written on: the base takes a median of about 465 ms, this change about 291 ms. The
  independent verifier's runs reproduced the direction, not the figures (about 563 ms →
  392 ms). The saving is the second parse the sensor no longer does. The status check
  itself measured within noise on that machine: the same change with the check
  short-circuited to "complete" also took about 291 ms.

## Consequences

- The cache entry format gains a `parse` field, which carries `reproducible`. An entry without
  it is a miss; in practice every existing entry is already a miss, because a new binary is a
  new analyzer identity.
- A file stopped for a not-reproducible reason (D4) is parsed again on every run that meets it:
  an extra parse, never a frozen stop.
- Setting or changing any `GOT_`, `GOTREESITTER_` or `GTS_` variable invalidates the cache
  for that run (a miss, not a prune): the parser may obey it.
- The Go provider reports no parse status; its results say `ParseStatusReported: false`,
  and its entries store "not reported", which is a hit.
- `SensorResult` gains two fields. It is never serialized into an MCP response
  (`report.Report.SensorResults` is never populated), so **no tool response changes here**:
  the detection exists in the core, the sensor and the cache, and is not yet exposed to the
  agent. Exposing it is the next change.

## Not covered, declared

- **Valid code the parser misreads into an error-free tree** of the wrong shape reads as
  complete. The check trusts the parser's own marks and has nothing else to compare against.
- **Paths that do not go through the security sensor** parse on their own and report no
  status: `ExtractQueryFilters` (the code side of the code×schema cross), the stateless
  file-level surface tools, and every direct caller of `Parse`.
- **Go files**: not reported (above).
- **A budget so low the rule patterns cannot compile** (4 MB and below, measured) is not
  reported per file: the scan fails loudly instead.
- **The `v0.20.2`-style stop with the error flag false** and the root ending before the file
  is not reproducible through the real parser on `v0.54.0`, so it is not pinned with a
  hand-built tree. A stop of that kind would still be reported by check 1, which reads the
  stop reason, not the flag.
- **`error_root`** is reached only with `GOT_C_RECOVERY=0` (above) and is not pinned;
  removing it falls through to `error_node`. **`error_flag`** is pinned through the real
  parser only under `GOT_C_RECOVERY=0`; whether the default settings ever produce it is
  unknown.
- **Broken code with no mark, under the non-default setting `GOT_C_RECOVERY=0`.** With that
  setting the parser returns trees with no error flag, no `ERROR` and no `MISSING` node for
  inputs that are broken, and each reads as **complete**. Measured on 2026-11-26: of 101
  inputs the default settings report incomplete (the 20 committed still-an-error rows and 81
  generated ones), 22 read complete under the setting, each identical on 10 of 10 runs. Six
  are committed rows — both `broken_*` rows (`const x = ;`, `if (x {`) and the
  tagged-template row without `await`, each in `.ts` and `.tsx`; the rest are garbage tails
  after valid statements (`#!x`, `a b c`, `<div>`, `1a`, `0x`, `@@@`, `if (a {`, a lone
  backslash). 19 of the 22 have a root that ends exactly at the end of the content. The other
  3 are the lone-backslash shape, a line holding only `\` as the last line of the file —
  byte for byte, in C escape notation (`\n` a newline, `\\` one backslash), `const a = 1;\n\\\n` (15 bytes, root ends at 12) and
  `const a = 1;\nconst b = 2;\n\\\n` (28 bytes, root ends at 25), the first as `.ts` and
  `.tsx`. The independent verifier measured 10 of its 13 broken inputs reading complete under
  the setting, all with a full-span root. So no mark and no span check can detect the
  full-span ones; a span check would reach at most the lone-backslash shape. Nothing at this
  layer can detect it. Whether codefit should tell the caller that parser settings are
  overridden is deferred to the exposure task (T3). It is not fixed.
- **Broken code with no mark, under the default settings.** At least one broken shape is
  accepted by the parser with no mark and a full-span root: a double comma inside an object
  literal (`const o = { a: 1,, b: 2 };`, also with three commas and as a call argument). It
  is reported **complete**, identical on 10 of 10 runs (the first spelling also as `.tsx`). It is not fixed;
  the check trusts the parser's marks.
- **Which stops are reproducible rests on upstream's word** for `node_limit`,
  `stack_depth_limit` and the arena/scratch guards of `memory_budget` (D4); only the arena
  guard is measured here. The `error_flag` shape has no reported stop and is cached; whether
  it is reproducible is unknown. A not-reproducible stop is not counted anywhere a later run
  could see: the next run simply parses the file again.
- **Out-of-scope files** under a narrowed scan are not analysed and carry no status.
- **Layer 1** (the regex secrets scan) reads the raw text, not the tree; its findings in an
  incomplete file are unaffected by the parse and are still reported.
