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

## `scanall_ts_nodb_prechange.json` / `scanall_ts_withdb_prechange.json`

Captured from the **PRE-CHANGE** tree — commit `337f158` (`main`, before P0-5 /
`scan-all-db-without-language`) — via `git worktree add --detach`, by running the real
`HandleScanAll` over a first-scan (no baseline yet) TypeScript project, with and without a
configured `database.schema_paths`, and dumping the full serialized response
(`json.MarshalIndent`).

They are what makes the "TypeScript happy path is unchanged" modified requirement checkable:
`scanall_regression_test.go` re-runs the SAME fixtures against the current tree, deletes the
new `security` key from both the golden and the live response, and asserts the remainder is
byte-identical — proving every PRE-EXISTING field's value (not just the narrow
`summary`/`scope`/`score`/`baseline` invariant above) and the baseline delta are unchanged,
and that `security` is the only added key.

**Do not regenerate them either**, for the same reason: re-capture from `337f158` (or a
later pre-change commit if the fixture changes), never from `main` after P0-5 merges.
