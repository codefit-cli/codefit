package mcp

import (
	"fmt"
	"path/filepath"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/providers"
	dbsensor "github.com/codefit-cli/codefit/internal/sensors/db"
)

// ScanDBRequest is the input to codefit-scan-db: a project root and language.
type ScanDBRequest struct {
	Root     string `json:"root"`
	Language string `json:"language"`
}

// ScanDBResponse is the standalone DB-structure result. Measured distinguishes
// "audited" from "not audited": when false, Note says why (no schema_paths, no
// schema parser, or the sensor is disabled) and Findings/Surface are empty — it is
// NOT a "clean" result. Score is a plain dimension score (0-100), like
// codefit-scan-security; this tool does not compute by_dimension.
type ScanDBResponse struct {
	Measured bool                   `json:"measured"`
	Note     string                 `json:"note,omitempty"`
	Findings []findings.Finding     `json:"findings"`
	Surface  []findings.SurfaceItem `json:"surface"`
	Score    int                    `json:"score"`
}

// HandleScanDB runs the DB sensor over the project and returns its structural
// findings + surface. A thin adapter: it resolves the provider, loads the config,
// and delegates to the DB sensor (which reads the schema, runs the core rules, and
// stamps identity). Standalone — it touches nothing in scan-all.
func HandleScanDB(req ScanDBRequest) (ScanDBResponse, error) {
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
		p, note := schemaParserForPaths(req.Root, cfg.Database.SchemaPaths, cfg.Database.Type)
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
	return ScanDBResponse{
		Measured: r.Measured,
		Note:     r.Note,
		Findings: r.Res.Findings,
		Surface:  r.Res.Surface,
		Score:    r.Res.Score,
	}, nil
}
