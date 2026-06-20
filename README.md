# codefit

[![ci](https://github.com/codefit-cli/codefit/actions/workflows/ci.yml/badge.svg)](https://github.com/codefit-cli/codefit/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/codefit-cli/codefit?include_prereleases&sort=semver)](https://github.com/codefit-cli/codefit/releases)
[![license](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

> **The AI code auditor that audits itself.**

codefit is an open-source tool, written in Go, that audits software written
(partly or fully) by AI. It detects what a developer **never sees** during
normal development: security vulnerabilities, algorithmic complexity that scales
badly, structural database problems, regression risk, and quality issues that
only surface under deep review. Its guiding principle: *codefit audits what the
developer is never going to see* — if a dimension is visible during normal
development, it is out of scope.

It does not replace TDD, SDD, linters, or infra scanners. It is the independent
audit layer that validates AI-generated code is secure, correct, and scalable
**before** it merges to production. codefit runs in two modes over the same
sensor core: a **CLI** (reactive, for the terminal and CI/CD) and a stateless
**MCP server** (proactive, where AI agents call the sensors as tools while they
generate code).

## Installation

**From source (Go 1.24+):**

```bash
go install github.com/codefit-cli/codefit/cmd/codefit@latest
```

**Pre-built binaries** are attached to each [GitHub
release](https://github.com/codefit-cli/codefit/releases) for linux/amd64,
linux/arm64, windows/amd64, and darwin/arm64 (`.tar.gz` for Unix, `.zip` for
Windows, with `checksums.txt`). Download, extract, and put `codefit` on your
`PATH`.

codefit is a single static binary with no runtime dependencies (built with
`CGO_ENABLED=0`). Docker is optional and only needed for the complexity sensor.

## Quick start

```bash
codefit auth login                 # configure your LLM provider (keychain-backed)
codefit init                       # detect the project, write .codefit.yaml
codefit scan --no-llm              # fast, free, static-only audit
codefit scan --since origin/main   # audit only what changed in your PR
codefit status                     # provider, model, Docker availability
```

The static layers (`--no-llm`) need no API key and run in seconds — ideal for
pre-commit hooks and CI.

## Modes: CLI and MCP

**CLI** — you or a pipeline run it explicitly:

```bash
codefit scan --since origin/main --fail-on critical
```

**MCP** — an AI agent calls the sensors as tools while generating code, enabling
an auto-correction loop before the code ever reaches you:

```bash
codefit mcp serve                  # stdio transport by default
```

```json
{
  "mcpServers": {
    "codefit": { "command": "codefit", "args": ["mcp", "serve"] }
  }
}
```

Both modes share the exact same sensor core — any sensor available in one is
available in the other.

## Supported languages

| Language / Ecosystem | Phase | Sensors |
| --- | --- | --- |
| **Go** | v1.0 (Phase 0) | Security, Code Review, Best Practices, Tests |
| TypeScript / React / Next.js | v1.0 | Security, Code Review, DB, Complexity, Best Practices, Tests |
| Java / Spring | v1.1 | Security, Code Review, DB, Complexity, Best Practices, Tests |
| Python / FastAPI / Django | v1.2 | Security, Code Review, DB, Complexity, Best Practices, Tests |

Go ships first because codefit is written in Go and audits itself from day one.
Adding a language means implementing one `LanguageProvider` interface — it never
touches the core (see [CONTRIBUTING.md](CONTRIBUTING.md)).

## Minimal configuration

`.codefit.yaml`, committed to your repo:

```yaml
version: "1"
project:
  name: "my-app"
  language: "go"          # go | typescript | java | python
  path_criticality:       # weights finding severity by location
    production:
      - "internal/**"
      - "cmd/**"
    test:
      - "**/*_test.go"
```

Credentials are **never** written here — they live in your OS keychain or env
vars (`ANTHROPIC_API_KEY`, etc.).

## Contributing

Contributions are welcome — new sensors, language providers, and false-positive
reports especially. See [CONTRIBUTING.md](CONTRIBUTING.md) for setup, the TDD
workflow, and the concrete steps to add a language. Please follow our
[Code of Conduct](CODE_OF_CONDUCT.md), and report security issues per our
[Security Policy](SECURITY.md).

## License

[Apache 2.0](LICENSE).
