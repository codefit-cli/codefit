# Spec — Change scope (filtering-pyramid layer 0)

**Status:** draft · **Phase:** 3, thread H0, slice S1 · **Target:** `v0.2.7` (a PATCH:
no PRD phase closes here)

Layer 0 of the filtering pyramid (PRD §19) is the cheapest tier: *do not analyze a
file that did not change*. It has existed as an unwired abstraction since Fase 0 —
`internal/core/pipeline` declares `LayerChanges` and `internal/core/cache` implements
a content-hash finding store, and both say `Status: INERT` in their own `doc.go`,
with zero production importers. This slice makes the scope real. The cache (S2)
reuses findings across runs; this slice only decides **what is audited at all**.

The regression-risk half of RF-06 (Fase 3, thread H2b) cannot exist without it: a
report of "what your change may have broken" needs to know what changed.

## Why the agent supplies the scope, not git

codefit does not shell out to git and does not read `.git`. Two reasons, both
standing project doctrine:

1. **codefit has no power over the user's git** (CLAUDE.md, autonomy principle). It
   does not read the index, does not diff refs, does not assume a branch model.
2. **MCP-first**: the caller is an AI coding agent that already knows which files it
   touched — it just wrote them. Asking git would be codefit re-deriving, worse and
   less portably, a fact the caller already holds.

So the scope is an **input**: an optional list of project-relative paths on the scan
request. Absent scope means a full audit.

This also retires `AuditContext.Since`, a dead `string` field whose comment promised
"a git ref for incremental (`--since`) mode". It has never had a reader or a writer.
A field that names a capability codefit does not have is the same class of lie as a
manifest that over-promises; it is replaced by the scope this spec defines, not kept
alongside it.

## The honesty contract (the reason this slice is not trivial)

A partial audit that is indistinguishable from a full one is a lying auditor. Every
requirement below exists to make the narrowing **visible**.

### R1 — A partial scan declares itself

Every scan response that ran under a partial scope carries a `scope` block:

```json
"scope": {
  "mode": "partial",
  "requested": 3,
  "audited": 3,
  "auditable_total": 412,
  "unmatched": ["src/deleted.ts"],
  "note": "Partial audit: 3 of 412 auditable files were in scope. Findings, score and `blocked` describe only those files; the rest were not examined in this pass."
}
```

- `mode` is `full` or `partial`. A full scan still emits the block, with `mode: full`
  and an empty note — the field's presence is not conditional, so a consumer never has
  to infer the mode from an absence.
- **`note` MUST be non-empty whenever `mode` is `partial`.** Test-locked, in both
  directions.

### R2 — `blocked: false` from a partial scan is a narrower claim

`scoring.IsBlocked` is unchanged and stays non-configurable. But under a partial
scope, `blocked: false` means *no critical in the audited slice*, not *no critical*.
The note states this. `blocked: true` needs no caveat — a critical found is a critical
found.

### R3 — Requested-but-never-seen files are reported

If the caller passes a path the audit never reaches — deleted, wrong extension,
outside the project, excluded by a skip dir — it lands in `unmatched`. Without this,
an agent that passes three wrong paths receives "0 findings" and reads it as clean.
`unmatched` is the difference between *audited and clean* and *never looked*.

### R4 — A dimension whose inputs are all out of scope is NOT MEASURED

The DB dimension reads `database.schema_paths`, not a repository walk. If no schema
path is in scope, the DB sensor did not run, so its score is `null` (not measured)
and its categories stay out of the baseline's category scope. This needs no new
mechanism: `by_dimension` null (PRD §21) and `OwnedCategories` scoping (ADR 0019)
already model exactly this, and the existing `scoring.MissingWeights` guard still
fails loudly on a measured dimension with no weight.

### R5 — A partial scan MUST NOT be able to prune the baseline

This is the requirement that makes the slice correct rather than merely functional,
and it is a **live corruption risk**, not a hypothetical.

`baseline.Diff(prev, observed, scope)` already guards against a false `gone`, but its
`scope` is a set of **categories** (ADR 0019): an item is eligible to be `gone` only
if a sensor owning its category ran. Locked by
`internal/core/baseline/scoped_test.go`.

