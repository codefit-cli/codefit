package mcp

import (
	"path/filepath"
	"sort"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/registry"
)

// This file implements the surface tool HANDLERS and their JSON contracts. The
// stdio transport is built (see NewServer/Serve) and unmarshals a tool call into
// these requests, marshalling the responses back; HTTP/SSE is still unbuilt.
// Keeping the logic here means the transport only has to dispatch to handlers
// that already work and are tested.

// FileInput is one source file a surface tool reasons over (path + content), so
// the tool stays stateless — the caller passes the bytes, codefit reads nothing.
type FileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// SurfaceIDORRequest is the input to codefit-surface-idor.
type SurfaceIDORRequest struct {
	Files []FileInput `json:"files"`
}

// SurfaceResponse is the §11 surface contract: the items the agent must reason.
type SurfaceResponse struct {
	Surface []findings.SurfaceItem `json:"surface"`
}

// HandleSurfaceIDOR enumerates the IDOR surface across the given files and
// returns it in the canonical JSON contract. Files that are not handled by a
// provider (or carry no IDOR surface) contribute nothing.
func HandleSurfaceIDOR(req SurfaceIDORRequest) (SurfaceResponse, error) {
	return handleSurface(req, string(surface.CategoryIDOR))
}

// HandleSurfaceAuthz enumerates the broken-authorization surface across the given
// files. It orders the items so the actionable ones — where no known authz helper
// was detected — come FIRST, the rest after (where a check exists, the agent
// verifies sufficiency). It does NOT reduce the list: the complete enumeration is
// preserved, only reordered, so findings surface instead of being buried in
// volume. Ordering by a fact is not a severity judgment — codefit does not say
// "no check is worse", it just surfaces the fact for the agent.
func HandleSurfaceAuthz(req SurfaceIDORRequest) (SurfaceResponse, error) {
	resp, err := handleSurface(req, string(surface.CategoryAuthz))
	if err != nil {
		return resp, err
	}
	sortUncheckedFirst(resp.Surface, "known_authz_detected")
	return resp, nil
}

// HandleSurfaceOverfetch enumerates the over-fetching surface and orders by
// STRUCTURAL CERTAINTY, reusing the queryable facts — it does NOT filter:
//
//	1st  local find, no select   (structurally confirmed over-fetch — actionable)
//	2nd  local find, with select (limited)
//	3rd  frontier (service — codefit cannot see the find; kept last, honest)
//
// The complete enumeration is preserved; only the order changes, lowering the
// uncertain (frontier) without dropping them — the unchecked-first principle on
// the local/frontier axis (ADR 0005). Ordering by a fact is not severity:
// codefit ranks by what it can assert with most certainty, the agent judges.
func HandleSurfaceOverfetch(req SurfaceIDORRequest) (SurfaceResponse, error) {
	resp, err := handleSurface(req, string(surface.CategoryOverfetch))
	if err != nil {
		return resp, err
	}
	sort.SliceStable(resp.Surface, func(i, j int) bool {
		return overfetchCertaintyRank(resp.Surface[i]) < overfetchCertaintyRank(resp.Surface[j])
	})
	return resp, nil
}

// overfetchCertaintyRank ranks an over-fetch item by how certainly codefit can
// assert over-fetching from structure alone (lower = more certain, ordered first).
func overfetchCertaintyRank(it findings.SurfaceItem) int {
	switch {
	case it.StructuralFacts["local_access_detected"] && !it.StructuralFacts["field_limiting_detected"]:
		return 0 // local find with no select — structurally confirmed
	case it.StructuralFacts["local_access_detected"]:
		return 1 // local find that limits fields
	default:
		return 2 // frontier — codefit could not see the find
	}
}

// HandleSurfaceNPlus1 enumerates the N+1 (DB-201) surface and orders by
// STRUCTURAL CERTAINTY, reusing the queryable facts — it does NOT filter
// (ADR 0005): a loop over three literal elements is enumerated exactly like a
// loop over an unbounded query result.
//
//	1st  local + sequential await — the worst, most certain shape
//	2nd  local + Promise.all-wrapped (concurrent) — still N queries
//	3rd  frontier (a service/repository call — codefit cannot see the query;
//	     kept last, honest, never dropped)
func HandleSurfaceNPlus1(req SurfaceIDORRequest) (SurfaceResponse, error) {
	resp, err := handleSurface(req, string(surface.CategoryNPlus1))
	if err != nil {
		return resp, err
	}
	sort.SliceStable(resp.Surface, func(i, j int) bool {
		return nplus1CertaintyRank(resp.Surface[i]) < nplus1CertaintyRank(resp.Surface[j])
	})
	return resp, nil
}

