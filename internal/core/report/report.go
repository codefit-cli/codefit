package report

import (
	"encoding/json"
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
	SchemaVersion  string               `json:"schema_version"`
	CodefitVersion string               `json:"codefit_version"`
	Timestamp      time.Time            `json:"timestamp"`
	Project        string               `json:"project"`
	Language       string               `json:"language"`
	Commit         string               `json:"commit,omitempty"`
	Score          scoring.ScoreSummary `json:"score"`
	Blocked        bool                 `json:"blocked"`
	BlockReason    string               `json:"block_reason,omitempty"`
	Baseline       *BaselineSummary     `json:"baseline,omitempty"`
	Findings       []findings.Finding   `json:"findings"`
	// Surface is the auditable structural surface the agent must reason about
	// (PRD section 10), aggregated across sensors.
	Surface []findings.SurfaceItem `json:"surface,omitempty"`
	// Endpoints is the synthesis view (codefit-scan-all): the deterministic
	// findings and the surface grouped by the handler they belong to, each
	// endpoint's concerns ordered by certainty (deterministic → confirmed →
	// frontier). It is the complete picture per endpoint for the agent to reason;
	// the flat Findings/Surface remain for traceability.
	Endpoints []EndpointReport `json:"endpoints,omitempty"`
	// CoverageNote states which classes of problems were not audited (derived
	// from the coverage manifest), so the report always informs its own limits
	// (PRD section 21). Populated once the coverage manifest exists (Fase C).
	CoverageNote   string                  `json:"coverage_note,omitempty"`
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

// Renderer writes an AuditReport to w. Renderers never reach into the core;
// they only consume the canonical report.
type Renderer interface {
	Render(w io.Writer, r AuditReport) error
}

// JSONRenderer writes the canonical JSON, indented. In the MCP-first model the
// report is delivered as JSON to the agent; this is the only renderer in v1
// (an HTML renderer is a future addition over the same canonical report).
type JSONRenderer struct{}

func (JSONRenderer) Render(w io.Writer, r AuditReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return fmt.Errorf("encoding report JSON: %w", err)
	}
	return nil
}
