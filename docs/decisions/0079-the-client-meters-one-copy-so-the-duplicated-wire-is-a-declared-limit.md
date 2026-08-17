# 0079 — The client meters one copy, so the duplicated wire is a declared limit

**Status:** accepted · **Date:** 2026-08-17 · **Phase:** 3, priority P0-14 (`docs/roadmap.md`)
· **Does not supersede** [ADR 0062](0062-the-response-budget-is-calibrated-by-bisection-not-chosen.md):
it CONFIRMS that ADR's number by measuring the one thing it had to assume. `ResponseBudgetBytes`
stays 40 000 and no arithmetic in 0062 changes.

## Context

Every codefit tool response crosses the wire twice. `internal/mcp/server.go`'s `addTool` returns
`nil` for the `*CallToolResult`, and go-sdk v1.6.1 (`mcp/server.go:389`) then copies the same
output JSON into a `TextContent` block whenever the handler leaves `res.Content == nil`. A
committed integration test (`TestServerProtocolEndToEnd`) proves the two copies byte-identical
over a real client/server transport pair.

P0-14 filed that duplication with one question deliberately left open, because answering it
wrongly would have corrupted the response budget:

> **which copy the client meters is UNMEASURED.** […] Measuring which copy is metered is the
> first move here, before any budget number moves.

The stakes were asymmetric. If the client metered the whole wire, ADR 0062's bracket was in
half-sized units and the budget was wrong by ~2×. If it metered one copy, the budget was right
and the duplication cost bandwidth only. Both readings were plausible from the artifacts alone,
and neither could be settled by reading code — the metering happens inside the client.

## What was measured

Two binaries built from the same tree, differing by one line in `addTool`:

- `SPIKE-DUPLICATED` — `main`'s behaviour: `return nil, out, err`, two copies on the wire.
- `SPIKE-SINGLE-COPY` — returns `&mcpsdk.CallToolResult{Content: []mcpsdk.Content{}}`. A
  non-nil, empty `Content` suppresses the SDK's copy while `StructuredContent` is still
  populated (`mcp/server.go:384` precedes the `res.Content == nil` branch at `:389`). The SDK
  itself uses that idiom at `mcp/server.go:756`.

Both were connected as separate MCP servers to a live client (**Claude Code 2.1.196**,
2026-08-17) and driven over stdio with the SAME content: `codefit-coverage` for `typescript`,
sized by the number of ids passed to `detail`.

| call | binary | ids | result | size the client reported |
|---|---|---|---|---|
| lower control | duplicated | 30 | **accepted** | response declared `bytes: 64 661` |
| discriminator | duplicated | 35 | **rejected** | `result (74,580 characters)` |
| discriminator | single copy | 35 | **rejected** | `result (74,580 characters)` |

At 35 ids the payload is ~74 968 B. **Metering both copies would have reported ~149 936.** The
client reported 74 580 — one copy — and reported the **identical** figure for the binary that
duplicates and the one that does not.

A second, independent control: the two tool results the client persisted are **74 918 bytes and
byte-identical** (`cmp` → IDENTICAL). The client does not merely count one copy; it stores one.

## Decision

**The client meters one copy. The duplication is recorded as a declared limit, not removed.**

Three facts decide it together:

1. **Removing it buys zero headroom** in this client, which is what the measurement above shows.
2. **The duplication is spec-recommended.** The MCP specification describes the `TextContent`
   copy as backward compatibility for clients that read `content` but not `structuredContent`,
   and the SDK's own comment at `mcp/server.go:386-388` cites it. Suppressing it opts out of a
   compatibility bridge.
3. **`addTool` is the single seam every tool passes through** — 16 registration sites. A change
   there is a protocol-layer decision affecting every response codefit emits.

Zero benefit against non-zero compatibility risk, across every tool at once. The honest outcome
is a limit stated with its measurement, not a fix.

## The positive control, and why it matters more than the result

ADR 0062's bisection (2026-08-09, `scan-all` content) bracketed the client at **64 097 accepted
/ 74 195 rejected**. This measurement (2026-08-17, `coverage` content — a different tool, a
different content shape, eight days later) brackets it at **64 661 accepted / 74 580 rejected**.

Within ~1% at both ends. The method reproduces, so the result is not an artifact of how it was
measured. Without that control the numbers above would be one unreplicated observation.

Related arithmetic that also reconciles, recorded so a future reader does not mistake the small
deltas for noise: an offline probe marshals the 35-id response to 74 968 B; the persisted file is
74 918 B; the client reports 74 580 **characters**. The bytes-to-characters gap is the prose's
multi-byte UTF-8 punctuation. The `bytes` field a response declares (64 661 at 30 ids) sits ~695 B
under what the whole response marshals to (65 356) — the envelope, matching the 182 848 / 182 152
delta measured at 68 ids.

## ADR 0062's calibration was made WITH the duplication live

Established from the artifact the bisection actually drove, not from a nearby commit:

- `addTool` at `d054534` — the exact commit of the `v0.2.6` binary ADR 0062 names — reads
  `return nil, out, err`, byte-identical to `main` today.
- `go.mod` pins `github.com/modelcontextprotocol/go-sdk v1.6.1` at that commit and today: same
  version, same auto-serialization branch.
- ADR 0062 states the `v0.2.6` binary was driven over stdio, i.e. through that seam.

So the accepted/rejected bracket was always measured against a duplicated wire. `ResponseBudgetBytes
= 40 000` needs no correction, and P0-14's warning — that ADR 0062's ratio **must not be
double-counted** on the strength of the duplication finding — is now a measured fact rather than a
caution.

## What this does NOT establish

- **One client, one date, one tool.** Claude Code 2.1.196 on 2026-08-17, driving
  `codefit-coverage`. Cursor, VSCode, OpenCode and any other MCP client have their own metering,
  unmeasured here. This is the same narrow scoping ADR 0062 declared for itself, kept deliberately.
- **The wire cost is real and unchanged.** Every response still puts twice its bytes through the
  pipe. What the measurement rules out is a *budget* consequence, not a *transport* one.
- **Nothing about a client that reads only `content`.** No such client was exercised. Its
  existence is the reason point 2 of the decision holds, and it remains unquantified.
- **P0-4's remaining half is untouched.** The structural per-bucket cap for `db.surface` — a
  response that exceeds its budget with nothing withholdable — is not addressed by anything here.

## Consequences

- `internal/mcp/server.go` is unchanged. No tool's response shape moves.
- P0-14 closes as a **declared limit with a measurement**, not as a fix, and `docs/roadmap.md`
  says so in place of its "measure this first" instruction.
- The one-line suppression is recorded above, so re-opening this decision for a different client
  costs a rebuild, not a rediscovery.

## Alternatives considered

**Remove the duplication.** Rejected: measured zero headroom gain in the client that was
measured, against opting out of a spec-recommended compatibility shim, across all 16 tools at
once.

**Keep it open pending more clients.** Rejected: an open question with no owner and no scheduled
measurement is how a declared limit goes stale — the lesson
[ADR 0058](0058-a-declared-limit-can-go-stale-and-nobody-re-verifies-it.md) already paid for. The
limit is stated with its exact scope instead, so a future reader knows precisely which client was
measured and what re-measuring would cost.

**Shrink the text copy to a pointer instead of the full payload.** Rejected as worse than either
option: a client that reads only `content` would receive a stub instead of data, which breaks it
more quietly than removing the block outright.