**That guard does not cover a file scope.** A security finding in `x.ts` that was not
scanned this pass still belongs to the `security` category, which *did* run. `Diff`
sees it unobserved and in scope, and marks it `gone`. `codefit-baseline-prune` removes
what is `gone`. A partial scan would therefore **delete the audit memory of every file
it did not look at** — silent, and in the direction of going blind.

The fix: the baseline scope becomes **two-dimensional**. An item is eligible for
`gone` only if

```
categoryRan(item.Category)  AND  fileScope.Includes(item.File)
```

The existing fail-safe generalizes: a zero-value scope includes nothing, so a caller
that forgets to pass one under-reports and never prunes. Under-report, never corrupt.

Additionally, `codefit-baseline-prune` **does not accept a scope at all**. Pruning
audit memory always requires a full audit. This is a deliberate asymmetry: scanning
may be cheap and partial; forgetting may not.

## The scope type

`internal/core/scope` — a pure leaf, importing nothing from codefit.

```go
func Full() Scope                      // includes everything
func Of(rels []string) Scope           // includes exactly these, canonicalized
func (s Scope) IsFull() bool
func (s Scope) Includes(rel string) bool
func (s Scope) Files() []string        // sorted; nil when full
```

- **Canonicalization on both sides.** Construction and lookup both run
  `filepath.ToSlash(filepath.Clean(p))`. This repository has already paid for
  separator drift once (`v0.2.6`, Windows checkout portability); the scope is not
  going to reintroduce it.
- **Project-relative paths only.** Resolving a caller's absolute path against the
  project root happens in the MCP adapter, which knows the root; the leaf does not.
- **Absent or empty means FULL.** An empty list is not read as "audit nothing". The
  fail-safe direction for a scope is to audit *more*, never less; an agent that means
  "nothing changed" does not call the tool.
- The zero value `Scope{}` includes nothing — deliberately the safe direction for
  `Diff`'s prune guard (R5), and the reason `Full()` is an explicit constructor rather
  than the zero value.

## Slicing

| Commit | Content |
|---|---|
| **A** | `core/scope` leaf + `baseline.Diff` two-dimensional scope (R5). Core only, no wiring. |
| **B** | `AuditContext.Scope` replaces the dead `Since`; the security walk and the scan-all cross walk consult it; R4 falls out of the DB dimension's existing config-driven inputs. |
| **C** | The agent-facing surface: `changed_files` on `codefit-scan-security` and `codefit-scan-all`, the `scope` block (R1–R3), prune explicitly excluded (R5). |

`codefit-scan-db` takes no `changed_files`: its inputs are the configured schema
paths, and a caller who wants to know whether the schema changed passes it or does
not call the tool.

## Test contract

Every item below is a test, and each is proven by **mutation** — break the exact
behavior, watch it fail, restore, watch it pass — with both runs recorded in the
commit or PR.

1. `Full()` scope through `Diff` produces a delta **byte-identical** to today's. This
   is the no-regression floor for R5.
2. An in-scope, unobserved, category-ran item **is** `gone` (today's behavior, kept).
3. An **out-of-scope**, unobserved, category-ran item is **NOT** `gone`, is absent
   from the delta, and is carried forward to `Next.Items` verbatim. *This is the
   corruption test; the mutation is removing the file condition from the eligibility
   check.*
4. Zero-value `Scope{}` prunes nothing.
5. `mode: partial` with an empty `note` fails; `mode: full` with a non-empty note
   fails.
6. A requested path the walk never reaches appears in `unmatched`.
7. Scope canonicalization: `src\a.ts`, `./src/a.ts` and `src/a.ts` are the same file.
8. A partial scan whose scope contains no configured schema path reports the DB
   dimension as `null` (not measured), not `100`.

## Out of scope for this slice

- The content-hash cache (S2) — `core/cache` stays inert until then. Its wiring
  carries its own hazard, already identified: the cache key must include the codefit
  and rule versions, or an upgrade that adds rules serves stale findings for an
  unchanged file and reports "clean" under rules it never ran.
- Any change to what codefit detects. No rule moves in this slice; every finding,
  surface item and baseline fingerprint is identical for a full scan.
