package surface

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/syntax"
)

// StableID derives a short, stable id for a surface item from (file, line,
// category). It is a PURE function of those three components: the enumeration
// stamps it on each item, and the confirmation step recomputes it to validate
// that a verdict refers to a real item — this is what keeps codefit stateless
// (it stores nothing between enumeration and confirmation, it recomputes).
func StableID(file string, line int, category string) string {
	h := sha256.Sum256([]byte(file + "\x00" + strconv.Itoa(line) + "\x00" + category))
	return hex.EncodeToString(h[:])[:12]
}

// Query enumerates the auditable structural surface of one category over a
// parsed file. Each category+provider implements it; the framework orchestrates
// queries without knowing any category (it never knows about IDOR or Next).
type Query interface {
	// Enumerate returns the surface items found in the file rooted at root. file
	// is the project-relative path (a query may restrict itself by path, e.g.
	// Next App Router handlers live in app/**/route.ts).
	Enumerate(root syntax.Node, file string) []findings.SurfaceItem
}

// Run executes each query over the file and returns the union of their items
// with stable ids stamped. The framework is agnostic: it only orchestrates.
func Run(queries []Query, root syntax.Node, file string) []findings.SurfaceItem {
	var out []findings.SurfaceItem
	for _, q := range queries {
		out = append(out, q.Enumerate(root, file)...)
	}
	StampIDs(out)
	return out
}

// StampIDs fills the stable id of any item that does not yet have one. It is
// idempotent, so emitting boundaries (the sensor, the MCP handler) can call it
// without caring whether a provider already stamped.
func StampIDs(items []findings.SurfaceItem) {
	for i := range items {
		if items[i].ID == "" {
			items[i].ID = StableID(items[i].File, items[i].Line, items[i].Category)
		}
	}
}

// Verdict is the agent's judgment on one surface item after reasoning over it.
type Verdict string

const (
	VerdictVulnerable    Verdict = "vulnerable"
	VerdictNotVulnerable Verdict = "not_vulnerable"
	VerdictUncertain     Verdict = "uncertain"
)

// Confirmation is what the agent returns after reasoning over a surface item.
// It carries everything codefit needs to integrate the verdict statelessly: the
// surface id (revalidated by recomputation) plus the anchor (file/line/category)
// and the agent's judgment. Severity is optional — the agent, which understands
// what resource is exposed, judges it; codefit falls back to a class default.
type Confirmation struct {
	SurfaceID  string            `json:"surface_id"`
	Category   string            `json:"category"`
	File       string            `json:"file"`
	Line       int               `json:"line"`
	Verdict    Verdict           `json:"verdict"`
	Reasoning  string            `json:"reasoning"`
	Confidence float64           `json:"confidence"`
	Severity   findings.Severity `json:"severity,omitempty"`
}

// Integration is the result of folding the agent's verdicts into the report.
// Each verdict lands in exactly one bucket: Findings (vulnerable, probabilistic),
// Dismissed (not_vulnerable, kept for traceability, no score impact), Uncertain
// (flagged for human review — developer autonomy), Invalid (the surface id did
// not recompute, so the verdict refers to nothing codefit can anchor).
type Integration struct {
	Findings  []findings.Finding
	Dismissed []Confirmation
	Uncertain []Confirmation
	Invalid   []Confirmation
}

// Integrate folds agent verdicts into the report. It is a pure function of the
// confirmations passed in — codefit keeps no session. A vulnerable verdict
// becomes a probabilistic finding (confidence < 1.0 always) anchored to the
// surface item; the id is revalidated by recomputing StableID.
func Integrate(confs []Confirmation) Integration {
	var in Integration
	for _, c := range confs {
		if c.SurfaceID != StableID(c.File, c.Line, c.Category) {
			in.Invalid = append(in.Invalid, c)
			continue
		}
		switch c.Verdict {
		case VerdictVulnerable:
			in.Findings = append(in.Findings, findingFrom(c))
		case VerdictNotVulnerable:
			in.Dismissed = append(in.Dismissed, c)
		case VerdictUncertain:
			in.Uncertain = append(in.Uncertain, c)
		default:
			in.Invalid = append(in.Invalid, c)
		}
	}
	return in
}

// findingFrom builds the probabilistic finding for a confirmed-vulnerable item.
func findingFrom(c Confirmation) findings.Finding {
	return findings.Finding{
		ID:            strings.ToUpper(c.Category),
		Dimension:     findings.DimensionSecurity,
		Severity:      severityFor(c),
		File:          c.File,
		Line:          c.Line,
		Title:         titleFor(c.Category),
		Description:   titleFor(c.Category),
		Reasoning:     c.Reasoning,
		Confidence:    clampConfidence(c.Confidence),
		Probabilistic: true,
	}
}

// clampConfidence enforces the invariant that a reasoned (probabilistic) finding
// is never reported with full certainty — that is reserved for deterministic
// matches.
func clampConfidence(c float64) float64 {
	if c >= 1.0 {
		return 0.99
	}
	return c
}

var validSeverity = map[findings.Severity]bool{
	findings.SeverityCritical: true, findings.SeverityHigh: true,
	findings.SeverityMedium: true, findings.SeverityLow: true, findings.SeverityInfo: true,
}

// defaultSeverity is the class fallback when the agent does not provide one.
var defaultSeverity = map[string]findings.Severity{
	"idor":      findings.SeverityHigh,
	"authz":     findings.SeverityHigh,
	"overfetch": findings.SeverityMedium,
}

// severityFor uses the agent's severity when given (it judged the concrete
// resource exposed); otherwise the class default, otherwise medium.
func severityFor(c Confirmation) findings.Severity {
	if validSeverity[c.Severity] {
		return c.Severity
	}
	if d, ok := defaultSeverity[c.Category]; ok {
		return d
	}
	return findings.SeverityMedium
}

var titles = map[string]string{
	"idor":      "Possible IDOR (confirmed by agent reasoning)",
	"authz":     "Possible broken authorization (confirmed by agent reasoning)",
	"overfetch": "Possible sensitive-data over-fetching (confirmed by agent reasoning)",
}

func titleFor(category string) string {
	if t, ok := titles[category]; ok {
		return t
	}
	return "Surface item confirmed by agent reasoning: " + category
}

// Summary renders the complete-audit metric for a category: how many surface
// items were enumerated and how the agent's verdicts split. It reports complete
// coverage (every enumerated item reasoned), not a sample.
func Summary(category string, enumerated int, in Integration) string {
	return fmt.Sprintf("%s: %d enumerated, %d vulnerable, %d dismissed, %d uncertain",
		strings.ToUpper(category), enumerated, len(in.Findings), len(in.Dismissed), len(in.Uncertain))
}
