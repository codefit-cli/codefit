# `internal/mcp/testdata`

## `scanall_prechange_invariant.json` / `scanall_prechange_actionable_endpoints.json`

Captured from the **PRE-CHANGE** tree — commit `79e34b0`, before a line of the
`scan-all` response-budget change existed — by running the real `scan-all` handler over
the fixture `budgetFixture` writes in `internal/mcp/scanall_budget_test.go`.

They are what makes R4 of `docs/specs/scan-all-response-budget.md` checkable rather than
asserted: `scanall_prechange_invariant.json` holds the `summary` / `scope` / `score` /
`baseline` the pre-change response produced, and
`scanall_prechange_actionable_endpoints.json` holds the `file:line` of every endpoint the
pre-change response DETAILED in `actionable`. The post-change response must agree with
both — same conclusions, same set of endpoints, less detail per endpoint.

**Do not regenerate them.** Their whole value is that they were produced by code that
predates the change; re-emitting them from the current tree turns the lock into a
tautology. If the fixture is edited, these have to be re-captured from `79e34b0` (or the
fixture change abandoned), never refreshed from `main`.
