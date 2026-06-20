# Changelog

All notable changes to codefit are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project follows
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] — pending release

First public release: Phase 0 (Foundations) + the Go provider and the security
sensor.

### Added

- Three-layer architecture: `core/` (universal engine), `sensors/` (audit
  logic), `providers/` (per-language), so adding a language never touches the
  core.
- Project config parser (`.codefit.yaml`) with located validation errors and
  `path_criticality` support (RF-11).
- Global user config and LLM auth: OS keychain (go-keyring) with an AES-256-GCM
  encrypted-file fallback, env-var resolution, and an interactive `auth login`
  wizard.
- Core engine: filtering pyramid, content-hash cache, scoring, the canonical
  JSON report (`schema_version` 1.0) with JSON/plain renderers and TTY
  detection, and an abstract LLM client with an Anthropic implementation
  (prompt caching).
- **Go `LanguageProvider`** backed by `go/ast` (no CGO): static security and
  best-practice detectors.
- **Security sensor** (regex + AST layers) with severity adjustment by path
  criticality.
- CLI: `init`, `scan`, `bench`, `review`, `run`, `baseline`, `report`,
  `mcp serve`, `auth`, `set`, `status` (skeletons where not yet implemented;
  `scan`, `report`, `auth`, `set`, `status` functional).
- Docker sandbox manager for the (upcoming) complexity sensor.
- Self-audit: codefit scans its own code in CI (`scan --no-llm --fail-on
  critical`).
- CI/CD: GitHub Actions for test/lint/build, goreleaser releases, and weekly
  dependency vulnerability scanning.

### Notes

- `CGO_ENABLED=0` everywhere; cross-compiles to linux/amd64, linux/arm64,
  windows/amd64, darwin/arm64.

[Unreleased]: https://github.com/codefit-cli/codefit/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/codefit-cli/codefit/releases/tag/v0.1.0
