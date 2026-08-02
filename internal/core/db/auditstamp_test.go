package db

import "testing"

// The vocabulary is MEASURED, and these tables are the measurement written
// down. Every name asserted true below was read off a real corpus by the real
// parser (the corpus and table are named in each case), and every name asserted
// false is a real column from a table DB-052 fires on — i.e. a name the rule
// has already seen and must keep NOT treating as an audit stamp.
//
// The two halves are not decoration of each other. The first locks that the
// vocabulary is wide enough to stop lying about tables that do stamp their
// rows; the second locks that it stays an EQUALITY test over the normalized
// name — a substring test would swallow creator, update_trace_id and
// commission_created, all of which are real columns in the measured corpora.

func TestIsAuditTimestampName_RecognizedVocabulary(t *testing.T) {
	tests := []struct {
		name     string
		evidence string
	}{
		// Pre-existing pair, kept and re-measured.
		{"created_at", "dub, formbricks (Prisma corpora): 107 tables"},
		{"createdAt", "same convention, camelCase spelling"},
		{"updated_at", "dub, formbricks: 83 tables"},
		{"updatedAt", "same convention, camelCase spelling"},

		// The column that started this: an append-only event table whose
		// creation time is spelled `timestamp`.
		{"timestamp", "plantalinda batch_log/inventory_movement; synapse monthly_active_users/user_daily_visits"},

		// Creation, other spellings.
		{"create_date", "pagila customer, sakila-full customer, dw-ngthao dim_customers"},
		{"creation_date", "dw-p4pa debt_position_type_orgs + 2 more"},
		{"creation_time", "synapse ui_auth_sessions"},
		{"creation_ts", "synapse users"},
		{"created_ts", "synapse local_media_repository, remote_media_cache"},
		{"inserted_ts", "synapse event_txn_id"},
		{"added_ts", "synapse device_lists_remote_resync"},
		{"added_at", "synapse user_threepids"},

		// Modification.
		{"update_date", "dw-p4pa payment_assessment_detail + 2 more"},
		{"last_update", "pagila + sakila-full: 35 tables"},
		{"ModifiedDate", "AdventureWorks Customer (vendored tsql excerpt)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !IsAuditTimestampName(tc.name) {
				t.Errorf("IsAuditTimestampName(%q) = false, want true — evidence: %s", tc.name, tc.evidence)
			}
		})
	}
}

// TestIsAuditTimestampName_RejectsNonStamps is the anti-overmatch lock, and
// every entry is a REAL column name from a table DB-052 fires on in the
// measured corpora (or, for the type names at the end, a real SQL type
// spelling). normalizeAuditIdent drops separators, so `update_trace_id` becomes
// `updatetraceid`: a substring or prefix test over the same vocabulary admits
// all of these, silences the tables that carry them, and turns a noisy rule
// into a lying one.
func TestIsAuditTimestampName_RejectsNonStamps(t *testing.T) {
	names := []string{
		// Names containing a stamp verb but naming something else.
		"creator",                      // dub, formbricks
		"update_trace_id",              // dw-p4pa
		"update_operator_external_id",  // dw-p4pa
		"insertion_prev_event_id",      // synapse
		"domain_configuration_updates", // synapse
		"commission_created",           // dw-barousse
		"ts_added_ms",                  // synapse
		"wp_creation_date_sk",          // dw-p4pa
		"last_federation_update_ts",    // synapse
		"dv_create_date",               // tpcds dbgen_version
		"dv_create_time",               // tpcds dbgen_version

		// Business dates: a table full of these still has no audit stamp.
		"payment_date",   // pagila payment
		"start_date",     // multiple
		"end_date",       // multiple
		"birth_date",     // employees
		"order_date",     // northwind
		"hire_date",      // employees
		"stage_at_time",  // plantalinda batch_log
		"processed_time", // dw-barousse

		// TYPE spellings. `timestamp` is in the vocabulary as a COLUMN name;
		// none of its type-name relatives may sneak in with it.
		"timestamptz",
		"datetime",
		"datetime2",
		"smalldatetime",
		"date",
		"time",
	}
	for _, n := range names {
		t.Run(n, func(t *testing.T) {
			if IsAuditTimestampName(n) {
				t.Errorf("IsAuditTimestampName(%q) = true, want false — this is a business column, not an audit stamp", n)
			}
		})
	}
}

// TestIsAuditTimestampName_NormalizesSeparatorsAndCase locks the normalization
// itself, independently of which names are in the vocabulary: the same word in
// snake_case, kebab-case, camelCase and PascalCase is ONE name.
func TestIsAuditTimestampName_NormalizesSeparatorsAndCase(t *testing.T) {
	for _, n := range []string{"last_update", "last-update", "lastUpdate", "LastUpdate", "LAST_UPDATE"} {
		if !IsAuditTimestampName(n) {
			t.Errorf("IsAuditTimestampName(%q) = false, want true (same name, different spelling)", n)
		}
	}
	if IsAuditTimestampName("") {
		t.Error(`IsAuditTimestampName("") = true, want false`)
	}
}
