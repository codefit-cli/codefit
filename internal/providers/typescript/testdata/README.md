# testdata — TypeScript provider fixtures

Test fixtures only — **not compiled into the codefit binary**. Per the project
[dependency policy](../../../../SECURITY.md#dependency-policy), even test
fixtures pulled from third parties get their provenance recorded.

## Small fixtures (AST navigation tests) — authored here
- `example.ts`  — import, typed async function, template literal.
- `example.tsx` — React component with hooks and JSX.

## Stress fixtures (safety-cap exit criterion) — real project code
Real TypeScript from open-source projects, pinned to an exact commit and
spot-checked as legitimate TS (no network/exec/eval shenanigans):

| File | Source | Commit | License |
|---|---|---|---|
| `real_vscode_strings.ts` (1411 lines) | microsoft/vscode `src/vs/base/common/strings.ts` | `e6f4d6c6f2977850cdae6b9e53f706f3c5faa63b` | MIT |
| `real_excalidraw_actions.tsx` (1344 lines) | excalidraw/excalidraw `packages/excalidraw/components/Actions.tsx` | `28a9b1711dc0625b8ab5d643dc871810ee13642f` | MIT |

These cover the **size/iteration cap** (large real files).

## Generated stress fixture
- `deeply_nested.tsx` — ~80 levels of nested ternary/JSX, generated to stress the
  **stack-depth cap** specifically (a different cap than size). Generated, not
  downloaded, so no MB-scale file bloats the repo.

## Known limit (documented, not tested)
Minified bundles of megabytes on a single line can hit the runtime's caps and
yield `HasError()`. codefit does not audit minified bundles, so this is an
accepted limit — not exercised by a test (a multi-MB fixture would bloat the repo).