// nplus1CertaintyRank ranks an N+1 item by how certainly codefit can assert
// the query-in-loop shape from structure alone (lower = more certain, ordered
// first). Mirrors overfetchCertaintyRank's discipline: order by fact, never
// filter — the frontier is ranked LAST but never dropped.
func nplus1CertaintyRank(it findings.SurfaceItem) int {
	switch {
	case !it.StructuralFacts["local_access_detected"]:
		return 3 // frontier — codefit could not see the query
	case it.StructuralFacts["promise_all_wrapped"]:
		return 2 // local, concurrent (Promise.all) — still N queries
	case it.StructuralFacts["awaited_in_loop"]:
		return 0 // local + sequential await — the worst, most certain shape
	default:
		return 1 // local, neither sequentially awaited nor Promise.all-wrapped
	}
}

// sortUncheckedFirst stably orders items so those whose given structural fact is
// false (the actionable case: no known check/limit detected) come before those
// where it is true. Ordering by a fact is not a severity judgment.
func sortUncheckedFirst(items []findings.SurfaceItem, fact string) {
	sort.SliceStable(items, func(i, j int) bool {
		return !items[i].StructuralFacts[fact] && items[j].StructuralFacts[fact]
	})
}

// handleSurface runs the providers over the files and returns the surface items
// of one category — the shared body of the codefit-surface-* tools.
func handleSurface(req SurfaceIDORRequest, category string) (SurfaceResponse, error) {
	items := make([]findings.SurfaceItem, 0)
	for _, f := range req.Files {
		p := providerFor(f.Path)
		if p == nil {
			continue
		}
		all, err := p.AnalyzeSurface(providers.SourceFile{Path: f.Path, Content: []byte(f.Content)})
		if err != nil {
			return SurfaceResponse{}, err
		}
		for _, it := range all {
			if it.Category == category {
				items = append(items, it)
			}
		}
	}
	surface.StampIDs(items) // idempotent: guarantees every emitted item is anchorable
	return SurfaceResponse{Surface: items}, nil
}

// ConfirmSurfaceRequest carries the agent's verdicts back to codefit.
type ConfirmSurfaceRequest struct {
	Confirmations []surface.Confirmation `json:"confirmations"`
}

// ConfirmSurfaceResponse is the integration result: probabilistic findings plus
// the traceability buckets. Stateless — built purely from the verdicts sent.
type ConfirmSurfaceResponse struct {
	Findings  []findings.Finding     `json:"findings"`
	Dismissed []surface.Confirmation `json:"dismissed"`
	Uncertain []surface.Confirmation `json:"uncertain"`
	Invalid   []surface.Confirmation `json:"invalid"`
}

// HandleConfirmSurface integrates the agent's verdicts: codefit-confirm-surface.
// It recomputes each surface id to validate the verdict before integrating, and
// keeps no session.
func HandleConfirmSurface(req ConfirmSurfaceRequest) ConfirmSurfaceResponse {
	in := surface.Integrate(req.Confirmations)
	return ConfirmSurfaceResponse{
		Findings:  in.Findings,
		Dismissed: in.Dismissed,
		Uncertain: in.Uncertain,
		Invalid:   in.Invalid,
	}
}

// providerFor resolves the language provider for a file by extension,
// filtered to entries the registry EXPOSES for surface tools
// (Entry.Exposure.SurfaceTools) — the MCP adapter is a CONSUMER of the
// registry, never a second source of the language->provider mapping (D2).
// Today only TypeScript is exposed for surface tools.
func providerFor(path string) providers.LanguageProvider {
	e, ok := registry.ByExtension(filepath.Ext(path))
	if !ok || !e.Exposure.SurfaceTools {
		return nil
	}
	return e.New(nil)
}
