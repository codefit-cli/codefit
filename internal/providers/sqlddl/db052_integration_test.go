package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// DB-052's audit-timestamp rule, measured through the REAL parser over real DDL
// text. It belongs here rather than in dbrules' own tests for one reason that is
// the whole point of the rule: only the parser can put a TYPE and a NAME on the
// same column, and the rule reads both. A hand-built db.Column can assert
// whatever it likes about which of the two was read, and about which types the
// dialect actually produces for `TIMESTAMP(3)` or `BIGINT`.
//
// The DDL below is transcribed from the source schemas named in each case, not
// paraphrased.

// db052Tables returns the names DB-052 fired on, read from each item's
// "table: <name>" structural signal.
func db052Tables(t *testing.T, s *db.Schema) []string {
	t.Helper()
	_, surf := dbrules.Run(s)
	var out []string
	for _, it := range surf {
		if it.Category != string(surface.CategoryDBNoTimestamps) {
			continue
		}
		name := ""
		for _, sig := range it.StructuralSignals {
			if len(sig) > 7 && sig[:7] == "table: " {
				name = sig[7:]
			}
		}
		out = append(out, name)
	}
	return out
}

func assertDB052(t *testing.T, s *db.Schema, want ...string) {
	t.Helper()
	got := db052Tables(t, s)
	if len(got) != len(want) {
		t.Fatalf("DB-052 fired on %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("DB-052 fired on %v, want %v", got, want)
		}
	}
}

// The reporting case, transcribed verbatim from the pg_dump of the project
// that reported it: an append-only event table whose creation time is a
// double-quoted column literally named "timestamp". DB-052 listed that column
// in its own `columns:` signal and asked the question anyway.
//
// _user is in the same schema and in the same test on purpose: it has NO time
// column at all, so it is the half that proves the widening did not simply
// switch the rule off.
func TestDB052_PG_QuotedTimestampColumn(t *testing.T) {
	s := parseSQL(t, sqlddl.Postgres(), `
CREATE TABLE public.batch_log (
    id bigint NOT NULL,
    ec double precision,
    humidity double precision,
    notes character varying(2000),
    ph double precision,
    photo_url character varying(255),
    stage_at_time character varying(255),
    temperature double precision,
    "timestamp" timestamp(6) without time zone NOT NULL,
    batch_id bigint NOT NULL,
    type character varying(255)
);

CREATE TABLE public._user (
    id bigint NOT NULL,
    email character varying(255),
    firstname character varying(255),
    lastname character varying(255),
    password character varying(255),
    role character varying(255)
);
`)
	assertDB052(t, s, "_user")
}

// The type/name separation, and the reason this file lives next to the parser.
// event_log has a column TYPED timestamp and no column NAMED like an audit
// stamp — DB-052 must still fire on it. A rule that read the type instead of
// (or as well as) the name would go quiet here and silently stop auditing
// every table that stores any temporal value at all.
//
// stage_at_time is in the same table as a second trap: it normalizes to
// `stageattime`, which contains neither vocabulary entry but does contain
// "time".
func TestDB052_PG_TimestampIsATypeNotAName(t *testing.T) {
	s := parseSQL(t, sqlddl.Postgres(), `
CREATE TABLE public.event_log (
    id bigint NOT NULL,
    logged_value timestamp without time zone NOT NULL,
    expires_at timestamptz,
    stage_at_time character varying(255)
);
`)
	assertDB052(t, s, "event_log")
}

// Sakila/Pagila's spelling — 35 tables across the measured corpora, the single
// largest source of DB-052 false positives. Transcribed from
// testdata/mysql/sakila_excerpt.sql (actor) and pagila_excerpt.sql (category).
func TestDB052_MySQL_LastUpdate(t *testing.T) {
	s := parseSQL(t, sqlddl.MySQL(), `
CREATE TABLE actor (
  actor_id SMALLINT UNSIGNED NOT NULL AUTO_INCREMENT,
  first_name VARCHAR(45) NOT NULL,
  last_name VARCHAR(45) NOT NULL,
  last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (actor_id)
);

CREATE TABLE film_text (
  film_id SMALLINT NOT NULL,
  title VARCHAR(255) NOT NULL,
  description TEXT,
  PRIMARY KEY (film_id)
);
`)
	assertDB052(t, s, "film_text")
}

