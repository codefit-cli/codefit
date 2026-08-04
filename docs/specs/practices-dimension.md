# Spec — The `practices` dimension (RF-05)

**Status:** draft · **Phase:** 3, thread H1 · **Target:** `v0.3.0` line (alphas on the way;
the MINOR lands only when Phase 3 is complete)

A dimension, per ADR 0016: a **sensor + its rules + a permanent standalone MCP tool**
(`codefit-check-practices`), developed standalone until complete, whose mandatory close is
being wired into `scan-all`.

## What this thread actually is

It is not "add a sensor to run rules that already exist". The rules that exist do not meet
the project's own doctrine, and one of them is a **false affirmation** — the class of defect
this whole tool exists to catch in other people's code.

`internal/providers/golang/practices.go`, five rules, all emitted at `Confidence: 1.0` with
no `Probabilistic` flag — every one an affirmation:

| id | what its message CLAIMS | what the code CHECKS |
|---|---|---|
| PRAC-004 | "started without a visible WaitGroup or channel to synchronize it" | **every `go` statement.** No WaitGroup/errgroup/channel detection exists in the file |
| PRAC-001 | "**Possibly** ignored error" | last LHS is `_` and RHS is a call — and it says "possibly" at certainty 1.0 |
| PRAC-003 | "interface{}/any discards type information" | every empty interface, including generic constraints and variadic sinks where `any` is idiomatic |
| PRAC-005 | "library code should return errors instead" | any `panic` outside a `_test.go` file — it does not distinguish a library from a `main` |
| PRAC-002 | defer governed by a loop | matches its claim |

PRAC-004 asserts a fact it never established. ADR 0017 forbids exactly this. Shipping it
would make codefit an auditor that does the thing it audits for.

## R1 — A practices rule affirms only what it checked

The dimension stays **deterministic**, as RF-05 and the PRD tool table declare — no
`codefit-surface-practices` family. That is the right call: `any` is present or it is not,
`console.log` is there or it is not, an async function has a catch or it does not. These are
decidable by shape.

The consequence is a rule-level bar, applied to every rule this thread ships or keeps:

**A rule's message may state only what its code established.** A rule that cannot check what
its message claims has two honest outcomes — teach it to check, or drop it. There is no third
one, and "soften the wording to match the weaker check" is not an escape: a finding nobody
can act on is noise, and noise in an affirmation channel is worse than silence.

Applied to what exists: PRAC-004 either detects synchronization or goes. PRAC-003 either
excludes the idiomatic positions (generic constraints, variadic parameters, marker
interfaces) or goes. PRAC-001 drops "Possibly" and narrows to what it can prove, or goes.
PRAC-005 stops hardcoding `_test.go`.

Each decision is recorded, including the drops. A rule removed for being unsound is a
coverage fact and belongs in the manifest, exactly as `DB-012` and `DW-022` are recorded as
permanently not covered.

## R2 — Test-path handling goes through the machinery that already exists

PRAC-005 decides "is this a test?" with `strings.HasSuffix(p.path, "_test.go")`. The project
already has `config.PathCriticality`, the Go provider already declares
`Test: ["**/*_test.go"]`, and the security sensor already applies it on the way out
(RF-10). The practices sensor applies criticality the same way. No rule carries its own
notion of a test file.

## R3 — Findings without a fingerprint never reach the baseline

`observedFrom` drops any observation whose `FP` is empty. `stampFingerprints` lives inside
the security sensor and is the declared single boundary where baseline identity is assigned.
Practices findings get none today, so **wiring the sensor without this would produce a
dimension the baseline silently cannot track** — every run reporting the same findings as
new, forever, with `baseline-accept` unable to hold them.

The practices sensor stamps fingerprints. Whether the stamping helper moves to a shared home
or is duplicated is an implementation choice; that it happens is not.

## R4 — Practices is not endpoint-shaped

`AggregateEndpoints` anchors only on `idor`/`authz` surface items and bins findings onto the
nearest preceding anchor, defaulting to line 0 = module scope. A practices finding in a file
with no handler would become an `EndpointReport` at line 0 and land in the **actionable**
bucket at the top of the sort order — a `console.log` presented with the same weight as a
missing ownership check.

ADR 0016 settled this: a non-endpoint dimension gets **its own section**, never the
endpoint-centric bucketing. Practices follows the DB precedent — its own section in
`ScanAllResponse`, `omitempty`, absent entirely when the sensor did not run.

## R5 — The dimension declares its categories, and the disjointness lock knows it exists

`OwnedCategories()` returning the dimension string. The enforcement test
`TestOwnedCategories_NoOverlap` iterates a hand-maintained `registeredSensors()` list, and
its own comment records that a sensor added without being listed there is the one gap Go
cannot close automatically. **Registering the practices sensor in that list is part of this
thread, not a follow-up** — an unregistered sensor is exactly how a category silently
escapes baseline scope.

