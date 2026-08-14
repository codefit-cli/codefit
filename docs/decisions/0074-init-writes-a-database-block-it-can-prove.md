# 0074 — `init` writes a database block it can prove

Date: 2026-08-14
Status: accepted
Extends: [ADR 0072](0072-a-configured-schema-path-always-leaves-a-trace.md)
(a configured schema path always leaves a trace) — this ADR is about what codefit
may put *into* `database.schema_paths` in the first place, and 0072's floor stays
underneath it unchanged. Builds on
[ADR 0018](0018-sql-ddl-parser-declared-subset-incremental-reducer-input-selection.md)
(schema parsing resolves by input shape, not by language) and
[ADR 0071](0071-init-never-refuses-over-language-it-declares.md)
(init never refuses over language — it declares). Supersedes nothing.

## Context

`codefit init` detected a schema from exactly one shape: a Prisma
`schema.prisma`. Every other project — a Java service with Flyway migrations, a
Go service with plain SQL DDL — got a `.codefit.yaml` whose `database:` section
was a comment block, and that block hard-coded an **invented** example path:

```
#   database:
#     type: "postgresql"   # postgresql | mysql | sqlserver
#     schema_paths:
#       - "db/migrations"
```

Nothing in the file distinguished that path from one codefit had found. A
developer whose migrations happened to live in `db/migrations` would read an
example as a finding; one whose migrations lived elsewhere would read a finding
as an example. The report beside it said SQL migration directories "are NOT
detected" — a sentence about codefit's capability, true of every project at once
and therefore silent about the one in front of the developer.

Worse, the schema half of detection was gated on the *language* half: `Detect`
returned early when no language provider resolved, so an undetected root ran no
schema enrichment at all. The projects a language-independent SQL-DDL parser was
built for were exactly the projects detection never looked at.

## Decision

**Detection never GUESSES a schema. It may only PROVE one.**

A directory becomes a live `database.schema_paths` entry only when all of the
following hold, measured in this order:

1. **The apply order is proven.** At least one `.sql` at the directory's own
   level, and *every* `.sql` at that level carries an integer version the
   scan-time resolver's `^V(\d+)__` matches. One stray filename disqualifies the
   whole level, because a name `flywayOrderedSQL` cannot version lands in its
   LEXICAL bucket, and from that point the directory's apply order is a fact
   about the alphabet rather than about its contents.
2. **The real parser reconstructs at least one table.** The proof runs
   `db.ResolveSchemaPath` (the literal scan-time reader, so Flyway ordering and
   the byte-order-mark decode come from the source of truth, not a copy) and then
   `schemasource.ParserForPaths(root, paths, "")` — the *same* binding
   `codefit-scan-db` makes when `database.type` is unset — and counts
   `len(schema.Tables)`.
3. **Exactly one directory proved.** Two or more, and NONE is written.

Anything else gets the `database:` block **commented, naming the real path and
the reason**. Absence stays first-class (ADR 0072): a config that audits no
schema is strictly better than one that audits the wrong schema.

### Discovery depth is 6. Read depth is still 1.

These are two different numbers and this ADR states them side by side so a future
reader cannot merge them.

**Read depth is 1**, unchanged. `flywayOrderedSQL` reads exactly one level of a
configured directory; that is its DECLARED LIMIT and ADR 0072's deferral. This
change does not touch it, and `internal/sensors/db/sources.go` is byte-identical
across this change as the evidence.

**Discovery depth is 6** — directory levels below the project root, root itself
being 0. It bounds only the *search* for candidate directories. The number comes
from the layout this change exists to serve: Flyway's canonical location on a JVM
project is `src/main/resources/db/migration`, five levels down. A bound of 3 or 4
would miss the single most common shape of the exact case codefit was blind to,
which would be self-defeating; 6 is that depth plus one level of headroom. Deeper
would contradict artifacts codefit already ships — both the generated config and
the generated skill instruct the developer to run `codefit init` per sub-project
root, and language detection does not recurse at all.

