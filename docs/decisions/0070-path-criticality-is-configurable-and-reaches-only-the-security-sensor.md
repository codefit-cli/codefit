# 0070 — Path criticality is configurable, and it reaches only the security sensor

Date: 2026-08-12
Status: accepted
Supersedes: nothing. It implements RF-10, which no earlier ADR ever revised, and
declares the reach gap that the requirement's wording ("cada finding") does not
have.

## Context

PRD v1.4 RF-10 (`docs/PRD-codefit-v1.4.md:412-414`) says that codefit weights a
finding's severity by its path classification, and states the consequence
literally: *"Findings de seguridad en archivos de test se degradan a `info`
(configurable)."* Section 14 (`:788`) defines what "configurable" means:

```yaml
sensors:
  security:
    test_severity: "info"   # info | downgrade | keep
```

The code did neither half. It hardcoded `downgrade` — critical→high, high→medium,
and so on — and shipped no `test_severity` key at all. `test_severity` appeared
in **zero** Go files.

Two things make this worth an ADR rather than a one-line default flip:

1. **`downgrade` is not an invented behaviour.** It is one of the PRD's own three
   sanctioned modes. The defect is that it was (a) the wrong default and (b) the
   *only* reachable mode.
2. **No ADR ever decided it.** Searching `docs/decisions/` for the rule returns
   only ADR 0050 and ADR 0056 discussing *when* `applyCriticality` runs, never
   *what severity it should target*. The deviation was undecided drift, not a
   deliberate revision of the requirement.

The claim had also travelled: it entered in PRD v1.3 and was carried verbatim
into v1.4, and `CLAUDE.md` restates it faithfully. Correcting the documents to
match the code would have enshrined a bug in three places and contradicted the
PRD, which `CLAUDE.md` itself declares the source of truth for scope and design.

## Decision

**Implement RF-10 in full rather than flip the hardcoded constant.**

`sensors.security.test_severity` accepts `info | downgrade | keep`, defaulting to
`info` when unset. Only the full enum honours the word "configurable": a project
that prefers today's behaviour writes `downgrade` and keeps it, which a bare
default flip would have taken away from it.

Three consequences are decided here explicitly.

### 1. `keep` is accepted, and its consequence is stated at materialisation

`keep` applies no adjustment at all, so a critical security finding on a test
path stays critical — which means it keeps `RequiresConsent` and makes
`scoring.IsBlocked` true. It is the **only** mode that changes the blocking
answer; `info` and `downgrade` can never produce a blocking finding from a test
path.

codefit does not refuse the mode. Refusing a mode the PRD names would be codefit
overriding a decision the autonomy principle hands to the developer. It does not
stay silent either: the security sensor emits **one warning per run**, and only
when `keep` has actually left a security finding at critical on a test path.

Rejected alternatives:

- *Validation error* — codefit deciding for the developer.
- *Warning at load* — fires for every run of every project that set the mode,
  including the many where no test finding exists and nothing is blocked. The
  consequence is only real once a finding survives.

### 2. The key lives on a security-specific type

`Sensors.Security` was retyped from the shared `SensorToggle` to a new
`config.SecuritySensor`, which embeds `SensorToggle` with `yaml:",inline"`.

Putting `TestSeverity` on `SensorToggle` is a shorter edit and was rejected:
it would make `sensors.db.test_severity`, `sensors.tests.test_severity` and
three more **parse, validate and do nothing**. New dead config, in a tool whose
premise is catching what the developer never sees. `,inline` keeps `enabled` —
and its three-state `*bool` — defined once. Go cannot forbid the field, so
`internal/config/sensortoggle_lock_test.go` is the control: it fails naming
every sensor that would silently gain the key.

### 3. Declared reach: only the security sensor applies criticality

RF-10 says criticality weights *"cada finding"*. It does not.
`applyCriticality` has exactly one caller — `internal/sensors/security`. The DB
sensor never applies it, and this change does not add it.

That is deliberate. DB findings are schema-scoped: they are located by table and
column inside a schema source, so a project-relative `production` / `test` /
`example` glob does not classify them in any way that means what RF-10 means. A
glob would match the schema FILE, not the object the finding is about.

Closing the gap requires deciding what a "test" table even is, which is a
different question from the one this change answers. It stays open, and it stays
**declared**: named in `applyCriticality`'s doc comment and in
`internal/sensors/db/doc.go`, the file a DB-sensor author actually reads.

## Consequences

- A project with no `.codefit.yaml`, or with no `test_severity` key, now reports
  test-path security findings at `info` instead of one level down. The security
  dimension score rises wherever such findings exist (severity penalty 10→0 for
  a critical), which is the intended RF-10 weighting, not a scoring change.
- Blocking behaviour is unchanged under `info` and `downgrade`, and newly
  possible under `keep`.
- `internal/sensors/security/selfaudit_test.go`'s dogfooding gate is now
  mode-dependent. It carries a precondition that fails naming the configuration
  as the cause, so choosing `keep` in this repository cannot masquerade as a
  sensor regression.
- `Report.IncludeInfo` remains declared and unread, and that is now load-bearing:
  forcing a finding to `info` is only harmless while nothing filters `info` out
  of a report. `internal/config/includeinfo_tripwire_test.go` is a repo-wide
  `go/ast` census that fails if a production reader appears without a recorded
  decision. It carries its own positive probe — it must find the known
  declaration before its "no readers" verdict is believed, because a census that
  walked nothing would report clean forever.
- The generated config (`codefit init`) deliberately does **not** emit
  `test_severity`. Emitting it would freeze today's default into every generated
  project, so a future change of the default would never reach them.
- Nine RF citations in code and config were corrected in the same change: seven
  said RF-11 (path criticality's number in PRD v1.2 only) where RF-10 is meant,
  and two said RF-10 where the subject is the adoption baseline, RF-08.
