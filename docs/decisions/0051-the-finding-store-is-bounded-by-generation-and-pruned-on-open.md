# ADR 0051 — The finding store is bounded by GENERATION, and pruned on open

**Status:** Accepted · **Date:** 2026-08-03 · **Phase:** 3, thread H0, slice S3

**Supersedes in scope [ADR 0050](0050-the-cache-key-is-the-analyzers-own-bytes.md)
§Consequences "Every dev build orphans a whole generation of entries, and there is no
eviction", and the "No eviction, no size bound" bullet of its §"Declared, not forgotten".**
All eight of 0050's DECISIONS are untouched and still hold — the key still names *(analyzer,
path, content)*, the analyzer identity is still the running executable's own bytes, an
unresolvable identity still disables the cache for the run, the entry still holds findings
and surface pre-criticality, and every failure is still a miss. What changes is a
CONSEQUENCE that ADR accepted as the price of its decision 2 and called "a later concern,
deliberately not smuggled into this slice". It was the next one.

## Context

ADR 0050 put the analyzer identity in the key so that a codefit shipping new rules can never
serve a verdict computed under the old ones. That is the right trade and it stands. Its
arithmetic is what this ADR pays for.

Because the identity is in the key, **every codefit build mints a fresh GENERATION of
entries for the whole tree and orphans the previous one entirely.** For a user on release
binaries that is one generation per upgrade. For anyone developing rules — the person the
cache is supposed to help first — it is one per `go build`, several times an hour, each one
a full copy of the project's entries. With entries stored flat in one directory there was
nothing to collect them: the store grew for the life of the project and only `rm -rf` ever
shrank it.

Two smaller growths ride along *inside* a single generation, and they are the ones that do
not stop even for a user who never rebuilds:

- **Every edit to a file mints a new key and orphans the old entry.** The old bytes will
  never be presented again.
- **A file deleted from the project leaves its entry behind forever.** Nothing will ever ask
  for that path again.

Both are entries nothing can hit, which is the useful way to say it: what has to be
collected is not "old" data, it is **unreachable** data.

### The store shares a directory with the user

`.codefit/cache` is a directory inside the user's project. It is theirs; codefit merely
writes in it. Any code that deletes files there is one loose glob away from deleting
something it did not create, and the failure would be silent and permanent. That constraint
shapes the decision as much as the arithmetic does.

## Decision

### 1. An entry lives under a GENERATION directory, so a superseded build is one thing to drop

Entries move from `Dir/<key>.json` to `Dir/<gen>/<key>.json`.

The unit that has to be droppable is the unit that goes stale, and what goes stale all at
once is a whole build's worth of entries. Under the flat layout, collecting a superseded
generation meant identifying its members one by one — and the key is a hash, so nothing in
an entry's *name* says which analyzer wrote it. A directory per generation makes the
question trivial: the answer is the directory name, and dropping it is one `RemoveAll`.

`<gen>` is **16 hex characters, not the full 64**. It is a *label*, not the boundary that
keeps two analyzers apart — the full identity is still inside the key hash, so a 16-char
collision would cost a shared directory, never a shared entry. Sixty-four here would make
every entry path `.codefit/cache/<64>/<64>.json` and put a project with a deep root within
reach of Windows' `MAX_PATH`.

### 2. The generation label is DERIVED, never the identity as given

An identity that is the expected 64-hex SHA-256 is truncated to its first 16 characters. An
identity of any other shape is **hashed** into that form rather than used as-is.

Production only ever passes the real thing, so this is about what the code cannot be allowed
to do rather than what it is expected to meet. The label is used as a **path element** and is
matched against the prune's generation pattern, and both properties break badly for an
arbitrary string: `"../../x"` would address a directory *outside* the cache, and any name the
prune cannot recognise is a generation nothing will ever collect — a leak reintroduced by the
mechanism built to close it. Deriving the label keeps both total. It returns `""` when there
is no analyzer identity, matching `Cache.Key`: a cache that cannot key cannot address a
directory either.

### 3. `Open` prunes, once per process per generation directory, and collects three things

- **Superseded generations.** The current generation survives **always** and is never a
  candidate, plus the **2 most recently modified** others — three in all. Not one: a
  developer alternating between an installed codefit and a dev build would then have every
  run destroy the other binary's generation and never see a hit again, which is the cache
  failing at exactly the desk it was built for.
