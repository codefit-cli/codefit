# 0072 — A configured schema path always leaves a trace

Date: 2026-08-13
Status: accepted
Extends: [ADR 0068](0068-a-negative-claim-needs-positive-evidence-the-statement-census-and-the-measured-budget.md)
(the unread floor and invariant I2), one level up — from the configured FILE to
the configured PATH. Supersedes nothing.

## Context

The DB dimension's unread floor (`internal/sensors/db/unread.go`) exists to make
one failure impossible: reporting "audited, 0 findings" over content codefit
never read. It reasons about a configured schema FILE that contributed nothing
to the neutral model, and it is correct about every file it is handed.

It was handed the wrong denominator. `wholeScanUnproductive(unread, total)` took
`total` from the count of files the resolver produced — a quantity **the
resolver itself decides**. A configured path that resolved to no file at all was
therefore subtracted from *both* sides of the predicate and disappeared: not in
the unread list, not in the total, not in the note.

Two defects follow, both measured live on `main` before this change:

**Case 1 — the total case.** `database.schema_paths` naming a directory that
holds one `.go` and zero `.sql` — the ordinary golang-migrate embed layout, and
the shape `flywayOrderedSQL` declines by design:

```json
{"findings":null,"measured":true,"score":100,"surface":null}
```

`total` was 0, the floor's `total > 0` guard short-circuited to false, the rules
ran over an empty schema, and the response was byte-for-byte the shape of a
clean audit over a directory codefit never opened a byte of.

**Case 2 — the partial case, and the worse one.** Two configured paths,
`db/real` (one `.sql`, one table) and `db/empty` (zero `.sql`). The scan audits
`db/real` correctly and *is* a legitimate measurement: `measured: true`, score
100, real findings. And `db/empty` is mentioned **nowhere** in the response.

Case 1 at least looks suspicious — a score of 100 over zero tables invites a
second look. Case 2 looks completely normal, which is exactly why it matters
more. It is a violation of invariant I4 of `docs/specs/audit-protocol.md`: a
partial result that does not declare itself partial.

Nothing in the repository locked either case. Probe over
`internal/sensors/db` for the empty-resolution shape: 0 matches, with the same
path returning hits for other terms as a positive control.

## Decision

**The unit of account moves from the resolved FILE to the CONFIGURED PATH.**

`readSchemaSources` returns `schemaResolution{Paths []resolvedPath, Content
map[string][]byte}`, where `resolvedPath{Configured string, Files
[]providers.SourceFile}` carries one entry **per configured path** and `Files`
may legitimately be empty. Every configured path yields an entry; there is no
branch in which one does not. A flattening `sources()` accessor feeds
`ParseSchema` the same ordered slice it received before.

`Content` **stays keyed by resolved-file path**. That is deliberate and it is
what keeps this change away from fingerprinting, snippets, `file:line`
positions, and the code×schema cross — all of which are properties of a file and
of nothing else.

A path that resolved to zero files reaches the floor as a first-class entry
carrying a new reason, `reasonResolvedNoFiles`, classified as a **defect** by
`defect()`. `wholeScanUnproductive` is recomputed over the configured-path count,
and `unreadSources` returns the unproductive-path count from the same pass that
builds the list, so the floor's numerator and the note's list cannot drift apart.

Every downstream mechanism is reused unchanged: defect-first ordering, full
enumeration of the blind list, `Measured`, `joinTraces`, the measured-path note,
`scanall.go`'s forwarding, `by_dimension.db`. There is no second note surface.

### The zero-guard is deleted, not commented out

`wholeScanUnproductive`'s `total > 0` short-circuit is **removed outright**.

With configured paths as the unit, `db.go`'s early return on
`len(SchemaPaths) == 0` guarantees `total >= 1` by the time the floor is
reached, so the condition is dead code.

This is the single easiest step in the change to second-guess, and the reasoning
is recorded here because a reviewer will reasonably want to keep it
"defensively". **Keeping it is the less safe option.** That guard is precisely
what hid this bug: handed `total == 0` it short-circuited to false, the floor
never engaged, and the scan scored 100. Deleted, a hypothetical `total == 0`
evaluates `0 == 0` → true → **not measured**. Unreachable by construction, *and*
fail-closed if the construction ever changes. Keeping it would defend the false
all-clear, not against it.

## Rejected alternatives

