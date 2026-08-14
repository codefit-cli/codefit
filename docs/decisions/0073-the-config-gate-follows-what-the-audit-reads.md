# 0073 — The config gate follows what the audit reads

Date: 2026-08-14
Status: accepted
Extends: [ADR 0071](0071-init-never-refuses-over-language-it-declares.md) (init
declares its gaps rather than refusing) and
[ADR 0018](0018-sql-ddl-parser-declared-subset-incremental-reducer-input-selection.md)
(the SQL-DDL parser's declared subset and input selection).
Supersedes nothing.

## Context

`internal/scaffold/config.go` gated the entire `database:` block behind
`{{if .ORM}}`, and `database.schema_paths` sat **inside** that gate.

Those two fields are not interchangeable, and a census of the tree says so
plainly:

| field | production readers |
|---|---|
| `config.Database.ORM` | **zero**. `config/validate.go` has no `allowedORMs` and never touches it. It is unvalidated free text that round-trips and is read by nothing. |
| `config.Database.SchemaPaths` | three files, six sites: `sensors/db/db.go`, `mcp/scandb.go`, `mcp/scanall.go`. |

So the input the DB dimension actually consumes was gated on a field the DB
dimension ignores. That is not a stylistic mismatch; it produces wrong output in
both directions, and one of them was already live.

**The live direction — drizzle, reproduced with the real binary.** `detect.go`'s
`enrichTypeScript` sets `ORM = "drizzle"` (or `"typeorm"`) and **never** sets
`SchemaPaths`. A drizzle project's generated config was, in its entirety:

```yaml
database:
  orm: drizzle
```

A `database:` header that looks configured and configures nothing any sensor
reads. Worse, `cli/init.go`'s `writeSchemaGap` early-returned on a non-empty
ORM, so the schema-gap declaration that
[ADR 0071](0071-init-never-refuses-over-language-it-declares.md) added — the
sentence telling the developer this config audits no schema — was **suppressed
for exactly the project that needed it**. Probed with a positive control: zero
mentions of `schema_paths` or `migration` anywhere in the generated file, and no
gap section at all in the report.

**The future direction.** The moment SQL-migration detection fills `SchemaPaths`
for a Flyway project, `ORM` stays `""`, so under the old predicate that project
would still get no `database:` block *and* a config still claiming schemas are
not detected — while holding a detected one.

**Why no test caught it.** Both existing locks
(`TestRenderConfig_DeclaresTheSchemaGapWheneverNoORM`,
`TestFormatReport_SchemaGapDeclaredWheneverNoORM`) set `ORM` **and**
`SchemaPaths` together in their counter-cases. `info.ORM != ""` and
`len(info.SchemaPaths) > 0` were therefore true together in every case they had,
and the two candidate predicates were indistinguishable to them. The suite could
not see the defect, and would not have seen the fix either.

## Decision

**The gate follows the field the audit reads.** The `database:` block is emitted
iff `len(SchemaPaths) > 0`, in all three sites — the template gate, the template
else-branch, and `writeSchemaGap` — and they move **together**. A half-applied
move is a state the suite cannot observe from one side alone (control C4 below
demonstrates it: the report half stays green while the config half goes red).

Six decisions follow from it.

**D1 — the skill's claim is transferred to a baited lock, not conditioned now.**
`skill.go` tells an agent that "generated configs for a project like this carry
NO such key". That stays true today and becomes conditional the day detection
lands. Conditioning it *now* would mean adding a `HasSchemaPaths` field, a
template branch and a test for a state **no real run can produce** — agent-facing
prose about an unreachable case, provable only by a hand-built struct, in a diff
whose whole point is a predicate move.

Instead the obligation is transferred **mechanically**. The R6 regression lock
uses a **baited fixture**: a temp root holding `pom.xml` *and*
`db/migrations/V1__init.sql` with real DDL. Today detection finds neither, so no
block is written, the claim holds, and the lock is green. The day SQL-migration
detection lands, that same fixture acquires `SchemaPaths`, the config gains a
live `schema_paths:` key, and the lock goes **red** — forcing the decision rather
than recording a wish. The assertion is a cross-artifact equivalence over the
bytes `Generate` actually wrote, anchored on the claim sentence so a rewording
fails loudly instead of passing vacuously.

An unbaited fixture would sail through that change still green, proving nothing.

**D2 — `orm:` stays, and is labelled.** Deleting a valid, round-tripping,
user-visible key from a committed artifact buys zero functional gain and
coarsens the rollback of a change that is otherwise three predicate lines. But an
unread field printed beside read ones invites the belief that setting it does
something — and the autonomy principle says a consequence is informed, not left
to be discovered. So `orm:` is emitted when non-empty, with one comment stating
that no sensor reads it and that `schema_paths` is what turns the dimension on.

**D3 — drizzle/typeorm lose the whole block, `orm:` included, and are told.**
With no `SchemaPaths` there is no block, so those projects lose the `orm:` key
they used to get. Accepted: that block configured nothing. The developer meets
the change three ways — the report still prints `Detected: orm drizzle` (the
detection *fact* survives), the report now prints the schema-gap section it used
to suppress, and `CHANGELOG.md` carries a **Changed** entry.

**D4 — `type:` is named with its consequence, never made required.** `""` stays
valid to the loader; the *instruction* was the defect. The commented example
showed `schema_paths` alone, steering a MySQL or SQL Server user straight into
`sqlDialectParser("")` → `sqlddl.New()` → PostgreSQL, silently. The example now
shows `type: "postgresql"  # postgresql | mysql | sqlserver` and states what
omitting it costs: the DDL is parsed as PostgreSQL with no announcement, so a
MySQL/T-SQL schema is silently mis-parsed and every DB finding afterwards reasons
over a schema that does not exist. `sqlite` is named as the one value codefit
refuses outright rather than guessing at. Fixing `TODO(J)`'s default itself is
**out of scope** — only the instruction that steers into it.

**D5 — `writeSchemaGap` reads `ORM` for WORDING, never to gate.** A report
printing `orm drizzle` under *Detected* and `Not configured — database schema`
below it states two true facts that read as a contradiction. One clarifier
explains how both hold at once, rather than leaving the developer to reconcile
them.

**D6 — the skill's own example gets the same `type:` fix.** Separable from D1,
and the skill is the *first* artifact an agent reads: the same silent wrong parse
would otherwise be taught upstream of everything else.

## Rejected alternatives

- **Delete `orm:` as a dead field.** Rejected as D2 explains. It is also the
  exact break used as mutation control C1, because "a reviewer cleaning up a dead
  field" is the realistic way it would happen.
- **Gate on `ORM != "" || len(SchemaPaths) > 0`.** Preserves the drizzle
  orm-only block — the defect — under a longer predicate.
- **Condition the skill claim now (D1 option A).** Rejected above; the bait is
  strictly stronger, because it fails at the moment the fact changes rather than
  documenting an intention.
- **Make `type:` required.** Rejected: `""` is valid, `config.validate` accepts
  it, and requiring it would break existing configs to fix a comment.
- **Add a `markerLiteralAllowlist` entry if the AST census trips.** Rejected
  structurally: an entry there **is** the defect signal (R7). Control C7 confirms
  the census trips on a marker name in the new prose; the fix is rewording, and
  `marker_literal_guard_test.go` is byte-identical to `main`.

## Consequences

**What changes for a user.** Re-running `codefit init --force` on a
drizzle/typeorm project drops the `orm:`-only `database:` block and adds the
schema-gap section to the report. Existing committed configs are untouched — only
a re-run rewrites anything, and what it removes configured nothing. A Prisma
project's output is unchanged in every live key, value and order; the only delta
is comment text, locked by the parity golden.

**What is now locked that was not.** `len(SchemaPaths) > 0` is asserted in both
directions, in both artifacts. The `schema without orm` case is unreachable
through detection today and is therefore built by hand — the one place this
project's prefer-the-real-parser rule cannot apply, stated in a comment at each
site. It cannot pass on the old predicate **by construction**: `writeSchemaGap`
was `if info.ORM != "" { return }`, so with `ORM == ""` the old code had no path
at all that skipped the section.

**Seven mutation controls**, each a runtime break that leaves the tree building
(`go build ./...` exit 0 under every one):

| # | break | red test |
|---|---|---|
| C1 | delete `orm: {{q .ORM}}` from the template | `TestGenerate_PrismaConfigParity` — both halves |
| C2 | restore the template gate to `{{if .ORM}}` | `TestInit_DrizzleProjectOmitsDatabaseBlockAndDeclaresGap` |
| C3 | restore `writeSchemaGap` to `info.ORM != ""` | `TestFormatReport_SchemaGapDeclaredWheneverNoSchemaPaths` |
| C4 | half-apply: template on `.ORM`, `writeSchemaGap` moved | `TestRenderConfig_..._WheneverNoSchemaPaths` reds while the report half stays green |
| C5 | make the gate unconditional | `TestGenerate_SkillClaimHoldsForBaitedMigrationDir` — an empty `database:` for everyone |
| C6 | delete the `type:` line from the example | `TestRenderConfig_CommentedExampleNamesTypeAndItsConsequence` |
| C7 | name a registry marker in the new prose | `TestScaffoldNamesNoMarkerFileByHand`, with **no** allowlist entry added |

C6 is worth recording for a second reason: its first run went red on the wrong
assertion. `strings.Contains(comments, "type:")` stayed green with the example
line deleted, because the paragraph explaining the consequence names `type:` too
— prose describing the example stood in for the example. The assertion was
tightened to look for an indented key line, and C6 re-run against it.

## Follow-up

**Detection of SQL migration directories** is the named next change, and it is
the one that will trip R6's bait. When it does, the decision it forces is
whether `skill.go`'s claim becomes conditional or the fixture stops acquiring a
schema — not whether to silence the lock.

Out of scope here and stated so it is not assumed: the nested-tree capability
([ADR 0072](0072-a-configured-schema-path-always-leaves-a-trace.md) is unrelated
to it), `TODO(J)`'s PostgreSQL default itself, and golang-migrate's
lexical-ordering trap.
