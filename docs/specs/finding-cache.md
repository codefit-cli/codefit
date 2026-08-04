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

> **Correction — this formula was WRONG as written, and shipped with a third component.**
> It omits the file's project-relative **path**. Two files with identical bytes are an
> ordinary thing (this repository's own fixtures contain them), and under this formula they
> would share one entry: the second file would be reported carrying the first file's path,
> with a colliding baseline fingerprint — a direct violation of R1, which dominates. The
> shipped key is `sha256(analyzer ‖ path ‖ content)`, mutation-proven by a test that fails
> when the path is dropped. Recorded here rather than silently rewritten, because the point
> of a written contract is lost if it is edited to match whatever was built; see ADR 0050.
> The rest of R2 is unchanged and shipped as stated.

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

  > **Update — this limit was LIFTED in slice S3, and "a later concern" turned out to be
  > the next one.** Recorded here rather than deleted, because the point of a written
  > contract is lost if it is edited to match whatever was built; see ADR 0051 (which
  > supersedes the matching "no eviction" consequence of ADR 0050) and the R2 note above.
  > What is lifted is EVICTION; a **size** bound is still not claimed — S3 caps what is
  > retained, not how many bytes that comes to.
  >
  > What made it urgent is the analyzer identity R2 put in the key. Every codefit build
  > mints a fresh GENERATION of entries for the *whole tree* and orphans the previous
  > generation entirely — on a branch where the binary changes several times an hour, the
  > store grows by a full copy of the project's entries per build and nothing ever
  > collected them. Two smaller growths ride along inside one generation: every edit to a
  > file mints a new key and orphans the old entry, and a file deleted from the project
  > leaves its entry behind permanently.
  >
  > **Shipped policy.** Entries move from `Dir/<key>.json` to `Dir/<gen>/<key>.json`, where
  > `<gen>` is the first 16 hex chars of the analyzer identity — a *label*, not the boundary
  > that separates two analyzers (the full identity is still inside the key hash). Sixteen
  > rather than 64 keeps `.codefit/cache/<gen>/<key>.json` clear of Windows' MAX_PATH on a
  > deep project root. On `Open`, once per process **per generation directory**: the current
  > generation is kept ALWAYS,
  > plus the 2 most recently modified others (3 in all — not 1, because a developer
  > alternating between an installed binary and a dev build would otherwise have each run
  > destroy the other's generation); entry files in the current generation older than 30 days
  > are removed; and entries left flat in `Dir` by this layout's predecessor are removed once
  > as a migration. A hit does not rewrite its entry, so a live entry ages out too — that
  > costs one re-analysis, which is the safe direction.
  >
  > **This code deletes files, so it only ever recognises the two shapes it wrote itself:**
  > a generation directory matching `^[0-9a-f]{16}$` and an entry file matching
  > `^[0-9a-f]{64}\.json$`. Anything else under `Dir` — a user's note, another tool's file,
  > a directory nobody here created — is never touched at any age, and the prune is best
  > effort: every failure it can meet is swallowed, because a cache that cannot clean itself
  > still has to work. `rm -rf .codefit/cache` stays safe and stays the escape hatch.

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

## Test contract added by S3 (generations and pruning)

Each mutation-proven like the ones above, and each asserting in BOTH directions in the
same run — what was removed AND what survived — because a prune test whose fixture has
nothing to delete passes trivially.

9. Entries live under `Dir/<gen>/`, and the generation name is always well shaped and
   always directly under `Dir` (an identity that is not 64-hex is hashed into the shape
   rather than truncated as-is: a name like `../../x` would address a directory outside
   `Dir`, and a name the prune cannot recognise is a generation nothing will ever collect).
10. The current generation plus the 2 most recently modified others survive; the rest are
    collected.
11. The current generation is never collected, **however old it is** — planted as the
    oldest directory of all and still standing after the prune.
12. A `README.md`, a `notes/` directory and a `keep-me.json` in the cache directory all
    survive a prune that really deletes a generation around them.
13. An entry older than 30 days is collected; one at 29 days is not; a file that is not
    entry-shaped is not, at any age.
14. Flat entries from the pre-generation layout are removed; `keep-me.json` and
    `deadbeef.json` are not.
15. A missing cache directory, a cache directory that is a *file*, and a cache with no
    analyzer identity all prune without failing and without deleting anything.
16. `Open` prunes, and prunes once per process per generation directory.
