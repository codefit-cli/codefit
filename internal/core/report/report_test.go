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

func ptr(i int) *int { return &i }

func sampleReport() report.AuditReport {
	return report.AuditReport{
		SchemaVersion:  report.SchemaVersion,
		CodefitVersion: "0.1.0",
		Timestamp:      time.Unix(0, 0).UTC(),
		Project:        "demo",
		Language:       "go",
		Score: scoring.ScoreSummary{
			Global:      64,
			ByDimension: map[findings.Dimension]*int{findings.DimensionSecurity: ptr(41)},
		},
		Blocked: true,
		Findings: []findings.Finding{
			{ID: "SEC-001", Dimension: findings.DimensionSecurity, Severity: findings.SeverityCritical,
				File: "src/auth.go", Line: 12, Title: "Hardcoded key", Suggestion: "Use env var"},
		},
	}
}

func TestReportCarriesSurfaceAndCoverageNote(t *testing.T) {
	r := sampleReport()
	r.Surface = []findings.SurfaceItem{{Category: "authz", File: "h.go", Line: 3}}
	r.CoverageNote = "Not audited: race conditions, architectural design flaws."

	var buf bytes.Buffer
	if err := (report.JSONRenderer{}).Render(&buf, r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{`"surface"`, `"authz"`, `"coverage_note"`, "race conditions"} {
		if !strings.Contains(out, want) {
			t.Errorf("report JSON missing %q:\n%s", want, out)
		}
	}
}

func TestReportShowsUnmeasuredDimensionAsNull(t *testing.T) {
	r := sampleReport()
	r.Score = scoring.Compute(
		[]findings.Dimension{findings.DimensionSecurity}, nil, scoring.DefaultWeights())
	var buf bytes.Buffer
	if err := (report.JSONRenderer{}).Render(&buf, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"complexity": null`) {
		t.Errorf("unmeasured dimension should render as null:\n%s", buf.String())
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
