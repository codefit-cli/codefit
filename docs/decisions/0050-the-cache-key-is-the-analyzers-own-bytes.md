# ADR 0050 — The finding cache is keyed on the analyzer's own bytes, and every failure is a miss

**Status:** Accepted · **Date:** 2026-08-03 · **Phase:** 3, thread H0, slice S2

**Superseded in scope (2026-08-03) by
[ADR 0051](0051-the-finding-store-is-bounded-by-generation-and-pruned-on-open.md).**
All eight DECISIONS below are untouched and still hold. What is no longer true is a
CONSEQUENCE this ADR accepted as the price of decision 2, and this ADR is not rewritten —
read the following together with 0051:

- **§Consequences "Every dev build orphans a whole generation of entries, and there is no
  eviction" is FALSE as of slice S3.** Its diagnosis stands word for word — every rebuild
  still mints a fresh generation for the entire tree, and every edit and every deleted file
  still orphans an entry inside one. What is no longer true is the second half: **the
  orphans are now collected.** Entries live under `Dir/<generation>/`, and `Open` keeps the
  current generation plus the two most recently modified others, drops entries in the
  current generation unwritten for 30 days, and removes the flat entries this text's layout
  left behind.
- **The "No eviction, no size bound" bullet under §"Declared, not forgotten" is spent.**
  Eviction exists. A *size* bound still does not: 0051 caps what is retained (three
  generations, 30 days), not how many bytes that comes to, and it says so.
- **"A bounded store is a later concern, deliberately not smuggled into this slice" came
  true in the narrowest possible way** — it was the very next slice. The sentence was right
  to keep it out of S2 and wrong about the horizon.
- **`rm -rf .codefit/cache` stays safe and stays the escape hatch**, and that is the one
  line of this consequence that needed no correction at all. What changes is the warning
  attached to it: it will *not* "be needed more often than 'entries accumulate' suggests".

## Context

ADR 0048 decided **which files get audited**. This decides **which results get
recomputed**. They are orthogonal: a full scan with a warm cache still opens, counts and
reports on every file — it just does not re-parse the ones whose bytes did not change.

The justification is not speed for its own sake. **The full scan is the honest one:** it is
the only scan that can prune the baseline, and the only one whose `blocked: false` means
what it appears to mean. If the full scan is expensive and the narrowed scan is cheap, every
caller narrows, and codefit degrades into a tool that permanently looks through a slit and
can never forget anything. The cache is what keeps the honest scan affordable (PRD §19,
optimization 2).

`internal/core/cache` had existed inert since Phase 0 with one function, `HashFile`, that
keyed an entry on **file content alone**. Shipping that shape would have made codefit lie in
a specific, ugly way: you upgrade codefit, the new binary ships new rules, the file did not
change, its hash matches — and codefit returns findings computed under the OLD rules while
reporting "clean" under rules it never ran. An auditor reporting a verdict it did not
compute is the same failure class as the baseline corruption ADR 0048 closed, in the same
direction: going blind while looking busy.

## Decision

### 1. The key names the ANALYZER, the PATH and the CONTENT

`sha256(analyzer identity ‖ project-relative path ‖ content)`, NUL-separated, the path in
slash form so it is the same key on every OS.

All three can change the answer, so all three are named. Nothing else can: the analysis of a
file is a pure function of *(analyzer, path, bytes)*.

### 2. The analyzer identity is the SHA-256 of the running executable, not a version string

`internal/version.Version` is the obvious candidate and it **does not work — it fails
exactly where it matters most**. It is the constant `"v0.1.0-dev"` for any plain `go build`,
`go run` or `go test`, so during rule development, where the rules change several times an
hour, every build would present the same key. **The rule author is the first person a stale
cache bites.**

Hashing the binary's own bytes (`os.Executable()`, hashed once per process and memoized) is
right rather than clever:

- It covers **every** input that can change a verdict — the YAML rules, the Go-coded
  detectors, the provider's parser, the surface queries — because all of them are *in the
  binary*. A version string covers none of them in a dev build.
- Under `go run` and `go test` the executable is a fresh temporary build, so editing a rule
  changes the identity automatically. Correct by construction, in the exact case the version
  string fails.
- There is nothing to remember to bump. A cache key you have to maintain by hand is a cache
  key that will be wrong.
- Two builds of identical source produce different binaries and therefore a miss. That is
  wasted work, never a stale verdict — the safe direction.

Cost: one SHA-256 of a ~5 MB binary, once per process. Against a walk that parses hundreds
of files it does not register.

### 3. The PATH is in the key — a correction to the spec, not a detail

`docs/specs/finding-cache.md` R2 wrote the formula as analyzer + content. That was a defect,
found while implementing and fixed here.

Two files with identical bytes are a real and ordinary thing — **this repository's own
fixtures contain them.** Under an analyzer+content key they share one entry, so the second
file is reported carrying the *first* file's path in every finding and surface item, and
therefore a colliding baseline fingerprint (which is derived from the file path, ADR 0009).
That breaks R1 outright and corrupts the audit memory downstream. The path is in the key,
and a test locks it: identical bytes at different paths must not share an entry.

