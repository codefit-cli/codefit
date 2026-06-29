package findings

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Fingerprint is the BASELINE identity of a finding or surface item: a short hash
// of (category, file, normalized content), deliberately WITHOUT the line so it is
// stable when code moves (e.g. an import added above) and re-detects only when the
// item's own content changes. The content is hashed, never stored, so a fingerprint
// of a hardcoded-secret finding can be committed to .codefit-baseline without
// leaking the secret. Whitespace is normalized so re-indentation does not churn it.
func Fingerprint(category, file, content string) string {
	h := sha256.Sum256([]byte(category + "\x00" + file + "\x00" + normalizeContent(content)))
	return hex.EncodeToString(h[:])[:12]
}

// normalizeContent collapses runs of whitespace to a single space and trims, so a
// reformat that only changes spacing does not change the fingerprint.
func normalizeContent(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

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
	// Reasoning explains why the finding was flagged. Only set for findings the
	// agent reasoned from mapped surface; it records the agent's rationale so it
	// survives beyond the conversation (PRD section 11).
	Reasoning string `json:"reasoning,omitempty"`
	// Confidence is 1.0 for deterministic (regex/AST) findings and < 1.0 for
	// findings the agent reasoned from surface.
	Confidence float64 `json:"confidence"`
	// Probabilistic is true when the finding comes from the agent's reasoning
	// over mapped surface rather than a deterministic match.
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
	// Fingerprint is the content identity used by the baseline (see Fingerprint).
	// It is a one-way hash, safe to commit. Empty until stamped at the scan boundary.
	Fingerprint string `json:"fingerprint,omitempty"`
}

// ConsentRecord is the committed record that a critical security finding was
// reviewed and explicitly accepted. It lives in .codefit.yaml as an audit trail.
type ConsentRecord struct {
	AcceptedBy string `json:"accepted_by"`
	AcceptedAt string `json:"accepted_at"`
	Reason     string `json:"reason"`
}

// SurfaceItem is one element of the auditable surface a sensor enumerates for
// the agent to reason about (PRD section 10). codefit does not decide whether a
// surface item is vulnerable — it maps the complete structural surface of a
// category (IDOR endpoints, protectable handlers, data serializations, ...) so
// the agent reasons over each with its own LLM, with no blind spots.
type SurfaceItem struct {
	// ID is a stable anchor derived from (file, line, category) — see
	// core/surface.StableID. The agent returns it in a verdict, and codefit
	// recomputes it to validate the verdict statelessly.
	ID string `json:"id"`
	// Category is the surface class: "idor" | "authz" | "overfetch" | ...
	Category string `json:"category"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	// Snippet is the relevant source fragment for the agent to read.
	Snippet string `json:"snippet"`
	// StructuralSignals are verifiable syntactic FACTS about the item ("reads
	// params.id", "no call to a known authz helper was detected in the body"),
	// never judgments ("is vulnerable", "missing authorization"). They are what
	// the agent reasons over.
	StructuralSignals []string `json:"structural_signals"`
	// StructuralFacts is the queryable form of the same facts: boolean detections
	// a consumer can filter/sort by without parsing prose (e.g. authz →
	// "known_authz_detected", idor → "local_access_detected"). They are FACTS
	// ("a known pattern was/wasn't detected"), never judgments and never severity:
	// known_authz_detected=false means "no known authz pattern was seen", NOT "this
	// is unauthorized". Ordering by a fact is allowed; concluding from it is the
	// agent's.
	StructuralFacts map[string]bool `json:"structural_facts"`
	// IndirectCall names the external function a handler delegates to when the
	// resource access is NOT local to the body (e.g. an Express controller calling
	// a service that owns the Prisma query). It is paired with the structural fact
	// indirect_access=true. codefit does NOT follow the call across files — it
	// names the callee so the agent can reason about where the access happens
	// (cross-file option C). Empty when the access is local.
	IndirectCall string `json:"indirect_call,omitempty"`
	// ReasonToReview is the QUESTION the agent must answer about this item, never
	// a conclusion.
	ReasonToReview string `json:"reason_to_review"`
	// Fingerprint is the content identity used by the baseline (see Fingerprint).
	// Empty until stamped at the scan boundary.
	Fingerprint string `json:"fingerprint,omitempty"`
}

// SensorResult is what a single sensor returns after a run: its deterministic
// findings, the surface to be reasoned by the agent, the dimension score, how
// long it took, and an error string if it failed (kept as a string so partial
// results survive serialization).
type SensorResult struct {
	Sensor     string        `json:"sensor"`
	Score      int           `json:"score"`
	Findings   []Finding     `json:"findings"`
	Surface    []SurfaceItem `json:"surface,omitempty"`
	DurationMs int64         `json:"duration_ms"`
	Error      string        `json:"error,omitempty"`
}
