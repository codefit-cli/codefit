package pipeline_test

import (
	"testing"

	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/pipeline"
)

// fakeLayer is a configurable LayerProcessor that records whether it ran.
type fakeLayer struct {
	layer  pipeline.FilterLayer
	emit   []findings.Finding
	passed []string
	ran    *bool
}

func (l fakeLayer) Layer() pipeline.FilterLayer { return l.layer }
func (l fakeLayer) Process(files []string, _ auditctx.AuditContext) (pipeline.PipelineResult, error) {
	if l.ran != nil {
		*l.ran = true
	}
	return pipeline.PipelineResult{PassedToNextLayer: l.passed, Findings: l.emit}, nil
}

func TestPipelineAggregatesFindings(t *testing.T) {
	p := &pipeline.Pipeline{Layers: []pipeline.LayerProcessor{
		fakeLayer{layer: pipeline.LayerPatterns, emit: []findings.Finding{{ID: "P1"}}, passed: []string{"a.go"}},
		fakeLayer{layer: pipeline.LayerAST, emit: []findings.Finding{{ID: "A1"}}, passed: []string{"a.go"}},
		fakeLayer{layer: pipeline.LayerLLM, emit: []findings.Finding{{ID: "L1"}}},
	}}
	got, err := p.Run([]string{"a.go"}, auditctx.AuditContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d findings, want 3: %+v", len(got), got)
	}
}

func TestPipelineEarlyExitSkipsLLM(t *testing.T) {
	llmRan := false
	p := &pipeline.Pipeline{Layers: []pipeline.LayerProcessor{
		fakeLayer{
			layer: pipeline.LayerAST,
			emit:  []findings.Finding{{ID: "SEC-001", Dimension: findings.DimensionSecurity, Severity: findings.SeverityCritical}},
		},
		fakeLayer{layer: pipeline.LayerLLM, emit: []findings.Finding{{ID: "L1"}}, ran: &llmRan},
	}}
	ctx := auditctx.AuditContext{FailOn: "critical"}
	got, err := p.Run([]string{"a.go"}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if llmRan {
		t.Error("LLM layer ran despite a blocking critical finding and --fail-on critical")
	}
	if len(got) != 1 {
		t.Errorf("got %d findings, want only the pre-LLM one", len(got))
	}
}

func TestPipelineNoEarlyExitWhenFailOnNotMet(t *testing.T) {
	llmRan := false
	p := &pipeline.Pipeline{Layers: []pipeline.LayerProcessor{
		fakeLayer{
			layer: pipeline.LayerAST,
			emit:  []findings.Finding{{ID: "REV-1", Dimension: findings.DimensionReview, Severity: findings.SeverityLow}},
		},
		fakeLayer{layer: pipeline.LayerLLM, emit: []findings.Finding{{ID: "L1"}}, ran: &llmRan},
	}}
	ctx := auditctx.AuditContext{FailOn: "critical"}
	if _, err := p.Run([]string{"a.go"}, ctx); err != nil {
		t.Fatal(err)
	}
	if !llmRan {
		t.Error("LLM layer should run when no finding meets --fail-on")
	}
}
