# Spec — `codefit-scan-all`

**Status:** current contract, updated for `scan-all-db-without-language` (P0-5) ·
**Component:** `internal/mcp.HandleScanAll` / `buildScanAll`
(`internal/mcp/scanall.go`)

This is the mini-spec CLAUDE.md's SDD convention asks for: what the handler does,
what it receives, what it returns, and the edge cases it must handle. It documents
the **current** contract on `main`, not a target — see the doc comments on
`ScanAllResponse`, `SecuritySection` and `DBSection` in `internal/mcp/scanall.go` for
the field-level detail; this file is the shape, not a duplicate of every comment.

## What it does

Runs codefit's two wired audit dimensions — security (endpoint surface + deterministic
rules) and DB (schema structure) — over a project, unions their observations against
the committed baseline once, and returns an agent-first synthesis: three
resolution-level buckets for endpoints (`actionable` / `resolved_clean` /
`frontier_pending`), a parallel flat section for the DB dimension, the per-dimension
score, and the baseline delta. It is the **mandatory entry point** the generated skill
tells an agent to call first (`internal/scaffold/skill.go`).

## What it receives

```go
type ScanAllRequest struct {
    Root         string   // project root, absolute path
    Language     string   // "typescript" | "ts" | "tsx" | anything else
    ChangedFiles []string // optional; layer 0 narrowing (empty/absent = full audit)
}
```

`Language` is **not validated against a fixed list** by the request itself — it is
resolved twice, independently, against the single source
(`internal/mcp.languageProviders` / `SupportedLanguageNames()`):

- **Security dimension:** `providerForLanguage(Language, ...)`. Resolves for
  `typescript`/`ts`/`tsx` only (Lock A, `internal/mcp/language_source_test.go`).
  Anything else — including `"go"` — resolves `nil`, and security does not run for
  this pass (`secRan == false`).
- **DB dimension:** does **not** consult `Language` at all. Its schema parser is
  picked from `database.schema_paths`' file kind (`.prisma` vs `.sql`) and
  `database.type`, entirely independent of the language provider
  (`schemaParserForPaths`). This is the fact P0-5 acts on: the DB dimension's own
  inputs can be fully satisfiable for a language the security dimension cannot audit.

## What it returns

`(ScanAllResponse, error)`. The **error** return is reserved for:

1. A baseline or project-config file that exists but fails to load/parse (unchanged,
   pre-existing behaviour — a present-but-invalid `.codefit.yaml` is never scanned
   silently with defaults).
2. A `runSecurity` failure when security DID run (`secRan == true`) — a sensor/config
   error, not "no provider".
3. **Nothing measurable** (P0-5): neither a security provider resolved nor did the DB
   dimension run this pass. Named error, both missing inputs, fires **before** any
   baseline write (see Edge cases below).

Every other outcome is a `(ScanAllResponse, nil)`, including a Go project auditing
only its schema, and a TypeScript project with no database configured.

`ScanAllResponse`'s ALWAYS-present sections: `Summary`, `Scope`, `Score`, `Baseline`,
`Budget`, `Actionable`, `ResolvedClean`, `FrontierPending`, and — since P0-5 —
`Security`. The only CONDITIONALLY-present section is `DB` (`omitempty`, nil when no
`database.schema_paths` is configured for this project at all — a fact independent of
whether it ran *this pass*).

```go
type SecuritySection struct {
    Measured bool   `json:"measured"`
    Note     string `json:"note,omitempty"` // non-empty iff Measured is false
}
```

`Security.Measured` is `true` for every language that resolves a security provider
(unconditionally true for every TypeScript request today) and `false` otherwise, with
a note explaining why and what it means for the rest of the response (the schema may
still have been measured independently).

## Edge cases (the ones this spec exists to pin)

| Case | `secRan` | `dbRan` | Result |
|---|---|---|---|
| TypeScript, no schema configured | true | false | Unchanged pre-P0-5 shape: `security.measured=true`, `db=nil`, `by_dimension.db=null`. |
| TypeScript, schema configured and in scope | true | true | Unchanged pre-P0-5 shape: both dimensions measured. |
| Go (or any unresolved language), schema configured and in scope | **false** | true | **New in P0-5.** No error. `db.measured=true`, `security.measured=false` with a note, `by_dimension.security=null`, `by_dimension.db` scored, `scope.auditable_total` = distinct schema sources read (not 0), `db.note` names the skipped code×schema cross (Go has no `QueryExtractor`). |
| Go (or any unresolved language), no schema configured at all | **false** | false | **New in P0-5: an ERROR**, not a 200. Names the unresolved language, the single-sourced supported set, and that `database.schema_paths` is not configured. **No baseline file is written** — the guard runs before `diffBaseline`'s save. |
| Go, schema configured but a narrowed (`changed_files`) pass excludes it | false | false | Same as the row above: an error. **Accepted consequence** (ADR 0059) — a narrowed pass that cannot measure anything now refuses instead of returning an all-null 200. |
| Go, schema configured but fails to parse/read | false | false | Same error shape as "no schema configured", but the DB-specific clause is the **verbatim** `dbSection.Note` (e.g. "missing schema file") instead of the generic "not configured" sentence. |
| Any language, baseline has a security-owned item, this pass is DB-only (`secRan=false`) | false | true | **Invariant SCANNED-OPT-IN** (ADR 0059): the item is neither `Gone` nor a `GoneCandidate`, and `codefit-baseline-prune` leaves it in the file. Categories only ever get ADDED to `scanned` inside their owning dimension's `if <dim>Ran` block. |

## Out of scope for this contract (owned elsewhere)

- `codefit-scan-security` / `codefit-scan-endpoint`: single-dimension tools, unchanged
  by P0-5, still hard-error `unsupported language %q` on an unresolved language — no
  DB dimension to fall back to.
- Wiring `golang.New()` into `providerForLanguage` so `secRan` can ever be true for
  Go — an open scope decision (roadmap P4-1), deliberately fenced by Lock A.
- Unifying `providerForLanguage`, `surface.go`'s `providerFor`, and
  `scaffold/detect.go`'s `detectLanguage` into one implementation (roadmap P1-1b).
- The response byte budget (`ResponseBudgetBytes`, `BudgetBlock`) and the
  three-bucket rendering/withholding order — see
  `docs/specs/scan-all-response-budget.md`.
- The `changed_files` narrowing contract itself (what counts as "in scope", how
  `ScopeBlock` is built) — see `docs/specs/change-scope.md`.
