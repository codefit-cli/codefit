package db

import "testing"

// The rule is VERB + TIME AFFIX + TYPE, and these tables are what each of the
// three parts is for. Every name asserted true was either read off a real
// corpus by the real parser (the corpus and table are named) or is the
// morphological sibling of one — the whole point of moving off a fixed list is
// that a sibling no longer has to wait for a corpus to show it. Every name
// asserted false is a real column from a table DB-052 fires on, i.e. a name the
// rule has already seen and must keep NOT treating as an audit stamp.
//
// The halves are not decoration of each other. The first locks that the rule is
// wide enough to stop lying about tables that do stamp their rows; the second
// locks that a TIME AFFIX ALONE never earns the answer — across the 29 measured
// corpora, 74 of the 80 distinct columns ending in `At` are business event
// times (expiresAt, startedAt, paidAt, bannedAt, …), so a suffix-only rule
// would silence tables that genuinely record nothing about their own creation.
//
// DECLARED ORNAMENTS. 21 mutations were run against this rule and its two
// consumers; 168 of the 175 cases in this file, the gate's equivalence test and
// db052_integration_test.go were killed by at least one of them. Four cases here
// were killed by NONE, and are documentation of the corpora rather than locks:
// `creator` / `creatorId` (their stem `creat` is not a verb in any form the rule
// knows, so no widening of the verb set reaches them), `modifiers` and
// `registeredDomains` (same, and neither carries a time affix). The empty name
// in TestIsAuditTimestampColumn_TypeNamesAreNotColumnNames is the fifth: it is a
// defensive case no mutation of this rule can turn true. They are kept because
// they name real columns of tables DB-052 fires on, and declared because a
// reader must not count them as protection.

// stamp is the type every measured audit stamp in the corpora actually carries
// in the neutral model, so a name-focused case is not accidentally decided by
// the type gate. The type gate has its own tests below.
func stamp(name string) Column { return Column{Name: name, Type: TypeDateTime} }

func TestIsAuditTimestampColumn_VerbPlusTimeAffix(t *testing.T) {
	tests := []struct {
		name     string
		evidence string
	}{
		// MEASURED — read off a real corpus, on a table DB-052 was firing on.
		{"created_at", "dub, formbricks (Prisma corpora): 107 tables"},
		{"createdAt", "same convention, camelCase spelling"},
		{"updated_at", "dub, formbricks: 83 tables"},
		{"updatedAt", "same convention, camelCase spelling"},
		{"create_date", "pagila customer, sakila-full customer, dw-ngthao dim_customers"},
		{"creation_date", "dw-p4pa debt_position_type_orgs + 2 more"},
		{"creation_time", "synapse ui_auth_sessions"},
		{"creation_ts", "synapse users"},
		{"created_ts", "synapse local_media_repository, remote_media_cache"},
		{"inserted_ts", "synapse event_txn_id"},
		{"added_ts", "synapse device_lists_remote_resync"},
		{"added_at", "synapse user_threepids"},
		{"update_date", "dw-p4pa payment_assessment_detail + 2 more"},
		{"last_update", "pagila + sakila-full: 36 columns"},
		{"ModifiedDate", "AdventureWorks Customer (vendored tsql excerpt)"},
		{"modifiedAt", "dub jackson_store"},

		// The column that started the previous change: an append-only event
		// table whose creation time is spelled `timestamp`. It carries NO verb,
		// so it cannot be reached by the verb rule and stays an explicit entry.
		{"timestamp", "plantalinda batch_log/inventory_movement; synapse monthly_active_users"},

		// SIBLINGS. Not in any of the 29 corpora, and admitted anyway — this is
		// the redesign. A fixed list can only ever know the spellings that
		// happened to be measured, so every project using one of these got a
		// false warning about a table that plainly stamps its rows.
		{"created_on", "creation verb + the other English time preposition"},
		{"date_created", "same words, time word first"},
		{"inserted_at", "measured verb `inserted` (synapse inserted_ts), other suffix"},
		{"modified_at", "measured verb `modified` (ModifiedDate), other suffix"},
		{"last_modified", "measured `last` prefix (last_update) + measured verb"},
		{"updated_ts", "measured verb + measured suffix, combination unmeasured"},
		{"date_modified", "time word first"},
		{"insertion_time", "morphological closure of `creation`/`insertion`"},
		{"changed_at", "modification verb"},
		{"change_date", "modification verb"},
		{"added_on", "creation verb"},
		{"add_time", "creation verb, bare stem"},
		{"createdDateTime", "camel spelling, `datetime` suffix"},
		{"last_updated_at", "prefix AND suffix around a verb"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !IsAuditTimestampColumn(stamp(tc.name)) {
				t.Errorf("IsAuditTimestampColumn(%q) = false, want true — %s", tc.name, tc.evidence)
			}
		})
	}
}

