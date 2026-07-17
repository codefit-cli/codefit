# ADR 0026 — PostgreSQL trigger→function link as a dialect datum, not a branch

**Status:** Accepted · **Date:** 2026-07-17 · **Phase:** 2.2 (DB debt Slice A — Unit A2)

## Context

Capturing routine bodies (ADR 0025) exposed a dialect asymmetry. In MySQL and
SQL Server, a trigger carries its logic **inline** in its own `BEGIN … END` body.
In PostgreSQL it does not: `CREATE TRIGGER … EXECUTE FUNCTION fn()` only wires an
event to a **separate function**; the actual logic lives in that function's body.

Applying the routine-body completeness formula (ADR 0025) to a PG trigger produced
a **false incomplete**: the PG trigger statement has no body to be complete or
truncated, yet the generic formula marked it incomplete, mislabelling every PG
trigger. Worse, a routine-body rule pointed at a PG trigger would find no logic —
because the logic is in the function, one hop away — and could wrongly conclude the
trigger "does nothing." The link from trigger to its function had to be modeled.

The constraint: this must NOT become an `if dialect.Name == "postgres"` branch.
ADR 0022 established one shared reducer driven by per-dialect DATA; a new dialect
adds a `Dialect` value, never a branch in `split.go` or `reduce.go`.

## Decision

### The asymmetry is a dialect datum: `TriggerHasInlineBody`

`Dialect` gains `TriggerHasInlineBody bool` (`dialect.go:60`): Postgres `false`
(`dialect.go:123`), MySQL `true` (`dialect.go:149`), SQLServer `true`
(`dialect.go:175`). `triggerBody` (`reduce.go:179`) consults this datum instead of
branching on `dialect.Name`:

```go
func (b *builder) triggerBody(st stmt) db.Body {
	if !b.dialect.TriggerHasInlineBody {
		return db.Body{Text: st.text, Complete: true, Note: "this dialect's
			triggers carry no inline body — the statement only wires an event to a
			function/procedure; see Trigger.ExecutesFunction for the executed
			routine, whose own Body carries the logic"}
	}
	return routineBody(st)
}
```

A no-inline-body trigger is `Complete: true` (there is nothing truncated — the
statement is fully captured; it simply has no body), with a `Note` redirecting the
reader to the function. This kills the false incomplete without a dialect branch: a
future dialect with the same shape only sets the flag.

### The link itself lives in the neutral model, resolved once

`db.Trigger` carries `ExecutesFunction` (`db.go:117`), populated during reduction
by a second regex on the same statement: `reTriggerExecutes` (`reduce.go:57`)
matches `EXECUTE FUNCTION|PROCEDURE fn(` and its target is normalized into
`trig.ExecutesFunction` (`reduce.go:91`). Resolution of a trigger to the actual
executed routine is `Schema.ExecutedProcedure(t)` (`db.go:141`) — it lives HERE, in
the neutral model, **never in a rule and never in a provider** (`db.go:137`). A
rule that wants the trigger's real logic follows the link to the function's own
`Body`; it does not re-parse or guess.

## Alternatives considered

- **`if dialect == postgres` in `triggerBody`** — rejected: forks the shared
  reducer, exactly what ADR 0022 forbids. Adding a dialect would then touch shared
  code.
- **Leave the false incomplete and document it** — rejected: it mislabels every PG
  trigger and would make a routine-body rule read a PG trigger as empty logic (a
  false negative). A one-field datum fixes it cleanly.
- **Resolve the trigger→function link inside a rule or the provider** — rejected:
  the link is a fact about the neutral schema (a trigger names a function that also
  lives in the schema); resolving it once in the model keeps every rule and every
  provider from reimplementing the crossing (ADR 0014, 0015).

## Consequences

- PG triggers are captured `Complete: true` with an explicit redirect Note; their
  logic is reachable via `ExecutesFunction` → the function's `Body`.
- Adding a dialect still means writing one `Dialect` descriptor value; the
  trigger-body shape is now part of that descriptor, no branch added.
- The trigger→function crossing is resolved once, in the neutral model, available
  to any future routine-body rule without provider or rule-level parsing.

## Related

- ADR 0014 — neutral DB model (the link and its resolver live here).
- ADR 0015 — DB rules as pure functions over the neutral schema.
- ADR 0022 — per-dialect DATA descriptor, no dialect branches in shared code.
- ADR 0025 — routine-body capture + `Complete` (the formula that false-incompletes a PG trigger).
