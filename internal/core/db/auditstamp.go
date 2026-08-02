package db

import "strings"

// IsAuditTimestampColumn reports whether a COLUMN denotes a row-level audit
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
// It takes a Column and not a name so the type gate below cannot be skipped by
// a caller holding only a string. There is deliberately no name-only export.
//
// # The rule: a VERB, a TIME AFFIX, and a TYPE that can hold a time
//
// The three parts are separately load-bearing, and the measurement behind each
// is recorded here rather than in a changelog nobody reads at the call site.
// ADR 0047 is the long form; ADR 0046 is the fixed list this supersedes.
//
// A VERB of creation or modification (auditStampVerbs) is what makes a name an
// audit stamp. The SUFFIX alone cannot: across the 29 measured corpora, 75
// distinct columns end in `At`, and 74 of them are business event times —
// expiresAt, startedAt, finishedAt, lastSyncAt, paidAt, clickedAt, bannedAt,
// publishedAt, sentAt and the rest. A table whose only time column is
// `expires_at` genuinely does not record when its row came into being, so
// admitting the suffix alone would go quiet over a table that should still
// speak. That is the error nobody sees, and the reason the verb is required.
//
// A TIME AFFIX (auditStampTimePrefixes / auditStampTimeSuffixes) is what makes
// the verb about a moment. It is load-bearing in the other direction too:
// `created_by` is a creation verb on a genuine audit field and it is a PERSON,
// not a time. DB-052 asks about time tracking, so `_by` is not a time affix and
// `created_by` does not match. formbricks declares `created_by TEXT` beside
// `created_at` and `last_sync_at` in one migration; all three are tested.
//
// A bare verb with NO affix does not match. `created` says WHETHER, not WHEN,
// and no corpus shows one used as a stamp; `last_update` (36 measured columns)
// reaches the answer through its `last` prefix, which is itself a time word.
//
// # Why a rule and not a list
//
// This function used to be a fixed list of sixteen measured names. Every entry
// was real, which was right, and the list could still only ever know spellings
// that happened to appear in the corpora that happened to be cloned. It
// rejected `created_on`, `date_created`, `inserted_at`, `modified_at`,
// `last_modified` and `updated_ts` for lack of measured support — all
// unmistakably audit stamps — so any project spelling it one of those ways got
// the same false warning the list was built to remove. A verb plus a time affix
// closes the family instead of enumerating the members of it.
//
// Prefixed forms stay out, which is what the fixed list's equality test bought
// and this rule keeps by requiring the affixes to be the WHOLE remainder:
// tpcds spells its dbgen stamps `dv_create_date` / `dv_create_time` and dw-p4pa
// spells one `cst_create_date`; admitting those means admitting every
// `<anything>_create_date`, including a business column.
func IsAuditTimestampColumn(c Column) bool {
	return auditStampName(c.Name) && auditStampType(c.Type)
}

// auditStampType is the type gate, and its measurement is narrow and unanimous:
// across the 29 corpora plus the reporting project, EVERY column whose name
// passes auditStampName is typed datetime (258 occurrences) or int (9).
//
// The nine ints are the reason this is not "must be a date type": synapse stores
// epoch timestamps as integers — `creation_ts BIGINT UNSIGNED`,
// `added_at BIGINT NOT NULL`, plus created_ts, inserted_ts, added_ts and
// creation_time — and a date-only gate would reject all of them and reinstate
// exactly the false positives this rule exists to remove.
//
// Nothing else is admitted, and the DIRECTION is why that is safe. Rejecting a
// type keeps the table FIRING: a visible false positive the agent can dismiss
// from the item's own `columns:` list. Admitting one SILENCES a table: the
// error nobody sees. Zero corpora show a stamp typed string, text, float, bool,
// json, enum, bytes or unknown, so none of them is admitted on a hunch —
// including TypeUnknown, the parser's honest fallback, because an unclassified
// type is not evidence of a time.
//
// What it rejects that a name-only rule could not: a boolean. dw-barousse
// really declares `commission_created BOOLEAN`, and a `created_flag BOOLEAN`
// says WHETHER the row was created, never when.
func auditStampType(t Type) bool {
	return t == TypeDateTime || t == TypeInt
}