// AdventureWorks' spelling, in T-SQL, with the bracket-delimited types this
// dialect uses. Transcribed from testdata/tsql/adventureworks_excerpt.sql.
func TestDB052_TSQL_ModifiedDate(t *testing.T) {
	s := parseSQL(t, sqlddl.SQLServer(), `
CREATE TABLE Sales.Customer (
    CustomerID [int] NOT NULL PRIMARY KEY,
    AccountNumber [varchar](10) NOT NULL,
    ModifiedDate [datetime2] NOT NULL
);

CREATE TABLE Sales.SalesReason (
    SalesReasonID [int] NOT NULL PRIMARY KEY,
    Name [nvarchar](50) NOT NULL
);
`)
	assertDB052(t, s, "SalesReason")
}

// The other spellings the corpora produced, each on its own table so a single
// missing entry is named by the failure. dbgen_version is the ANTI-case in the
// same schema: tpcds spells its stamps dv_create_date / dv_create_time, and
// EQUALITY does not admit a prefixed name — admitting it would mean admitting
// every <anything>_create_date.
func TestDB052_PG_MeasuredSpellings(t *testing.T) {
	s := parseSQL(t, sqlddl.Postgres(), `
CREATE TABLE customer (
    customer_id integer NOT NULL,
    create_date date NOT NULL
);

CREATE TABLE debt_position_type_orgs (
    id bigint NOT NULL,
    creation_date timestamp with time zone,
    update_date timestamp with time zone
);

CREATE TABLE users (
    name text NOT NULL,
    creation_ts bigint
);

CREATE TABLE ui_auth_sessions (
    session_id text NOT NULL,
    creation_time bigint NOT NULL
);

CREATE TABLE local_media_repository (
    media_id text NOT NULL,
    created_ts bigint
);

CREATE TABLE event_txn_id (
    event_id text NOT NULL,
    inserted_ts bigint NOT NULL
);

CREATE TABLE device_lists_remote_resync (
    user_id text NOT NULL,
    added_ts bigint NOT NULL
);

CREATE TABLE user_threepids (
    user_id text NOT NULL,
    added_at bigint NOT NULL
);

CREATE TABLE dbgen_version (
    dv_version varchar(16),
    dv_create_date date,
    dv_create_time time
);
`)
	assertDB052(t, s, "dbgen_version")
}

// THE REDESIGN. Every table here stamps its rows under a spelling that appears
// in NO measured corpus, and the fixed list this rule replaced fired on all six
// — a false warning about a table whose audit trail is right there in the
// item's own `columns:` list. A verb plus a time affix closes the family the
// list could only enumerate.
//
// business_event is the anti-case in the same schema: `expires_at` is a time
// column with no creation verb, so the schema is not simply going quiet.
func TestDB052_PG_SiblingSpellingsNoCorpusHappenedToShow(t *testing.T) {
	s := parseSQL(t, sqlddl.Postgres(), `
CREATE TABLE stamped_created_on   (id bigint NOT NULL, created_on    timestamp NOT NULL);
CREATE TABLE stamped_date_created (id bigint NOT NULL, date_created  timestamp NOT NULL);
CREATE TABLE stamped_inserted_at  (id bigint NOT NULL, inserted_at   timestamp NOT NULL);
CREATE TABLE stamped_modified_at  (id bigint NOT NULL, modified_at   timestamp NOT NULL);
CREATE TABLE stamped_last_modified(id bigint NOT NULL, last_modified timestamp NOT NULL);
CREATE TABLE stamped_updated_ts   (id bigint NOT NULL, updated_ts    bigint NOT NULL);
CREATE TABLE business_event       (id bigint NOT NULL, expires_at    timestamp NOT NULL);
`)
	assertDB052(t, s, "business_event")
}

