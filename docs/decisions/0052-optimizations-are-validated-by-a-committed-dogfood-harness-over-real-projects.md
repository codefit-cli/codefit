# ADR 0052 — codefit's optimizations are validated by a COMMITTED harness over real projects, not by fixtures alone

**Status:** Accepted · **Date:** 2026-08-03 · **Phase:** 3, thread H0, slice S4

**Supersedes in scope the first two bullets of [ADR 0051](0051-the-finding-store-is-bounded-by-generation-and-pruned-on-open.md)
§"Still true, and NOT lifted by this slice"** — "The cache has never been run against a real
project" and "No speedup has been measured anywhere". Both were true when 0051 was written and
both are false now. Nothing else in 0051 or in
[ADR 0050](0050-the-cache-key-is-the-analyzers-own-bytes.md) is touched: the key is still the
analyzer's own bytes, the store is still bounded by generation, every failure is still a miss,
and a warm scan is still byte-identical to a cold one — that last property is now asserted
against real code rather than only against fixtures.

## Context

The change scope (ADR 0048) and the finding cache (ADRs 0050, 0051) shipped with the same
declared limit, written into the README, the CHANGELOG and VERSIONING: *covered by tests and
the CI self-audit, and that is all.* Both are **optimizations**, and an optimization carries
two questions a fixture cannot answer.

**Does the contract survive real code?** The unit tests build the input they then assert on.
They prove that `scope.Canon` normalizes what the test spells, that an entry round-trips, that
a warm run reuses what a cold run wrote. They cannot prove any of that holds across 300 files
of somebody's real TypeScript, with the barrel files, the generated clients, the `.d.ts`, the
deep `node_modules`-adjacent directories and the path spellings a repository actually contains.
This is CLAUDE.md's standing warning about hand-built fixtures: a struct assembled by the test
can hold a shape the production path never produces, and the test then locks a reality that
does not exist.

**And is it worth anything?** The cache's justification is a cost model — the *honest* full
scan, the only one that may prune the baseline, must not be the expensive option — argued from
first principles with no number attached. An argued optimization is a hypothesis. codefit
already learned this once on the DB side: the code↔schema cross was impeccably correct under
table-driven tests while eating 64% of the surface channel on a real schema, and only the
dogfood harness of that phase turned "the rule is correct" into "the rule is useful".

The instrument for the cross harness already existed and had already settled the hard parts:
committed in the repository rather than a script one `git clean` from gone, build-tagged out of
the gate, and skip-if-absent so a contributor without the clones is not blocked. What was open
was whether the same instrument may be pointed at somebody's *working* clone.

## Decision

### 1. The harness is COMMITTED, and the measurement is part of the repository

`internal/mcp/dogfood_cache_test.go` lives in the tree next to `dogfood_cross_test.go`. A
benchmark run once on a laptop and pasted into a PR description is an anecdote whose method
nobody can inspect and whose result nobody can re-derive; a committed harness is a method under
review, and the next person to touch the cache re-runs it instead of re-deriving how to measure
it.

It runs the **real** security sensor through the **real** provider `providerForLanguage`
resolves, so what it measures is what an agent gets — not a reimplementation of the walk, which
CLAUDE.md's verification discipline rules out.

### 2. Build-tagged and skip-if-absent: whoever has the clones measures, whoever does not breaks nothing

The corpus is somebody's private clones. Their paths are per machine, so the configuration —
`dogfood.local.json` at the repository root — is **gitignored and never committed**, and it is
therefore absent for every contributor and for CI.

Two consequences follow and both are decisions, not accidents. The file is behind
`//go:build dogfood`, so `CGO_ENABLED=0 go build ./...`, `go vet`, `go test -race ./...` and
`golangci-lint` never compile it: the gate cannot be made to depend on files it cannot have. And
with the tag supplied but the config missing, the whole test **skips clean** — exit 0, no lie —
rather than failing. A harness that fails without private data trains everyone to ignore it.
The loader that decides what an absent config means was moved into the new file and is now
**shared** by both harnesses, so the two cannot drift into disagreeing about it.

The cost is stated rather than hidden: **these results are not reproducible by anyone else.**
Another contributor points the same harness at their own projects and gets their own numbers.
That is the trade for measuring real code at all, and it is why the numbers are always published
with the machine, the projects, the file counts and the date attached.

### 3. Read-only over the clones — which is why it drives the sensor, not the MCP handler

The dogfood roots are working trees somebody develops in. The harness writes **nothing** inside
one: no `.codefit.yaml`, no `.codefit/` directory, no temp file. The configuration is
synthesized **in memory**, and the cache directory is an absolute `t.TempDir()` — asserted to be
absolute, because `cacheFor` joins a *relative* dir onto `ProjectRoot` and a relative path would
put the cache inside the project.

That constraint chose the entry point. `runSecurity` loads `.codefit.yaml` **from the project
root**, so enabling the cache through the MCP handler would require writing a config into the
clone. The harness therefore drives `security.Sensor` directly. The honest cost: the handler
layer around the cache — `scopeBlockFor` aside, which the scope test does exercise through the
production function — remains covered by tests only. Trading a clean working tree for one layer
of coverage is the right way round; the reverse asks a user to let a test scribble in their
repository.

### 4. Every number is guarded against passing by vacuum