// auditStampName reports whether a column NAME says "when this row was created
// or changed". The name must decompose, with nothing left over, into an
// optional time prefix, a creation/modification verb, and an optional time
// suffix — with AT LEAST ONE affix present.
//
// Requiring the affixes to consume the whole remainder is what keeps
// `dv_create_date`, `wp_creation_date_sk`, `ts_added_ms`,
// `last_federation_update_ts` and `addedToMarketplaceAt` out: each is a real
// column of a table DB-052 fires on, and each leaves a non-verb remainder.
func auditStampName(name string) bool {
	n := normalizeAuditIdent(name)

	// `timestamp` carries no verb and cannot be reached by the rule below. It
	// is measured as a COLUMN name — `"timestamp" timestamp(6) without time
	// zone` is how the reporting project's append-only event tables spell their
	// creation time, and synapse's monthly_active_users spells it
	// `timestamp BIGINT` ("last time we saw the user … in ms since epoch").
	if n == "timestamp" {
		return true
	}

	for _, p := range auditStampTimePrefixes {
		if !strings.HasPrefix(n, p) {
			continue
		}
		for _, s := range auditStampTimeSuffixes {
			if p == "" && s == "" {
				// A bare verb is not enough: `created` says WHETHER, not WHEN.
				continue
			}
			if len(p)+len(s) > len(n) || !strings.HasSuffix(n, s) {
				continue
			}
			if auditStampVerbs[n[len(p):len(n)-len(s)]] {
				return true
			}
		}
	}
	return false
}

// auditStampVerbs are the verbs of ROW LIFECYCLE — the row coming into being or
// being changed. Each stem is listed in the forms a column name actually uses.
//
// MEASURED in the corpora: create (create_date), created (created_at,
// created_ts), creation (creation_date, creation_time, creation_ts), inserted
// (inserted_ts), added (added_at, added_ts), update (last_update, update_date),
// updated (updated_at), modified (ModifiedDate, modifiedAt).
//
// MORPHOLOGICAL CLOSURE, not measured: insert, insertion, add, modify, change,
// changed. They are the remaining forms of stems the corpora do show, and
// closing the family is the point of the redesign.
//
// DELIBERATELY ABSENT, with reasons rather than omission:
//   - `register` / `registered`. A `registered_at` is when the USER registered
//     — a business event that merely tends to coincide with row creation, the
//     same category as `started_at`. The corpora show `registeredDomains`
//     (dub), a business column, and nothing else.
//   - `new`. Not a verb here: audit-log tables spell their payload columns
//     `old_value` / `new_value`, and `new_date` is a business date.
//   - `renew` / `renewed`. synapse declares `last_renewed_ts` and dub declares
//     `autoRenewalDisabledAt`; neither is about the row's own lifecycle.
var auditStampVerbs = map[string]bool{
	// Creation / insertion.
	"create":    true,
	"created":   true,
	"creation":  true,
	"insert":    true,
	"inserted":  true,
	"insertion": true,
	"add":       true,
	"added":     true,

	// Modification.
	"update":   true,
	"updated":  true,
	"modify":   true,
	"modified": true,
	"change":   true,
	"changed":  true,
}

// auditStampTimeSuffixes are the words that turn a verb into a moment when they
// TRAIL it. `at`, `ts`, `time` and `date` are measured (created_at, created_ts,
// creation_time, create_date); `on`, `datetime` and `timestamp` are their
// siblings — `on` and `at` fill the same English temporal slot, and
// `created_on` is precisely the spelling the fixed list rejected and warned
// falsely about.
//
// The empty string is a member so a verb can be reached by its PREFIX alone,
// which is how `last_update` — 36 measured columns, the single largest source
// of DB-052 false positives — is recognized.
//
// `by` is NOT here and must not be: `created_by` is a person.
var auditStampTimeSuffixes = []string{"", "at", "on", "ts", "time", "date", "datetime", "timestamp"}

// auditStampTimePrefixes are the words that turn a verb into a moment when they
// LEAD it. `last` is measured (last_update); `date` covers the inverted word
// order (`date_created`, `date_modified`).
//
// `ts` and `time` are NOT prefixes: synapse declares `ts_added_ms`, a real
// column of a firing table, and a `ts` prefix is the first half of admitting
// it.
var auditStampTimePrefixes = []string{"", "last", "date"}

// normalizeAuditIdent lowercases an identifier and drops separators, so
// createdAt, created_at, Created-At and CREATEDAT are one name.
//
// It is only ever used for the whole-name decomposition above, where every
// affix is anchored and the remainder must be an exact verb. Dropping the
// separator makes the affix test blunter than it looks — with `_` gone,
// "risk", "disk" and "asterisk" all end in "sk" — which is why the remainder is
// matched by EQUALITY against the verb set and never by prefix or substring.
func normalizeAuditIdent(s string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(s))
}
