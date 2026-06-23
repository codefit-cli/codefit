package mcp

import (
	"path/filepath"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// This file implements the surface tool HANDLERS and their JSON contracts. The
// transport (stdio/HTTP-SSE) is still a skeleton (see Server); when it is built
// it will unmarshal a tool call into these requests and marshal the responses.
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
// files in the same contract.
func HandleSurfaceAuthz(req SurfaceIDORRequest) (SurfaceResponse, error) {
	return handleSurface(req, string(surface.CategoryAuthz))
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

// providerFor resolves the language provider for a file by extension. The MCP
// adapter is the single place that maps language → provider (the core never
// does); today only TypeScript carries surface queries.
func providerFor(path string) providers.LanguageProvider {
	switch filepath.Ext(path) {
	case ".ts", ".tsx":
		return typescript.New()
	default:
		return nil
	}
}