The two never collide because **candidacy is evaluated per DIRECTORY, never per
subtree**. `db/nested/sql/V1__a.sql` makes `db/nested/sql` a candidate and leaves
`db/nested` — which holds no `.sql` at its own level — out entirely. Discovery
therefore cannot hand the resolver a path whose files live below the level it
reads.

The bound is DECLARED, not silent: the generated config and the init report both
state it, so "no schema source found" can never be mistaken for "this project has
none".

### `discoverySkipDirs` is a UNION, and the union is locked

```
discoverySkipDirs = dirsToSkip ∪ buildOutputDirs ∪ fixtureDirs
```

`dirsToSkip` is what the route-handler walk already prunes. The other two are
correctness findings, not tidiness:

- **Build output.** Maven copies `src/main/resources/**` into `target/classes/**`,
  so a project that has been compiled holds two directories with byte-identical
  proven DDL. Without pruning, the headline case trips the two-proven rule and
  gets no live block on every project that has ever run `mvn package` — an answer
  nobody could act on when one of the twins is a build artifact.
- **Test fixtures.** Found by dogfooding, not by reasoning: the first run of this
  feature over codefit's own repository walked five candidate directories under
  `testdata`, proved exactly one — a two-table Flyway fixture written to exercise
  the DB sensor — and wrote it into codefit's own `.codefit.yaml` as codefit's
  schema. Exactly one proved, so the ambiguity rule stayed silent, and the answer
  was confident, measured and wrong.

The union is computed rather than listed, and the relation is locked two ways: a
test walks a real proving migration directory planted under every skipped name
and asserts none is discovered, and a second test asserts discovery skips at
least everything the route walk skips. A name added to `dirsToSkip` therefore
cannot drift out of discovery.

Rejected: de-duplicating twins by identical content. It cannot say *which* copy
is the source without a policy, and picking one would be the guess this whole ADR
removes. Skipping build output is a fact about build systems; choosing between
twins is not.

## DECLARED COST — the dialect is never measured

In ADR 0072's format, because it is the same kind of debt.

codefit does not sniff the SQL dialect (roadmap P0-11 owns that). The proof
therefore runs under the default binding, which is PostgreSQL. **The proof says
the DDL reconstructs; it does not say the dialect is right.**

Three things follow, all of them declared rather than hidden:

1. **Init-time proof and scan-time behaviour cannot disagree.** The proof calls
   the identical binding `scandb.go` makes when `database.type` is absent. The
   residual is "the dialect may be wrong", never "init and the scan read
   different schemas". That is strictly smaller than the state this replaced,
   where the config hand-wrote `type: "postgresql"` into an example with no
   measurement behind it at all.
2. **A live block carries a commented `type:` line directly above the key**,
   naming the allowed values and the consequence of leaving it out, and the init
   report names the dialect the proof ran under in the same breath as the proof.
3. **A dialect that does not reconstruct never goes live.** This was MEASURED,
   not assumed: a MySQL-flavoured migration set (backtick identifiers,
   `AUTO_INCREMENT`, `ENGINE=InnoDB`) fed to the default binding reconstructs
   **zero** tables — both `CREATE TABLE` statements land in the parser's
   unreducible bucket. The proof gate requires at least one table, so such a
   directory fails the proof and receives the commented block. codefit reaches
   the correct outcome without ever guessing a dialect.

   `TestProve_MySQLFlavouredDDLDoesNotReconstruct` keeps that measurement as a
   permanent control. If a future parser change makes that DDL reduce, the test
   goes red and the residual has to be re-argued rather than silently acquired.

**The cost, stated plainly:** a MySQL or SQL Server schema whose DDL happens to
be dialect-neutral enough to reconstruct under the PostgreSQL binding *can* be
written live, and codefit will have measured a schema whose dialect it never
checked. The commented `type:` line and the report sentence are the mitigation;
they are not a fix.

