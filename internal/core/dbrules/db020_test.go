package dbrules_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// --- DB-020: view sensitive-column exposure (surface, name-heuristic, never
// affirms — same epistemology as DB-053, whose token vocabulary this rule
// reuses verbatim). ---

func db020Schema(name, bodyText string, complete bool) *db.Schema {
	return &db.Schema{Views: []db.View{
		{
			Name: name,
			Pos:  db.Pos{File: "s.sql", Line: 100},
			Body: db.Body{Text: bodyText, Complete: complete},
		},
	}}
}

func TestDB020_FiresOnBareSensitiveColumn(t *testing.T) {
	s := db020Schema("v_users", `CREATE VIEW v_users AS SELECT id, password FROM users;`, true)
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBViewSensitiveColumn)
	if len(got) != 1 {
		t.Fatalf("DB-020 = %d, want 1 (bare 'password' column)", len(got))
	}
	if got[0].StructuralFacts["encryption_hint_in_name"] {
		t.Error("plain 'password' has no encryption hint")
	}
	for _, sig := range got[0].StructuralSignals {
		if sig == "column: password" {
			return
		}
	}
	t.Errorf("expected a structural signal naming the column, got %v", got[0].StructuralSignals)
}

func TestDB020_FiresOnAliasedSensitiveColumn(t *testing.T) {
	// "expr AS alias" — the ALIAS is the exposed name, not the qualifier.
	s := db020Schema("v_secrets", `CREATE VIEW v_secrets AS SELECT u.id, u.secret_col AS apiKey FROM users u;`, true)
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBViewSensitiveColumn)
	if len(got) != 1 {
		t.Fatalf("DB-020 = %d, want 1 (aliased apiKey)", len(got))
	}
}

func TestDB020_FiresOnTableQualifiedSensitiveColumn(t *testing.T) {
	// "table.column" with no alias — the COLUMN part is the exposed name.
	s := db020Schema("v_tokens", `CREATE VIEW v_tokens AS SELECT u.id, u.token FROM users u;`, true)
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBViewSensitiveColumn)
	if len(got) != 1 {
		t.Fatalf("DB-020 = %d, want 1 (u.token -> token)", len(got))
	}
}

func TestDB020_NegativeNoSensitiveColumn(t *testing.T) {
	s := db020Schema("v_actor", `CREATE VIEW v_actor AS SELECT a.actor_id, a.first_name, a.last_name FROM actor a;`, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBViewSensitiveColumn); len(got) != 0 {
		t.Errorf("no sensitive column present, must not fire, got %d", len(got))
	}
}

func TestDB020_AbstainOnIncompleteBody(t *testing.T) {
	// Defensive abstain (ADR 0004): even though "password" is textually
	// present, Complete=false means this rule must NOT trust it — never a
	// deterministic finding, never even a surface item from a body we cannot
	// prove is whole.
	s := db020Schema("v_maybe", `CREATE VIEW v_maybe AS SELECT id, password FROM users`, false)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBViewSensitiveColumn); len(got) != 0 {
		t.Errorf("Complete=false body must NEVER be evaluated, got %d surface items", len(got))
	}
}

func TestDB020_NeverAffirms(t *testing.T) {
	// Locks ADR 0015/0017: this rule is pure surface, it must never return a
	// deterministic Finding, even for the most unambiguous case.
	s := db020Schema("v_users", `CREATE VIEW v_users AS SELECT id, password FROM users;`, true)
	findingsOut, items := dbrules.Run(s)
	if len(findingsOut) != 0 {
		t.Fatalf("dbrules.Run should have 0 deterministic findings from views-only schema, got %d: %+v", len(findingsOut), findingsOut)
	}
	if len(surfaceWithCategory(items, surface.CategoryDBViewSensitiveColumn)) == 0 {
		t.Fatal("sanity check failed: expected the fixture to fire as SURFACE at all")
	}
}

func TestDB020_SelectStarIsDeclaredMiss(t *testing.T) {
	// "SELECT *" cannot be enumerated — the column list is not literal in the
	// DDL text. This must NOT crash and must NOT fire (even though, in this
	// contrived case, the underlying table obviously has a "password" column
	// nowhere visible in Body.Text).
	s := db020Schema("v_all", `CREATE VIEW v_all AS SELECT * FROM users;`, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBViewSensitiveColumn); len(got) != 0 {
		t.Errorf("SELECT * must be a declared miss, not evaluated, got %d", len(got))
	}
}

func TestDB020_LeadingCommaStyleIsHandled(t *testing.T) {
	// Real-shaped leading-comma style (AdventureWorks vEmployee's own style).
	body := "CREATE VIEW v_lead AS\nSELECT\n    u.id\n    ,u.password\nFROM users u;"
	s := db020Schema("v_lead", body, true)
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBViewSensitiveColumn)
	if len(got) != 1 {
		t.Fatalf("leading-comma style: DB-020 = %d, want 1 (u.password)", len(got))
	}
}

