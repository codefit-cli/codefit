package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// DB-052's audit-timestamp vocabulary, measured through the REAL parser over
// real DDL text. It belongs here rather than in dbrules' own tests for one
// reason that is the whole point of the widening: only the parser can put a
// TYPE and a NAME on the same column, and `timestamp` is both. A hand-built
// db.Column can assert whatever it likes about which of the two the rule read.
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