| # | Rejected | Why |
|---|---|---|
| D1 | Keep the flat `[]SourceFile` and add a parallel `[]unresolvedPath` | A parallel list forces the floor to compute over the union of two vocabularies — reintroducing the exact class of bug being fixed (a list the predicate forgets to count) and requiring a second cap, ordering and defect machinery to stay in sync with the first. |
| D2 | A separate path-level reason vocabulary with its own note producer | `defect()` already gives full enumeration and defect-first ordering, and `Result.Note` has four producers by construction, not five. The cost of reusing the closed vocabulary is stated below and was taken knowingly. |
| D3 | Keep `total > 0` as a defensive guard | See above. Dead by construction, and fail-open if the construction moves. |
| D4 | Reuse the existing file sentence for the new reason | Its denominator is PATHS, not files. "1 of the 0 configured schema file(s)" is not a rounding error, it is a lie about what codefit was pointed at. The new reason gets its own sentence with its own denominator. |
| D5 | A new response field for the Case 2 trace | Already plumbed: `db.go` computes the trace unconditionally, `joinTraces` composes it into the measured note, and `scanall.go` forwards it. I4 is satisfied by the note, not by a new surface. |
| D6 | Make `flywayOrderedSQL` recurse into subdirectories now | See the deferral below. |

## DECLARED COST

Of the same class as the cost `internal/sensors/db/unread.go` already accepts
one level down, and stated in the same terms.

**A project whose configured `schema_paths` genuinely resolve to nothing — a
migrations directory that is really empty of schema files, a path pointed at a
package that holds only an embed stub — loses its db score instead of scoring
100.** Losing a score for a schema nobody read is the correct direction: a score
of 100 over content codefit never opened is the single failure the whole floor
exists to make impossible. The note says exactly which path, states the
consequence, and states what to do about it.

**`unreadSource.Path` becomes heterogeneous.** For the five file-level reasons
it is a resolved file path; for `reasonResolvedNoFiles` it is the
`database.schema_paths` entry exactly as written in `.codefit.yaml`, because
there is no file to name — the entry itself is what the developer has to fix.
The alternative was a second list, and a second list is a second thing the
predicate can forget to count. Nothing keys off `Path` (it is rendered into a
note and nothing else), so the heterogeneity is confined to the sentence a human
reads, and each unit gets its own sentence so the two counts are never mixed.

## The note states the action, never the diagnosis

The note names the configured path exactly as written, states that it resolved
to no readable schema file at all, states the consequence (nothing under it
reached any rule; this is not a clean bill of health), and states the **action**:
point the entry at the schema files it should audit, or remove it from
`database.schema_paths` so the scan stops claiming to cover it.

It never names an extension or an ordering rule. Which shape the resolver
accepts is a property of *today's* resolver and ages into a lie the moment the
resolver grows; the outcome and the action stay true. This is ADR 0034 §2.8's
measurement/diagnostics boundary applied to a path instead of a file.

## Deferred, named, and not silent

**Nested schema trees.** `flywayOrderedSQL` lists one level deep, so a directory
whose `.sql` sit in a subdirectory resolves to zero files. This change ends the
*lie* — that case is now reported as not-measured with the path named — and
deliberately does **not** add the *capability*. Recursing without a
cross-directory ordering rule would have to pick an order silently, which is the
same trap one level down that the golang-migrate naming convention already sets
here (`NNN_name.up.sql` does not match `^V(\d+)__`, so it orders lexically:
1, 10, 2). The limit is declared at `flywayOrderedSQL` itself, where a reader
meets it.

**`ScanDBResponse.Score`.** It is an `int` without `omitempty`, so a
not-measured response serialises `"score": 0` beside `"measured": false`. That is
pre-existing and unchanged here — byte-identical before and after — but this
change makes it fire more often, since Case 1 now lands on that shape. Moving it
to a pointer is a JSON contract change that deserves its own controls, and
bundling it would have made a failing control here ambiguous. Recorded as a
named follow-up in `docs/roadmap.md`.

## Consequences

- A configured schema path can no longer disappear from the audit. `len(Paths)`
  always equals `len(database.schema_paths)`, locked by a construction test.
- Case 1 flips from `measured: true` / score 100 to `measured: false`, with
  `by_dimension.db: null` and `summary.db` absent in `codefit-scan-all`.
  Declared in `CHANGELOG.md` with the migration.
- Case 2 keeps `measured: true` and every real finding, and the note names the
  path that resolved to nothing.
- Prerequisite relation: this lands **before** the `codefit init` schema-path
  detection work. That change makes a configured *directory* the ordinary input
  rather than the rare one, and init-time proof does not bind scan time — a
  directory that reconstructed at `init` can later hold no `.sql`. Landing it
  first would have made this false all-clear routine.
