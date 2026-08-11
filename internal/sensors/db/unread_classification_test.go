package db_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
	sdb "github.com/codefit-cli/codefit/internal/sensors/db"
)

// THE DEFECT THIS FILE CLOSES.
//
// The unread-schema floor asks the neutral model one question: "does any Pos
// name this file?". For a file codefit could not read, the answer is no — which
// is why the floor works at all. But the answer is ALSO no for a file codefit
// read PERFECTLY:
//
//	a migration whose every statement is `ADD COLUMN IF NOT EXISTS <col>` on a
//	column an earlier migration already declares, and
//	a migration that is pure INSERT/GRANT.
//
// Both are reduced correctly. Neither can leave a Pos, by definition. So the
// floor inferred blindness from an absence with innocent causes and told the
// agent "codefit read NOTHING from this file — whatever it declares, codefit did
// not see it" about a file it had read and resolved completely. Measured on a
// real project: 13 of 45 migrations named under that sentence.
//
// Every test here runs the REAL sensor over the REAL committed corpus in
// testdata/migrations_traceless (see its README entry). A hand-built db.Schema
// cannot exhibit this defect at all: it lives in what the reducer discards.

// tracelessCorpus copies the committed migration corpus into a temp project and
// configures the DIRECTORY as the single schema path — the Flyway shape the
// defect was measured on.
func tracelessCorpus(t *testing.T) auditctx.AuditContext {
	t.Helper()
	root := t.TempDir()
	dst := filepath.Join(root, "migrations")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join("testdata", "migrations_traceless"))
	if err != nil {
		t.Fatalf("reading the committed corpus: %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("the corpus has %d files, want 5 — this test asserts against its CONTENT, so a changed corpus must fail loudly", len(entries))
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join("testdata", "migrations_traceless", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return writeCfg(t, root, []string{"migrations"})
}

func auditTraceless(t *testing.T, ctx auditctx.AuditContext) sdb.Result {
	t.Helper()
	res, err := sdb.New(sqlddl.New()).Audit(ctx)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	return res
}

// blindClause returns the part of the note that names blind files — everything
// from the "read NOTHING" sentence to its terminating period — so a test can
// assert that a file is NOT in it without being fooled by the file appearing in
// one of the benign sentences instead.
func blindClause(note string) string {
	i := strings.Index(note, "read NOTHING")
	if i < 0 {
		return ""
	}
	rest := note[i:]
	if j := strings.Index(rest, "codefit did not see it."); j >= 0 {
		return rest[:j]
	}
	return rest
}

// B1 — a correct no-op is NOT reported as blindness.
func TestSensorDB_ResolvedNoOp_IsNotReportedAsBlindness(t *testing.T) {
	res := auditTraceless(t, tracelessCorpus(t))

	if strings.Contains(blindClause(res.Note), "V2__user_license_fields.sql") {
		t.Errorf("a file whose every statement was already satisfied is named under the blindness reason:\n%s", res.Note)
	}
	if !strings.Contains(res.Note, "V2__user_license_fields.sql") {
		t.Errorf("the no-op file is not reported at all — it must be recorded, just not as a defect:\n%s", res.Note)
	}
	if !strings.Contains(res.Note, "already declares") {
		t.Errorf("the note does not state WHY the no-op file left no trace:\n%s", res.Note)
	}
}

// B2 — the three states stay distinct, and blindness keeps its scope.
//
// This is the end-to-end lock on the reducer's prohibition: V5 (CREATE DOMAIN)
// reaches the same residual dispatch branch every DML statement used to reach.
// If "declares no schema" were ever inferred from that branch instead of from a
// positive head match, V5 would be reported as benign here.
func TestSensorDB_TracelessCorpus_ThreeStatesStayDistinct(t *testing.T) {
	res := auditTraceless(t, tracelessCorpus(t))
	blind := blindClause(res.Note)

	for _, f := range []string{"V4__widen_email.sql", "V5__unknown_form.sql"} {
		if !strings.Contains(blind, f) {
			t.Errorf("%s is NOT under the blindness reason, but codefit really did not see what it declares:\n%s", f, res.Note)
		}
	}
	if strings.Contains(blind, "V3__seed_roles.sql") {
		t.Errorf("a pure data/permissions file is named under the blindness reason:\n%s", res.Note)
	}
	if !strings.Contains(res.Note, "V3__seed_roles.sql") || !strings.Contains(res.Note, "data or permissions") {
		t.Errorf("the pure-DML file is not reported with its own non-alarming fact:\n%s", res.Note)
	}
	if strings.Contains(res.Note, "V1__initial_schema.sql") {
		t.Errorf("the CONTRIBUTING file is named in the unread note at all:\n%s", res.Note)
	}
	if !res.Measured {
		t.Errorf("Measured = false although V1 contributed two tables; note:\n%s", res.Note)
	}
}

// B3 — a scan whose EVERY configured source is traceless is not a measurement,
// whatever the reason.
//
// Without this the fix would introduce the regression it exists to prevent: once
// states (b) and (c) stop being defects, a `schema_paths` glob that happens to
// match only seed files reports Measured=true, score 100, zero tables — which is
// byte-for-byte the shape of a clean audit. Invariant I2: not measured is never
// clean.
func TestSensorDB_EveryConfiguredSourceTraceless_IsNotMeasured(t *testing.T) {
	cases := map[string]map[string][]byte{
		"only data and permissions": {
			"seed.sql": []byte("INSERT INTO role (name) VALUES ('admin');\nGRANT SELECT ON role TO reader;\n"),
		},
		"only comments": {
			"notes.sql": []byte("-- the first migration lands next sprint\n"),
		},
		// Mixed benign REASONS, still nothing contributed: the predicate is
		// "every configured source is traceless", not "every source is traceless
		// for the same reason".
		"data in one file, comments in the other": {
			"seed.sql":  []byte("INSERT INTO role (name) VALUES ('admin');\n"),
			"notes.sql": []byte("/* staged for review */\n"),
		},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			order := make([]string, 0, len(files))
			for f := range files {
				order = append(order, f)
			}
			res := auditTraceless(t, inlineProject(t, order, files))
			if res.Measured {
				t.Errorf("Measured = true over a scan in which NO configured source contributed anything — score would read as a clean audit; note:\n%s", res.Note)
			}
			if res.Note == "" {
				t.Error("not measured and silent about it")
			}
		})
	}
}

