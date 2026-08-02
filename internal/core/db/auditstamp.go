package db

import "strings"

// IsAuditTimestampName reports whether a COLUMN NAME denotes a row-level audit
// timestamp — the moment the row was created, last changed, or recorded.
//
// It lives here, in the neutral model, for the same reason IndexLike does: the
// notion is shared by rules in two different packages and must be defined ONCE
// so the two cannot drift (ADR 0015). Its consumers today are
//
//   - dbrules' DB-052, which asks it PER TABLE ("does this table stamp its
//     rows?"), and
//   - paradigm's no_audit_timestamps warehouse signal, which asks it PER SCHEMA
//     ("does NOT ONE table stamp its rows?").
//
// Before this function existed, paradigm replicated db052's two-name convention
// in a private normalizer and said so in a comment ending "if a third consumer
// appears, the normalizer should move to a shared home rather than be copied
// again". Widening the vocabulary made that comment come due: two copies of a
// 2-name list drift silently, and two copies of a 16-name list drift fast. The
// DW-005 / timeDimensionName incident in this repository is exactly that
// failure, already paid for once.
//
// # It matches a NAME, never a TYPE
//
// `timestamp` is in the vocabulary as a COLUMN name, because
// `"timestamp" timestamp(6) without time zone` is how a real append-only event
// table in the field spells its creation time. It is ALSO a SQL type name, and
// nothing here looks at a column's type: a column named `logged_value` typed
// `timestamp` is not an audit stamp and this function says so. The rule-level
// lock for that is in internal/providers/sqlddl (db052_integration_test.go),
// over real DDL parsed by the real parser, because only the parser can put a
// type and a name in the same column.
//
// Not looking at the type is a DECLARED LIMIT in both directions: a
// `created_at` column typed String counts, and synapse's `creation_ts` typed
// bigint (epoch milliseconds) counts too. The second is the reason not to add a
// type gate on a hunch — epoch integers are a completely ordinary way to stamp
// a row, and a type filter would silently reinstate the false positives this
// vocabulary exists to remove.
//
// # Equality, never substring
//
// The comparison is EQUALITY over the normalized name. normalizeAuditIdent
// drops separators, so a substring test would admit `update_trace_id`,
// `insertion_prev_event_id`, `commission_created`, `ts_added_ms` and `creator`
// — all real columns of tables DB-052 fires on in the measured corpora, none of
// them an audit stamp. auditstamp_test.go locks every one of them.
//
// # Every entry is measured, and that is the admission rule
//
// A name earns its place by appearing, in a real corpus, on a table DB-052 was
// firing on — i.e. by being a case where the rule's sentence ("this table has
// no created/updated audit timestamps") was FALSE. The counts below are
// distinct tables, measured over 29 corpora (26 external + 3 vendored
// excerpts) plus the reporting project's own dump:
//
//	created_at / updated_at   107 / 83  dub, formbricks, dw-salesmart, dw-barousse
//	last_update                    35   pagila, sakila-full, and both excerpts
//	create_date                     4   pagila, sakila-full, dw-ngthao
//	creation_date                   3   dw-p4pa
//	update_date                     3   dw-p4pa
//	timestamp                       2   synapse (+2 in the reporting project)
//	created_ts                      2   synapse
//	creation_time                   1   synapse
//	creation_ts                     1   synapse
//	inserted_ts                     1   synapse
//	added_ts                        1   synapse
//	added_at                        1   synapse
//	ModifiedDate                    1   AdventureWorks (vendored tsql excerpt)
//
// SYMMETRIC SIBLINGS ARE DELIBERATELY ABSENT: created_on, date_created,
// inserted_at, modified_at, last_modified, updated_ts and the rest of the
// obvious closure are NOT here. They are plausible, and plausible is what got
// this repository twice already. Admitting a name SILENCES a table, so a
// guessed entry buys a false negative — the failure mode that hides — in
// exchange for nothing measured. The next corpus that shows one is the argument
// for adding it, and the doc comment above db052 states this limit to the
// reader of the finding.
//
// Prefixed forms are out for the same reason equality is: tpcds spells its
// dbgen stamps `dv_create_date` / `dv_create_time`, and admitting those means
// admitting every `<anything>_create_date`, including a business column.
func IsAuditTimestampName(name string) bool {
	return auditTimestampNames[normalizeAuditIdent(name)]
}

// auditTimestampNames is the vocabulary, keyed by NORMALIZED name (lowercase,
// separators dropped). Grouped by what the name says about the row, which is
// also the order the doc comment above justifies them in.
var auditTimestampNames = map[string]bool{
	// Creation / insertion.
	"createdat":    true,
	"createdate":   true,
	"createdts":    true,
	"creationdate": true,
	"creationtime": true,
	"creationts":   true,
	"insertedts":   true,
	"addedat":      true,
	"addedts":      true,

	// Modification.
	"updatedat":    true,
	"updatedate":   true,
	"lastupdate":   true,
	"modifieddate": true,

	// Record / event time: the row itself IS the event, and its one time
	// column is when it happened.
	"timestamp": true,
}

// normalizeAuditIdent lowercases an identifier and drops separators, so
// createdAt, created_at, Created-At and CREATEDAT are one name. It is the
// convention dbrules.normalizeIdent already used for this comparison, moved
// here with the vocabulary rather than copied a third time.
func normalizeAuditIdent(s string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(s))
}
