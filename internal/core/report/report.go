package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/scoring"
)

// SchemaVersion is the version of the canonical JSON report format, independent
// of the codefit binary version so report consumers can migrate in a controlled
// way (PRD §18).
const SchemaVersion = "1.0"

// AuditReport is the canonical, language-agnostic audit result. JSON is the
// single source of truth; every renderer derives from this struct.
type AuditReport struct {
	SchemaVersion  string                  `json:"schema_version"`
	CodefitVersion string                  `json:"codefit_version"`
	Timestamp      time.Time               `json:"timestamp"`
	Project        string                  `json:"project"`
	Language       string                  `json:"language"`
	Commit         string                  `json:"commit,omitempty"`
	Score          scoring.ScoreSummary    `json:"score"`
	Blocked        bool                    `json:"blocked"`
	BlockReason    string                  `json:"block_reason,omitempty"`
	Baseline       *BaselineSummary        `json:"baseline,omitempty"`
	Findings       []findings.Finding      `json:"findings"`
	RegressionRisk *RegressionRisk         `json:"regression_risk,omitempty"`
	SensorResults  []findings.SensorResult `json:"sensor_results"`
}

// BaselineSummary reports how many findings were new vs. recorded as historical
// debt when a baseline is active (RF-10).
type BaselineSummary struct {
	Active            bool `json:"active"`
	NewFindings       int  `json:"new_findings"`
	BaselinedFindings int  `json:"baselined_findings"`
}

// RegressionRisk groups regression-risk items by level (RF-06, --since mode).
type RegressionRisk struct {
	High   []RegressionItem `json:"high,omitempty"`
	Medium []RegressionItem `json:"medium,omitempty"`
}

// RegressionItem is a single function/symbol at risk from recent changes.
type RegressionItem struct {
	Symbol    string   `json:"symbol"`
	File      string   `json:"file"`
	Callsites []string `json:"callsites,omitempty"`
	Reason    string   `json:"reason,omitempty"`
}

// Format identifies an output renderer.
type Format string

const (
	FormatJSON     Format = "json"
	FormatMarkdown Format = "markdown"
	FormatHTML     Format = "html"
)

// Renderer writes an AuditReport to w. Renderers never reach into the core;
// they only consume the canonical report.
type Renderer interface {
	Render(w io.Writer, r AuditReport) error
}

// JSONRenderer writes the canonical JSON, indented.
type JSONRenderer struct{}

func (JSONRenderer) Render(w io.Writer, r AuditReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return fmt.Errorf("encoding report JSON: %w", err)
	}
	return nil
}

// HTMLRenderer is a placeholder until the standalone HTML report is built.
type HTMLRenderer struct{}

func (HTMLRenderer) Render(io.Writer, AuditReport) error {
	return errors.New("HTML renderer not yet implemented")
}

// ChooseRenderer selects a renderer for the output format. For markdown the
// plain renderer is used regardless of TTY today; a future TUI renderer will
// branch on isTTY && !noTUI here (PRD §18).
func ChooseRenderer(format string, isTTY, noTUI bool) Renderer {
	switch Format(format) {
	case FormatJSON:
		return JSONRenderer{}
	case FormatHTML:
		return HTMLRenderer{}
	default:
		_ = isTTY
		_ = noTUI
		return PlainRenderer{}
	}
}