func TestDB020_QuotedIdentifierAliasWithSpaceIsHandled(t *testing.T) {
	// A canonical ANSI-quoted alias (what split() re-emits every dialect's
	// bracket/backtick identifier as) containing a space, comma-adjacent to
	// the next column, must parse cleanly (no mis-split, no crash). Neither
	// alias here is itself sensitive-token-shaped — "zip code" is genuinely
	// clean, and "national id" proves the rule checks the EXPOSED alias, not
	// the underlying origin column: a.ssn is renamed away to "national id",
	// so per this rule's own declared semantics ("the alias is the exposed
	// name" — this is what a consumer of the VIEW actually sees), it does
	// NOT fire on the origin name. That is the correct, spec-consistent
	// behavior, not a gap: DB-020 audits what the view exposes, never what a
	// base table's column happens to be called.
	body := `CREATE VIEW v_q AS SELECT a.postal_code AS "zip code", a.ssn AS "national id" FROM address a;`
	s := db020Schema("v_q", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBViewSensitiveColumn); len(got) != 0 {
		t.Fatalf(`DB-020 = %d, want 0 (neither "zip code" nor "national id" is a sensitive-token alias; a.ssn's alias hides the origin name by design)`, len(got))
	}
}

func TestDB020_FunctionCallWithoutAliasIsDeclaredMiss(t *testing.T) {
	// A function-call expression with NO alias has no extractable name — a
	// per-item declared miss (ADR 0004's bounded-scan line), never guessed,
	// never crashes, never blocks extraction of the OTHER columns.
	body := `CREATE VIEW v_fn AS SELECT id, UPPER(password) FROM users;`
	s := db020Schema("v_fn", body, true)
	_, items := dbrules.Run(s)
	if got := surfaceWithCategory(items, surface.CategoryDBViewSensitiveColumn); len(got) != 0 {
		t.Errorf("UPPER(password) with no alias must be a declared miss (no name to check), got %d", len(got))
	}
}

func TestDB020_CaseExpressionInsideFunctionDoesNotBreakTopLevelAs(t *testing.T) {
	// A CAST(...)'s own internal "AS" is at paren-depth 1 and must be
	// ignored; the outer, top-level "AS token" is what must be matched.
	body := `CREATE VIEW v_cast AS SELECT id, CAST(u.raw_token AS varchar(50)) AS token FROM users u;`
	s := db020Schema("v_cast", body, true)
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBViewSensitiveColumn)
	if len(got) != 1 {
		t.Fatalf("DB-020 = %d, want 1 (outer alias 'token', not the CAST's internal AS)", len(got))
	}
}

func TestDB020_ProjectionWithFunctionCallCommaDoesNotMisSplit(t *testing.T) {
	// A function call with internal commas inside parens (paren-depth 1) must
	// not be mistaken for a top-level column-list separator; the columns on
	// either side must still be correctly recognized.
	body := `CREATE VIEW v_concat AS SELECT id, CONCAT(first_name, ' ', last_name) AS full_name, ssn FROM users;`
	s := db020Schema("v_concat", body, true)
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBViewSensitiveColumn)
	if len(got) != 1 {
		t.Fatalf("DB-020 = %d, want 1 (only 'ssn'; 'full_name' is not sensitive, CONCAT's internal commas must not split it into bogus columns)", len(got))
	}
}

func TestDB020_MultipleSensitiveColumns_EachEmitOwnItem(t *testing.T) {
	body := `CREATE VIEW v_multi AS SELECT id, password, ssn FROM users;`
	s := db020Schema("v_multi", body, true)
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBViewSensitiveColumn)
	if len(got) != 2 {
		t.Fatalf("DB-020 = %d, want 2 (password, ssn), items=%+v", len(got), got)
	}
}

func TestDB020_AnchorLineIsPerColumnNotJustViewHeader(t *testing.T) {
	body := "CREATE VIEW v_multiline AS\nSELECT\n    id,\n    password\nFROM users;"
	s := db020Schema("v_multiline", body, true)
	_, items := dbrules.Run(s)
	got := surfaceWithCategory(items, surface.CategoryDBViewSensitiveColumn)
	if len(got) != 1 {
		t.Fatalf("DB-020 = %d, want 1", len(got))
	}
	// view Pos.Line is 100 (db020Schema); "password" sits 3 newlines into the
	// body text ("CREATE VIEW...\n" / "SELECT\n" / "    id,\n" -> line 4th),
	// so the anchor must NOT just equal the view header's own line (100).
	if got[0].Line == 100 {
		t.Errorf("anchor line = %d, want the password column's OWN line, not just the view header's", got[0].Line)
	}
	if got[0].Line != 103 {
		t.Errorf("anchor line = %d, want 103 (100 + 3 newlines up to 'password')", got[0].Line)
	}
}

// --- All() enumerates DB-020 ---

func TestAll_IncludesDB020(t *testing.T) {
	ids := map[string]bool{}
	for _, r := range dbrules.All() {
		ids[r.ID()] = true
	}
	if !ids["DB-020"] {
		t.Errorf("dbrules.All() missing DB-020 (have %v)", ids)
	}
}
