package mcp

import (
	"fmt"
	"path/filepath"

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

// ScanAllResponse is the agent-first synthesis as an ACTIONABLE summary, not the
// raw item dump. Endpoints codefit resolved LOCALLY (≥1 deterministic or
// surface_confirmed concern) are returned WHOLE in Actionable; the frontier-only
// endpoints (every concern is surface_frontier — the data left the handler body)
// are only NAMED in FrontierPending and fetched on demand via codefit-scan-
// endpoint. This keeps the response small enough not to truncate, while declaring
// exactly what was left out and why (ADR 0008). The split criterion is the FACT
// local_access_detected, folded into CertainConcerns — not an arbitrary cut.
type ScanAllResponse struct {
	Summary         ScanAllSummary          `json:"summary"`
	Actionable      []report.EndpointReport `json:"actionable"`
	FrontierPending FrontierPending         `json:"frontier_pending"`
}

// FrontierPending declares the endpoints codefit did NOT resolve locally: the data
// left the handler body, so codefit concluded nothing and the agent must follow it
// in the code. They are named (not detailed) with a Note explaining why they are
// not detailed and how to fetch any of them. This is not hiding — it is
// prioritising while declaring the rest.
type FrontierPending struct {
	Count     int                       `json:"count"`
	Note      string                    `json:"note,omitempty"`
	Endpoints []report.FrontierEndpoint `json:"endpoints,omitempty"`
}

// ScanAllSummary is the at-a-glance count, not a judgment.
type ScanAllSummary struct {
	Endpoints             int `json:"endpoints"`
	DeterministicFindings int `json:"deterministic_findings"`
	SurfaceItems          int `json:"surface_items"`
	CertainConcerns       int `json:"certain_concerns"`
}

// HandleScanAll runs the full audit over the project and returns the actionable
// summary plus the named frontier-pending list. It reuses the real security sensor
// (the deterministic rules plus the three surface queries already run together
// there), groups the result by endpoint, and partitions by the local-resolution
// fact — it adds no detection, only the aggregation and the split.
func HandleScanAll(req ScanAllRequest) (ScanAllResponse, error) {
	res, err := runSecurity(req.Root, req.Language)
	if err != nil {
		return ScanAllResponse{}, err
	}

	endpoints := report.AggregateEndpoints(res.Findings, res.Surface)
	actionable, frontier := report.PartitionByResolution(endpoints)
	certain := 0
	for _, ep := range endpoints {
		certain += ep.CertainConcerns
	}
	return ScanAllResponse{
		Summary: ScanAllSummary{
			Endpoints:             len(endpoints),
			DeterministicFindings: len(res.Findings),
			SurfaceItems:          len(res.Surface),
			CertainConcerns:       certain,
		},
		Actionable: actionable,
		FrontierPending: FrontierPending{
			Count:     len(frontier),
			Note:      frontierNote(len(frontier), len(actionable)),
			Endpoints: frontier,
		},
	}, nil
}

// frontierNote phrases what the frontier-pending list means, honestly. When there
// are no frontier endpoints it is silent. When some endpoints were resolved and
// some are frontier, it explains they are named-only and how to fetch them. When
// NOTHING was resolved locally (all frontier), it states emphatically that codefit
// concluded nothing locally — this is NOT a clean result, every endpoint requires
// following the data in the code — the same principle as the frontier signal
// wording: absence of actionable items is not "clean".
func frontierNote(frontierCount, actionableCount int) string {
	if frontierCount == 0 {
		return ""
	}
	if actionableCount == 0 {
		return fmt.Sprintf("codefit concluded nothing locally: every one of these %d endpoint(s) is "+
			"frontier — the data leaves the handler body, so no local analysis could resolve it. This is "+
			"NOT a clean result; it means all of them require following the data in the code. Request "+
			"%s with any of these files for its full concerns.", frontierCount, ToolScanEndpoint)
	}
	return fmt.Sprintf("%d endpoint(s) are frontier-only: the data leaves the handler body, so codefit "+
		"concluded nothing locally about them — they require following the data in the code. They are named "+
		"here, not detailed (the detail would not save the trip to the code). Request %s with one of these "+
		"files for its full concerns.", frontierCount, ToolScanEndpoint)
}

// ScanEndpointRequest is the input to codefit-scan-endpoint: a project root,
// language, and the file (relative to root, as it appears in scan-all's File
// fields) to re-analyse on demand.
type ScanEndpointRequest struct {
	Root     string `json:"root"`
	Language string `json:"language"`
	File     string `json:"file"`
}

// ScanEndpointResponse carries the full per-endpoint detail for one file:
// every handler in it with all its concerns (signals, reason_to_review, certainty,
// fact fields) — the same concern contract as scan-all. Found is false when the
// file has no auditable concerns (not a route handler, or nothing to enumerate).
type ScanEndpointResponse struct {
	File      string                  `json:"file"`
	Found     bool                    `json:"found"`
	Endpoints []report.EndpointReport `json:"endpoints,omitempty"`
	Note      string                  `json:"note,omitempty"`
}

// HandleScanEndpoint re-analyses a single file on demand and returns its endpoints
// with full detail. STATELESS: it re-runs the static analysis over the project and
// filters to the requested file — it retrieves nothing stored. codefit does not
// keep the surface items waiting to be asked for; it recomputes the request. Static
// analysis is cheap, and re-running the same pipeline guarantees the detail here is
// identical to what scan-all would have shown for that endpoint (ADR 0008).
func HandleScanEndpoint(req ScanEndpointRequest) (ScanEndpointResponse, error) {
	res, err := runSecurity(req.Root, req.Language)
	if err != nil {
		return ScanEndpointResponse{}, err
	}
	endpoints := report.AggregateEndpoints(res.Findings, res.Surface)
	want := filepath.Clean(req.File)
	var matched []report.EndpointReport
	for _, ep := range endpoints {
		if filepath.Clean(ep.File) == want {
			matched = append(matched, ep)
		}
	}
	if len(matched) == 0 {
		return ScanEndpointResponse{
			File:  req.File,
			Found: false,
			Note: "No auditable concerns for this file: it is not a route handler codefit enumerates, " +
				"or there was nothing to surface. This is a fact about the file, not a clearance.",
		}, nil
	}
	return ScanEndpointResponse{File: req.File, Found: true, Endpoints: matched}, nil
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