## R6 — The weight re-balance

`practices` is a dimension of its own, with its own weight. Approved: **complexity 15 → 10,
practices 5**, keeping the sum at 100.

It carries the smallest weight by doctrine. codefit's rector principle is that it audits what
the developer never sees; `any`, `console.log` and a missing catch are the *most* visible
things in normal development — a linter flags them in the editor. That the dimension exists is
right; that it should weigh like `db` is not.

The 5 points come from `complexity`, which is never measured (post-v1.0), so **the re-balance
moves no score produced today** — `Compute` accumulates `totalWeight` only over measured
dimensions. Locked as a no-regression test.

Blast radius, all of it in scope:
- `scoring.DefaultWeights()`.
- **`missing_weights_test.go` inverts.** It hardcodes `// practices is NOT in DefaultWeights`
  and asserts `MissingWeights([practices], DefaultWeights()) == [practices]`. It must be
  rewritten against a dimension that is genuinely unweighted.
- `internal/mcp/score_test.go` asserts `review`/`complexity`/`tests` are null; `practices`
  joins them until the sensor runs.
- **ADR 0021 goes stale.** Its line "this guard protects a future dimension (e.g. practices,
  absent from DefaultWeights)" becomes false. ADRs are immutable — a superseding note, never
  an edit.
- The PRD's defaults line and config sketch. The PRD is exempt from reflect-today, so this is
  recorded, not corrected.

**Declared, not fixed here:** `cfg.Report.ScoreWeights` is validated to sum to 100 but is
never read — `DefaultWeights()` is hardcoded at both call sites. The config knob does nothing
today. That is a pre-existing lie in the config surface and it gets a declared limit, not a
silent pass.

## R7 — The tool is standalone and permanent

`codefit-check-practices` — its constant already exists, unregistered. Input `{root,
language, changed_files?}`, honouring layer 0 like every other scanning tool, and returning
the scope block on the same contract.

## Out of scope, stated

- **`codefit-review-code` and `codefit-scan-tests`** are threads H3 and H2 — not this one.
- **The `sensors.practices.rules` per-rule severity map** the PRD sketches is not modeled in
  `config` today (`Practices` is a bare toggle). It is a slice of this thread, taken after
  the rules are sound: configuring the severity of a rule that should not exist is backwards.
- **The Go provider is unreachable through MCP.** `providerForLanguage` maps only TypeScript,
  and there is no `internal/providers/golang/coverage.go`. So `codefit-check-practices` on a
  Go project cannot run regardless of this thread. Declared here because it is the reason the
  TypeScript rules — not the Go ones — are what make this dimension a product.

## Slicing

| slice | content |
|---|---|
| **S1** | The weight re-balance (R6) alone: `DefaultWeights`, the inverted test, the score-shape test, the ADR + its superseding note. No sensor yet. Small, isolated, and it makes the `MissingWeights` guard stop lying about the future. |
| **S2** | The rules audit (R1, R2): each existing Go PRAC rule taught to check what it claims, or dropped with its reason recorded in the manifest. No new rules. |
| **S3** | The sensor + the standalone tool (R3, R4, R5, R7): walk, criticality, fingerprints, `OwnedCategories`, registration in the disjointness list, `codefit-check-practices`. |
| **S4** | The TypeScript rules — `rules/typescript/practices/*.yaml`, killing the `AnalyzePractices` stub. This is where the dimension becomes a product. |
| **S5** | The per-rule severity config (`sensors.practices.rules`). |
| **DoD** | Wired into `scan-all` with its own section and its `by_dimension` entry. Per ADR 0016 the dimension is not ready until `scan-all` runs it. |

## Test contract

Each proven by **mutation** — break the behavior, watch it fail, restore, watch it pass.

1. The weight re-balance moves **no** score produced today: same findings, same measured set,
   identical `ScoreSummary` before and after. *(Mutation: give `complexity` a measured value.)*
2. `MissingWeights` still fails loudly for a genuinely unweighted measured dimension.
3. Every rule this thread keeps: a fixture that has the thing the message names, and a fixture
   that has the *shape* but not the claimed property, which must NOT fire. This is the direct
   test of R1 — PRAC-004's second fixture is a goroutine with a `WaitGroup`.
4. A practices finding carries a non-empty fingerprint and survives a baseline round trip:
   observed, accepted, then reported as `known` rather than `new`.
5. A practices finding in a path classified `test` is degraded by criticality, with no rule
   consulting the filename itself.
6. Practices findings never enter `AggregateEndpoints`: a project with a practices finding and
   no handler produces no endpoint at line 0.
7. `TestOwnedCategories_NoOverlap` includes the practices sensor, and fails if its categories
   collide with another sensor's.
8. `scan-all` without the practices sensor is **byte-identical** to today — the section is
   absent, not empty, and `by_dimension.practices` is `null`, never `100`.