This is the entire risk of moving a cache test off fixtures. A run over a real repository that
quietly analysed nothing — wrong extensions, everything under a skipped directory, a walk that
returned early — produces two identical empty results and "passes" while proving that nothing
equals nothing. So the harness fails loudly on: zero audited files, a provider that was never
called, a cache holding fewer entries than files audited, a warm run that re-analysed anything,
a scope that does not actually narrow, a requested path that was never reached, a denominator
that collapsed onto the scope, and a narrowed pass that reached a different verdict than the
full pass on the same files.

The scope test spells its requested paths **non-canonically** on purpose — a `./` prefix plus OS
separators, which is how an agent hands over a git diff on Windows. The paths come from the full
run, so the only thing that can make them fail to match is `scope.Canon`; without that spelling,
"nothing went unmatched" would be a tautology.

Each guard was proven red by mutation before being trusted, per CLAUDE.md: a test that has never
failed on purpose controls nothing.

### 5. A project may honestly produce nothing — so the payload requirement is asserted across the CORPUS

Two of the four projects produce zero findings and zero surface. That was **checked**, not
assumed and not designed around: metricasbatch is a Vite React SPA with no route handlers, and
plantalinda's only Next.js route handler returns a static `new Response("ok")` whose body
touches no data. [ADR 0005](0005-surface-frontier-finite-vs-infinite.md)'s declared frontier is
**correct** to emit nothing for either. The alternative — relaxing the per-project guard until
the empty projects pass — would have disabled exactly the vacuum check decision 4 exists for.

So the requirement that a warm cache preserves *findings* and preserves *surface* is asserted
**after the loop, over the corpus**: at least one measured project must have produced findings
and at least one must have produced surface, or the whole test fails with "add a project that
does". Each empty project still proves the walk, the store and the warm hit over hundreds of
real files — and proves that an *empty* analysis is cached rather than recomputed, which is the
majority case in a healthy repository — but it is not allowed to stand in for evidence it cannot
give.

## Consequences

### The two declared limits are lifted, and the documents that carried them are corrected in place

The README, CHANGELOG and VERSIONING sentences saying the scope and the cache had "not been
exercised on a real project yet" and that "no speedup has been measured anywhere" were true when
written and are now false. They are **edited where they stand**, not contradicted by a newer
paragraph further down the same file: a document that asserts both is worse than one that
asserts the stale thing, because a reader cannot tell which sentence is the current one.

### A number now exists, and it is published as a measurement, never as a property

Cold versus warm on one Windows machine, on 2026-08-03: 317 files in 5989 ms cold against
514 ms warm; 147 in 2473 against 168; 309 in 5023 against 265; 14 in 465 against 11. Warm runs
re-analysed **0 files** and every pair was byte-identical.

The cold column is the unstable one — repeated runs varied by roughly **±2x** (5989 to 11627 ms
on the largest project) because the operating system caches the filesystem underneath, while the
warm figures were stable. codefit therefore does not claim a factor. "N times faster" as a
property of the tool would be a promise made out of one machine's page cache; the numbers are
published with their machine, their projects, their file counts and their date so that a reader
can tell a measurement from a guarantee.

### The corpus is four projects one person had, and half of it exercises almost nothing

This is a limitation of the evidence, not a footnote to it. Four private clones are **not** a
representative sample: one provider (TypeScript), one operating system, one desk, and no
project large enough to test the retention bound the previous ADR left as a threshold. Two of
the four contribute no findings and no surface at all, so the whole payload-survival half of the
evidence rests on the other two. The corpus grows the way the cross harness's did — by adding
entries to a local config — and every number should be re-read as it grows.

### The 30-day retention age is still untuned

ADR 0051 called it "the piece most likely to be re-tuned once anyone measures a real project".
Someone has now measured real projects, and it is still untuned: this harness measures a cold
run against a warm one **inside a single session** and says nothing about how long an entry
should live. The threshold stands, unproven, and the sentence that pointed at this measurement
as its trigger is corrected rather than quietly satisfied.

### Nothing shipped changes

No production file is touched by this slice. No audit rule, no MCP tool, no response field, no
description an agent reads. `rules/`, every provider `coverage.go`, `internal/core/dbcoverage`,
`dbrules`, `dwrules`, `paradigm` and `crossrules` are untouched, and `COVERAGE.md` and the
`codefit-coverage` manifest report exactly what they reported before. The gate is unaffected in
both directions: it does not compile the harness, and the harness does not need it to.

## Related

- ADR 0048 — the change scope this harness exercises over real code.
- ADR 0050 — the cache's key and failure model; untouched here.
- ADR 0051 — the generation bound; this ADR lifts two of its "still true" bullets and leaves its
  30-day threshold standing and unproven.
- ADR 0005 — the surface frontier that makes two of the four projects honestly empty.
- `internal/mcp/dogfood_cross_test.go` — the code↔schema cross harness whose committed,
  build-tagged, skip-if-absent shape this one adopts.
- `docs/specs/finding-cache.md` — the contract whose test-contract item 1 asks for cold vs. warm
  byte-identity "over a real project with real findings AND real surface items". One project in
  this corpus (317 files, 1 finding, 386 surface items) satisfies it literally; the other three
  are why decision 5 exists.
