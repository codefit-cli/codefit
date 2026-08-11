# ADR 0067 — Every surface producer emits non-nil StructuralFacts; Go's authz item states only what go/ast body-scanning can establish

**Status:** Accepted · **Date:** 2026-08-10 · **Phase:** 3 (regression fix, unreleased)

## Context

ADR 0065 exposed Go for `codefit-scan-security` and the `codefit-surface-*` family. Hours
after that PR merged — before any release — `internal/providers/golang/surface.go`'s
`surfaceItems` was found to never set `SurfaceItem.StructuralFacts`. A Go `map[string]bool`
zero value is `nil`, and `encoding/json` marshals a nil map as `null`. The MCP SDK validates
tool output against the schema derived from `SurfaceItem` (`internal/mcp/server.go`'s
`addTool` → the SDK's `applySchema` on `StructuredContent`), which requires
`structural_facts` to be an object. The result: any Go project with an HTTP handler
hard-failed `codefit-scan-security` and `codefit-surface-authz` with

```
validating tool output: ... /properties/structural_facts: type: <invalid reflect.Value> has type "null", want "object"
```

No test caught it because every existing Go surface fixture was a string literal inside
`surface_test.go` — there was no committed `.go` fixture driving the real parser through
`internal/mcp`'s real server, and no test asserted the JSON *wire form* of a surface item,
only its Go struct shape (which a nil map satisfies at compile time).

## Decision

**Every `LanguageProvider.AnalyzeSurface` result carries a non-nil `StructuralFacts` map on
every emitted item — a cross-provider, registry-enforced contract, not a per-call-site
habit.**

### What was built

- **`internal/providers/registry/surfacecontract_test.go`**
  (`TestSurfaceProducers_EmitNonNilStructuralFacts`): iterates `registry.All()`, not a
  hardcoded provider list. For every entry whose `Capability().Surface` is non-empty, it
  requires a committed fixture at `internal/providers/registry/testdata/surface/<canonical><ext>`
  (missing fixture is a named `t.Fatalf`, not a silent skip — the fixture obligation
  self-extends to any future provider that declares surface), asserts the fixture produces
  at least one item (a fixture producing nothing proves nothing), and asserts both
  `StructuralFacts != nil` *and* the marshaled JSON contains `"structural_facts":{` — the
  wire form is the property that actually broke; a struct-field assertion alone would have
  passed while the tool errored.
- **`internal/providers/golang/surface.go`**: Go's authz item now carries real facts from a
  go/ast body scan instead of an unset map:
  - `authz_denial_response_detected` — **always present**. True when the handler body writes
    a 401/403 denial (`http.Error(w, msg, http.StatusUnauthorized|StatusForbidden)`, or
    `<writer>.WriteHeader(...)` on the handler's own writer parameter, read off the
    signature, never hardcoded to `"w"`). This is the fact that keeps the item non-hollow in
    the *default* case — `internal/mcp/surface.go`'s `providerFor` and
    `internal/mcp/scanall.go`'s DB-only paths both call `e.New(nil)`/pass no helpers today,
    so "no helper configured" is the common path, not the corner.
  - `known_authz_detected` — **present only when the project has registered at least one
    authz helper name** (`Provider.authzHelpers` non-empty). A `false` computed against an
    empty searched set would be exactly the vacuous claim this change's spec (Engram
    `sdd/go-surface-structural-facts/spec`) forbids. When present, it is true iff
    the body calls one of the registered names (matched by identifier or selector name —
    `*ast.Ident` or `SelectorExpr.Sel`, never resolved via `go/types`).
- **`internal/providers/golang/golang.go`**: `Provider` gained `authzHelpers map[string]bool`,
  a variadic `Option`/`WithAuthzHelpers` (mirroring
  `internal/providers/typescript/typescript.go`), so `golang.New()` keeps compiling at every
  existing call site.
- **`internal/providers/registry/registry.go`**: the `"go"` entry's `New` function stopped
  discarding its `authzHelpers []string` parameter
  (`func(_ []string) providers.LanguageProvider { return golang.New() }` →
  `func(authzHelpers []string) ... { return golang.New(golang.WithAuthzHelpers(authzHelpers)) }`).
  This is the wiring that makes `known_authz_detected` per-project rather than permanently
  absent: `internal/mcp/scanall.go`'s `providerForLanguage` and `internal/mcp/scan.go` both
  already threaded `recognizedHelpers(root, language)` through to `e.New(authzHelpers)` —
  only Go's entry was throwing the value away.
- **`internal/providers/golang/testdata/handlers.go`**: a committed real-handler fixture (not
  a string literal in a `_test.go` file — the exact gap that let this regression ship),
  driving `authz_denial_response_detected` and `known_authz_detected` through the real
  parser across all four true/false combinations.

### Declared limit, not a silent false

Go's authz item inspects **only the handler's own function body**. It does not see
route-registration or middleware/wrapper authorization (`mux.Use`, chi middleware,
`RequireAuth(handler)`), and it has no `go/types`, so a name match is a name match, not a
resolved symbol. Nothing in `StructuralFacts` represents that unchecked layer as a key
defaulting to `false` — the only two keys are the ones above, and every signal that reports
"no helper call detected" explicitly says it only inspected the body. This mirrors the
frontier TypeScript already draws (ADR 0005): what codefit cannot see is stated, never
implied absent.

### What was deliberately NOT built

- **No invented Go authz-helper vocabulary.** TypeScript's built-ins (`getServerSession`,
  `getToken`) anchor to one dominant named ecosystem (NextAuth); Go has no equivalent, so a
  built-in list would be exactly the name-driven over-promise `CLAUDE.md` flags as a
  known-limit smell. `known_authz_detected` is project-registered-only for Go.
- **`internal/mcp/surface.go:200`'s `providerFor` still calls `e.New(nil)`** — the
  `codefit-surface-*` family receives no project helpers for *any* language, TypeScript
  included. This is pre-existing, cross-provider, and unrelated to the regression this ADR
  fixes; it is filed in `docs/roadmap.md`, not fixed here.
- **No nil→`{}` normalizer at the MCP/sensor boundary.** A boundary normalizer would mask a
  future provider's omission in production instead of failing its own test — this codebase
  enforces contracts with locks, not with silent repair.

## Consequences

- `codefit-scan-security` and `codefit-surface-authz` no longer hard-fail on a Go project
  containing an HTTP handler — verified end to end through the real MCP server
  (`internal/mcp/surface_go_regression_test.go`), not only at the struct level.
- Go's authz surface items carry real, queryable facts for the first time; consumers that
  render `StructuralSignals`/`StructuralFacts` for Go now see actual detections instead of an
  empty map that happened to satisfy the Go compiler.
- `internal/mcp/baseline.go`'s and `internal/scaffold/skill.go`'s existing prose ("register a
  helper, re-run `codefit-scan-all`, `known_authz_detected` clears") was already
  language-generic and was **false for Go** before this change (the parameter was discarded);
  it is now **true for Go** too, with no doc edit needed — verified by
  `rg -n "authz" COVERAGE.md internal/scaffold/skill.go internal/mcp/server.go`, which found no
  Go-specific claim that this change contradicts.
- A future provider that declares a surface category and ships no fixture at
  `internal/providers/registry/testdata/surface/<canonical><ext>` fails
  `TestSurfaceProducers_EmitNonNilStructuralFacts` immediately, naming the exact path to add —
  the omission class this ADR closes cannot recur silently for the next language.
