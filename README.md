# codefit

[![ci](https://github.com/codefit-cli/codefit/actions/workflows/ci.yml/badge.svg)](https://github.com/codefit-cli/codefit/actions/workflows/ci.yml)
[![license](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

> **The MCP-first auditor for AI-generated code — codefit maps, the agent reasons.**

codefit is an open-source tool, written in Go, that audits software written
(partly or fully) by AI. It detects what a developer **never sees** during normal
development: security vulnerabilities, algorithmic complexity that scales badly,
structural database problems, regression risk, and quality issues that only
surface under deep review. Its guiding principle: *codefit audits what the
developer is never going to see* — if a dimension is visible during normal
development, it is out of scope.

## ⚠️ Project status: in active development (Phase 1)

codefit runs over the **MCP stdio transport** today: `codefit mcp serve` exposes
the audit tools and an agent can call them. What is **in `main` now**:

- **TypeScript provider** (gotreesitter, pure Go, no CGO).
- **Deterministic security rules** for TypeScript — five categories (see below).
- **Surface mapping** — three categories (IDOR, broken authorization,
  over-fetching) for Next.js / Prisma, **validated against a real backend**.
- **`scan-all` synthesis** — the per-endpoint aggregation with three certainty
  levels, returned as an **actionable summary**: deterministic findings and the
  endpoints codefit resolved locally go whole; frontier-only endpoints (the data
  left the handler body) are named in `frontier_pending` and fetched on demand with
  **`scan-endpoint`**. Keeps the response small enough not to truncate (ADR 0008).
- **MCP stdio server** — `codefit mcp serve` exposes the tools over the protocol
  (built on the official MCP Go SDK; verified by a client↔server integration
  test). The Go provider and the self-audit (codefit scans its own code in CI)
  round it out.

**Still in active development (Phase 1):** the HTTP/SSE transport (`--port`) is
**not** wired yet — use stdio. The plumbing commands `init` / `update` are
scaffolding (the tools work without them — config is optional). Phases 2–4 (DB,
complexity, code review, knowledge packs) are on the roadmap.

## MCP-first, pure

codefit runs **exclusively as an MCP server** that AI agents (Claude Code,
OpenCode, Cursor, …) consume as a set of tools. **There is no audit CLI, and
codefit never calls an LLM or manages any credentials.**

It runs the deterministic layers (patterns + AST), maps the structural
**surface** of the vulnerability classes that require reasoning, and returns
`findings + surface` to the agent, which reasons over the surface with **its own
LLM**. The intelligence is the agent's. That is what democratizes auditing:
anyone already coding with AI can audit without extra API keys or infrastructure.

```
agent generates code
  └─► codefit (MCP tool): deterministic findings + mapped surface (JSON)
        └─► agent reasons the surface with its own LLM → fixes or proceeds
```

Connect it to your agent (stdio):

```json
{
  "mcpServers": {
    "codefit": { "command": "codefit", "args": ["mcp", "serve"], "cwd": "/path/to/project" }
  }
}
```

## The differentiator: surface mapping

Deterministic rules are what any linter does. The honest **surface mapping** that
the agent reasons over is what makes codefit different.

Classes like IDOR, broken authorization, and over-fetching cannot be caught by a
fixed pattern — they need semantic understanding. So codefit does not mark
candidates surgically (inheriting the AST's blind spot). It **enumerates the
complete structural surface** of each class — every endpoint that reaches a
resource by an id, every sensitive handler, every serialization of a domain
object — and hands all of it to the agent, with **structural signals that are
facts** ("reads `params.id`", "no known authz helper detected in the body") and a
**reason-to-review that is a question** ("does this verify ownership before
access?"). codefit never judges; the agent reasons each item.

What codefit can confirm from structure it enumerates (FINITE); what requires
following the data out of the handler it hands to the agent (the frontier); what
it does not cover it **declares** in the coverage manifest
([COVERAGE.md](COVERAGE.md)). The principle is recorded in
[ADR 0005](docs/decisions/0005-surface-frontier-finite-vs-infinite.md); the
per-endpoint synthesis with its three certainty levels in
[ADR 0006](docs/decisions/0006-scan-all-endpoint-synthesis.md).

## Deterministic security rules (TypeScript)

Five categories, each a fact codefit asserts with certainty 1.0: hardcoded
secrets, weak cryptography (MD5/SHA-1, insecure `Math.random` for tokens),
dangerous `eval`/`new Function`, inline SQL injection, and inline XSS via
`dangerouslySetInnerHTML`. Rules are declarative YAML in a Semgrep-format subset,
matched by codefit's own pure-Go engine (no OCaml/OpenGrep embedded) — see
[`rules/`](rules/). Their exact scope and known limits are in
[COVERAGE.md](COVERAGE.md).

## What codefit does NOT do (and why)

codefit **complements** linters and type-checkers; it does not replace them. An
unused `any`, a style nit, an obvious type error — those are visible during normal
development, so a linter already catches them and they are **out of scope**.
codefit spends its effort on the invisible: a missing ownership check on an
endpoint, a model serialized with every column to the client, a hash that is weak
for security. It is the independent audit layer that validates AI-generated code
is secure and correct **before** it merges — not another linter.

## Supported languages

| Language / Ecosystem | Status |
| --- | --- |
| **Go** | Provider + static security/best-practice detectors. codefit audits itself in CI. |
| **TypeScript / Next.js / Prisma** | Deterministic security rules (5 categories) + surface mapping (IDOR, authz, over-fetching), validated against a real backend. |
| Java / Spring | Roadmap |
| Python / FastAPI / Django | Roadmap |

Adding a language means implementing one `LanguageProvider` — it never touches
the core, the sensors, the MCP server, or the reporting (see
[CONTRIBUTING.md](CONTRIBUTING.md) and `docs/decisions/`).

## Building from source

```bash
go install github.com/codefit-cli/codefit/cmd/codefit@latest   # Go 1.25+
```

codefit is a single static binary with no runtime dependencies (`CGO_ENABLED=0`),
cross-compiling to linux/amd64, linux/arm64, windows/amd64, and darwin/arm64.
There is no LLM or auth configuration — codefit manages no models and no
credentials. Run `codefit mcp serve` and point your agent at it (see above).

## Contributing

Contributions are welcome — new rules, surface categories, language providers,
and false-positive reports especially. See [CONTRIBUTING.md](CONTRIBUTING.md) and
[`rules/README.md`](rules/README.md). Please follow our
[Code of Conduct](CODE_OF_CONDUCT.md), and report security issues per our
[Security Policy](SECURITY.md).

## License

[Apache 2.0](LICENSE).
