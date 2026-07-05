package sqlddl_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// Real Pagila objects: the dollar-quoted function bodies ($$ and $_$) must not
// break statement splitting, and views/functions/triggers must populate the model
// (Name/Pos, and Table for triggers). Bodies/columns are NOT captured (declared
// limit for the future DB-020..041 rules).
func TestPagila_ViewsProcsTriggersPopulated(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("testdata", "pagila_excerpt.sql"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := sqlddl.New().ParseSchema([]providers.SourceFile{{Path: "pagila.sql", Content: content}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	if len(s.Views) != 1 {
		t.Errorf("views = %d, want 1 (actor_info)", len(s.Views))
	}
	if len(s.Procedures) != 2 {
		t.Errorf("procedures = %d, want 2 (_group_concat, last_updated) — dollar-quoted bodies must not split", len(s.Procedures))
	}
	if len(s.Triggers) != 2 {
		t.Fatalf("triggers = %d, want 2", len(s.Triggers))
	}
	// The actor table parsed cleanly alongside the objects (splitting held).
	if len(s.Tables) != 1 || s.Tables[0].Name != "actor" {
		t.Errorf("tables = %v, want [actor]", tableNames(s))
	}
	// Trigger.Table is parsed from the ON clause (schema qualifier stripped).
	var lastUpdated bool
	for _, tr := range s.Triggers {
		if tr.Name == "last_updated" {
			lastUpdated = true
			if tr.Table != "actor" {
				t.Errorf("last_updated trigger.Table = %q, want actor", tr.Table)
			}
		}
	}
	if !lastUpdated {
		t.Error("expected the last_updated trigger to be recognized")
	}
	// View names are populated (bodies are not captured — no field for them).
	if s.Views[0].Name != "actor_info" {
		t.Errorf("view name = %q, want actor_info", s.Views[0].Name)
	}
}
