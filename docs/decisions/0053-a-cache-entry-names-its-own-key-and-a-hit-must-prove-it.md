# ADR 0053 — A cache entry names its own key, and a hit has to prove it

**Status:** Accepted · **Date:** 2026-08-04 · **Phase:** 3, thread H0, correctness fix

**Extends [ADR 0050](0050-the-cache-key-is-the-analyzers-own-bytes.md) decision 7 ("every
cache failure degrades to a MISS").** No decision of 0050 or
[0051](0051-the-finding-store-is-bounded-by-generation-and-pruned-on-open.md) is reversed.
What changes is the *test* the reader applies before calling something a hit: 0050 said a
corrupt entry is a miss, and the code implemented "corrupt" as "`json.Unmarshal` returned an
error". That definition was too narrow, and the gap it left produced the one output this
project treats as its worst possible defect — a clean verdict codefit never computed.

## Context

`(*cache.Cache).Get` read the entry file, unmarshalled it, and returned `(e, true)` on any
successful unmarshal. Its only test of corruption was the parser's error.

**Valid JSON is not proof of provenance.** `null`, `{}`, `{"unrelated":1}` and
`{"findings":[],"surface":null}` all parse cleanly and all unmarshal into a **zero `Entry`**,
which the reader then serves as a hit. And a hit with no findings and no surface does not
mean "nothing was stored". Under decision 6 of ADR 0050 it means something much stronger and
much worse: **this exact analyzer analysed exactly these bytes at exactly this path and found
nothing.**

Reproduced deterministically on unmodified `main` (`c990005`), on all four payloads, before
any fix existed. The observed consequence, end to end: **codefit reported score 100 and no
SEC-001 for a file that leaks a credential.** That is verbatim the failure `docs/specs/
finding-cache.md` R1 and ADR 0050 exist to prevent — "an auditor reporting a verdict it did
not compute", "going blind while looking busy" — arriving through the one component built to
make the honest full scan affordable.

### How it was found, and what it does NOT depend on

The investigation started from a **flake**: an intermittent, unexplained failure attributed
to the cache. It was then run **on the order of a thousand iterations and never reproduced
once.** It is not diagnosed, it is not fixed here, and nothing in this ADR rests on it.

This is recorded plainly because the honest version matters more than the tidy one. The flake
bought a careful reading of `Get`, and the reading — not the flake — found the defect. The
evidence for the fix is deterministic and independent: four payloads, red before, green
after, in a test that fails when the guard is removed. Had the flake never happened, the
defect would still be here and this fix would still be correct.

### The blast radius, stated at its real size

Neither softened nor inflated:

- **The cache is opt-in.** `config.Cache.Enabled` has no default applied in `validate` and
  `codefit init` writes no `cache:` section (ADR 0050, decision 8). A project that never
  turned the cache on was never exposed. This is real, and it is why this lands as a fix
  rather than an emergency.
- **`.codefit/cache` is an ordinary directory inside the user's project**, and it is theirs —
  the same constraint that shaped ADR 0051's prune. Anything at all that leaves valid JSON at
  `<gen16>/<64hex>.json` is enough: a stray `{}`, an editor or sync or backup artifact, a
  half-restored backup, another tool. There is no exotic precondition and no race to win.
- **The result is permanent and silent.** Nothing re-analyses that file afterwards. The
  poisoned entry survives every subsequent scan until the file's bytes change or the analyzer
  binary changes — and it never announces itself, because a hit is indistinguishable from a
  real one by design.

## Decision

### 1. The entry carries the key it was stored under

```go
type Entry struct {
    Key      string                 `json:"key"`
    Findings []findings.Finding     `json:"findings"`
    Surface  []findings.SurfaceItem `json:"surface"`
}
```

`Set` stamps `e.Key = key` before marshalling. `Get`, after unmarshalling, returns a **miss**
unless `e.Key == key`. The entry is self-describing: it states what it is the answer to, and
the reader checks the claim instead of assuming it.

### 2. Self-describing beats enumerating the malformed shapes

The obvious alternative is to make `Get` stricter about what it accepts — reject `null`,
require the object to carry the expected members, use `json.Decoder` with
`DisallowUnknownFields`, and so on. That is the wrong shape of fix for a specific reason:
**it requires knowing the failure modes in advance.** Each check closes one shape, the list
is unbounded, and the next shape nobody enumerated is served as a hit exactly like these
four were. A fix that only closes the cases someone thought of is not a fix for "the reader
cannot tell whose answer this is".

The stamp inverts the burden. It does not ask *is this payload malformed?* — a question with
no complete answer — but *does this payload prove it belongs to this key?* — a question with
one. Every shape that cannot answer it, known or not, invented today or arriving tomorrow, is
a miss. It also closes a case none of the four payloads covers and none of the shape checks
would have: a **perfectly well-formed entry sitting at another key's path**, which is what a
copied, restored or synced file actually looks like. Test-locked
(`TestGetRejectsAWellFormedEntryStoredUnderAnotherKey`).

This is the same move ADR 0050 decision 2 made when it chose the binary's own bytes over a
version string: prefer the check that is **correct by construction** over the one that has to
be maintained by hand and is wrong the first time someone forgets.

### 3. Old entries cost one miss each, and there is no migration code

Entries written by an earlier build have no `key` member, unmarshal with `Key == ""`, and are
therefore a miss. Each one is re-analysed once and rewritten stamped. That is one extra
analysis per stale entry, in the safe direction, self-healing on the first run — and ADR
0051's generation prune collects whatever is not rewritten.

A migration path would have to distinguish "an old entry of mine" from "a foreign payload
with no key", and **there is nothing in the bytes that can tell them apart** — that
indistinguishability is the defect itself. Any such code would have to trust the class this
ADR exists to distrust. So there is none, deliberately.

### 4. The stamp does not become a second key

`Key` is verified, never *used* to address anything. The path is still derived from the key
the caller passes (ADR 0051 decision 1), the key is still
`sha256(analyzer ‖ path ‖ content)` (ADR 0050 decision 1), and a mismatch produces
`(Entry{}, false)` — the same miss as a missing file. Nothing reads a path out of an entry,
so a hostile stamp buys nothing beyond making its own entry unreadable.

## Consequences

### A hole this fix does NOT close: the empty read

`os.ReadFile` returns `([]byte{}, nil)` when the first read reports EOF. A source file ever
**observed** as zero-length — a truncate-then-write from an editor, a sync tool mid-copy —
would therefore be analysed as empty, produce nothing, and have that nothing cached under the
key for *empty content at that path*. Reported as score 100 for that file.

**This is not proven to occur**, it is not fixed here, and it is deliberately not folded into
this change: it is a different defect with a different cause (what the *walk* accepts as a
file's content, not what the *cache* accepts as an entry) and it wants its own reproduction
before it gets its own fix. It is written down so it is met as a known open item rather than
rediscovered as a surprise.

> **Update (2026-08-05) — reproduced, and the CACHE half above is disproved (`docs/roadmap.md`
> P0-3, SDD change `empty-read-hole`, Engram `sdd/empty-read-hole/reproduction`).** The
> reproduction this section asked for was run against the real sensor and the real cache on
> unmodified `main` @ `ac91109`:
> ```
> PROBE pass1 (file empty)   findings=0  audited=1     <- "nothing" gets cached
> PROBE pass2 (real content) findings=1  SEC-001 high leak.ts:1
> PROBE control (no cache)   findings=1
> *** NOT REPRODUCED: pass2 matches the uncached control ***
> ```
> **The cache half is disproved structurally, not empirically.** The key is
> `sha256(analyzer identity ‖ path ‖ content)` (decision above, unchanged). Empty content and
> real content at the same path hash to two different keys, so the pass over real content is
> always a MISS and analyses for real — an entry cached from an empty read can **never** be
> served for non-empty content at that path. Not "was not observed to happen": cannot happen,
> by construction of the key this ADR already specifies. Locked so it cannot regress silently:
> `TestCache_EmptyReadNeverPoisonsLaterRealContentAtSamePath`
> (`internal/sensors/security/cache_test.go`), driving the real sensor and the real cache
> end-to-end, with an uncached control run in the assertion, and mutation-proved — the guard
> fails exactly as this section predicted when the key is edited to ignore content.
>
> **What stays true is the WALK half, exactly as this section already said, and it is not a
> cache defect.** `os.ReadFile` reporting `([]byte{}, nil)` on a file mid-write is a real
> property of the read, present with or without the cache: codefit reports what it read. It is
> also **transient** — the next scan reads the real bytes and finds them, unlike a cache
> poisoning, which would be permanent and silent. There is no sound fix: codefit cannot
> distinguish a file legitimately empty (common) from one mid-write, so no heuristic closes
> this without a new false-positive class. No production code changed for this update — see
> the CHANGELOG `[Unreleased]` entry and `docs/roadmap.md` P0-3.

### A declared limit, proven during the investigation: the cache stops warming under concurrency on Windows

Go's `syscall.Open` opens files with `FILE_SHARE_READ|FILE_SHARE_WRITE` and **not**
`FILE_SHARE_DELETE`. So on Windows, `os.Rename` over an entry file another reader currently
holds open fails with access denied. With two concurrent MCP tool calls over one project —
the exact case ADR 0050 decision 7 made the write atomic for — `Set` fails repeatedly.

The failure direction is safe: a failed `Set` is reported through `slog` and the next run is
a miss, so nothing stale and nothing wrong is ever served. But the honest description is not
"degrades gracefully", it is **the cache effectively stops warming under concurrency on
Windows, and logs a warning per file while doing it.** Not fixed here — it is a platform
file-sharing question, not a provenance one, and mixing it into this change would blur what
this change proves. Declared in `CHANGELOG.md` under "Not yet covered".

### No audit rule changed, and no tool contract moved

Nothing under `rules/` changed, and neither did any provider `coverage.go`,
`internal/core/dbcoverage`, `dbrules`, `dwrules`, `paradigm` or `crossrules`. The fix is
inside the cache reader and writer; it can only turn a hit into a miss, and a miss is a real
analysis producing the real verdict. No MCP tool gained a parameter, no response field
changed, no tool description became stale, and the skill `codefit init` generates needs no
change — an agent could never tell a hit from a miss, which was always the point.

### What the guard is worth is measured, not asserted

The four payloads were **run red on unmodified `main` first**, with their output read, and
the guard was then **mutation-proved**: with `e.Key != key` removed, all four fail again.
`TestSetStampsTheKeySoItsOwnEntriesStillHit` is the control in the other direction — a guard
that rejected everything would satisfy the four payloads and quietly make the cache inert.
This package has already shipped one test that locked a reality the production path could not
produce; the pairing is there so this one cannot become that.

## Related

- ADR 0050 — the key and the failure model. This extends its decision 7 and reverses none of
  its decisions.
- ADR 0051 — the generation prune, which is what collects the unstamped entries decision 3
  leaves behind.
- `docs/specs/finding-cache.md` — the contract. R1 (cold and warm are byte-identical) is what
  the defect violated; R4's "a corrupt entry is a miss" is what this ADR gives a workable
  definition of.
