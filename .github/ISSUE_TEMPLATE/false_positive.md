---
name: False positive
about: codefit reported a finding that is wrong or noisy
title: "[false-positive] "
labels: false-positive
---

## Finding reported

- Finding ID (e.g. `SEC-001`, `PRAC-002`):
- Sensor / dimension:
- Severity reported:

## Why it is a false positive

Explain why this code is actually correct/safe, or why the finding is noise in
this context.

## Minimal code that triggers it

```go
// the smallest snippet that reproduces the finding
```

## Environment

- codefit version:
- Language and provider:
- Relevant `.codefit.yaml` (especially `path_criticality` and `ignore`):

## Expected behavior

Should this not be flagged at all, flagged at a lower severity, or only flagged
in certain paths?

---

> Signal-to-noise is critical for an auditor. Thank you for reporting — false
> positives directly shape which rules we tune.
