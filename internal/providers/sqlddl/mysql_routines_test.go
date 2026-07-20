package sqlddl_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// Real MySQL Sakila routine coverage (feat/tsql-routine-fixtures): verifies the
// delimiter-aware parser captures the newly vendored, VERBATIM upstream Sakila
// routine bodies WHOLE (Complete=true), so future DB-030/DB-031/DB-040/DB-041
// rules read a complete body rather than one silently cut at the first
// internal ';'. The bodies are byte-for-byte upstream (see
// testdata/mysql/sakila_real_objects.sql's header for source + commit +
// license and the byte-diff provenance); this is the live proof the parser
// does not truncate them. findProc/findTrigger live in tsql_hypothesis_test.go.

// TestMySQL_SakilaRewardsReport_MultiStatement_FullCapture — rewards_report is
// a real multi-statement MySQL PROCEDURE wrapped in the upstream "DELIMITER //"
// client directive. split() honors the active-terminator override, so every
// internal ';' stays inside one statement and the whole body is captured
// (Complete=true). It has NO PREPARE/EXECUTE (DB-030 negative) and NO
// DECLARE ... HANDLER (DB-031 positive) — asserted here from the real body.
func TestMySQL_SakilaRewardsReport_MultiStatement_FullCapture(t *testing.T) {
	s := goldenSchema(t, filepath.Join("mysql", "sakila_real_objects.sql"), sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())))
	body := findProc(t, s, "rewards_report").Body
	if !body.Complete {
		t.Errorf("Complete = false, want true — the real Sakila rewards_report body must be captured whole; Note=%q Text=%q", body.Note, body.Text)
	}
	if body.Note != "" {
		t.Errorf("Note = %q, want empty (no truncation occurred)", body.Note)
	}
	// First and last internal statements both present => the whole body survived.
	if !strings.Contains(body.Text, "DECLARE last_month_start DATE") {
		t.Errorf("Body.Text = %q, want the FIRST internal statement", body.Text)
	}
	if !strings.Contains(body.Text, "DROP TABLE tmpCustomer") {
		t.Errorf("Body.Text = %q, want the LAST internal statement (whole body captured)", body.Text)
	}
	// Real classification, read from the real body: DB-030 negative, DB-031 positive.
	if strings.Contains(body.Text, "PREPARE") || strings.Contains(body.Text, "EXECUTE") {
		t.Errorf("rewards_report unexpectedly contains dynamic SQL; upstream has none (DB-030 negative): %q", body.Text)
	}
	if strings.Contains(strings.ToUpper(body.Text), "DECLARE EXIT HANDLER") || strings.Contains(strings.ToUpper(body.Text), "DECLARE CONTINUE HANDLER") {
		t.Errorf("rewards_report unexpectedly contains an exception handler; upstream has none (DB-031 positive): %q", body.Text)
	}
}

// TestMySQL_SakilaInventoryHeldByCustomer_HasHandler_FullCapture —
// inventory_held_by_customer is a real MySQL FUNCTION carrying a genuine
// "DECLARE EXIT HANDLER FOR NOT FOUND": a DB-031 NEGATIVE. Wrapped in
// "DELIMITER $$", captured whole (Complete=true) with the handler intact.
func TestMySQL_SakilaInventoryHeldByCustomer_HasHandler_FullCapture(t *testing.T) {
	s := goldenSchema(t, filepath.Join("mysql", "sakila_real_objects.sql"), sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())))
	body := findProc(t, s, "inventory_held_by_customer").Body
	if !body.Complete {
		t.Errorf("Complete = false, want true — inventory_held_by_customer body must be captured whole; Note=%q Text=%q", body.Note, body.Text)
	}
	if !strings.Contains(body.Text, "DECLARE EXIT HANDLER FOR NOT FOUND") {
		t.Errorf("Body.Text = %q, want the real DECLARE EXIT HANDLER (DB-031 negative) present verbatim", body.Text)
	}
}

// TestMySQL_SakilaFilmTriggers_CascadeToFilmText_FullCapture — the three real
// film triggers each write the SEPARATE film_text table (a DB-040 cascade
// POSITIVE) and make no external call (DB-041 NEGATIVE). Wrapped in
// "DELIMITER ;;", each multi-statement body is captured whole (Complete=true).
func TestMySQL_SakilaFilmTriggers_CascadeToFilmText_FullCapture(t *testing.T) {
	s := goldenSchema(t, filepath.Join("mysql", "sakila_real_objects.sql"), sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())))
	cases := map[string]string{
		"ins_film": "INSERT INTO film_text",
		"upd_film": "UPDATE film_text",
		"del_film": "DELETE FROM film_text",
	}
	for name, wantWrite := range cases {
		trig := findTrigger(t, s, name)
		if trig.Table != "film" {
			t.Errorf("%s Table = %q, want %q", name, trig.Table, "film")
		}
		if !trig.Body.Complete {
			t.Errorf("%s Complete = false, want true — DELIMITER-wrapped body must be captured whole; Note=%q Text=%q", name, trig.Body.Note, trig.Body.Text)
		}
		if !strings.Contains(trig.Body.Text, wantWrite) {
			t.Errorf("%s Body.Text = %q, want the cascade write %q (DB-040 positive)", name, trig.Body.Text, wantWrite)
		}
		if strings.Contains(strings.ToUpper(trig.Body.Text), "EXECUTE") || strings.Contains(strings.ToUpper(trig.Body.Text), "CALL ") {
			t.Errorf("%s Body.Text = %q, expected NO external call (DB-041 negative)", name, trig.Body.Text)
		}
	}
}
