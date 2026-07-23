// Package query is codefit's neutral, format-agnostic model of a database query
// filter issued from application code — the CODE side of the code↔schema cross
// (index-vs-query), mirroring how internal/core/db models the SCHEMA side.
//
// A provider's QueryExtractor FILLS this model from source (a Prisma where clause
// today, another ORM/dialect later); a core cross-rule (internal/core/crossrules)
// CONSUMES it together with a db.Schema. The one-way dependency arrow of ADR 0014
// holds: query depends on core/db (for the shared db.Pos anchor) and on nothing
// else; providers depend on query, never the reverse.
//
// It speaks LOGICAL naming — the model and field names db.Table.Name and
// db.Column.Name carry — never the physical DB name (@@map/@map), which no query
// written in code ever names (ADR 0029).
package query
