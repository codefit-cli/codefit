package sqlddl_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/dbrules"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// N2 (design SS3c) — the AUTHORED fixture. Before this fixture existed, a
// regression test against internal/providers/sqlddl/testdata/ would have
// passed VACUOUSLY: OWNER TO/RENAME/ENABLE/ALTER COLUMN appear zero times
// anywhere in the corpus (confirmed by grep, three times independently
// during planning). testdata/pg_constructed_n2_recognized_skips.sql is
// authored so the statements this test needs actually exist in the file
// under test.
func TestSQLDDL_N2_Fixture_RecognizedSkipsDoNotMuteCompleteness(t *testing.T) {
	path := filepath.Join("testdata", "pg_constructed_n2_recognized_skips.sql")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	p := sqlddl.New()
	s, err := p.ParseSchema([]providers.SourceFile{{Path: path, Content: content}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	if len(s.Tables) != 2 {
		t.Fatalf("parsed %d tables, want 2 (widget, widget_no_pk)", len(s.Tables))
	}

	var widget, widgetNoPK *struct {
		Complete bool
		Note     string
		PK       []string
	}
	for _, tb := range s.Tables {
		v := &struct {
			Complete bool
			Note     string
			PK       []string
		}{Complete: tb.Complete, Note: tb.Note, PK: tb.PrimaryKey}
		switch tb.Name {
		case "widget":
			widget = v
		case "widget_no_pk":
			widgetNoPK = v
		}
	}
	if widget == nil || widgetNoPK == nil {
		t.Fatalf("expected tables widget and widget_no_pk, got %v", s.Tables)
	}

	// N2 positive control: OWNER TO / ENABLE ROW LEVEL SECURITY / ALTER
	// COLUMN / (CONSTRAINT-prefixed) CHECK are all recognized, declared
	// skips — they must NOT demote widget's completeness signal.
	if !widget.Complete {
		t.Errorf("widget.Complete = false, want true — recognized skips are not incompleteness (N2)")
	}
	if widget.Note != "" {
		t.Errorf("widget.Note = %q, want empty", widget.Note)
	}

	// Honesty must not cost capability (spec "Domain: Positive Control"):
	// widget_no_pk is complete and genuinely has no PK, so DB-050 MUST still
	// affirm on it at confidence 1.0.
	if !widgetNoPK.Complete {
		t.Fatalf("widget_no_pk.Complete = false, want true")
	}
	if len(widgetNoPK.PK) != 0 {
		t.Fatalf("widget_no_pk.PrimaryKey = %v, want empty (this table genuinely declares none)", widgetNoPK.PK)
	}
	fs, _ := dbrules.Run(s)
	var affirmedWidgetNoPK bool
	for _, f := range fs {
		if f.ID == "DB-050" && strings.Contains(f.Description, "widget_no_pk") {
			affirmedWidgetNoPK = true
			if f.Confidence != 1.0 || f.Probabilistic {
				t.Errorf("DB-050 on widget_no_pk: Confidence=%v Probabilistic=%v, want 1.0/false", f.Confidence, f.Probabilistic)
			}
		}
	}
	if !affirmedWidgetNoPK {
		t.Error("DB-050 did not affirm on widget_no_pk — recognized-skip recording must not cost DB-050 its capability elsewhere in the same file (N2)")
	}
}
