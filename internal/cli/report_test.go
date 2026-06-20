package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/report"
	"github.com/codefit-cli/codefit/internal/core/scoring"
)

func writeReportFile(t *testing.T) string {
	t.Helper()
	ar := report.AuditReport{
		SchemaVersion:  report.SchemaVersion,
		CodefitVersion: "0.1.0",
		Project:        "demo",
		Language:       "go",
		Score:          scoring.ScoreSummary{Global: 77, ByDimension: map[findings.Dimension]int{findings.DimensionSecurity: 77}},
		Findings:       []findings.Finding{{ID: "SEC-009", Title: "x", Severity: findings.SeverityHigh}},
	}
	data, _ := json.Marshal(ar)
	p := filepath.Join(t.TempDir(), "codefit-report.json")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRenderReportFileJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := renderReportFile(writeReportFile(t), "json", &buf); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("json output invalid: %v", err)
	}
	if m["schema_version"] != "1.0" {
		t.Errorf("schema_version = %v", m["schema_version"])
	}
}

func TestRenderReportFileMarkdown(t *testing.T) {
	var buf bytes.Buffer
	if err := renderReportFile(writeReportFile(t), "markdown", &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "77") || !strings.Contains(buf.String(), "SEC-009") {
		t.Errorf("markdown output missing score/finding:\n%s", buf.String())
	}
}

func TestRenderReportFileHTMLNotImplemented(t *testing.T) {
	err := renderReportFile(writeReportFile(t), "html", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("html should be not-implemented, got: %v", err)
	}
}

func TestRenderReportFileMissing(t *testing.T) {
	err := renderReportFile(filepath.Join(t.TempDir(), "nope.json"), "json", &bytes.Buffer{})
	if err == nil {
		t.Error("missing report file should error")
	}
}
