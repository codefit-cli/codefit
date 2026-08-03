# Contributing to codefit

Thanks for your interest in improving codefit — the AI code auditor that audits
itself. This guide covers setup, conventions, and the concrete steps to add a
new language.

## Development setup

Requirements: **Go 1.25+** (the MCP SDK requires it; the build toolchain is
pinned to a patch ≥ 1.25.2 in `go.mod`). Docker is optional (only the complexity
sensor needs it). No C toolchain is required — codefit builds with
`CGO_ENABLED=0`.

```bash
git clone https://github.com/codefit-cli/codefit
cd codefit
make build        # builds ./bin/codefit (./bin/codefit.exe on Windows — the suffix comes from `go env GOEXE`)
make test         # go test ./...
make lint         # golangci-lint run
```

## Non-negotiable constraints

These are enforced by CI; a PR that breaks them will not pass:

- **`CGO_ENABLED=0` always.** No dependency may require CGO. Verify with
  `CGO_ENABLED=0 go build ./...`.
- **Cross-compiles cleanly** for linux/amd64, linux/arm64, windows/amd64.
- **Single binary, no runtime dependencies.**
- **Go is parsed with `go/ast`** (stdlib), not tree-sitter (see
  `docs/decisions/0001-go-parser-and-provider-interface.md`).

## Methodology

codefit is built test-first (TDD) and spec-first (SDD):

- **No production code without a failing test first** (red → green → refactor).
- Public functions get tests before they are implemented.
- Coverage target: **> 80%** in `internal/core` and `internal/sensors`.
- codefit audits itself: code without tests is flagged by its own test sensor.

## Conventions

- **Errors** are always wrapped with context: `fmt.Errorf("...: %w", err)`.
- **Logging** uses structured `slog`.
- **Names** are descriptive; avoid cryptic abbreviations.
- Each package has a `doc.go` describing its purpose.
- **Commits** follow [Conventional Commits](https://www.conventionalcommits.org)
  (`feat:`, `fix:`, `test:`, `docs:`, `refactor:`). No AI attribution lines.
- **Versioning** follows SemVer with pre-releases, mapped to the PRD phases — see
  [VERSIONING.md](VERSIONING.md) for the Phase→MINOR map and what each pre-release
  (`-alpha`/`-beta`/`-rc`) means. The build derives the version from the git tag.

### Don't let `.gitignore` swallow source

A broad `.gitignore` pattern like `coverage.*` matches `coverage.go` too, so a
source file can silently never reach git (this happened once). CI rejects any
`name.*` catch-all pattern. Optionally, check locally that no tracked-worthy
`.go` is being ignored before you commit:

```sh
git ls-files --others --ignored --exclude-standard | grep '\.go$' && \
  echo "A .go source file is being ignored — fix .gitignore" || echo "clean"
```

## Running the test suite

```bash
go test ./...                 # everything
go test -race ./...           # with the race detector (as CI does)
go test -cover ./internal/... # with coverage
```

## Architecture in one minute

Three layers (see PRD §13–14):

```
core/      universal engine: pipeline, cache, scoring, report, surface  (language-agnostic)
sensors/   audit logic per dimension: security, review, db, ...        (language-agnostic)
providers/ one per language: parsing + ecosystem rules + surface       (language-specific)
```

The core never depends on a language. A sensor asks the active provider for
language-specific findings; it never knows which parser the provider uses.

## Adding a new LanguageProvider

This is the extensibility path (PRD §14): adding a language **must not touch the
core, the sensors, the MCP server, or the reporting**. You implement one
interface.

1. Create `internal/providers/<lang>/` and a type implementing
   `providers.LanguageProvider`:

   ```go
   type LanguageProvider interface {
       Language() string
       Frameworks() []string
       FileExtensions() []string
       DefaultPathCriticality() config.PathCriticality
       AnalyzeSecurity(src SourceFile) ([]findings.Finding, error)
       AnalyzePractices(src SourceFile) ([]findings.Finding, error)
       AnalyzeSurface(src SourceFile) ([]findings.SurfaceItem, error)
   }
   ```

2. **Identity**: return the language name, recognized frameworks, file
   extensions, and sensible `path_criticality` defaults for the ecosystem.

3. **Parsing**: the provider owns its parser. Go uses `go/ast`. For
   TypeScript/Java/Python, use tree-sitter **pure Go, no CGO** — behind the
   parser-agnostic `core/syntax.Node` boundary (ADR 0003).

4. **Detection**: implement `AnalyzeSecurity` / `AnalyzePractices` to return
   deterministic `findings.Finding` values with their natural (pre-path)
   severity; the sensor applies path-criticality. Implement `AnalyzeSurface` to
   **enumerate** the structural surface (IDOR/authz/over-fetching, …) as
   `findings.SurfaceItem` values — facts and a question, never a judgment. Detect
   by structural shape, never by name; declare the frontier (ADR 0005).

5. **Register** the provider for its language id in the MCP adapter (the single
   place that maps language → provider).

6. **Test it**: write table-driven tests with small source snippets asserting
   the finding IDs and surface signals each detector emits — and at least one
   clean-input test. Calibration decisions are validated against real code.

If adding your language requires changing the core, that is a design bug — open
an issue so we fix the seam, not your provider.

## Adding a new dimension

An audit dimension (security, db, review, complexity, tests) is built and closed
the same way every time. The doctrine is **ADR 0016**; this is the short version.

1. A dimension is a **sensor + its rule(s)/parser(s)/surface + a permanent
   standalone MCP tool** (`codefit-scan-<dim>`). The standalone tool is not
   scaffolding — it lets a developer or agent audit that one dimension on demand,
   for good.

2. **Rule logic lives in the core, over the neutral model** (e.g.
   `core/db.Schema`), so every future provider inherits it — the provider ONLY
   parses (ADR 0015). If a rule needs an ORM/language-specific fact, that means the
   neutral model is incomplete: enrich the core once, never put specific logic in
   the rule (ADR 0014).

3. **Develop it standalone** — slice by slice, TDD, dogfood on a real project —
   until every rule/parser/surface it owns is complete. Do **not** touch `scan-all`
   during this phase. Lock each parser/rule limit as a test (a contract), not an
   assumption.

4. **Definition of Done = wire it into `scan-all`.** A dimension is not "ready"
   until `scan-all` runs it. Today `scan-all` runs **security and DB** — DB as its
   own section, including the DW star-schema/SCD family and the code×schema cross;
   review, complexity, and tests are **not** wired yet, and saying so is the honest
   state, not a bug. Design every slice toward this wiring from slice 1.

5. **Non-endpoint dimensions** (e.g. DB — a table without a primary key does not
   hang off an HTTP route) get their **own section/bucket** in the `scan-all`
   response; do not force them into the endpoint-centric bucketing (ADR 0006). The
   `by_dimension` per-dimension score is switched on as part of this close wiring.

## Pull requests

- Keep PRs focused. Large changes should be split into reviewable slices.
- Ensure `make test`, `make lint`, and `CGO_ENABLED=0 go build ./...` pass.
- The self-audit — a Go integration test (`TestSelfAudit` in
  `internal/sensors/security`) that runs the real sensors over codefit's own source
  tree — must stay green. It runs as part of `make test` (there is no audit CLI in
  the MCP-first model).
- Describe what changed and why; link related issues.

## License

By contributing, you agree your contributions are licensed under the project's
[Apache 2.0](LICENSE) license.
