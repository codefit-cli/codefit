package report_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/report"
	"github.com/codefit-cli/codefit/internal/core/scoring"
)

func sampleReport() report.AuditReport {
	return report.AuditReport{
		SchemaVersion:  report.SchemaVersion,
		CodefitVersion: "0.1.0",
		Timestamp:      time.Unix(0, 0).UTC(),
		Project:        "demo",
		Language:       "go",
		Score: scoring.ScoreSummary{
			Global:      64,
			ByDimension: map[findings.Dimension]int{findings.DimensionSecurity: 41},
		},
		Blocked: true,
		Findings: []findings.Finding{
			{ID: "SEC-001", Dimension: findings.DimensionSecurity, Severity: findings.SeverityCritical,
				File: "src/auth.go", Line: 12, Title: "Hardcoded key", Suggestion: "Use env var"},
		},
	}
}

func TestJSONRendererProducesCanonicalJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := (report.JSONRenderer{}).Render(&buf, sampleReport()); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if m["schema_version"] != "1.0" {
		t.Errorf("schema_version = %v, want 1.0", m["schema_version"])
	}
	score, ok := m["score"].(map[string]any)
	if !ok || score["global"].(float64) != 64 {
		t.Errorf("score.global missing or wrong: %v", m["score"])
	}
}

func TestPlainRendererContainsScoreAndFindings(t *testing.T) {
	var buf bytes.Buffer
	if err := (report.PlainRenderer{}).Render(&buf, sampleReport()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"64", "SEC-001", "src/auth.go"} {
		if !strings.Contains(out, want) {
			t.Errorf("plain output missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(strings.ToUpper(out), "SCORE") {
		t.Errorf("plain output should show a SCORE header:\n%s", out)
	}
}

func TestHTMLRendererNotImplemented(t *testing.T) {
	err := (report.HTMLRenderer{}).Render(&bytes.Buffer{}, sampleReport())
	if err == nil || !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("HTML renderer should report not-implemented, got: %v", err)
	}
}

func TestChooseRenderer(t *testing.T) {
	cases := map[string]string{
		"json":     "report.JSONRenderer",
		"html":     "report.HTMLRenderer",
		"markdown": "report.PlainRenderer",
		"":         "report.PlainRenderer", // default
	}
	for format, wantType := range cases {
		r := report.ChooseRenderer(format, false, false)
		if got := typeName(r); got != wantType {
			t.Errorf("ChooseRenderer(%q) = %s, want %s", format, got, wantType)
		}
	}
}

func typeName(v any) string {
	switch v.(type) {
	case report.JSONRenderer:
		return "report.JSONRenderer"
	case report.PlainRenderer:
		return "report.PlainRenderer"
	case report.HTMLRenderer:
		return "report.HTMLRenderer"
	default:
		return "unknown"
	}
}