// TestIsAuditTimestampColumn_TimeSuffixAloneIsNotEnough is the ACCEPTANCE test
// for this redesign. Every name here ends in a time affix and carries a
// perfectly good date/time type, and NOT ONE of them says when the row was
// created or changed: they are business event times. A table whose only time
// column is `expires_at` genuinely does not record when its row came into
// being, so admitting the suffix alone would go quiet over a table that should
// still speak — the error that hides.
//
// The four the redesign was specified against are measured, on tables DB-052
// fires on today: dub jackson_ttl (expiresAt BigInt), dub Account (expires_at),
// formbricks DataMigration and WorkflowRunLog (started_at + finished_at),
// formbricks Connector (last_sync_at).
func TestIsAuditTimestampColumn_TimeSuffixAloneIsNotEnough(t *testing.T) {
	names := []string{
		"expires_at",   // synapse sticky_events, formbricks PasswordResetToken
		"expiresAt",    // dub jackson_ttl, dub Link, formbricks Invite
		"started_at",   // formbricks DataMigration
		"startedAt",    // dub ProgramApplicationEvent, formbricks WorkflowRunLog
		"finished_at",  // formbricks DataMigration
		"finishedAt",   // formbricks WorkflowRunLog
		"last_sync_at", // formbricks Connector
		"lastSyncAt",   // formbricks FeedbackSource
		"read_at",      // synapse
		"validated_at", // synapse
		"paidAt",       // dub
		"clickedAt",    // dub
		"bannedAt",     // dub
		"publishedAt",  // formbricks
		"sentAt",       // formbricks
		"lastLoginAt",  // formbricks — `last` prefix, still no creation verb
		"trialEndsAt",  // dub
		"payment_date", // pagila payment
		"start_date",   // multiple
		"end_date",     // multiple
		"birth_date",   // employees
		"order_date",   // northwind
		"hire_date",    // employees
		"processed_time",
		"stage_at_time", // plantalinda batch_log — normalizes to `stageattime`

		// The three verbs considered for the creation set and REJECTED, locked
		// here so the rejection is a control and not a comment. No corpus shows
		// any of these with a time suffix; each is a business event or a
		// payload word, not the row's own lifecycle:
		//   register — when the USER registered, the same category as
		//              started_at. dub really declares `registeredDomains`.
		//   renew    — synapse declares `last_renewed_ts`, dub declares
		//              `autoRenewalDisabledAt`; neither is about the row.
		//   new      — an audit-log table's payload is `old_value`/`new_value`.
		"registered_at",
		"registeredAt",
		"registration_date",
		"renewed_at",
		"renewal_date",
		"new_date",
		"newDate",
	}
	for _, n := range names {
		t.Run(n, func(t *testing.T) {
			if IsAuditTimestampColumn(stamp(n)) {
				t.Errorf("IsAuditTimestampColumn(%q) = true, want false — "+
					"a time affix with no creation/modification verb is a business event time, not an audit stamp", n)
			}
		})
	}
}

// TestIsAuditTimestampColumn_ByIsNotATimeSuffix locks the suffix in the OTHER
// direction. `created_by` is a creation verb on a genuine audit field, and it
// is a PERSON. DB-052 asks whether the table records WHEN, so `created_by`
// must not answer it — and `_by` must not be reachable as a time affix.
//
// Measured: formbricks declares `created_by TEXT` beside `created_at` and
// `last_sync_at` in its Connector migration, and `createdBy String` on ApiKey,
// Chart, Dashboard, FeedbackSource, Survey and Workflow.
func TestIsAuditTimestampColumn_ByIsNotATimeSuffix(t *testing.T) {
	for _, n := range []string{"created_by", "createdBy", "updated_by", "updatedBy", "modified_by", "last_modified_by"} {
		t.Run(n, func(t *testing.T) {
			// Typed as the string it really is, AND as a datetime, so the
			// rejection is proven to come from the NAME and not from the type
			// gate covering for it.
			if IsAuditTimestampColumn(Column{Name: n, Type: TypeString}) {
				t.Errorf("IsAuditTimestampColumn(%q, string) = true, want false — that column is a PERSON", n)
			}
			if IsAuditTimestampColumn(stamp(n)) {
				t.Errorf("IsAuditTimestampColumn(%q, datetime) = true, want false — `_by` is not a time suffix", n)
			}
		})
	}
}

// TestIsAuditTimestampColumn_VerbAloneIsNotEnough: a verb has to be ATTACHED to
// a time word. Every name here contains a creation or modification verb and
// names something else, and every one is a real column of a table DB-052 fires
// on in the measured corpora. A prefix, substring or stem test over the same
// verbs admits all of them and silences those tables.
func TestIsAuditTimestampColumn_VerbAloneIsNotEnough(t *testing.T) {
	names := []string{
		"creator",                      // dub, formbricks
		"creatorId",                    // formbricks Invite
		"update_trace_id",              // dw-p4pa
		"update_operator_external_id",  // dw-p4pa
		"insertion_prev_event_id",      // synapse
		"domain_configuration_updates", // synapse
		"commission_created",           // dw-barousse
		"ts_added_ms",                  // synapse — `ts` leads, `ms` trails
		"wp_creation_date_sk",          // dw-p4pa — a date KEY, not a date
		"last_federation_update_ts",    // synapse — `last`+`ts` around a NOUN
		"last_renewed_ts",              // synapse — renewal is not a row verb
		"dv_create_date",               // tpcds dbgen_version
		"dv_create_time",               // tpcds dbgen_version
		"cst_create_date",              // dw-p4pa
		"dwh_create_date",              // dw-ngthao
		"addedToMarketplaceAt",         // dub — verb, but not the row's creation
		"autoRenewalDisabledAt",        // dub
		"changeSet",                    // synapse
		"changeHistoryLog",             // synapse
		"modifiers",                    // synapse
		"registeredDomains",            // dub
		"created",                      // bare verb: says WHETHER, not WHEN
		"updated",
		"modified",
	}
	for _, n := range names {
		t.Run(n, func(t *testing.T) {
			if IsAuditTimestampColumn(stamp(n)) {
				t.Errorf("IsAuditTimestampColumn(%q) = true, want false — "+
					"a verb only counts when a time word is attached to it", n)
			}
		})
	}
}