// THE ACCEPTANCE TEST for the redesign: a TIME SUFFIX ALONE never counts.
//
// Across the 29 measured corpora, 80 distinct columns end in `At` and 74 of them
// are business event times. A table whose only time column is `expires_at` or
// `started_at` genuinely does not record when its row came into being, so
// admitting the suffix would go quiet over a table that should still speak.
//
// Both tables are transcribed VERBATIM, and both fire DB-052 today in the
// measured corpora:
//   - "WorkflowRunLog" from formbricks migration 20260608120000, whose only time
//     columns are startedAt / finishedAt;
//   - "DataMigration" from formbricks migration 20241209051259, whose only time
//     columns are started_at / finished_at.
//
// connector_sync is NOT verbatim and says so: it is formbricks' own Connector
// column declarations (migration 20260414000000) with that table's created_at
// and updated_at removed, so `last_sync_at` and `created_by` are the only
// candidates left. `created_by` is the other half of the acceptance test — a
// creation verb on a real audit field that names a PERSON, not a time, which is
// why `_by` is not a time suffix.
func TestDB052_PG_TimeSuffixWithoutACreationVerb(t *testing.T) {
	s := parseSQL(t, sqlddl.Postgres(), `
CREATE TABLE "WorkflowRunLog" (
    "id" TEXT NOT NULL,
    "runId" TEXT NOT NULL,
    "sequence" INTEGER NOT NULL,
    "stepId" TEXT NOT NULL,
    "stepType" TEXT NOT NULL,
    "input" JSONB NOT NULL DEFAULT '{}',
    "output" JSONB NOT NULL DEFAULT '{}',
    "error" TEXT,
    "startedAt" TIMESTAMP(3),
    "finishedAt" TIMESTAMP(3),

    CONSTRAINT "WorkflowRunLog_pkey" PRIMARY KEY ("id")
);

CREATE TABLE "DataMigration" (
    "id" TEXT NOT NULL,
    "started_at" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "finished_at" TIMESTAMP(3),
    "name" TEXT NOT NULL,

    CONSTRAINT "DataMigration_pkey" PRIMARY KEY ("id")
);

CREATE TABLE connector_sync (
    "id" TEXT NOT NULL,
    "name" TEXT NOT NULL,
    "workspaceId" TEXT NOT NULL,
    "last_sync_at" TIMESTAMP(3),
    "created_by" TEXT,
    CONSTRAINT "connector_sync_pkey" PRIMARY KEY ("id")
);
`)
	assertDB052(t, s, "WorkflowRunLog", "DataMigration", "connector_sync")
}

// The epoch half of the type gate, and the reason it is not "must be a date
// type". synapse stores its stamps as milliseconds since the epoch in BIGINT
// columns, so a date-only gate would reject every one of them and reinstate the
// false positives this rule exists to remove.
//
// sticky_events is the anti-case in the same schema, transcribed verbatim from
// synapse delta 93/01_sticky_events.sql: its `expires_at BIGINT` is the SAME
// neutral type as `added_at BIGINT` on the table above it, and it still fires —
// which is the type gate proving it did not become the whole rule.
func TestDB052_PG_EpochIntegerStampsAndTheirLookalike(t *testing.T) {
	s := parseSQL(t, sqlddl.Postgres(), `
CREATE TABLE user_threepids (
    user_id text NOT NULL,
    added_at bigint NOT NULL
);

CREATE TABLE sticky_events (
  stream_id INTEGER NOT NULL PRIMARY KEY,
  instance_name TEXT NOT NULL,
  event_id TEXT NOT NULL,
  room_id TEXT NOT NULL,
  event_stream_ordering INTEGER NOT NULL UNIQUE,
  sender TEXT NOT NULL,
  expires_at BIGINT NOT NULL
);
`)
	assertDB052(t, s, "sticky_events")
}

// The other end of the type gate: the NAME says created_at and the TYPE is one
// no corpus ever produced for a stamp. The table keeps firing, and that is the
// deliberate direction — a false positive the agent can dismiss from the
// `columns:` list, rather than silence over a table with no audit trail.
//
// This is a real behavior change against both the fixed list and main, and the
// only one the redesign makes in the noisier direction. It moved nothing in the
// 29 measured corpora, where every name passing the verb rule is typed datetime
// or int, and it is locked here so the trade stays visible.
func TestDB052_PG_TypeGateKeepsAnUnmeasuredStampTypeVisible(t *testing.T) {
	s := parseSQL(t, sqlddl.Postgres(), `
CREATE TABLE stamp_as_text (
    id bigint NOT NULL,
    created_at character varying(64) NOT NULL
);

CREATE TABLE stamp_as_flag (
    id bigint NOT NULL,
    created_flag boolean NOT NULL
);

CREATE TABLE stamp_as_timestamp (
    id bigint NOT NULL,
    created_at timestamp with time zone NOT NULL
);
`)
	assertDB052(t, s, "stamp_as_text", "stamp_as_flag")
}
