package mcp

import (
	"fmt"
	"path/filepath"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/coverage"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/scoring"
	"github.com/codefit-cli/codefit/internal/sensors/security"
)

// ScanRequest is the input to codefit-scan-security: a project root and language.
type ScanRequest struct {
	Root     string `json:"root"`
	Language string `json:"language"`
}

// ScanResponse is the deterministic + surface result over the project (the §11
// contract): flat findings and surface, the dimension score, and whether the
// project is blocked (an unconsented critical security finding).
type ScanResponse struct {
	Findings []findings.Finding     `json:"findings"`
	Surface  []findings.SurfaceItem `json:"surface"`
	Score    int                    `json:"score"`
	Blocked  bool                   `json:"blocked"`
}

// HandleScanSecurity runs the security sensor over the project and returns the
// flat findings + surface. A thin adapter: it resolves the provider, runs the
// sensor (the deterministic rules + the surface queries, already wired there),
// and returns its result — no detection logic lives here.
func HandleScanSecurity(req ScanRequest) (ScanResponse, error) {
	res, err := runSecurity(req.Root, req.Language)
	if err != nil {
		return ScanResponse{}, err
	}
	return ScanResponse{
		Findings: res.Findings,
		Surface:  res.Surface,
		Score:    res.Score,
		Blocked:  scoring.IsBlocked(res.Findings),
	}, nil
}

// runSecurity resolves the provider and runs the security sensor over the project
// root — the shared body of codefit-scan-security and codefit-scan-all.
func runSecurity(root, language string) (findings.SensorResult, error) {
	provider := providerForLanguage(language)
	if provider == nil {
		return findings.SensorResult{}, fmt.Errorf("unsupported language %q", language)
	}
	cfg, _ := config.Load(filepath.Join(root, ".codefit.yaml")) // optional; nil → no criticality adjust
	ctx := auditctx.AuditContext{ProjectRoot: root, Language: language, Config: cfg}
	res, err := security.New(provider).Run(ctx)
	if err != nil {
		return findings.SensorResult{}, fmt.Errorf("security scan: %w", err)
	}
	return res, nil
}

// CoverageRequest is the input to codefit-coverage.
type CoverageRequest struct {
	Language string `json:"language"`
}

// CoverageResponse carries the coverage manifest: what is audited
// deterministically vs reasoned over surface vs not covered.
type CoverageResponse struct {
	Manifest coverage.Manifest `json:"manifest"`
}

// HandleCoverage returns the coverage manifest for the language. Thin adapter:
// the manifest is the provider's single source of truth.
func HandleCoverage(req CoverageRequest) (CoverageResponse, error) {
	p := providerForLanguage(req.Language)
	if cm, ok := p.(interface {
		CoverageManifest() coverage.Manifest
	}); ok {
		return CoverageResponse{Manifest: cm.CoverageManifest()}, nil
	}
	return CoverageResponse{}, fmt.Errorf("no coverage manifest for language %q", req.Language)
}