- **Stale entries in the current generation** — entry files not written in **30 days**. A hit
  does not rewrite its entry, so a live entry ages out too; that costs **one re-analysis**,
  which is the safe direction and the same direction every other failure in this package
  takes. What this actually collects is the unreachable set from §Context: the orphans of
  every edit, and the entries of every deleted file.
- **The flat entries of the pre-generation layout**, once, as a **migration**. Nothing will
  ever read those paths again, so they are removed rather than carried as a permanent
  compatibility path.

Pruning at `Open` rather than per scan is because codefit is a long-lived MCP server that
opens the same cache on every tool call: maintenance is a startup cost, and a second pass in
the same process has nothing new to find.

### 4. The prune only recognises the two shapes it writes itself, and is BEST EFFORT

A generation directory is `^[0-9a-f]{16}$`. An entry file is `^[0-9a-f]{64}\.json$`.
**Anything else under the cache directory is never touched, at any age, under any pressure**
— another tool's file, a user's note, a directory nobody here created. This is not
defensiveness for its own sake; §Context is why. Test-locked over a fixture holding a
`README.md`, a `notes/` directory and a `keep-me.json`, all of which must survive a prune
that really deletes generations around them
(`TestPruneOnlyRemovesKnownShapes`).

Every failure the prune can meet — an unreadable directory, a file it may not remove, a race
with a second codefit process — is **swallowed**, and the prune reports nothing. A cache that
cannot clean itself still has to work. This is ADR 0050's decision 7 applied to maintenance:
**the cache may never be the reason an audit does not happen.**

## Consequences

### The store is bounded, and `rm -rf` is now a convenience rather than a maintenance task

The bound is *(3 generations) × (entries reachable in the last 30 days)*, not a byte count —
this is a cap on what is *retained*, not a quota. `rm -rf .codefit/cache` stays safe and
stays the escape hatch; what changes is that it is no longer something a rule author will
need "more often than 'entries accumulate' suggests", which is what ADR 0050 warned. That
warning is spent.

### The bound is a retention policy, not a size limit — stated rather than glossed

A single generation of a very large project is still a full copy of that project's entries,
and three of them is three. codefit does not measure the directory, does not enforce a byte
ceiling, and does not evict by size or by least-recent-use. A project big enough for that to
matter is a real possibility and it is **not** addressed here.

### The 30-day age is the one number that costs correct work

Every other part of this prune collects entries nothing can hit. The age sweep is the one
that can remove a *live* entry — a file untouched for a month, still present, still
analysable from cache. The cost is one re-analysis and the entry is rewritten, so the error
is self-healing and points the safe way. It is a threshold, though, not a proof, and it is
the piece most likely to be re-tuned once anyone measures a real project.

### No audit rule changed, and no tool contract moved

The prune is invisible to a caller. No MCP tool gained a parameter, no response field
changed, no tool description became stale, and the skill `codefit init` generates needs no
change — an agent cannot tell a pruned cache from an unpruned one, which is the point.
`rules/`, every provider `coverage.go`, `internal/core/dbcoverage`, `dbrules`, `dwrules`,
`paradigm` and `crossrules` are untouched by this slice. A warm scan and a cold scan remain
**byte-identical**, which is the property the layout change had to preserve and does: the
reader reads the path the writer writes, and two pre-existing tests that had encoded the flat
layout were repointed rather than left passing on a path nothing uses.

### Still true, and NOT lifted by this slice

- **The cache has never been run against a real project.** It is covered by tests and by the
  CI self-audit, and that is all. The "validated in real use" that the security and DB
  dimensions earned does not extend to it, and this slice does not change that.
- **No speedup has been measured anywhere.** The justification is affordability of the honest
  full scan (ADR 0050 §Context), argued from the cost model, not from a benchmark. There is
  no number, and none is claimed.
- **The DB sensor and the code×schema cross are still NOT cached** — 0050's own "declared,
  not forgotten" bullet, which this ADR does not touch. Their inputs are configured
  `database.schema_paths` rather than a walk, and a schema reconstructed from an *ordered*
  set of migrations does not obviously invalidate per file.

## Related

- ADR 0050 — the cache's key and failure model; this ADR supersedes one of its consequences
  and none of its decisions.
- ADR 0049 — kept `internal/core/cache` because it was about to be wired.
- `docs/specs/finding-cache.md` — the contract, whose "no cache eviction / size bound"
  out-of-scope bullet is annotated as lifted here.
