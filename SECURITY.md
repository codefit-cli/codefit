# Security Policy

## Reporting a vulnerability

If you discover a security vulnerability **in codefit itself**, please report it
privately. Do **not** open a public issue for security problems.

Use GitHub's **private vulnerability reporting**:

1. Go to the repository's **Security** tab.
2. Click **Report a vulnerability**.
3. Fill in the advisory form with a description, affected versions, and
   reproduction steps.

This opens a private channel with the maintainers. We will acknowledge your
report, work with you on a fix, and credit you (unless you prefer to remain
anonymous) when the advisory is published.

If you cannot use private reporting, email the maintainers (see the repository
profile) with the subject line `codefit security`.

## Scope

This policy covers vulnerabilities in codefit's own code and release artifacts —
for example, a flaw that could let a malicious project being scanned execute
code on the auditor's machine, or a leak of credentials handled by codefit.

Findings that codefit *reports about other projects* are not vulnerabilities in
codefit; please use the regular [false-positive issue
template](.github/ISSUE_TEMPLATE/false_positive.md) for those.

## Supported versions

codefit is pre-1.0. Security fixes target the latest released version. Until
`v1.0.0`, only the most recent `v0.x` release receives fixes.

| Version | Supported |
| ------- | --------- |
| latest `v0.x` | ✅ |
| older         | ❌ |

## Dependency policy

codefit is a security tool. **Every dependency compiled into its binary becomes
part of the trust surface of every user who runs it** — a malicious or
compromised dependency in an auditor is worse than in an ordinary app, because
users run it precisely to be told their code is safe. So the bar for adding a
dependency to the core is deliberately high.

### Principles

- **Minimize dependencies.** The best dependency is the one you don't add. Prefer
  writing a small amount of code over pulling a package for it.
- **Prefer the standard library.** It is already audited, already in the trust
  surface, and maintained by the Go team.
- **Prefer pure Go, no CGO.** Pure Go is auditable by anyone who reads Go, and it
  preserves codefit's non-negotiable `CGO_ENABLED=0` single-binary,
  clean-cross-compile guarantee. A dependency that requires CGO is rejected.
- **Prefer a small transitive footprint.** A package that drags in ten others
  brings ten more things to trust. Fewer transitive dependencies wins.

### Adoption checklist

Every new **core** dependency (anything compiled into the `codefit` binary) must
pass this checklist **before merge**, with evidence — not from memory:

1. **Provenance.** Who maintains it? Is the account established with a real
   identity and history, or recent/anonymous? Is there more than one contributor,
   or a single point of failure? Does the maintainer have other serious work?
2. **Known vulnerabilities.** `govulncheck` over a build that includes the
   dependency comes back clean (0 vulnerabilities the code calls). An OSV.dev
   query for the package and its transitives shows nothing relevant. Paste the
   output.
3. **Transitive tree.** `go list -deps` and `go mod graph` show what actually
   enters the binary. List the new packages; the fewer, the better. Test-only
   dependencies that never compile into the binary are noted as such.
4. **Integrity / pinning.** The exact version is pinned with a checksum in
   `go.sum`, `go mod verify` passes, and `GOSUMDB` (sum.golang.org) is active with
   no bypass — so the published artifact cannot be swapped after the audit.
5. **Sensitive-code review.** Read the dependency's source for behavior it should
   not have. For a parser, for example, a legitimate one reads bytes, builds a
   tree, and returns nodes — it must not touch the **network** (`net`, `net/http`,
   dialing, URLs), **execute commands** (`os/exec`, `syscall` beyond the
   reasonable), or reach the **filesystem** beyond what its job needs. `unsafe` is
   acceptable where a runtime genuinely needs it (e.g. zero-copy), but must be
   bounded and sane. `init()` functions must not do more than registration. **Any
   network or command execution that has no business being there is a stop-and-
   discuss, not a merge.**

### Who audits, and when

- A maintainer audits every new core dependency **before** it is merged.
- Community PRs that add a dependency go through this same checklist; a PR that
  adds an unaudited core dependency is not merged until the audit is done and
  recorded.
- Build tooling and test-only dependencies (not compiled into the released
  binary) are held to a lighter bar, but the same principles apply.

### How it is documented

Each core dependency's audit — the evidence for every checklist item and the
verdict — is recorded as an **ADR** under `docs/decisions/`. See
[ADR 0002](docs/decisions/0002-typescript-parser-gotreesitter.md) (the
TypeScript parser) as the worked example.
