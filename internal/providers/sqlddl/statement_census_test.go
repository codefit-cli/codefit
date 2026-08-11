package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// THE STATEMENT CENSUS (db.Schema.Sources).
//
// Every other carrier in the neutral model records a POSITION, and a position is
// precisely what a correctly-reduced no-op cannot produce. These tests lock the
// only evidence channel that can tell "codefit read this file and it added
// nothing, correctly" apart from "codefit read this file and understood none of
// it" — the distinction the unread-schema note asserts and, before this census,
// could not establish.
//
// The negative controls (A3) are the load-bearing half: they lock that the
// census NEVER over-accounts, because an over-accounted statement is a file
// wrongly declared benign, which is the original defect with a friendlier face.

// parseNamed parses named sources, so a census assertion can name the file it is
// about instead of depending on a positional helper's naming convention.
func parseNamed(t *testing.T, files map[string]string, order ...string) *db.Schema {
	t.Helper()
	srcs := make([]providers.SourceFile, 0, len(order))
	for _, name := range order {
		body, ok := files[name]
		if !ok {
			t.Fatalf("parseNamed: no body for %q", name)
		}
		srcs = append(srcs, providers.SourceFile{Path: name, Content: []byte(body)})
	}
	s, err := sqlddl.New().ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	return s
}

func census(t *testing.T, s *db.Schema, path string) db.SourceStatements {
	t.Helper()
	c, ok := s.Sources[path]
	if !ok {
		t.Fatalf("no statement census for %q; have %v", path, censusPaths(s))
	}
	return c
}

func censusPaths(s *db.Schema) []string {
	var out []string
	for p := range s.Sources {
		out = append(out, p)
	}
	return out
}

// A1 — an idempotent re-declaration is ACCOUNTED FOR, not lost.
//
// V2 is the shape a Flyway migration set produces constantly: a later migration
// that re-adds columns an earlier one already declared, guarded by IF NOT
// EXISTS. Every statement in it is read and reduced correctly and contributes no
// Pos, which is why the absence of a Pos cannot mean blindness.
func TestSQLDDL_IdempotentAdd_IsAccountedAsAlreadySatisfied(t *testing.T) {
	s := parseNamed(t, map[string]string{
		"V1__initial.sql": `CREATE TABLE _user (
			id bigserial PRIMARY KEY,
			email text NOT NULL,
			license_status text
		);`,
		"V2__license.sql": `ALTER TABLE _user ADD COLUMN IF NOT EXISTS license_status text;
ALTER TABLE _user ADD COLUMN IF NOT EXISTS email text;`,
	}, "V1__initial.sql", "V2__license.sql")

	got := census(t, s, "V2__license.sql")
	if got.Total != 2 {
		t.Errorf("Total = %d, want 2", got.Total)
	}
	if n := got.Accounted[db.OutcomeAlreadySatisfied]; n != 2 {
		t.Errorf("Accounted[already-satisfied] = %d, want 2", n)
	}
	if got.Unaccounted() != 0 {
		t.Errorf("Unaccounted() = %d, want 0 — every statement in V2 was explained", got.Unaccounted())
	}
}

// A1b — a repeated CREATE TABLE IF NOT EXISTS is the other idempotent shape the
// reducer already recognizes and silently returns from (reduceCreateTable's
// "AJUSTE 1" branch).
func TestSQLDDL_RepeatedCreateTableIfNotExists_IsAccountedAsAlreadySatisfied(t *testing.T) {
	s := parseNamed(t, map[string]string{
		"V1__initial.sql": `CREATE TABLE _user (id bigserial PRIMARY KEY);`,
		"V2__again.sql":   `CREATE TABLE IF NOT EXISTS _user (id bigserial PRIMARY KEY);`,
	}, "V1__initial.sql", "V2__again.sql")

	got := census(t, s, "V2__again.sql")
	if got.Total != 1 || got.Accounted[db.OutcomeAlreadySatisfied] != 1 {
		t.Errorf("census = %+v, want Total 1 / already-satisfied 1", got)
	}
}

// A2 — a pure DML/permission file DECLARES NO SCHEMA, and says so positively.
//
// The outcome must come from a branch that RECOGNIZED each statement head. It
// must never be inferred from the dispatch's residual default branch, which also
// swallows forms nobody has written a branch for (A3 locks that direction).
func TestSQLDDL_PureDMLAndPermissions_AreAccountedAsDeclaringNoSchema(t *testing.T) {
	s := parseNamed(t, map[string]string{
		"V1__initial.sql": `CREATE TABLE role (id bigserial PRIMARY KEY, name text);`,
		"V3__seed.sql": `INSERT INTO role (name) VALUES ('admin');
INSERT INTO role (name) VALUES ('staff');
UPDATE role SET name = 'ADMIN' WHERE name = 'admin';
DELETE FROM role WHERE name = 'staff';
TRUNCATE TABLE role;
GRANT SELECT ON role TO reader;
REVOKE SELECT ON role FROM reader;`,
	}, "V1__initial.sql", "V3__seed.sql")

	got := census(t, s, "V3__seed.sql")
	if got.Total != 7 {
		t.Errorf("Total = %d, want 7", got.Total)
	}
	if n := got.Accounted[db.OutcomeDeclaresNoSchema]; n != 7 {
		t.Errorf("Accounted[declares-no-schema] = %d, want 7", n)
	}
	if got.Unaccounted() != 0 {
		t.Errorf("Unaccounted() = %d, want 0", got.Unaccounted())
	}
}

