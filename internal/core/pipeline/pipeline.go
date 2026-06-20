package pipeline

import (
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/findings"
)

// FilterLayer identifies a tier of the filtering pyramid, ordered from cheapest
// (0) to most expensive (3). Cheaper layers run first and only what they cannot
// conclude is escalated to the next (PRD §15).
type FilterLayer int

const (
	LayerChanges  FilterLayer = 0 // unchanged files (via --since or cache hash)
	LayerPatterns FilterLayer = 1 // regex / obvious patterns
	LayerAST      FilterLayer = 2 // tree-sitter structural analysis
	LayerLLM      FilterLayer = 3 // semantic LLM reasoning
)

// PipelineResult is what a layer returns: the findings it concluded and the
// files/fragments it escalates to the next layer.
type PipelineResult struct {
	PassedToNextLayer []string
	Findings          []findings.Finding
}

// LayerProcessor is one tier of the pyramid.
type LayerProcessor interface {
	Layer() FilterLayer
	Process(files []string, ctx auditctx.AuditContext) (PipelineResult, error)
}

// Pipeline orchestrates the layers in order, threading each layer's escalated
// files into the next, and early-exits before the LLM layer when the
// accumulated findings already satisfy --fail-on (so no tokens are spent on
// code that will block anyway).
type Pipeline struct {
	Layers []LayerProcessor
}

// Run executes the pipeline and returns the aggregated findings.
func (p *Pipeline) Run(files []string, ctx auditctx.AuditContext) ([]findings.Finding, error) {
	var all []findings.Finding
	current := files
	for _, layer := range p.Layers {
		if layer.Layer() == LayerLLM && meetsFailOn(all, ctx.FailOn) {
			break // skip the expensive layer; we already fail
		}
		res, err := layer.Process(current, ctx)
		if err != nil {
			return all, err
		}
		all = append(all, res.Findings...)
		current = res.PassedToNextLayer
	}
	return all, nil
}

// severityRank orders severities for --fail-on comparisons.
var severityRank = map[findings.Severity]int{
	findings.SeverityInfo:     0,
	findings.SeverityLow:      1,
	findings.SeverityMedium:   2,
	findings.SeverityHigh:     3,
	findings.SeverityCritical: 4,
}

// meetsFailOn reports whether any finding reaches the failOn threshold
// (critical|high|medium). An empty or unknown threshold never triggers.
func meetsFailOn(fs []findings.Finding, failOn string) bool {
	threshold, ok := severityRank[findings.Severity(failOn)]
	if !ok {
		return false
	}
	for _, f := range fs {
		if severityRank[f.Severity] >= threshold {
			return true
		}
	}
	return false
}
