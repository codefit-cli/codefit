package typescript

import (
	"github.com/codefit-cli/codefit/internal/core/coverage"
	"github.com/codefit-cli/codefit/internal/core/dbcoverage"
)

// CoverageManifest declares what codefit audits for TypeScript and how — what
// the codefit-coverage tool serves and what the human-facing COVERAGE.md mirrors
// (PRD §10, RF-07). Each entry carries an id, a one-line claim, and the full
// prose as its detail; the tool sends the claims always and the prose only when
// an agent names an id. An entry short enough to say everything in its claim
// carries no detail rather than repeating itself. It is NOT itself the root source: the
// rules (rules/<lang>/, internal/sensors/, and the DB dimension's four rule
// roots) are, and this manifest is a HAND-MAINTAINED mirror of them that has to
// be verified against them before it is edited. Calling it the source is how
// drift returns one level down. The two categories that split between
// a deterministic rule and mapped surface (ADR 0004) appear in BOTH lists, each
// side describing exactly what it covers, so an evaluator reads a division of
// labor, not a gap.
//
// CoverageManifest is a provider method (not yet on the shared LanguageProvider
// interface) — the interface convergence is deferred until the Go provider also
// emits this (ADR 0003).
//
// The DB dimension's entries (schema-only, language-independent — ADR 0018) live
// in core/dbcoverage and are appended here rather than duplicated: this file
// carries TypeScript/Next/Express/Fastify/NestJS entries only. They are appended
// at the END of each bucket, which is where they already sat before this
// composition existed, so the manifest's entry ORDER is unchanged. The
// composition itself is unchanged too: dbcoverage returns entries and this file
// appends them, exactly as it appended strings before.
func (*Provider) CoverageManifest() coverage.Manifest {
	return coverage.Manifest{
		Language: "typescript",
		Deterministic: append([]coverage.Entry{
			{
				ID:     "ts.hardcoded-secrets",
				Claim:  "Hardcoded secrets: a variable whose NAME looks like a credential (password, apiKey, token, secret, authToken, …) assigned a static string literal. codefit matches by the variable name plus a literal value — it does NOT scan values for the shape of an API key, a private key, or a connection string, so a hardcoded secret that is not tied to a credential-named variable is not caught here.",
				Detail: "Hardcoded secrets: a variable whose NAME looks like a credential assigned a static string literal. DECLARED SHAPE LIMIT, the widest one measured: the rule matches a const NAME = <string literal> declaration (and let). An OBJECT-LITERAL PROPERTY, a CLASS FIELD, a var, and a TYPE-ANNOTATED const are all SILENT. A shape census of a real TypeScript project counted 1191 object-literal string properties and 316 class-field string assignments against 31 const declarations with a string literal, so the shape this rule reaches is about 38x rarer than the one it does not. The engine is not the limit: the XSS rule matches an object property today. DECLARED NAME LIMIT: the credential name is matched as a raw SUBSTRING in TypeScript, so a name like tokenizer is reported as a credential; Go matches by component and does not. Both limits are pinned by TestShapeCensus.",
			},
			{
				ID:     "ts.weak-crypto",
				Claim:  "MD5 or SHA-1 hashing, called directly or through createHash, flagged wherever it appears; deciding whether a hash is security-relevant needs the data followed, which is surface. Also Math.random() assigned to a security-named variable.",
				Detail: "Weak cryptography: MD5 or SHA-1 hashing — called directly (md5(x), sha1(x)) or via createHash('md5'|'sha1'). These are flagged WHEREVER they appear; a non-security use such as a cache key or an ETag may therefore be a false positive, because deciding whether a hash is security-relevant requires following the data, which is surface. Also flagged: Math.random() assigned to a security-named variable (token, nonce, salt, …), which is not a cryptographically secure source. DECLARED SHAPE LIMIT: the Math.random() check declares a const T = ... declaration, so the same value in an OBJECT PROPERTY or in a RETURN is SILENT. Pinned by TestShapeCensus.",
			},
			{
				ID:     "ts.dangerous-code-evaluation",
				Claim:  "Dangerous code evaluation: eval() or new Function() called with a non-constant argument — an identifier, a call, a concatenation, or an interpolated template. A constant string-literal argument is static code and is not flagged.",
				Detail: "Dangerous code evaluation: eval() or new Function() with a non-constant argument. A constant string-literal argument is static code and is not flagged. DECLARED LIMIT: the STRING form of setTimeout/setInterval, passing code as a string rather than a function, is also an evaluation channel and is NOT flagged — the rule declares eval and new Function only. Pinned by TestShapeCensus.",
			},
			{
				ID:     "ts.sql-injection-inline",
				Claim:  "SQL injection built directly in the database call — a query passed to .query() or .execute() that is assembled inline by string concatenation or by an interpolated template literal, such as db.query(`SELECT ... ${userInput}`). When the query is assembled through an intermediate variable instead, that is surface (below), not here.",
				Detail: "SQL injection built directly in the database call, assembled inline by concatenation or by an interpolated template. Assembly through an intermediate variable is surface, not this rule. DECLARED METHOD-VOCABULARY LIMIT: only the method names .query() and .execute() are recognized, so other raw-SQL entry points stay SILENT even with an interpolated template — Prisma queryRawUnsafe, Knex raw, and better-sqlite3 prepare. This is NOT a shape limit: the interpolated template IS matched; the method name is what the rule does not recognize. Pinned by TestShapeCensus.",
			},
			{
				ID:    "ts.xss-inline-inner-html",
				Claim: "Cross-site scripting through React's dangerouslySetInnerHTML when the __html value is built inline — by concatenation or by an interpolated template. When __html is set from a plain variable, its safety depends on earlier sanitization, which is surface (below), not here; a constant __html is not flagged.",
			},
		}, dbcoverage.Deterministic()...),
		Reasoning: append([]coverage.Entry{
			{
				ID:    "ts.sql-injection-via-intermediate",
				Claim: "SQL injection where the query is assembled in steps through intermediate variables (for example: const q = \"...\" + input; db.query(q)) — codefit maps the database calls as surface so the agent can reason about where the query text came from.",
			},
			{
				ID:    "ts.xss-inner-html-from-variable",
				Claim: "Cross-site scripting where dangerouslySetInnerHTML receives a variable whose safety depends on whether it was sanitized earlier — codefit maps these so the agent can judge whether the value is safe.",
			},
			{
				ID:     "ts.idor",
				Claim:  "Next.js route handlers and Server Actions that take a client-controlled identifier and reach a resource, mapped so the agent verifies ownership. Detected by shape, never by filename or parameter name; the id-input and the local access are enumerated, and an id that leaves the body is enumerated with an honest frontier signal.",
				Detail: "IDOR: Next.js App Router route handlers AND Next.js Server Actions ('use server') that receive a client-controlled identifier and reach a resource — mapped so the agent verifies ownership is checked. For a route handler the id-input is read from the request (route param, query string, or request body); for a Server Action it arrives as the function's arguments (or a FormData), since an action is a POST endpoint whose input IS its arguments. Server Actions are detected by SHAPE — an async function under a 'use server' directive at file level (every exported async function) or at function level (an inline action in a Server Component, or a non-exported one) — never by filename, so an action in actions.ts, lib/, or inline is not a blind spot (ADR 0005). An object-shaped argument is covered: the parameter binding is seeded as the id-var, so a nested data.id flows to the access. codefit enumerates the id-input and the local Prisma access deterministically; when the id leaves the body (passed to a service/repository call), it is still enumerated with an honest signal saying the access may be indirect, and the agent follows the data. An id reached only after several intermediate revalidation steps may not be linked to its access — that deeper data-follow is the agent's, declared here (ADR 0005). id-input is matched by structure, not by name, so a filter query param or a non-id action argument (date, limit) may be over-enumerated; the signal names what it read so the agent dismisses it at a glance — this noise is accepted to avoid a name-based blind spot for non-standard id names (ADR 0005). Each item carries the queryable structural facts local_access_detected (true = a Prisma access is in the body; false = the id leaves the body to a service/repository, the frontier) and server_action (true = the entry is a Server Action, not a route handler) — facts, not judgments. IDOR is about resource OWNERSHIP, which codefit cannot verify from structure, so an IDOR with a local access stays ACTIONABLE even when an authz helper is present: a known/registered helper proves authentication/permission, not that the caller owns this resource (ADR 0006 amended; ADR 0013). known_authz_detected gates the authz concern, never the IDOR one.",
			},
			{
				ID:     "ts.broken-authorization",
				Claim:  "Route handlers and Server Actions performing a sensitive operation, with a fact stating whether a known authorization helper was detected. Broader than IDOR because it needs no client id, and matched by the structural operation rather than by route name.",
				Detail: "Broken authorization: Next.js App Router route handlers AND Server Actions that perform a sensitive operation — they touch data or mutate state (a Prisma read or write, or an indirect service/repository call) — mapped with a signal stating the operation and whether a known authorization helper was detected in the body, so the agent verifies the caller is permitted. This is broader than IDOR: it needs no client id, only a sensitive operation, so it enumerates more entries (a deliberate trade — an entry that does something sensitive is worth a glance even if no check is missing). A Server Action that mutates or reads with no detected authz helper is exactly the case worth surfacing — actions are POST endpoints developers often do not guard like endpoints. It is matched by the structural operation, never by route name (an endpoint without 'admin' in its path may still need authorization — ADR 0005). The indirect/service case and deep data-follow are the agent's, declared as for IDOR. Each item carries a queryable structural fact, known_authz_detected, the boolean form of the prose signal; consumers may order by it (unchecked first) but it is a FACT (a known pattern was/wasn't seen), not a severity — known_authz_detected=false means 'no known authz pattern was detected here', never 'this is unauthorized'. The recognized helper set is the built-in NextAuth-style names PLUS the project's own helpers, which the agent identifies by reasoning over the code and a human approves registering via codefit-baseline-register-authz-helper; codefit persists them in the committed baseline and recognizes them on later scans without the agent re-reasoning (ADR 0013). Registering a helper clears the AUTHZ gap (permission), never the IDOR/ownership gap. A SECOND fact, authz_result_used, says whether a detected guard actually DECIDED something here: its result reached a branch, a return, an assignment or another call, or a middleware guard ran before the handler. known_authz_detected=true beside authz_result_used=false is the precise statement that the guard was CALLED and its answer went NOWHERE, so it gates nothing at that site and the authz gap stays open (ADR 0082). It is a FACT, never a verdict: a helper that THROWS or REDIRECTS gates correctly with its result unused, and codefit cannot see the helper's body from the handler — the signal says so and hands the agent that exact question. Declared limit: authz_result_used is computed for TypeScript only; the Go provider does not compute it and OMITS the key rather than emitting a false one, so an absent fact never raises the gap (ADR 0067).",
			},
			{
				ID:     "ts.over-fetching",
				Claim:  "Points where a domain object is serialized from a Prisma find, with a fact stating whether the find limits its fields. codefit gives the structural fact; which fields are sensitive needs the schema and is the agent's judgment.",
				Detail: "Over-fetching of sensitive data: points where a domain object is serialized from a Prisma find — for a route handler the serialization sink is an explicit Response.json / NextResponse.json / JSON.stringify; for a Server Action it is the RETURN value, which the framework serializes to the client (an action has no Response.json). Mapped with a queryable structural fact, field_limiting_detected (true = the find has a select/omit clause; false = no clause, so all columns are returned). codefit does NOT judge whether the exposed fields are sensitive — it does not know that passwordHash is sensitive and name is not; that requires the domain/schema and is the agent's. codefit gives the structural fact (a full model serialized without select); the agent reads the schema and judges sensitivity. Serialization from a service/repository instead of an inline find is the frontier (codefit cannot see the find's field selection) — enumerated with an honest signal for the agent to follow. Matched by the structural serialization, never by model name (an unfamiliar model may still over-expose — ADR 0005). The tool orders by structural certainty using the queryable facts local_access_detected and field_limiting_detected — a local find with no select first (confirmed), the frontier last (present, honest) — without filtering anything; the frontier is never dropped, because filtering it would reintroduce a shape-based blind spot (ADR 0005).",
			},
			{
				ID:     "ts.nplus1-query-in-loop",
				Claim:  "Query call sites sitting lexically inside a loop or a per-element callback, across every framework this provider maps handlers for. Ordered by structural facts and never filtered: codefit cannot know a collection's real size at review time, so it names the iterated source instead of dismissing it.",
				Detail: "N+1 query-in-loop pattern (DB-201): every query call site — a local Prisma access OR a call at the cross-function frontier (the same service/repository frontier IDOR/authz/over-fetching already declare, reusing isPrismaCall/isServiceCall verbatim) — that sits lexically inside a loop construct: a for/for...of/for...in/while/do...while statement, or a per-element callback iteration (.forEach/.map/.flatMap/.filter/.reduce/.some/.every/.find). It reuses the same handler-discovery mechanism as IDOR/authz/over-fetching (auditTargets), so it applies uniformly to every framework this provider maps handlers for — Next.js route handlers and Server Actions, Express, Fastify, and NestJS — with no separate per-framework detector. Per ADR 0005 it is ORDERED, never FILTERED: a loop over a literal array of 3 elements is enumerated exactly like a loop over an unbounded query result, because codefit cannot know the collection's real size at review time — the iterated source is named as a fact (e.g. \"a literal array of 3 element(s)\", \"the variable 'users'\") so the agent dismisses an obviously-bounded loop at a glance, never filtered away. A call wrapped in Promise.all(...) is still enumerated as one query per element (concurrent rather than sequential, but still N round-trips) — the fact promise_all_wrapped distinguishes it from a directly-awaited sequential call (awaited_in_loop); nested_loop is exposed when the query sits under more than one enclosing loop. A cross-function-frontier call carries an honest signal that codefit did not follow it into the function that produced it and whether it performs a per-iteration query is NOT verified — the same frontier IDOR/authz/over-fetching already declare, never silently resolved here either. This is a database access pattern conceptually, but it is mapped as per-ENDPOINT surface (discovered from the application's code — loops plus query call sites — not from the schema), so it appears in scan-all's endpoint buckets alongside IDOR/authz/over-fetching, never in the schema-only DB section.",
			},
			{
				ID:     "ts.express-fastify-handlers",
				Claim:  "The same IDOR, authorization, over-fetching and N+1 surface mapped for Express and Fastify. Handlers are discovered by shape rather than by path, guards are read from route middleware and hooks, and a cross-file service call is named rather than followed.",
				Detail: "Express and Fastify route handlers: the same IDOR, broken-authorization, over-fetching, and N+1 surface above is mapped for these non-Next.js frameworks. Handlers are discovered by SHAPE, never by path — an Express router/app `.get|.post|.put|.patch|.delete('/path', ...middleware, handler)` call, and Fastify's options-object form `.get('/path', { handler, preHandler })` — so a same-named non-route call (map.get('/k', v), arr.get(0, cb)) is not mistaken for a route: a handler requires a string-literal path AND an inline function. The client id-input is read from req.params/req.query/req.body (Express) or request.params/query/body (Fastify), keyed off the handler's FIRST parameter (its name is the developer's, not assumed to be `req`), so a non-standard route param like `slug` is not a blind spot. The over-fetching sink is the response object's .json()/.send() (Express res, Fastify reply), keyed off the SECOND parameter. The authorization guard in these frameworks is route MIDDLEWARE, not a body call: codefit reads Express positional middleware (router.post('/x', auth.required, handler)) and Fastify preHandler/onRequest hooks, so known_authz_detected reflects a guard applied at the route, and the signal states honestly whether it looked in the body or also the route middleware. When the resource access or sensitive operation is NOT local to the handler body — the id is passed to a service/repository in another file (the common controller→service split) — codefit emits the queryable fact indirect_access=true and names the callee in indirect_call; it does NOT follow the call across files (cross-file option C), the agent reasons over the named function. Detection is by shape, so an id-input or sink that is structurally present but semantically irrelevant may be over-enumerated; the signal names what it read so the agent dismisses it at a glance.",
			},
			{
				ID:     "ts.nestjs-controllers",
				Claim:  "The same surface mapped for NestJS, where handlers are methods carrying an HTTP-verb decorator and the guard is the presence of @UseGuards. Module-bound middleware and global guards are NOT detected, so an app guarding that way reads as unverified across the board.",
				Detail: "NestJS controllers: the same IDOR, broken-authorization, over-fetching, and N+1 surface is mapped for NestJS, whose routes are class methods declared with DECORATORS. A handler is a method carrying an HTTP-verb decorator (@Get/@Post/@Put/@Patch/@Delete/...), detected by that shape — never by @Controller (a class without it can still expose routes — ADR 0005). The client id-input is read from the method's PARAMETER decorators (@Param('id'), @Query(), @Body()), each bound to an id-var the downstream follows; a framework @User-style injected param (the authenticated principal) is NOT treated as a resource id. The authorization guard is the @UseGuards decorator on the method or inherited from the class — detected by PRESENCE (the decorator IS the guard mechanism; guard class names are arbitrary, so codefit names the guard but does not match it against a known set), and a class-level guard applies to every method. The over-fetching sink is the RETURN value, which NestJS serializes to the client (like a Server Action — there is no res.json). When the resource access or sensitive operation lives in an injected service in another file (this.someService.method(...), the common controller→service split), codefit emits indirect_access=true and names the callee — it does not follow the call across files (option C). Known limits: a service method whose name collides with a Prisma method (create/update/delete/findMany/...) is reported as a LOCAL Prisma access rather than an indirect call (the Prisma-by-shape match above) — still surfaced for review with the real callee named; a handler that returns through an explicit @Res() response object is not mapped. Guard detection sees @UseGuards (method- and class-level) ONLY — NestJS authentication applied as module-bound middleware (consumer.apply(AuthMiddleware) in a module's configure()) or as a global guard (APP_GUARD provider / app.useGlobalGuards) is NOT detected, so on apps that guard via middleware rather than @UseGuards, known_authz_detected reads false across the board (a conservative 'verify', never a false 'secure').",
			},
		}, dbcoverage.Reasoning()...),
		NotCovered: append([]coverage.Entry{
			{
				ID:    "ts.race-conditions",
				Claim: "Race conditions in business logic.",
			},
			{
				ID:    "ts.architectural-design-flaws",
				Claim: "Architectural design flaws.",
			},
			{
				ID:    "ts.business-logic-correctness",
				Claim: "Business-logic correctness (this is not a security property).",
			},
			{
				ID:    "ts.deep-static-taint-analysis",
				Claim: "Deep static taint analysis — covered by surface mapping plus agent reasoning, not deterministically.",
			},
			{
				ID:    "ts.other-server-frameworks",
				Claim: "JavaScript server frameworks beyond Next.js, Express, Fastify, and NestJS: not yet covered — a known gap, not a silent one.",
			},
			{
				ID:     "ts.handler-passed-by-reference",
				Claim:  "An Express or Fastify route handler passed by reference rather than as an inline function is not enumerated: its body lives in another scope and is a cross-file case the agent must follow. An auth guard by reference is unaffected.",
				Detail: "An Express/Fastify route handler passed by REFERENCE — a named identifier rather than an inline function (router.get('/x', listUsers), where listUsers is defined elsewhere) — is not enumerated: codefit maps inline handler bodies, and a handler whose body lives in another scope is a cross-file case the agent must follow. (An auth GUARD by reference is unaffected: it is matched by name in the registration, so a known/registered guard still sets known_authz_detected without codefit reading its body.)",
			},
			{
				ID:    "ts.server-action-formdata-inline",
				Claim: "Server Action input read inline from a FormData (formData.get('key')) and passed DIRECTLY into a service call, with no intermediate variable and no local Prisma access, may not link to that indirect access; when bound to a variable first, it links. The local-access case always enumerates.",
			},
		}, dbcoverage.NotCovered()...),
		// TypeScript contributes no entry of its own here yet: the only promised
		// id delivered under another identifier today is the DB dimension's
		// DB-201, and dbcoverage owns the DB/DW namespace. Composed by append,
		// exactly like the three lists above, so a later TypeScript entry lands
		// in front of the DB ones without touching dbcoverage.
		DeliveredElsewhere: append([]coverage.Entry{}, dbcoverage.DeliveredElsewhere()...),
	}
}
