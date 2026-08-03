# ADR 0049 — The LLM-era scaffolding is removed; the pyramid survives it

**Status:** Accepted · **Date:** 2026-08-03 · **Phase:** 3, thread H0, slice S2

## Context

`internal/core/pipeline` declared `Pipeline`, `LayerProcessor`, `FilterLayer` and
`PipelineResult` from the first week of the project and was never used by anything. It has
been easy to read that as an unfinished wiring task. **It is not.** The history says
something more specific, and it changes what the right action is.

Three commits, and nothing else in six weeks:

| Date | Commit | |
|---|---|---|
| 2026-06-20 | `3a093f6` | `feat(core): config parser, auth and universal engine` — the package is born. The subject still says *auth*: that day's design carried LLM API keys. |
| 2026-06-21 | `3999505` | `refactor(pipeline): drop the LLM layer (MCP-first)` |
| 2026-06-21 | `27377b5` | `docs: mark cache and pipeline as inert (built, not yet wired)` |

As born, `Pipeline.Run` existed for one concrete job:

```go
if layer.Layer() == LayerLLM && meetsFailOn(all, ctx.FailOn) {
    break // skip the expensive layer; we already fail
}
```

with `LayerLLM FilterLayer = 3`, `meetsFailOn` and `severityRank` in the same file.

**That design was correct for the codefit of that day.** When layer 3 costs money, an
object that threads escalated file lists between layers earns its place: you need the exact
list of what escalates to the expensive tier so you send the minimum, and you need a place
to decide "spend nothing, this already blocks". Both are properties of a pipeline object.

`3999505` removed `LayerLLM`, the early exit and the `--fail-on` plumbing, because codefit
had become MCP-first: it never calls an LLM, and the agent reasons over the mapped surface
with its own model. What survived is a `for` loop over layers with no decision inside it.

Since then all three layers of the pyramid have been implemented and **none of them used
it**: layer 1 (regex) and layer 2 (AST) run inside the security sensor's `scanFile`, and
layer 0 shipped in ADR 0048 wired straight into the walk. The cache slice does not need it
either — it is consulted per file inside the walk, not as a stage transforming a file list.
The interface shape is why: `Process(files []string, …)` models layers that each hand a
file list onward, and the sensor has never worked that way. It runs one file through every
layer at once.

## Decision

**Delete `internal/core/pipeline`**, and with it three fields of `AuditContext` from the
same extinction:

- `NoLLM bool` — "disables every sensor layer that would require an LLM call". codefit has
  no such layer. Set in three tests, read nowhere.
- `FailOn string` — its own doc comment names the extinct machinery: *"the pipeline may
  early-exit before the LLM layer"*. No reader, no writer.
- `Interactive bool` — "so renderers and prompts can adapt". In the MCP-first model the
  report is JSON handed to the agent (PRD §21); there are no renderers. No reader, no
  writer. (The `nonInteractive` flag in `internal/cli/init.go` is an unrelated local
  variable of the `init` command and stays.)

This continues what ADR 0048 began by removing `AuditContext.Since`. It is the same
judgement applied consistently: **a type or field that names a capability nothing exercises
is the same class of claim as a coverage manifest that over-promises.** codefit is an
auditor; the standard it applies to a project's code applies to its own.

## What is NOT being decided

**The filtering pyramid stays.** It is doctrine (PRD §19) and it is implemented: layer 0
narrows what is opened, layer 1 concludes with regex, layer 2 concludes with the AST and
maps surface, and codefit stops there by design. What is deleted is one *expression* of the
pyramid that the code never adopted, not the pyramid itself. `FilterLayer`'s ordering
lives on as the doctrine every sensor already follows.

Nor is this a claim that no abstraction over the layers could ever be useful. It is the
narrower, evidenced claim that **this** one was shaped for a layer that no longer exists,
and that after two phases and three implemented layers there is no evidence it would be
adopted. If a future need arises, it will be designed against that need rather than
inherited from a discarded one.

## Consequences

- `AuditContext` now carries only what is read: `ProjectRoot`, `Language`, `Framework`,
  `Config`, `Scope`. A reader can no longer mistake a fossil for a feature.
- The PRD's §19 sentence naming `core/pipeline` as a home for the optimizations is now
  ahead of the code in one word. The PRD is a design document and is exempt from the
  "reflect today" rule (CLAUDE.md, documentary map), so it is left as written; this ADR is
  the record of the divergence.
- `internal/core/cache` is deliberately **not** deleted: unlike the pipeline it is about to
  be wired, by the slice this ADR ships alongside (`docs/specs/finding-cache.md`). Inert and
  obsolete are different states, and only one of them is a lie.

## Related

- ADR 0048 — the change scope; removed `AuditContext.Since` for the same reason.
- `docs/specs/finding-cache.md` — the slice that wires `core/cache` and that made this
  question unavoidable.