A second declared cost, smaller: a project keeping its real schema under
`testdata/` (or `fixtures/`) is now missed, and the config says codefit found
none. That is honest and correctable by hand. The alternative — auditing a
fixture while reporting a measured schema — is the confident wrong answer this
ADR exists to remove. Absence is the safe direction.

Rejected: parsing under all three dialects and refusing to promote on
disagreement. It triples the parse, and a disagreement does not identify the
right answer — it would deny a block to legitimate PostgreSQL projects whose DDL
reduces differently elsewhere. That is a guess wearing caution's clothes.

## Layering

The input→parser mapping site stays SINGLE (one production package imports
`internal/providers/sqlddl`, held as a COUNT rather than a location by
`internal/schemasource/layering_test.go`). The site MOVED to
`internal/schemasource` in the change preceding this one; it did not multiply.

```
scaffold → schemasource → {sensors/db, providers/sqlddl, providers/typescript}
mcp      → schemasource
cli      → {scaffold, mcp}
```

`internal/core` is untouched, so "el núcleo NUNCA importa un provider concreto"
holds by construction — which is why `internal/schemasource` is deliberately NOT
under `internal/core`.

`internal/sensors/db` exposes exactly two functions for this
(`ResolveSchemaPath`, `OrderingIsProven`) and keeps the machinery itself. The
shape gate lives beside the `^V(\d+)__` regex that owns it, so when
`flywayOrderedSQL` later learns golang-migrate ordering, init's strictness widens
with it and no scaffold line changes.

## Consequences

- **golang-migrate remains unreachable by strictness, and that is separable.**
  `1_init.up.sql` never matches `^V(\d+)__`, so golang-migrate cannot reach live
  emission and codefit cannot produce a silently-wrong schema for it. Extending
  `flywayOrderedSQL` to order those names stays a non-blocking follow-up.
- **The skill's claim became conditional.** `skill.go` told every agent that
  "generated configs for a project like this carry NO such key". That is false
  the moment a proof succeeds on an undetected root. The claim is now gated on
  what the run actually wrote, and the cross-artifact lock
  (`TestGenerate_SkillClaimHoldsForBaitedMigrationDir`, which this change turned
  red by design) was RETARGETED to a two-directional equivalence rather than
  deleted, keeping its exact name and its baited fixture.
  The frontmatter `description` stays UNGATED: progressive disclosure loads the
  skill from the description alone, so narrowing it would mean a schema task
  never loads the skill at all — the agent would see no skill, not a smaller one.
- **`init --force` re-proves from disk**, and a path that no longer proves is
  DROPPED with the report naming it and why. codefit informs the consequence; the
  developer decides whether to put it back by hand.
- **Init-time proof does not bind scan time.** A path proven at init that later
  resolves to zero readable schema files still reaches ADR 0072's floor:
  `measured: false`, no score, the path named. A config codefit wrote itself buys
  no exemption from the floor — if it did, the one path most likely to be trusted
  would be the one path with nothing underneath it.
- **The declaration changed KIND.** Both the config and the report used to state
  a capability gap ("SQL migration directories are NOT detected"). They now state
  codefit's SEARCH RESULT over this project: what it found and why each was left
  out, or that it looked and found none, with the depth it walked. A config that
  says nothing about having searched is indistinguishable from a codefit that
  never searched.

## Evidence

- `internal/sensors/db/sources.go` and
  `internal/sensors/db/unread_zero_resolution_test.go` are byte-identical across
  this change (`git diff --quiet` exit 0 for both, with a positive control on a
  file that did change returning exit 1). R10 is neither lifted nor tripped:
  `TestSensorDB_NestedOnlySQL_IsNotMeasuredAndNamesThePath` and
  `TestSensorDB_ZeroResolutionDirectory_IsNotMeasured` stay green and unmodified,
  and both go red under the ADR 0072 mutation (floor denominator recomputed over
  resolved FILES).
- Every rule above has a control proven by MUTATION — the break applied, the red
  observed, the break restored, the green re-observed — with the build exiting 0
  throughout, so no red was a compile error.
