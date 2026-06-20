package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/codefit-cli/codefit/internal/core/findings"
)

// dimensionLabel maps a dimension to its display label for the plain renderer.
var dimensionLabel = map[findings.Dimension]string{
	findings.DimensionSecurity:   "Security",
	findings.DimensionReview:     "Code Review",
	findings.DimensionDB:         "Database",
	findings.DimensionComplexity: "Complexity",
	findings.DimensionTests:      "Tests / Regression",
}

// PlainRenderer writes a human-readable, pipe-friendly text report. It is the
// default renderer in both interactive and non-interactive contexts until the
// TUI exists.
type PlainRenderer struct{}

func (PlainRenderer) Render(w io.Writer, r AuditReport) error {
	var b strings.Builder

	fmt.Fprintf(&b, "codefit %s · %s\n\n", r.CodefitVersion, r.Project)
	fmt.Fprintln(&b, strings.Repeat("━", 39))
	fmt.Fprintf(&b, "  SCORE GLOBAL          %d / 100\n", r.Score.Global)
	fmt.Fprintln(&b, strings.Repeat("━", 39))

	for _, dim := range orderedDimensions(r.Score.ByDimension) {
		fmt.Fprintf(&b, "  %-20s %3d / 100\n", dimensionLabel[dim], r.Score.ByDimension[dim])
	}
	fmt.Fprintln(&b, strings.Repeat("━", 39))

	if r.Blocked {
		reason := r.BlockReason
		if reason == "" {
			reason = "critical security findings without explicit consent"
		}
		fmt.Fprintf(&b, "\n⛔ BLOCKED: %s\n", reason)
	}

	if len(r.Findings) > 0 {
		fmt.Fprintf(&b, "\nFINDINGS (%d)\n", len(r.Findings))
		for _, f := range r.Findings {
			renderFinding(&b, f)
		}
	}

	_, err := io.WriteString(w, b.String())
	return err
}

func renderFinding(b *strings.Builder, f findings.Finding) {
	fmt.Fprintf(b, "  [%s] %s  (%s)\n", f.ID, f.Title, f.Severity)
	if f.File != "" {
		loc := f.File
		if f.Line > 0 {
			loc = fmt.Sprintf("%s:%d", f.File, f.Line)
		}
		fmt.Fprintf(b, "            %s\n", loc)
	}
	if f.Suggestion != "" {
		fmt.Fprintf(b, "            → %s\n", f.Suggestion)
	}
}

// orderedDimensions returns the dimensions present in the score map in a stable
// canonical order.
func orderedDimensions(byDim map[findings.Dimension]int) []findings.Dimension {
	canonical := []findings.Dimension{
		findings.DimensionSecurity,
		findings.DimensionReview,
		findings.DimensionDB,
		findings.DimensionComplexity,
		findings.DimensionTests,
	}
	var out []findings.Dimension
	for _, d := range canonical {
		if _, ok := byDim[d]; ok {
			out = append(out, d)
		}
	}
	// Append any non-canonical dimensions deterministically.
	var extra []string
	for d := range byDim {
		if !slicesContains(canonical, d) {
			extra = append(extra, string(d))
		}
	}
	sort.Strings(extra)
	for _, d := range extra {
		out = append(out, findings.Dimension(d))
	}
	return out
}

func slicesContains(s []findings.Dimension, v findings.Dimension) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