// B3b — the mirror control: ONE contributing source is enough to keep the scan a
// measurement. A partial failure must not throw away real findings.
func TestSensorDB_OneContributingSourceAmongTraceless_StaysMeasured(t *testing.T) {
	res := auditTraceless(t, inlineProject(t,
		[]string{"schema.sql", "seed.sql"},
		map[string][]byte{
			"schema.sql": []byte("CREATE TABLE role (id bigserial PRIMARY KEY, name text);\n"),
			"seed.sql":   []byte("INSERT INTO role (name) VALUES ('admin');\n"),
		},
	))
	if !res.Measured {
		t.Errorf("Measured = false although schema.sql declared a table; note:\n%s", res.Note)
	}
}

// B4 — the BLIND list is enumerated in FULL; the benign lists stay capped.
//
// An agent that cannot enumerate the blind files cannot go and check them, and
// the blind list is bounded by a CONFIGURED source list, not by schema size. The
// benign lists keep their cap so a 200-file seed directory cannot inflate the
// note with information nobody needs to act on.
//
// The interlock is deliberate: uncapping this list is only safe because a
// response pushed over its budget by a long note now SAYS so (the budget-note
// half of this same change) instead of claiming it fit.
func TestSensorDB_BlindFileList_IsEnumeratedInFull_BenignListStaysCapped(t *testing.T) {
	const n = 8

	blindFiles := map[string][]byte{}
	var blindOrder []string
	for i := 1; i <= n; i++ {
		name := fmt.Sprintf("blind%d.sql", i)
		blindFiles[name] = []byte(fmt.Sprintf("CREATE DOMAIN email_addr_%d AS text;\n", i))
		blindOrder = append(blindOrder, name)
	}
	blindFiles["schema.sql"] = []byte("CREATE TABLE role (id bigserial PRIMARY KEY);\n")
	blindOrder = append(blindOrder, "schema.sql")

	res := auditTraceless(t, inlineProject(t, blindOrder, blindFiles))
	for i := 1; i <= n; i++ {
		name := fmt.Sprintf("blind%d.sql", i)
		if !strings.Contains(res.Note, name) {
			t.Errorf("%s is not named: the blind list must be enumerated in full, or the agent cannot check the files:\n%s", name, res.Note)
		}
	}
	if strings.Contains(blindClause(res.Note), "more)") {
		t.Errorf("the blind list is truncated:\n%s", res.Note)
	}

	benignFiles := map[string][]byte{}
	var benignOrder []string
	for i := 1; i <= n; i++ {
		name := fmt.Sprintf("seed%d.sql", i)
		benignFiles[name] = []byte("INSERT INTO role (name) VALUES ('admin');\n")
		benignOrder = append(benignOrder, name)
	}
	benignFiles["schema.sql"] = []byte("CREATE TABLE role (id bigserial PRIMARY KEY, name text);\n")
	benignOrder = append(benignOrder, "schema.sql")

	benign := auditTraceless(t, inlineProject(t, benignOrder, benignFiles))
	if !strings.Contains(benign.Note, "more)") {
		t.Errorf("a %d-file benign list was NOT capped — the cap exists so a seed directory cannot inflate the note:\n%s", n, benign.Note)
	}
}
