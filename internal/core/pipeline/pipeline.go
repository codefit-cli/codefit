package pipeline

import (
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/findings"
)

// FilterLayer identifies a tier of the filtering pyramid, ordered from cheapest
// (0) to most expensive (2). Cheaper layers run first and only what they cannot
// conclude is escalated to the next (PRD section 15). codefit never goes past
// layer 2: it produces deterministic findings and maps surface; the agent does
// the layer-3 reasoning over the surface with its own LLM.
type FilterLayer int

const (
	LayerChanges  FilterLayer = 0 // unchanged files (via --since or cache hash)
	LayerPatterns FilterLayer = 1 // regex / obvious patterns
	LayerAST      FilterLayer = 2 // AST: deterministic findings + surface mapping
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
// files into the next.
type Pipeline struct {
	Layers []LayerProcessor
}

// Run executes the pipeline and returns the aggregated findings.
func (p *Pipeline) Run(files []string, ctx auditctx.AuditContext) ([]findings.Finding, error) {
	var all []findings.Finding
	current := files
	for _, layer := range p.Layers {
		res, err := layer.Process(current, ctx)
		if err != nil {
			return all, err
		}
		all = append(all, res.Findings...)
		current = res.PassedToNextLayer
	}
	return all, nil
}
