package mcp

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surfaceindex"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/schemasource"
	dbsensor "github.com/codefit-cli/codefit/internal/sensors/db"
)

// ScanDBRequest is the input to codefit-scan-db: a project root and language.
// Detail names surface item ids (from a prior Surface index, or from
// scan-all's db.surface) whose full findings.SurfaceItem the caller wants —
// mirrors CoverageRequest.Detail. Omit it and the answer is the index alone.
type ScanDBRequest struct {
	Root     string   `json:"root"`
	Language string   `json:"language"`
	Detail   []string `json:"detail,omitempty"`
}

// ScanDBResponse is the standalone DB-structure result. Measured distinguishes
// "audited" from "not audited": when false, Note says why (no schema_paths, no
// schema parser, or the sensor is disabled) and Findings/Surface are empty — it is
// NOT a "clean" result. Score is a plain dimension score (0-100), like
// codefit-scan-security; this tool does not compute by_dimension.
//
// Surface is the LIGHT index (surfaceindex.Entry), always complete — nothing is
// withheld (Count/Withheld/WithheldNote, mirroring DBSection — design D1/D4).
// Detail carries the full findings.SurfaceItem for every requested id that
// matched; Unrecognized names every id that matched nothing, with a note (D3):
// codefit is stateless and cannot tell "never existed" from "the schema moved".
type ScanDBResponse struct {
	Measured     bool                 `json:"measured"`
	Note         string               `json:"note,omitempty"`
	Findings     []findings.Finding   `json:"findings"`
	Surface      []surfaceindex.Entry `json:"surface"`
	Count        int                  `json:"count"`
	Withheld     int                  `json:"withheld"`
	WithheldNote string               `json:"withheld_note,omitempty"`
	Score        int                  `json:"score"`
	// Detail carries the full item for every requested id that matched, byte
	// for byte the pre-change flat Surface shape.
	Detail []findings.SurfaceItem `json:"detail,omitempty"`
	// Unrecognized names every requested id that matched nothing. Naming it is
	// the point: an empty success would say the item has nothing to declare,
	// which is a different and false answer from "there is no such item".
	Unrecognized     []string `json:"unrecognized,omitempty"`
	UnrecognizedNote string   `json:"unrecognized_note,omitempty"`
	// Bytes/IndexBytes/OverBudget/BudgetNote declare this response's own size,
	// wired only once Detail exists (design D5) — the exact shape that made
	// CoverageResponse under-declare its size before it carried Bytes/IndexBytes
	// (#1664, closed in S3): bytes is measured LAST, over index + detail.
	Bytes      int    `json:"bytes"`
	IndexBytes int    `json:"index_bytes"`
	OverBudget bool   `json:"over_budget,omitempty"`
	BudgetNote string `json:"budget_note,omitempty"`
}

// HandleScanDB runs the DB sensor over the project and returns its structural
// findings + surface. A thin adapter: it resolves the provider, loads the config,
// and delegates to the DB sensor (which reads the schema, runs the core rules, and
// stamps identity). Standalone — it touches nothing in scan-all.
func HandleScanDB(req ScanDBRequest) (ScanDBResponse, error) {
	return handleScanDBBudgeted(req, ResponseBudgetBytes)
}

// handleScanDBBudgeted is HandleScanDB with the response budget made an
// argument instead of a constant. A SEAM, not an option (same reasoning as
// handleCoverageBudgeted/handleScanAllBudgeted): production has exactly one
// caller and passes ResponseBudgetBytes. It exists so a test can drive the
// REAL sensor's REAL output through a lowered budget instead of synthesising
// a response big enough by hand.
func handleScanDBBudgeted(req ScanDBRequest, budget int) (ScanDBResponse, error) {
	// A missing config is fine (no schema_paths → not measured); a present-but-invalid
	// one is a hard error, the same rule as the security scan path.
	cfg, err := config.LoadOptional(filepath.Join(req.Root, ".codefit.yaml"))
	if err != nil {
		return ScanDBResponse{}, fmt.Errorf("loading project config: %w", err)
	}

	// Resolve the schema parser by the INPUT's shape (.prisma / .sql), not the app
	// language (ADR 0018). If schema_paths name a type with no parser, that is a
	// not-measured note. With no schema_paths at all, the sensor reports it.
	var parser providers.SchemaParser
	if cfg != nil && len(cfg.Database.SchemaPaths) > 0 {
		p, note := schemasource.ParserForPaths(req.Root, cfg.Database.SchemaPaths, cfg.Database.Type)
		if p == nil {
			return ScanDBResponse{Measured: false, Note: note}, nil
		}
		parser = p
	}
	ctx := auditctx.AuditContext{ProjectRoot: req.Root, Language: req.Language, Config: cfg}

	r, err := dbsensor.New(parser).Audit(ctx)
	if err != nil {
		return ScanDBResponse{}, err
	}
	if !r.Measured {
		return ScanDBResponse{Measured: false, Note: r.Note}, nil
	}
	return scanDBResponse(r, req.Detail, budget), nil
}

const (
	dbUnrecognizedNote = "These ids matched no surface item in this scan. codefit is stateless and cannot tell " +
		"WHY: the id may never have existed, or the schema may have changed between calls and the item is " +
		"genuinely gone — this response cannot distinguish the two. Read the index in this same response for " +
		"the ids that exist now."
	dbIndexOverBudgetNote = "This index is over the response budget and is still complete. db.surface has no " +
		"ranking axis to withhold by (design D4), so nothing is dropped — this is a fact about the schema's " +
		"size, not an authoring problem to fix."
	dbDetailOverBudgetNote = "This response is over the response budget because of the detail you asked for, " +
		"and it is still complete: every id you named came back whole and nothing was withheld. Ask for fewer " +
		"ids per call if your client caps a response."
)

// scanDBResponse projects a MEASURED db sensor result into the answer the
// agent receives: the always-complete light index, plus the full detail for
// any requested id. One place builds this shape, mirroring coverageResponse
// (scan.go) — the index and the detail are two views of one value, not two
// code paths that have to agree.
func scanDBResponse(r dbsensor.Result, want []string, budget int) ScanDBResponse {
	index, count := surfaceindex.Index(r.Res.Surface)
	resp := ScanDBResponse{
		Measured:     true,
		Note:         r.Note,
		Findings:     r.Res.Findings,
		Surface:      index,
		Count:        count,
		WithheldNote: dbWithheldNote,
		Score:        r.Res.Score,
	}
	if raw, err := json.Marshal(index); err == nil {
		resp.IndexBytes = len(raw)
	}
	if len(want) > 0 {
		resp.Detail, resp.Unrecognized = surfaceindex.Resolve(r.Res.Surface, want)
		if len(resp.Unrecognized) > 0 {
			resp.UnrecognizedNote = dbUnrecognizedNote
		}
	}
	// The size is declared LAST, over everything the response is actually
	// carrying (index + detail) — never the index alone. Measuring before the
	// detail is attached is how a response comes to under-declare its own size
	// (coverage's S3 fix, precedent #1664).
	resp.Bytes = resp.IndexBytes
	if len(resp.Detail) > 0 {
		if raw, err := json.Marshal(resp.Detail); err == nil {
			resp.Bytes += len(raw)
		}
	}
	if resp.Bytes > budget {
		resp.OverBudget = true
		resp.BudgetNote = dbIndexOverBudgetNote
		if len(resp.Detail) > 0 {
			resp.BudgetNote = dbDetailOverBudgetNote
		}
	}
	return resp
}
