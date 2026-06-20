package findings

// Severity ranks how serious a finding is. Higher severities can block a deploy
// (see the report's blocked field and the --fail-on flag).
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

// Dimension is the audit axis a finding belongs to. Each sensor maps to exactly
// one dimension, and the global score is a weighted average across dimensions.
type Dimension string

const (
	DimensionSecurity   Dimension = "security"
	DimensionReview     Dimension = "review"
	DimensionDB         Dimension = "db"
	DimensionComplexity Dimension = "complexity"
	DimensionPractices  Dimension = "practices"
	DimensionTests      Dimension = "tests"
)

// Finding is a single hallazgo produced by a sensor. The JSON tags define the
// canonical report contract; consumers (dashboards, CI, MCP clients) depend on
// it, so changes here are governed by the report's schema_version.
type Finding struct {
	ID          string    `json:"id"`
	Dimension   Dimension `json:"dimension"`
	Severity    Severity  `json:"severity"`
	File        string    `json:"file,omitempty"`
	Line        int       `json:"line,omitempty"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Suggestion  string    `json:"suggestion"`
	// Reasoning explains why an LLM flagged the finding. Only set for
	// probabilistic findings; it builds trust in non-deterministic results.
	Reasoning string `json:"reasoning,omitempty"`
	// Confidence is 1.0 for deterministic (regex/AST) findings and < 1.0 for
	// LLM-inferred ones.
	Confidence float64 `json:"confidence"`
	// Probabilistic is true when the finding comes from LLM inference.
	Probabilistic bool `json:"probabilistic"`
	// RequiresConsent is true for critical security findings, which cannot be
	// silenced with a plain ignore — they need an explicit ConsentRecord.
	RequiresConsent bool `json:"requires_consent"`
	// Baselined is true when the finding is pre-existing debt recorded in a
	// baseline snapshot; baselined findings never trigger the deploy block.
	Baselined bool `json:"baselined,omitempty"`
	// Suppressed, when non-nil, holds the consent under which a finding was
	// accepted and silenced.
	Suppressed *ConsentRecord `json:"suppressed,omitempty"`
}

// ConsentRecord is the committed record that a critical security finding was
// reviewed and explicitly accepted. It lives in .codefit.yaml as an audit trail.
type ConsentRecord struct {
	AcceptedBy string `json:"accepted_by"`
	AcceptedAt string `json:"accepted_at"`
	Reason     string `json:"reason"`
}

// SensorResult is what a single sensor returns after a run: its findings, the
// dimension score it computed, how long it took, and an error string if it
// failed (kept as a string so partial results survive serialization).
type SensorResult struct {
	Sensor     string    `json:"sensor"`
	Score      int       `json:"score"`
	Findings   []Finding `json:"findings"`
	DurationMs int64     `json:"duration_ms"`
	Error      string    `json:"error,omitempty"`
}
