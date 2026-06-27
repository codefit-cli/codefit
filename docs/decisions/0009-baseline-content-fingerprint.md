# ADR 0009 — Baseline identity by content fingerprint, not line

**Status:** Accepted · **Date:** 2026-06-27 · **Phase:** 1 (baseline, RF-08)

## Context

The baseline (RF-08) records codefit's view of the audited surface so a re-scan only
surfaces what changed. To do that it needs a **stable identity** per item across
scans. The obvious choice — `(file, line, category)`, which the surface already uses
for `SurfaceItem.ID` (`core/surface.StableID`) — is brittle: adding an import above a
handler shifts every line below it, so a line-based id would report the whole file as
new on a harmless edit. The baseline would churn and the silence it exists to provide
would never hold.

The baseline file is also **committed** (shared knowledge, like `.codefit.yaml`), so
the identity must never embed a secret: a hardcoded-secret finding cannot be tracked
by storing its matched line in a committed file.

## Decision

Identify each baseline item by a **content fingerprint**:

```
fingerprint = sha256(category + "\x00" + file + "\x00" + normalize(content))[:12]
normalize  = collapse runs of whitespace to one space, trim
```

- **No line.** Moving code does not change the fingerprint; an item is re-detected
  only when its own content changes (then the old fingerprint goes `gone` and the new
  one appears — see ADR 0010).
- **Content is hashed, never stored.** A finding's matched line feeds the hash but is
  never written to the baseline; the human-readable field is the finding `Title`
  ("Possible hardcoded API key"), never the secret. The fingerprint is one-way.
- **Surface vs finding content.** Surface items hash their snippet. Deterministic
  findings have no snippet, so they hash the **source line**, with the **rule ID folded
  into the category** (`security/SEC-010`) — so two distinct rules firing on the *same*
  line never collide. Collision would let accepting one finding silence another
  affirmation without consent, which ADR 0011 forbids.

Fingerprinting happens once, at the security-sensor boundary (`stampFingerprints`),
so no provider needs to know about the baseline.

## Consequences

- Re-indentation and code moves do not churn the baseline (robust silence).
- Two byte-identical items of the same category in the same file share a fingerprint
  and collapse — acceptable, the same way surface dedupes identical items.
- An accepted item whose content later changes loses its acknowledgment and re-shows
  (safe re-review) — a property, not a bug.
- No secret ever reaches the committed `.codefit-baseline`.
