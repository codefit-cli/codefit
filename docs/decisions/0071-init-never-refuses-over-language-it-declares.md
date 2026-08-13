# 0071 — `codefit init` never refuses over language; it declares what it did not find

Date: 2026-08-13
Status: accepted
Supersedes: nothing. It removes a behaviour no earlier ADR ever decided, and adds
the sentinel and the declaration that replace it.

## Context

`codefit init` refused to write anything when no marker file under the project
root resolved a language provider:

```
no supported language detected in %q: expected one of go.mod, package.json,
pyproject.toml/requirements.txt, or pom.xml/build.gradle
```

Two defects, one on top of the other.

**The message was written against the wrong table.** It lists the four values of
`config.allowedLanguages`, not the two entries of
`internal/providers/registry`'s table. Only `go.mod` and `package.json` can ever
make detection succeed — `tsconfig.json` can too and is *not named*, while
`pyproject.toml`, `requirements.txt`, `pom.xml` and `build.gradle` are named and
*cannot help*: creating one changes nothing. Reproduced with the real binary on
fixtures holding only a Java manifest, only a Python manifest, and only a
`tsconfig.json`: the first two exit 1 and are told to create the very file they
already hold; the third exits 0 on a marker the message never mentions.

**The refusal itself was the deeper defect.** `cfg.Project.Language` is
validated and then read by no production sensor or handler — a config saying
`language: java` is fully functional, and `internal/mcp/scanall_dbonly_test.go`
commits the proof that a Python project with `database.schema_paths` is audited
by `codefit-scan-all`. So init withheld a config over a field the audit never
reads, on a project the DB dimension could have audited. That is invariant I2 of
`docs/specs/audit-protocol.md` — the same "refused a dimension over a language
that dimension never needed" defect already fixed in `scan-all` (roadmap P0-5) —
still live in `init`, and it contradicts `CLAUDE.md`'s autonomy principle:
codefit informs, the developer decides, and never a "no" without foundation.

Three releases of drift went unnoticed because the only lock,
`TestDetectUnknownProjectErrors`, asserted `err != nil` and nothing about the
message. Nothing in the repository asserted its content.

## Decision

**D1 — one sentinel, spelled once.** `config.LanguageUndetected = "undetected"`
lives in `internal/config`, beside `allowedLanguages`, and joins that enum. `""`
stays rejected.

Rejected: widening the enum per language (open-ended, an edit per new language);
making the field optional (an absent language is ambiguous between an old
config, a hand-deleted key and a real non-detection — exactly the ambiguity a
sentinel removes); a const in `internal/scaffold` (`scaffold` imports `config`,
so it would invert the dependency); a const in `internal/providers/registry`
(the sentinel is the ABSENCE of a registered entry; housing it there invites a
matching fake table row).

Cost, stated rather than hidden: `undetected` becomes user-facing vocabulary
that every future reader of `Project.Language` must handle as "resolve no
provider", not as a language name. Zero production readers exist today, so the
debt is recorded, not paid. `internal/mcp`'s Lock A now asserts the sentinel
resolves no provider, so the assumption is checked rather than believed.

**D2 — `ProjectInfo.Detected()` is a method, not a field.** A `Detected bool`
field is a second source of truth that can disagree with `Language`. Every
renderer branches on this one predicate rather than comparing a string literal
it could misspell. It treats `""` as undetected too: `Detect` never produces it,
but `ProjectInfo`, `RenderSkill` and `RenderConfig` are exported, and baking
`language: ""` into every copy-paste example is the same fabrication as D5's
one shade quieter.

**D3 — marker names are derived, never typed.** `registry.InitDetectMarkerFiles()`
returns the marker files of the `InitDetect`-eligible entries in table order,
and every user-facing artifact reads it. A hardcoded marker list in `scaffold`
is forbidden by test, in both directions: each name must actually make
`ByMarkerFile` resolve, and the four manifests that cannot help must be absent.
This is the fix for the defect's root cause, not just its instance.

