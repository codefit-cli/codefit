package mcp

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/codefit-cli/codefit/internal/core/baseline"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/scope"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// The refusal reasons codefit-baseline-record-verdict reports (D5). A refused
// entry is NAMED with one of these, never silently dropped.
const (
	// ReasonSurfaceIDMismatch means the submitted surface_id does not recompute
	// from (file, line, category) — the cheap pre-check, no re-run needed.
	ReasonSurfaceIDMismatch = "surface_id_mismatch"
	// ReasonNoSurfaceItemAtAnchor means the id IS internally consistent, but a
	// fresh re-analysis enumerated no item at that anchor right now — moved,
	// fixed, or invented.
	ReasonNoSurfaceItemAtAnchor = "no_surface_item_at_anchor"
	// ReasonUnknownVerdict means Verdict is not one of the three surface.Verdict
	// values.
	ReasonUnknownVerdict = "unknown_verdict"
	// ReasonAnalysisFailed would name a per-file re-analysis failure. Declared
	// for the response contract's completeness; today runSecurity's walk fails
	// the WHOLE batch on any file error (a single aggregate error, handled as
	// the whole-run failure below), so this reason is unreachable through the
	// current handler — the same "declared, not yet reachable" shape as
	// surfaceCoverageFor's zero case (scan.go).
	ReasonAnalysisFailed = "analysis_failed"
)

// BaselineRecordVerdictRequest carries a batch of agent verdicts on surface
// items to persist. Verdicts reuse surface.Confirmation's shape — the same
// contract codefit-confirm-surface already accepts — so an agent that already
// reasoned through confirm-surface can persist the same verdicts here
// unchanged. Root+Language identify the project: D5's re-run needs both.
type BaselineRecordVerdictRequest struct {
	Root     string                 `json:"root"`
	Language string                 `json:"language"`
	Verdicts []surface.Confirmation `json:"verdicts"`
}

// PersistedVerdict names one verdict that was recorded, anchored by the FRESH
// item's content-hash fingerprint (never the line-based surface id).
type PersistedVerdict struct {
	Fingerprint string `json:"fingerprint"`
	File        string `json:"file"`
	Category    string `json:"category"`
	Verdict     string `json:"verdict"`
}

// RefusedVerdict names one verdict that was NOT recorded, and why (D5: refused
// and reported, never silently dropped).
type RefusedVerdict struct {
	SurfaceID string `json:"surface_id"`
	File      string `json:"file"`
	Category  string `json:"category"`
	Reason    string `json:"reason"`
}

// BaselineRecordVerdictResponse reports what was persisted and what was
// refused, explicit and per-entry — a batch is never all-or-nothing.
type BaselineRecordVerdictResponse struct {
	Persisted []PersistedVerdict `json:"persisted"`
	Refused   []RefusedVerdict   `json:"refused"`
	Note      string             `json:"note"`
}

// HandleBaselineRecordVerdict persists agent verdicts after re-validating each
// against a fresh re-analysis of its file (D5): a verdict whose item does not
// exist right now is refused, never silently dropped. Recording a verdict
// NEVER silences the item (D1) and NEVER overwrites a conflicting previous
// verdict on the same fp (D2, baseline.Item.InConflict) — see
// baseline.Baseline.RecordVerdict, which this handler is a thin adapter over.
//
// An empty batch returns early WITHOUT running any analysis: scope.Of(nil)
// resolves to scope.Full() (an empty scope is never read as "audit nothing"),
// so falling through here would silently full-scan the whole project for a
// batch that asked for nothing.
func HandleBaselineRecordVerdict(req BaselineRecordVerdictRequest) (BaselineRecordVerdictResponse, error) {
	if len(req.Verdicts) == 0 {
		return BaselineRecordVerdictResponse{Note: "no verdicts submitted — nothing to record"}, nil
	}

	helpers := recognizedHelpers(req.Root, req.Language)
	res, err := runSecurity(req.Root, req.Language, helpers, scope.Of(distinctFiles(req.Verdicts)))
	if err != nil {
		return BaselineRecordVerdictResponse{}, fmt.Errorf("re-analysing before recording verdicts: %w", err)
	}
	fresh := indexSurfaceByAnchor(res.Surface)

	path := filepath.Join(req.Root, baseline.Name)
	b, err := baseline.Load(path)
	if err != nil {
		return BaselineRecordVerdictResponse{}, fmt.Errorf("loading baseline: %w", err)
	}

	at := time.Now().UTC().Format("2006-01-02")
	var persisted []PersistedVerdict
	var refused []RefusedVerdict
	changed := false
	for _, c := range req.Verdicts {
		if c.SurfaceID != surface.StableID(c.File, c.Line, c.Category) {
			refused = append(refused, RefusedVerdict{SurfaceID: c.SurfaceID, File: c.File, Category: c.Category, Reason: ReasonSurfaceIDMismatch})
			continue
		}
		item, ok := fresh[c.SurfaceID]
		if !ok {
			refused = append(refused, RefusedVerdict{SurfaceID: c.SurfaceID, File: c.File, Category: c.Category, Reason: ReasonNoSurfaceItemAtAnchor})
			continue
		}
		if !validVerdict(c.Verdict) {
			refused = append(refused, RefusedVerdict{SurfaceID: c.SurfaceID, File: c.File, Category: c.Category, Reason: ReasonUnknownVerdict})
			continue
		}
		v := baseline.AgentVerdict{Verdict: c.Verdict, Reasoning: c.Reasoning, Confidence: c.Confidence, Severity: c.Severity, At: at}
		b.RecordVerdict(item.Fingerprint, item.Category, item.File, item.Snippet, v)
		changed = true
		persisted = append(persisted, PersistedVerdict{Fingerprint: item.Fingerprint, File: item.File, Category: item.Category, Verdict: string(c.Verdict)})
	}

	if changed {
		if err := b.Save(path); err != nil {
			return BaselineRecordVerdictResponse{}, fmt.Errorf("saving baseline: %w", err)
		}
	}

	return BaselineRecordVerdictResponse{
		Persisted: persisted,
		Refused:   refused,
		Note: fmt.Sprintf("%d verdict(s) recorded, %d refused. Recording does not silence — the item still "+
			"appears on every scan until a human accepts it via codefit-baseline-accept.", len(persisted), len(refused)),
	}, nil
}

// distinctFiles returns the unique project-relative files a verdict batch
// touches, so the D5 re-run is scoped to just what it needs to re-validate.
func distinctFiles(cs []surface.Confirmation) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range cs {
		if c.File == "" || seen[c.File] {
			continue
		}
		seen[c.File] = true
		out = append(out, c.File)
	}
	return out
}

// indexSurfaceByAnchor indexes a fresh re-analysis's surface items by their
// stable id (file+line+category) — D5's proof that a submitted verdict's item
// exists RIGHT NOW, not merely that the request is internally self-consistent.
func indexSurfaceByAnchor(items []findings.SurfaceItem) map[string]findings.SurfaceItem {
	idx := make(map[string]findings.SurfaceItem, len(items))
	for _, it := range items {
		idx[it.ID] = it
	}
	return idx
}

func validVerdict(v surface.Verdict) bool {
	switch v {
	case surface.VerdictVulnerable, surface.VerdictNotVulnerable, surface.VerdictUncertain:
		return true
	default:
		return false
	}
}
