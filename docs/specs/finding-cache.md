# Spec — Content-hash finding cache (filtering-pyramid optimization 2)

**Status:** draft · **Phase:** 3, thread H0, slice S2 · **Target:** `v0.2.7` (a PATCH,
together with S1: no PRD phase closes here)

S1 decided **which files get audited**. This slice decides **which results get
recomputed**. They are orthogonal: a full scan with a warm cache still opens, counts and
reports on every file — it just does not re-parse the ones whose bytes did not change.

## Why this is not a performance nicety

A full scan is the honest one. It is the only scan that can prune the baseline, and the
only one whose `blocked: false` means what it appears to mean. If the full scan is
expensive and the narrowed scan is cheap, every caller will narrow — and codefit degrades
into a tool that permanently looks through a slit and can never forget anything.

**The cache is what keeps the honest scan affordable.** That is its justification, not
speed for its own sake (PRD §19, optimization 2: "un `scan-all` recurrente cuesta casi
como un incremental").

## R1 — A cached scan and a cold scan are byte-identical

Not "equivalent". **Identical.** A cache that can change the output is not a cache, it is
a blind spot. This is the slice's Definition of Done and its hardest test: same project,
cold run vs. warm run, JSON compared byte for byte. One byte of difference and the cache
is wrong and gets reverted.

## R2 — The key is the ANALYZER plus the FILE, never the file alone

The inert `cache.HashFile` keys entries on file content only. Shipping that would make
codefit lie in a specific, ugly way:

> You upgrade codefit. The new version ships new rules. You scan. The file did not change,
> so its hash matches, so codefit returns the findings computed under the OLD rules — and
> reports "clean" under rules it never ran.

An auditor reporting a verdict it did not compute is the same failure class as the baseline
corruption S1 closed, in the same direction: going blind while looking busy.

**The naive fix — put `version.Version` in the key — does not work, and fails exactly where
it matters most.** `internal/version.Version` defaults to the constant `"v0.1.0-dev"` for
any plain `go build`, `go run` or `go test`. During development, where rules change several
times an hour, every build would present the same key. The rule author is the person the
stale cache would bite first.

**Decision: the key is `sha256(analyzer identity ‖ file content)`, where the analyzer
identity is the SHA-256 of the running executable** (`os.Executable()`, hashed once per
process and memoized).

Why this is right rather than clever:

- The analysis is a pure function of *(file bytes, analyzer)*. The key names both. Nothing
  to remember to bump.
- It covers **every** input that can change a verdict — YAML rules, Go-coded detectors, the
  provider's parser, the surface queries — because all of them are in the binary. A version
  string covers none of them in a dev build.
- Under `go run` and `go test` the executable is a fresh temporary build, so editing a rule
  changes the analyzer hash automatically. Correct by construction, in the exact case the
  version string fails.
- Two builds of identical source produce different binaries and therefore a cache miss.
  That is wasted work, never a stale verdict — the safe direction.
- If `os.Executable()` fails, the analyzer identity is unknown, so **the cache disables
  itself for that run**. Unknown key inputs mean do not reuse. Never guess.

Cost: one SHA-256 of a ~5 MB binary, once per process. Against a repository walk that
parses hundreds of files, it does not register.

## R3 — What is cached: the whole raw output of one file's analysis

`scanFile` returns `([]findings.Finding, []findings.SurfaceItem, error)`. The cache entry
holds **both** — today's inert `Cache.Get` returns only `[]findings.Finding`, which would
silently drop the surface and make a warm scan differ from a cold one, violating R1. The
API widens.

**The entry stores the output of `scanFile` BEFORE `applyCriticality` runs.** This is not
an implementation detail, it is what keeps the cache correct across config edits:
`path_criticality` is applied by the sensor after `scanFile` returns, so caching the
pre-criticality findings means changing `.codefit.yaml` re-weights severities on the next
scan **without** invalidating a single entry. Caching post-criticality findings would make
every config edit serve stale severities. Test-locked in both directions.

## R4 — Off unless asked, and never a silent failure

- `config.Cache.Enabled` is a plain bool with no default applied in `validate`, so a project
  without a `cache:` section has the cache **off**. That stays: the cache is opt-in.
- `config.Cache.Dir` empty defaults to `.codefit/cache`, already gitignored
  (`.gitignore:41`).
- A cache read that fails, is corrupt, or cannot be parsed is a **miss**, not an error — the
  file is analysed normally. A cache write that fails is reported as a note, never a failed
  scan. The cache may never be the reason an audit does not happen.

## R5 — `internal/core/pipeline` is DELETED, not wired

The recommendation this slice makes, for the architect to veto if he disagrees.

`core/pipeline` has declared `Pipeline`, `LayerProcessor`, `FilterLayer` and
`PipelineResult` since Phase 0, marked INERT, with zero production importers. All three
layers of the pyramid are now implemented and **none of them used it**: layer 1 (regex) and
layer 2 (AST) run inside `scanFile`, and layer 0 shipped in S1 wired straight into the
walk. This slice does not need it either — the cache is consulted per file inside the walk,
not as a stage transforming a file list.

Its shape is the reason: `LayerProcessor.Process(files []string, …)` models layers that
each take a file list and pass an escalated list onward. The sensor does not work that way
and never did; it processes one file through every layer at once.

So: the pyramid is real and implemented; the `Pipeline` type is not how codefit expresses
it, and after two phases and three layers there is no evidence it ever will be. A package
that names a capability nothing exercises is the same class of claim as
`AuditContext.Since` — which this thread already deleted for the same reason. It goes, with
an ADR recording that the doctrine survives its removal.

**This is the one decision here that destroys work rather than adding it, so it is called
out for an explicit yes/no rather than buried in a diff.**

## Out of scope, stated rather than silently skipped

- **The DB sensor is not cached.** Its inputs are the configured `schema_paths`, not a repo
  walk, and its expensive step is schema reduction over a handful of files. Caching it is a
  separate question with a separate invalidation story (a schema is reconstructed from an
  ordered set of migrations, so a per-file entry is not obviously the right unit). Declared,
  not forgotten.
- **No rule changes.** Every finding, surface item and baseline fingerprint is identical
  cold and warm — that is R1.
- **No cache eviction / size bound.** Entries accumulate under `.codefit/cache`. A bounded
  store is a later concern; `rm -rf .codefit/cache` is always safe and costs only time,
  which is precisely what distinguishes the cache from the committed baseline.

## Test contract

Each proven by **mutation**: break the exact behavior, watch it fail, restore, watch it
pass — both runs recorded in the commit or PR.

1. **Cold vs. warm are byte-identical** over a real project with real findings AND real
   surface items. The mutation: cache only the findings, dropping surface — warm must
   diverge.
2. Changing a file's bytes produces a miss and a recomputation.
3. **A different analyzer identity produces a miss** even when every file is unchanged. The
   mutation: key on file content alone — the test must see stale findings survive.
4. `os.Executable()` failing disables the cache; the scan still completes, fully.
5. Editing `path_criticality` in `.codefit.yaml` changes the reported severities **on a warm
   cache**, with no entry invalidated. The mutation: cache post-criticality findings.
6. A corrupt/unreadable entry is a miss and the file is analysed; the scan does not fail.
7. Cache disabled (default) behaves exactly as today — no directory created, no reads.
8. A file that produces zero findings and zero surface caches that empty result and is not
   re-analysed. (Otherwise the clean files — the majority — never benefit, and the cache
   quietly does nothing on a healthy repository.)
