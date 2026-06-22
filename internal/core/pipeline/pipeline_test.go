package pipeline_test

import (
	"slices"
	"testing"

	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/pipeline"
)

// fakeLayer is a configurable LayerProcessor that records the files it received.
type fakeLayer struct {
	layer    pipeline.FilterLayer
	emit     []findings.Finding
	passed   []string
	received *[]string
}

func (l fakeLayer) Layer() pipeline.FilterLayer { return l.layer }
func (l fakeLayer) Process(files []string, _ auditctx.AuditContext) (pipeline.PipelineResult, error) {
	if l.received != nil {
		*l.received = files
	}
	return pipeline.PipelineResult{PassedToNextLayer: l.passed, Findings: l.emit}, nil
}

func TestPipelineAggregatesFindings(t *testing.T) {
	p := &pipeline.Pipeline{Layers: []pipeline.LayerProcessor{
		fakeLayer{layer: pipeline.LayerPatterns, emit: []findings.Finding{{ID: "P1"}}, passed: []string{"a.go"}},
		fakeLayer{layer: pipeline.LayerAST, emit: []findings.Finding{{ID: "A1"}}, passed: []string{"a.go"}},
	}}
	got, err := p.Run([]string{"a.go"}, auditctx.AuditContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d findings, want 2: %+v", len(got), got)
	}
}

// TestPipelineThreadsEscalatedFiles checks each layer receives the previous
// layer's escalated files, not the original input.
func TestPipelineThreadsEscalatedFiles(t *testing.T) {
	var astGot []string
	p := &pipeline.Pipeline{Layers: []pipeline.LayerProcessor{
		fakeLayer{layer: pipeline.LayerPatterns, passed: []string{"suspicious.go"}},
		fakeLayer{layer: pipeline.LayerAST, received: &astGot},
	}}
	if _, err := p.Run([]string{"a.go", "b.go"}, auditctx.AuditContext{}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(astGot, []string{"suspicious.go"}) {
		t.Errorf("AST layer received %v, want only the escalated [suspicious.go]", astGot)
	}
}