// TestIsAuditTimestampColumn_TypeNamesAreNotColumnNames: `timestamp` is in the
// vocabulary as a COLUMN name and is also a SQL TYPE name. None of its
// type-name relatives may sneak in with it.
func TestIsAuditTimestampColumn_TypeNamesAreNotColumnNames(t *testing.T) {
	for _, n := range []string{"timestamptz", "datetime", "datetime2", "smalldatetime", "date", "time", "ts", "last", ""} {
		t.Run(n, func(t *testing.T) {
			if IsAuditTimestampColumn(stamp(n)) {
				t.Errorf("IsAuditTimestampColumn(%q) = true, want false", n)
			}
		})
	}
}

// TestIsAuditTimestampColumn_TypeGate is the third part of the rule, and the
// measurement behind it is narrow and unanimous: across the 29 corpora plus the
// reporting project, EVERY column whose name passes the verb rule is typed
// either datetime (258 occurrences) or int (9 occurrences) — and all nine ints
// are synapse epoch stamps declared `bigint` (creation_ts, created_ts ×2,
// inserted_ts, added_ts, added_at, creation_time). A strict "must be a date
// type" gate would wrongly reject every one of those.
//
// Nothing else is admitted, and the direction matters: rejecting a type keeps
// the table FIRING, which is the visible error the agent can dismiss from the
// item's own `columns:` list. Admitting one silences a table, which is the
// error nobody sees. Zero corpora show a stamp typed string, text, float, bool,
// json, enum, bytes or unknown, so none of them is admitted on a hunch.
func TestIsAuditTimestampColumn_TypeGate(t *testing.T) {
	admitted := []struct {
		typ      Type
		evidence string
	}{
		{TypeDateTime, "258 measured occurrences — created_at, last_update, ModifiedDate, …"},
		{TypeInt, "9 measured occurrences, all synapse `bigint` epoch stamps"},
	}
	for _, tc := range admitted {
		t.Run("admit_"+string(tc.typ), func(t *testing.T) {
			if !IsAuditTimestampColumn(Column{Name: "created_at", Type: tc.typ}) {
				t.Errorf("created_at typed %s = false, want true — %s", tc.typ, tc.evidence)
			}
		})
	}

	rejected := []struct {
		typ    Type
		reason string
	}{
		{TypeBool, "a created_flag says WHETHER, not WHEN — dw-barousse really declares commission_created BOOLEAN"},
		{TypeString, "unmeasured; a stamp is not stored as a name"},
		{TypeText, "unmeasured"},
		{TypeFloat, "unmeasured"},
		{TypeJSON, "unmeasured"},
		{TypeEnum, "unmeasured"},
		{TypeBytes, "unmeasured"},
		{TypeUnknown, "the parser's honest fallback — an unclassified type is not evidence of a time"},
		{Type(""), "the zero value: a column the model never typed testifies to nothing"},
	}
	for _, tc := range rejected {
		t.Run("reject_"+string(tc.typ), func(t *testing.T) {
			if IsAuditTimestampColumn(Column{Name: "created_at", Type: tc.typ}) {
				t.Errorf("created_at typed %q = true, want false — %s", tc.typ, tc.reason)
			}
		})
	}
}

// TestIsAuditTimestampColumn_NormalizesSeparatorsAndCase locks the
// normalization itself, independently of the vocabulary: the same words in
// snake_case, kebab-case, camelCase and PascalCase are ONE name.
func TestIsAuditTimestampColumn_NormalizesSeparatorsAndCase(t *testing.T) {
	for _, n := range []string{"last_update", "last-update", "lastUpdate", "LastUpdate", "LAST_UPDATE"} {
		if !IsAuditTimestampColumn(stamp(n)) {
			t.Errorf("IsAuditTimestampColumn(%q) = false, want true (same name, different spelling)", n)
		}
	}
	for _, n := range []string{"expires_at", "expiresAt", "EXPIRES_AT", "Expires-At"} {
		if IsAuditTimestampColumn(stamp(n)) {
			t.Errorf("IsAuditTimestampColumn(%q) = true, want false (same name, different spelling)", n)
		}
	}
}
