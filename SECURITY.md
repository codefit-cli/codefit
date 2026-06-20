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
