// Package dbrules holds codefit's deterministic database-structure rules. Each
// rule reasons over the neutral core/db.Schema and emits deterministic Findings
// (affirmations, Confidence 1.0) and/or SurfaceItems (questions for the agent) —
// it never sees a concrete ORM. Because a rule is language-neutral, every future
// ORM provider (JPA, Drizzle, EF Core) inherits it for free once it can produce a
// db.Schema (ADR 0015).
//
// dbrules lives BESIDE core/db, not inside it: a rule imports core/findings and
// core/surface, so it cannot live in the pure leaf core/db (ADR 0014). The
// dependency arrow is dbrules -> {core/db, core/findings, core/surface}; core/db
// still imports nothing.
package dbrules