### 4. An unresolvable analyzer identity DISABLES the cache for the run

If `os.Executable()` fails or the binary cannot be read, the identity is **unknown** — and
an unknown key input means **do not reuse**. The run scans everything, normally and fully,
and says so through `slog`. Falling back to a content-only key would be exactly the stale
verdict this ADR exists to prevent. `Cache.Key` returns `""` for an empty analyzer, so a
cache that does not know what produced its entries silently reads and writes nothing;
there is no path where a keyless cache serves an entry.

### 5. The entry holds findings AND surface, stored PRE-criticality

The per-file analysis returns findings **and** mapped surface. An entry that stored only the
findings would serve a warm scan that silently lost the surface, so the entry holds both.

Storing the output **before** `applyCriticality` runs is likewise not an implementation
detail — it is what keeps the cache correct across config edits. Path criticality is applied
by the sensor *after* the per-file analysis returns, so a cached entry survives an edit to
`.codefit.yaml`, and **editing `path_criticality` re-weights severities on the very next
scan without invalidating a single entry.** Caching post-criticality findings would serve
stale severities after every config edit. Locked in both directions.

### 6. An EMPTY result is cached like any other

A file with no findings and no surface is the ordinary case in a healthy repository. Treating
"nothing found" as "not cached" would leave the cache doing nothing exactly where most of
the work is — the clean majority would be re-analysed forever, and the cache would appear to
work while saving almost nothing.

### 7. Every cache failure degrades to a MISS; the write is atomic

A missing, unreadable or corrupt entry is a miss and the file is analysed normally. A failed
write is reported through `slog` and never appears in the JSON, because the audit already
happened — all that is lost is the saving on the next run. **The cache may never be the
reason an audit does not happen.**

The write is atomic (temp file in the same directory, then rename — the same shape the
committed baseline uses). codefit is an MCP server, so two tools over one project (an agent
firing `scan-security` and `scan-all` together) can reach the same entry path at once, and
`os.WriteFile` truncates before it writes. A torn entry does degrade safely on read, but it
degrades into re-analysing the file the cache exists to skip, and a crash mid-write would
leave that behind permanently.

### 8. Off unless asked

`config.Cache.Enabled` has no default applied in `validate`, so a project with no `cache:`
section has the cache **off**, and `codefit init` does not write one. An empty
`cache.dir` defaults to `.codefit/cache` inside the project (already gitignored, already
skipped by the walk); a relative `dir` resolves against the project root, an absolute one is
used as is.

## Consequences

### Every dev build orphans a whole generation of entries, and there is no eviction

This is the price of decision 2 and it is accepted, not overlooked. Because the analyzer's
bytes are in the key, **every rebuild mints a fresh generation of entries for the entire
tree and orphans the previous one permanently.** Nothing evicts them and nothing bounds the
directory's size. For a user on release binaries this is one generation per upgrade; for
anyone developing rules it is one per `go build`, several times an hour.

`rm -rf .codefit/cache` is always safe — it costs only time, and that is precisely what
distinguishes the cache from the committed baseline, which encodes decisions and must never
be deleted casually. But it will be needed more often than "entries accumulate" suggests,
and pretending otherwise would be the over-promising this project treats as a defect. A
bounded store is a later concern, deliberately not smuggled into this slice.

### No audit rule changed

Cold and warm are **byte-identical** — not equivalent, identical. That is the Definition of
Done, and it is test-locked over a real TypeScript project producing both real findings
(a leaked credential) and real surface (a Next App Router route reading `params.id`), with
the test hard-failing if the warm run re-analyses anything, so it can never silently compare
two cold runs. `rules/`, every provider `coverage.go`, `internal/core/dbcoverage`,
`dbrules`, `dwrules`, `paradigm` and `crossrules` are untouched by this slice.

### Nothing in the tool contract moved

The cache is a **project config setting**. No MCP tool gained a parameter, no response field
changed, and no tool description became stale — an agent cannot tell a warm scan from a cold
one, which is the entire point. It follows that the skill `codefit init` generates needs no
change: it teaches an agent how to *call* codefit, and there is nothing here to call
differently.

### Declared, not forgotten

- **The DB sensor and the code×schema cross are NOT cached.** Their inputs are the
  configured `database.schema_paths`, not a repository walk, and a schema is reconstructed
  from an *ordered* set of migrations, so a per-file entry is not obviously the right unit.
  A separate question with a separate invalidation story.
- **No eviction, no size bound** — see above.

## Related

- ADR 0048 — the change scope; decides which files are audited, where this decides which
  results are reused.
- ADR 0049 — deleted `internal/core/pipeline` and kept `internal/core/cache` precisely
  because it was about to be wired. This is that wiring.
- ADR 0009 — the baseline's content fingerprint, which is why the path had to enter the key.
- `docs/specs/finding-cache.md` — the contract, including the R2 formula this ADR corrects.
