package sqlddl_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// DB-020 (view sensitive-column exposure, Phase 2.2) real-fixture dogfood.
//
// A view's whole PURPOSE is usually a curated, hand-picked projection — so a
// real, well-designed view rarely re-exposes a raw sensitive column under
// its own sensitive-looking name. Every real view dogfooded here (Pagila's
// actor_info, AdventureWorks' vEmployee, Sakila's customer_list)
// legitimately confirms the NEGATIVE case: DB-020 correctly does not fire on
// genuine, sensitivity-free view projections. That is a real, honest finding
// about how views are actually written in practice — not a gap in the rule.
// The POSITIVE fire path (a view that DOES expose a sensitive-token column)
// is proven at the unit level in internal/core/dbrules/db020_test.go against
// synthetic-but-representative SQL text; no real vendored view in any of the
// three dogfooded corpora happened to contain one.

func TestDB020_Pagila_ActorInfo_RealView_NegativeCase(t *testing.T) {
	s := parseFixture(t, "pagila_excerpt.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.Postgres())))
	if len(s.Views) != 1 || s.Views[0].Name != "actor_info" {
		t.Fatalf("expected the real Pagila actor_info view to be parsed, got views=%+v", s.Views)
	}
	if !s.Views[0].Body.Complete {
		t.Fatalf("PostgreSQL view bodies are always Complete (viewBody), got Complete=false Note=%q", s.Views[0].Body.Note)
	}
	_, surf := dbrules.Run(s)
	if got := surfaceWithCategoryLocal(surf, surface.CategoryDBViewSensitiveColumn); len(got) != 0 {
		t.Errorf("actor_info (actor_id, first_name, last_name) has no sensitive-token column, DB-020 must not fire, got %d: %+v", len(got), got)
	}
}

func TestDB020_AdventureWorks_VEmployee_RealView_NegativeCase(t *testing.T) {
	s := parseFixture(t, "tsql/adventureworks_real_objects.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())))
	found := false
	for _, v := range s.Views {
		if v.Name == "vEmployee" {
			found = true
			if !v.Body.Complete {
				t.Fatalf("T-SQL VIEW bodies are always Complete (viewBody, unaffected by the T-SQL multi-statement truncation limit), got Complete=false Note=%q", v.Body.Note)
			}
		}
	}
	if !found {
		t.Fatalf("expected the real AdventureWorks vEmployee view to be parsed, got views=%+v", s.Views)
	}
	_, surf := dbrules.Run(s)
	if got := surfaceWithCategoryLocal(surf, surface.CategoryDBViewSensitiveColumn); len(got) != 0 {
		t.Errorf("vEmployee's real column list (EmailAddress, PhoneNumber, AdditionalContactInfo, ...) matches none of DB-053's sensitive tokens, DB-020 must not fire, got %d: %+v", len(got), got)
	}
}

func TestDB020_Sakila_CustomerList_RealView_NegativeCase(t *testing.T) {
	s := parseFixture(t, "mysql/sakila_real_objects.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())))
	if len(s.Views) != 1 || s.Views[0].Name != "customer_list" {
		t.Fatalf("expected the real vendored Sakila customer_list view to be parsed, got views=%+v", s.Views)
	}
	if !s.Views[0].Body.Complete {
		t.Fatalf("MySQL view bodies are always Complete (viewBody), got Complete=false Note=%q", s.Views[0].Body.Note)
	}
	_, surf := dbrules.Run(s)
	if got := surfaceWithCategoryLocal(surf, surface.CategoryDBViewSensitiveColumn); len(got) != 0 {
		t.Errorf("customer_list (ID, name, address, \"zip code\", phone, city, country, notes, SID) has no sensitive-token column, DB-020 must not fire, got %d: %+v", len(got), got)
	}
}

// TestDB020_Sakila_CustomerList_ColumnsAreGenuinelyExtracted proves the
// negative case above is a REAL negative, not a silent parser failure that
// happens to also produce zero items: it asserts the extractor actually
// walked the real view's leading-comma-free, trailing-comma, multi-function
// projection (CONCAT, IF, backtick-quoted alias) end to end, by cross-
// checking against a deliberately mutated copy of the same real body text
// with one column renamed to a sensitive token — proving DB-020 WOULD fire
// on this exact real-world shape if the data warranted it.
func TestDB020_Sakila_CustomerList_WouldFireIfColumnWereSensitive(t *testing.T) {
	s := parseFixture(t, "mysql/sakila_real_objects.sql", sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())))
	if len(s.Views) != 1 {
		t.Fatalf("expected exactly 1 view, got %d", len(s.Views))
	}

	// Rename the real view's own "AS SID" alias to a sensitive token,
	// preserving every other byte of the real, vendored SQL shape (trailing
	// commas, CONCAT/IF calls, backtick-canonicalized "zip code" alias).
	patched := s.Views[0]
	if !strings.Contains(patched.Body.Text, "AS SID") {
		t.Fatal("test setup broken: 'AS SID' not found in the real vendored body text")
	}
	patched.Body.Text = strings.Replace(patched.Body.Text, "AS SID", "AS ssn", 1)

	_, surf := dbrules.Run(&db.Schema{Views: []db.View{patched}})
	if got := surfaceWithCategoryLocal(surf, surface.CategoryDBViewSensitiveColumn); len(got) != 1 {
		t.Fatalf("expected DB-020 to fire once the real view's own final column is renamed to a sensitive token, got %d", len(got))
	}
}
