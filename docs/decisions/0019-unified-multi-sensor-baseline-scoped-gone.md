# ADR 0019 — A unified multi-sensor baseline: scope "gone" by the sensors that ran

**Status:** Accepted · **Date:** 2026-07-05 · **Phase:** 2 (DB dimension — baseline unification before scan-all wiring)

## Context

The committed baseline (`.codefit-baseline`, ADR 0009-0012) records every tracked
item across scans, so a re-scan can tell new / known / changed / gone. Today only
the security sensor feeds it: `HandleScanAll` runs security, and `baseline.Diff`
marks GONE every previous item whose fingerprint is not observed this run.
`HandleBaselinePrune` likewise removes items its security-only re-scan did not
observe.

Closing the db dimension means `scan-all` will run more than one sensor. But a run
does not always run every sensor (a project may have no schema; a scan may cover
one dimension). If the baseline treats "not observed this run" as "gone"
regardless of WHICH sensors ran, a security-only run would mark every db item gone
(and a db-only run every security item) — corrupting the committed state of the
other dimension and inviting an erroneous prune. This is a persisted-state
corruption risk, so it is fixed BEFORE any second sensor is wired in.

## Decision

### "Gone" is scoped to the categories the sensors that ran actually own

`baseline.Diff` takes `scanned`, the set of Item categories owned by the sensors
that ran this pass. A previous baseline item is eligible to be GONE only if its
category is in `scanned`; an item whose category is NOT scanned (its sensor did not
run) is carried forward UNTOUCHED — absent from the delta, never a gone/prune
candidate. This distinguishes "not observed because the sensor did not run" from
"not observed because it disappeared".

The scope is by `Item.Category`, which is ALREADY persisted — so there is **no
change to the committed baseline format and no migration**. `HandleBaselinePrune`
is scoped the same way: it only prunes items whose category its re-scan covered.

### Each sensor declares its own categories; the adapter only unions

`OwnedCategories() []string` is added to the `Sensor` interface. Security declares
`security / idor / authz / overfetch`; the db sensor declares `db` plus its per-rule
surface categories. The MCP adapter unions the `OwnedCategories` of the sensors
that ran (`scannedCategories`) — it does not know any category itself, so a new
sensor is scoped automatically without touching the adapter. The categories MUST be
disjoint across sensors (each owned by exactly one); a test enforces this
invariant, since the whole scoping mechanism rests on it.

### observedFrom unions multiple sensor results

`observedFrom` becomes variadic, unioning the observations of the sensors that ran
into ONE diff (deduped by fingerprint) — one committed baseline per project, never
one per sensor.

### The shown/presentation layer is separated from the diff

The generic diff (`Shown`/`State` by fingerprint) is distinct from the
endpoint-centric presentation security uses. Non-endpoint items (db, later) will
filter by `diff.Shown` directly into their own section — a documented seam, not
built in this slice.

### Fail-safe default

An empty `scanned` means nothing is in scope, hence nothing is reported gone or
pruned — a conservative degradation. A caller that forgets to pass scope never
corrupts the baseline; it only under-reports gone.

## Consequences

- `baseline.Diff` gains a `scanned` parameter; `applyBaseline` and
  `HandleBaselinePrune` pass the running sensors' category sets. For security-only
  today the result is identical and the saved baseline is byte-for-byte unchanged
  (all items are security-owned → all in scope → same delta and order).
- The baseline is ready to absorb the db sensor's already-fingerprinted items
  (slices 2/2b) without corrupting security's state — but no db sensor runs in
  scan-all yet (that is the next slice).
- No persisted-format change, no migration; scope is by the existing `Item.Category`.

## Related
- ADR 0009-0012 — the baseline (fingerprint, current-state, graduated safeguard, agent-driven tools).
- ADR 0016 — dimension lifecycle (this unblocks wiring db into scan-all's close).
