# ADR 0005 — The surface-mapping frontier: what codefit enumerates vs what it hands to the agent

**Status:** Accepted · **Date:** 2026-06-23 · **Phase:** 1 (surface mapping, IDOR)

## Context

The IDOR surface query, validated against a real Next.js/Prisma backend
(Bitácora), enumerated only ~38% of the real IDOR surface. The misses were not
random: they clustered on a few structural idioms the codebase uses pervasively
(async `params` destructuring, `new URL(req.url).searchParams`, resource access
through a service layer). This forced the question the whole surface mold turns
on: **how far does codefit's deterministic enumeration go, and where does it
stop and hand the rest to the agent?**

## The frontier principle

Surface mapping splits every category into three zones.

**FINITE — enumerated deterministically.** Some things have a bounded shape, so
codefit can cover them completely and stop. They are not an infinite chase.
- **id-input:** the HTTP protocol has a closed set of input channels — route
  segment, query string, body, headers. Covering the language/framework idioms
  for each (for Next 15/16: `params.id` and `const {id} = await params`;
  `req.nextUrl.searchParams` and `new URL(req.url).searchParams`; `await
  req.json()`/`formData()`) covers id-input, and then it is *done*.
- **local resource access:** the ORM/DB patterns of the stack are bounded
  (Prisma by shape: `<client>.<model>.<method>`).
These complete; they accumulate as new stacks are added.

**INFINITE — not chased; handed to the agent.** Some things require following
the data out of the handler body, which is taint — already decided to be the
agent's job (ADR 0004). codefit does **not** detect the indirection pattern
(it does not try to recognize "this is a service", by name or by shape). It
recognizes that **the identifier leaves the body** (it is passed as an argument
to a call that is not a recognized local access) and enumerates the handler with
an **honest signal** that says exactly that. The agent follows the data.

**NOT COVERED — declared, never silent.** Whatever neither zone reaches is stated
in the coverage manifest (e.g. an id revalidated through several layers before
reaching the access — deeper taint than a single argument hop). Honesty about the
blind spot, not a silent hole.

## Decision

A surface query enumerates an item when the FINITE id-input signal is present and
the resource access is **either** local (enumerated, FINITE) **or** indirect (the
id leaves the body — INFINITE, handed to the agent with an honest signal). codefit
never chases the indirection: it reports "no local access detected; the identifier
is passed to `<callee>` — the access may be in a service/repository layer" and
lets the agent follow. The coverage manifest declares what remains beyond a single
hop.

This is the same boundary as ADR 0004 (rule vs surface = local-and-conclusive vs
follow-the-data), now applied to the *enumeration condition* of a surface query,
not just to the rule/surface split:

> id-input + local access = enumerate, signal names the access.
> id-input + the id leaves the body = enumerate, signal says the access is
>   indirect and hands the data-follow to the agent.
> id-input but neither = not surface (the id is read but goes nowhere).

### Why not chase the indirection

Detecting "this call reaches a resource" structurally would mean either name
heuristics ("anything called `*Service`") or following the data through
re-assignments and validation layers (taint). The first is fragile and
stack-specific; the second is exactly what we decided the agent does. Chasing it
would re-import the blind spot from the other side — a query that looks complete
but silently misses whatever idiom we did not name. The honest move is to enumerate
on the reliable FINITE signal (a client id entered and left the body) and declare
the rest.

### Guidance for every category and language

This principle governs IDOR, authz, over-fetching, and future stacks (Spring/JPA/
Spring-Security in Java, etc.). For each: cover the FINITE input/access idioms of
the stack from industry best practices (the bulk), enumerate the INFINITE
indirection with an honest signal (the agent's long tail), and declare the NOT
COVERED in the manifest.
