// Package db is codefit's neutral, format-agnostic model of a database's
// structure — the schema a DB-dimension rule reasons over, blind to where it came
// from (Prisma or SQL-DDL — both parsers ship today).
//
// It is a LEAF: it imports nothing from internal/providers (or any other codefit
// package). The provider layer depends on db, never the reverse — the same one-way
// arrow core/findings keeps (ADR 0014). A provider's schema parser FILLS this
// model; the core only consumes it.
//
// The model defines the whole surface Phase 2 audits — OLTP structure plus the
// analytic facts the DW rules need (db.Index.Method, db.Table.Partitioning) —
// even what a given parser cannot express: a Prisma parse fills
// Tables/Columns/Indexes/ForeignKeys and leaves Views/Procedures/Triggers empty,
// which the SQL-DDL parser does fill. Every element carries an origin
// db.Pos{File, Line} so a finding and the baseline can anchor by
// file+line/content.
package db
