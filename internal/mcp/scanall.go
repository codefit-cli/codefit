package mcp

import (
	"github.com/codefit-cli/codefit/internal/core/report"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// ScanAllRequest is the input to codefit-scan-all: a project root and language.
// codefit walks the project, runs the deterministic sensor and the surface
// queries, and returns the complete per-endpoint picture.
type ScanAllRequest struct {
	Root     string `json:"root"`
	Language string `json:"language"`
}

// ScanAllResponse is the agent-first synthesis: endpoints with their concerns
// (deterministic + surface) grouped and ordered by certainty. JSON for the agent
// to reason; a human renderer (export-report) is a future opt-in over this same
// canonical data (PRD §27), not built here.
type ScanAllResponse struct {
	Endpoints []report.EndpointReport `json:"endpoints"`
	Summary   ScanAllSummary          `json:"summary"`
}

// ScanAllSummary is the at-a-glance count, not a judgment.
type ScanAllSummary struct {
	Endpoints             int `json:"endpoints"`
	DeterministicFindings int `json:"deterministic_findings"`
	SurfaceItems          int `json:"surface_items"`
	CertainConcerns       int `json:"certain_concerns"`
}

// HandleScanAll runs the full audit over the project and returns the per-endpoint
// synthesis. It reuses the real security sensor (the deterministic rules plus the
// three surface queries already run together there) and groups the result by
// endpoint — it adds no detection, only the aggregation.
func HandleScanAll(req ScanAllRequest) (ScanAllResponse, error) {
	res, err := runSecurity(req.Root, req.Language)
	if err != nil {
		return ScanAllResponse{}, err
	}

	endpoints := report.AggregateEndpoints(res.Findings, res.Surface)
	certain := 0
	for _, ep := range endpoints {
		certain += ep.CertainConcerns
	}
	return ScanAllResponse{
		Endpoints: endpoints,
		Summary: ScanAllSummary{
			Endpoints:             len(endpoints),
			DeterministicFindings: len(res.Findings),
			SurfaceItems:          len(res.Surface),
			CertainConcerns:       certain,
		},
	}, nil
}

// providerForLanguage resolves a provider by language name — the MCP adapter is
// the single place that maps language → provider (the core never does).
func providerForLanguage(lang string) providers.LanguageProvider {
	switch lang {
	case "typescript", "ts", "tsx":
		return typescript.New()
	default:
		return nil
	}
}