// A2b — MERGE is in the DML family too, and is spelled by T-SQL and PostgreSQL
// alike.
func TestSQLDDL_Merge_IsAccountedAsDeclaringNoSchema(t *testing.T) {
	s := parseNamed(t, map[string]string{
		"V1__m.sql": "MERGE INTO role AS t USING staging AS s ON t.id = s.id WHEN MATCHED THEN UPDATE SET name = s.name;",
	}, "V1__m.sql")

	got := census(t, s, "V1__m.sql")
	if got.Total != 1 || got.Accounted[db.OutcomeDeclaresNoSchema] != 1 {
		t.Errorf("census = %+v, want Total 1 / declares-no-schema 1", got)
	}
}

// A3 — THE NEGATIVE CONTROL, and the reason the whole census is safe.
//
// `ALTER COLUMN … TYPE` is a RECOGNIZED skip the model does not carry, and
// `CREATE DOMAIN` is a form no branch reads at all. Neither may be accounted:
// both must stay UNACCOUNTED so a file made only of them keeps being reported as
// blindness. If a future edit ever reads the dispatch's residual `default:`
// branch as "declares no schema", this test is what fails.
func TestSQLDDL_UnmodelledAndUnknownForms_AreNeverAccounted(t *testing.T) {
	cases := []struct {
		name string
		sql  string
	}{
		{"alter column type is recognized but not modelled", `ALTER TABLE _user ALTER COLUMN email TYPE varchar(320);`},
		{"create domain is a form no branch reads", `CREATE DOMAIN email_addr AS text;`},
		{"comment on is a form no branch reads", `COMMENT ON TABLE _user IS 'people';`},
		{"pragma is a form no branch reads", `PRAGMA foreign_keys = ON;`},
		{"drop table declares nothing but is not benign", `DROP TABLE _user;`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := parseNamed(t, map[string]string{
				"V1__initial.sql": `CREATE TABLE _user (id bigserial PRIMARY KEY, email text);`,
				"V9__x.sql":       tc.sql,
			}, "V1__initial.sql", "V9__x.sql")

			got := census(t, s, "V9__x.sql")
			if got.Total != 1 {
				t.Fatalf("Total = %d, want 1", got.Total)
			}
			if n := got.AccountedTotal(); n != 0 {
				t.Errorf("AccountedTotal() = %d (%v), want 0 — this form must stay unaccounted so the file keeps reporting as blindness", n, got.Accounted)
			}
			if got.Unaccounted() != 1 {
				t.Errorf("Unaccounted() = %d, want 1", got.Unaccounted())
			}
		})
	}
}

// A4 — accounting is PER STATEMENT, never per item.
//
// `ADD COLUMN IF NOT EXISTS a, DROP COLUMN c` is ONE statement with two parts.
// The add is idempotent; the drop is a real structural change. Accounting the
// add alone would report a column drop as "already satisfied" — a file that
// really did change the schema, declared benign.
func TestSQLDDL_MixedAlterStatement_IsNotAccountedAsSatisfied(t *testing.T) {
	s := parseNamed(t, map[string]string{
		"V1__initial.sql": `CREATE TABLE _user (id bigserial PRIMARY KEY, email text, nickname text);`,
		"V2__mixed.sql":   `ALTER TABLE _user ADD COLUMN IF NOT EXISTS email text, DROP COLUMN nickname;`,
	}, "V1__initial.sql", "V2__mixed.sql")

	got := census(t, s, "V2__mixed.sql")
	if got.Total != 1 {
		t.Fatalf("Total = %d, want 1 — this is ONE statement, not two", got.Total)
	}
	if n := got.AccountedTotal(); n != 0 {
		t.Errorf("AccountedTotal() = %d (%v), want 0 — one part of this statement really dropped a column", n, got.Accounted)
	}
	// And the drop must genuinely have happened, or the assertion above passes
	// for the wrong reason.
	if hasCol(table(t, s, "_user"), "nickname") {
		t.Error("nickname column survived: the fixture does not exercise a real drop, so the accounting assertion is vacuous")
	}
}

// A4b — the all-satisfied multi-part statement is the mirror control: when EVERY
// part is idempotent the statement IS accounted, once.
func TestSQLDDL_AllPartsIdempotent_IsAccountedOncePerStatement(t *testing.T) {
	s := parseNamed(t, map[string]string{
		"V1__initial.sql": `CREATE TABLE _user (id bigserial PRIMARY KEY, email text, nickname text);`,
		"V2__both.sql":    `ALTER TABLE _user ADD COLUMN IF NOT EXISTS email text, ADD COLUMN IF NOT EXISTS nickname text;`,
	}, "V1__initial.sql", "V2__both.sql")

	got := census(t, s, "V2__both.sql")
	if got.Total != 1 {
		t.Fatalf("Total = %d, want 1", got.Total)
	}
	if n := got.Accounted[db.OutcomeAlreadySatisfied]; n != 1 {
		t.Errorf("Accounted[already-satisfied] = %d, want 1 — one entry per STATEMENT, not per item", n)
	}
}

// A5 — the census counts statements per SOURCE, and a contributing file is
// counted like any other: the census is a measurement, not a filter.
func TestSQLDDL_ContributingFile_IsAlsoCensused(t *testing.T) {
	s := parseNamed(t, map[string]string{
		"V1__initial.sql": `CREATE TABLE a (id int);
CREATE TABLE b (id int);`,
	}, "V1__initial.sql")

	got := census(t, s, "V1__initial.sql")
	if got.Total != 2 {
		t.Errorf("Total = %d, want 2", got.Total)
	}
	if got.AccountedTotal() != 0 {
		t.Errorf("AccountedTotal() = %d, want 0 — these statements are in the schema, not accounted away", got.AccountedTotal())
	}
}
