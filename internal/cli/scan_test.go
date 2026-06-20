package cli

import (
	"testing"
	"time"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/report"
)

func resultWith(fs ...findings.Finding) []findings.SensorResult {
	return []findings.SensorResult{{Sensor: "security", Findings: fs}}
}

func TestAssembleReportSetsSchemaAndScore(t *testing.T) {
	cfg := &config.Config{Project: config.Project{Name: "demo", Language: "go"}}
	r := assembleReport(cfg, resultWith(
		findings.Finding{ID: "SEC-052", Dimension: findings.DimensionSecurity, Severity: findings.SeverityMedium},
	), "0.1.0", time.Unix(0, 0))

	if r.SchemaVersion != report.SchemaVersion {
		t.Errorf("schema = %q, want %q", r.SchemaVersion, report.SchemaVersion)
	}
	if r.Project != "demo" || r.Language != "go" {
		t.Errorf("project/language = %q/%q", r.Project, r.Language)
	}
	if r.Score.Global == 0 {
		t.Error("score should be computed")
	}
	if len(r.Findings) != 1 {
		t.Errorf("findings = %d, want 1", len(r.Findings))
	}
}

func TestAssembleReportBlocksOnCriticalSecurity(t *testing.T) {
	cfg := &config.Config{Project: config.Project{Name: "demo", Language: "go"}}
	r := assembleReport(cfg, resultWith(
		findings.Finding{ID: "SEC-001", Dimension: findings.DimensionSecurity, Severity: findings.SeverityCritical},
	), "0.1.0", time.Unix(0, 0))

	if !r.Blocked {
		t.Error("a critical security finding without consent must block")
	}
	if r.BlockReason == "" {
		t.Error("blocked report should carry a reason")
	}
}

func TestAssembleReportNotBlockedWhenClean(t *testing.T) {
	cfg := &config.Config{Project: config.Project{Name: "demo", Language: "go"}}
	r := assembleReport(cfg, resultWith(), "0.1.0", time.Unix(0, 0))
	if r.Blocked {
		t.Error("clean report should not be blocked")
	}
}

func TestShouldFail(t *testing.T) {
	high := report.AuditReport{Findings: []findings.Finding{
		{Dimension: findings.DimensionReview, Severity: findings.SeverityHigh},
	}}
	blocked := report.AuditReport{Blocked: true}
	clean := report.AuditReport{}

	cases := []struct {
		name   string
		report report.AuditReport
		failOn string
		want   bool
	}{
		{"blocked always fails", blocked, "critical", true},
		{"high finding with fail-on critical does not fail", high, "critical", false},
		{"high finding with fail-on high fails", high, "high", true},
		{"high finding with fail-on medium fails", high, "medium", true},
		{"clean never fails", clean, "critical", false},
	}
	for _, tc := range cases {
		if got := shouldFail(tc.report, tc.failOn); got != tc.want {
			t.Errorf("%s: shouldFail = %v, want %v", tc.name, got, tc.want)
		}
	}
}
