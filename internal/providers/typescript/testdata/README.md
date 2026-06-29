# testdata — TypeScript provider fixtures

Test fixtures only — **not compiled into the codefit binary**. Per the project
[dependency policy](../../../../SECURITY.md#dependency-policy), even test
fixtures pulled from third parties get their provenance recorded.

## Small fixtures (AST navigation tests) — authored here
- `example.ts`  — import, typed async function, template literal.
- `example.tsx` — React component with hooks and JSX.

## Express / Fastify surface fixtures — authored here
Minimal hand-written cases for the non-Next.js framework surface (IDOR, authz,
over-fetching). Each isolates one behaviour:
- `express_idor_indirect.ts` — handler delegates to a service in another file →
  `indirect_access` + `indirect_call` (cross-file option C).
- `express_idor_local.ts` — inline Prisma access by a client route param.
- `express_authz.ts` — guarded route (`auth.required` middleware) vs unguarded.
- `express_overfetch.ts` — `res.json`/`res.send` sink: whole model, field-limited,
  and service-sourced (frontier).
- `express_discovery_negatives.ts` — same-named non-route calls (`map.get`,
  `array.get`, `cache.get`) that must NOT be enumerated (shape discriminator).
- `fastify_handler_object.ts` — Fastify options-object form (`{ handler, preHandler }`).

## Stress fixtures (safety-cap exit criterion) — real project code
Real TypeScript from open-source projects, pinned to an exact commit and
spot-checked as legitimate TS (no network/exec/eval shenanigans):

| File | Source | Commit | License |
|---|---|---|---|
| `real_vscode_strings.ts` (1411 lines) | microsoft/vscode `src/vs/base/common/strings.ts` | `e6f4d6c6f2977850cdae6b9e53f706f3c5faa63b` | MIT |
| `real_excalidraw_actions.tsx` (1344 lines) | excalidraw/excalidraw `packages/excalidraw/components/Actions.tsx` | `28a9b1711dc0625b8ab5d643dc871810ee13642f` | MIT |

These cover the **size/iteration cap** (large real files).

## Dogfood fixture (Express surface, done criterion) — real project code
Real TypeScript from an open-source project, pinned to an exact commit and
spot-checked as legitimate TS (no network/exec/eval). Vendored verbatim so the
surface mapping runs against real-world code, not a hand-tailored sample.

| File | Source | Commit | License |
|---|---|---|---|
| `real_realworld_article_controller.ts` (251 lines) | gothinkster/node-express-prisma-v1-official-app `src/controllers/article.controller.ts` | `6ac99ea5aeadc4e001dd4d6933c2e269f878a969` | MIT |
| `real_realworld_nest_article_controller.ts` (105 lines) | lujakob/nestjs-realworld-example-app (branch `prisma`) `src/article/article.controller.ts` | `034bd650cd8b9afaca77d09872f4e40b853f142a` | ISC |

These are the Express and NestJS slices' done criteria: codefit must surface the
IDORs in the real controllers — `PUT`/`DELETE /articles/:slug` for Express
(`TestExpressIDOR_Dogfood`), and the service-delegated `@Param` slug handlers for
NestJS (`TestNestIDOR_Dogfood`).

## Generated stress fixture
- `deeply_nested.tsx` — ~80 levels of nested ternary/JSX, generated to stress the
  **stack-depth cap** specifically (a different cap than size). Generated, not
  downloaded, so no MB-scale file bloats the repo.

## Known limit (documented, not tested)
Minified bundles of megabytes on a single line can hit the runtime's caps and
yield `HasError()`. codefit does not audit minified bundles, so this is an
accepted limit — not exercised by a test (a multi-MB fixture would bloat the repo).