**D4 — one db-only vocabulary, made mechanical.** The shared fragment
`dbOnlyClause()` is interpolated by BOTH `CapabilityStatementForExposure` and
the new `UndetectedStatement()`, and a test asserts those exact bytes appear in
both outputs. Two hand-written sentences saying the same thing drift; asserting
that each merely "mentions the database" would pass on the divergence.

`CapabilityStatement` answers the sentinel BEFORE `registry.ByName`, so the
`"undetected is not a registered language"` fall-through — codefit talking to
itself about its own vocabulary — is unreachable.

**D5 — `RenderSkill`'s `"" → typescript` fallback is deleted, not gated.** The
skill is the FIRST artifact an agent reads and its examples are instructions, so
a fabricated language tells the agent to scan a Java repository as TypeScript.
Leaving the fallback behind the sentinel would let a future "sensible default"
refactor re-arm it. The frontmatter description is deliberately NOT gated: it
gates progressive disclosure and already names the database and schema triggers,
so narrowing it would mean a schema task loads no skill at all rather than a
smaller one.

**D6 — `path_criticality` is omitted WHOLE, replaced by a comment block.**
Emitting the key with nothing under it renders YAML `null`, which reads as
*configured and empty* rather than *deliberately unset*. The comment states the
RF-10 consequence (nothing is classified, so the test-path re-weighting never
fires and every finding keeps its natural severity) and shows how to set globs.
Inform, do not decide.

Empty is also the safe direction. Borrowing a registered provider's
`DefaultPathCriticality()` would silently reclassify — and downgrade — findings
in a tree codefit never inspected.

## Rejected, and not re-openable

**Adding Java and Python to the registry.** It moves the line without removing
it: Ruby, PHP, C#, Rust and every future language stay refused, and each new one
needs a stub provider whose `Capability()` declares nothing. The defect is that
init refuses at all, not which four languages it refuses.

**A `--language` flag.** The field has no production reader, so a flag would
imply a capability it cannot deliver. The committed `.codefit.yaml` stays the
editing surface.

**An empty-directory special case.** Any definition of "empty" is arbitrary
(does a README count? a `.gitignore`?), so a directory with nothing in it takes
exactly the same path as one full of files codefit does not recognize.

## Consequences

`codefit init` now exits 0 on any root. It writes `.codefit.yaml` and the skill,
and declares three things in three places — the generated config, the init
report and the README:

1. no auditable language provider resolved, and which markers were looked for;
2. code-level security and surface scanning therefore do not run here;
3. the DB dimension still audits the schema when `database.schema_paths` is
   configured.

**The second gap is declared, not fixed here.** `internal/scaffold/config.go`
gates the whole `database:` block — `schema_paths` included — behind a detected
ORM, and `SchemaPaths` is only ever populated from a Prisma schema. There is no
detection of a SQL migration directory. So a Flyway project still receives a
config that audits nothing, and the accepted consequence is named rather than
softened: such a project's `codefit-scan-all` reports `nothing to audit: no
security provider for language "undetected" ... and the database dimension did
not run`. That is honest, and it is exactly why detecting SQL migration
directories is the follow-up.

The declaration fires whenever no ORM was detected, NOT only when the language
was undetected — a TypeScript project without Prisma has the identical gap, and
declaring it only in the case that happens to be new would be the over-promise
this project exists to prevent.

`internal/mcp`'s Lock C previously derived its fixtures from a hardcoded marker
map. That map has no entry for the ABSENCE of a marker, so it could never have
seen the sentinel: it would have stayed green while covering nothing. It is now
derived from `registry.All()` plus a no-marker root, and the sentinel is a
permanent, declared entry in `initDetectsButScanAllCannotAudit` — permanent
because it is not a language and never will be one, so `scan-all` resolving
nothing for it is the correct outcome, not a gap awaiting a provider.
