package mcp

import (
	"fmt"
	"path/filepath"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/coverage"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/scope"
	"github.com/codefit-cli/codefit/internal/core/scoring"
	"github.com/codefit-cli/codefit/internal/sensors/security"
)

// ScanRequest is the input to codefit-scan-security: a project root, a language,
// and optionally the files to narrow the audit to.
type ScanRequest struct {
	Root     string `json:"root"`
	Language string `json:"language"`
	// ChangedFiles narrows the audit to these project-relative paths (layer 0 of
	// the filtering pyramid). codefit does not ask git which files changed — it
	// has no power over the user's git, and the calling agent already knows what
	// it touched. Absent or empty means a FULL audit, never "audit nothing", and
	// a narrowed run declares itself in the response's scope block.
	ChangedFiles []string `json:"changed_files,omitempty"`
}

// ScanResponse is the deterministic + surface result over the project (the §11
// contract): flat findings and surface, the dimension score, and whether the
// project is blocked (an unconsented critical security finding).
// Scope declares how much of the project this result describes. It is ALWAYS
// present, so `blocked: false` is never read as a wider claim than it is.
type ScanResponse struct {
	Findings []findings.Finding     `json:"findings"`
	Surface  []findings.SurfaceItem `json:"surface"`
	Score    int                    `json:"score"`
	Blocked  bool                   `json:"blocked"`
	Scope    ScopeBlock             `json:"scope"`
}

// HandleScanSecurity runs the security sensor over the project and returns the
// flat findings + surface. A thin adapter: it resolves the provider, runs the
// sensor (the deterministic rules + the surface queries, already wired there),
// and returns its result — no detection logic lives here.
func HandleScanSecurity(req ScanRequest) (ScanResponse, error) {
	scp := scope.Of(req.ChangedFiles)
	res, err := runSecurity(req.Root, req.Language, recognizedHelpers(req.Root, req.Language), scp)
	if err != nil {
		return ScanResponse{}, err
	}
	block := scopeBlockFor(scp, res.AuditedFiles, res.AuditableTotal)
	if err := block.Validate(); err != nil {
		return ScanResponse{}, err
	}
	return ScanResponse{
		Findings: res.Findings,
		Surface:  res.Surface,
		Score:    res.Score,
		Blocked:  scoring.IsBlocked(res.Findings),
		Scope:    block,
	}, nil
}

// runSecurity resolves the provider (recognizing the project's registered authz
// helpers) and runs the security sensor over the project root — the shared body of
// codefit-scan-security, -scan-all, and -scan-endpoint.
// scp is layer 0: the files this pass may analyse. The walk still counts every
// auditable file, so the caller can say how much of the project it left out.
func runSecurity(root, language string, helpers []string, scp scope.Scope) (findings.SensorResult, error) {
	provider := providerForLanguage(language, helpers)
	if provider == nil {
		return findings.SensorResult{}, fmt.Errorf("unsupported language %q", language)
	}
	// A missing config is fine (nil → defaults); a PRESENT but invalid one is a
	// hard error — scanning silently with no path_criticality would be a false
	// "all good", the very anti-pattern codefit exists to catch.
	cfg, err := config.LoadOptional(filepath.Join(root, ".codefit.yaml"))
	if err != nil {
		return findings.SensorResult{}, fmt.Errorf("loading project config: %w", err)
	}
	ctx := auditctx.AuditContext{ProjectRoot: root, Language: language, Config: cfg, Scope: scp}
	res, err := security.New(provider).Run(ctx)
	if err != nil {
		return findings.SensorResult{}, fmt.Errorf("security scan: %w", err)
	}
	return res, nil
}

// securityScope is the baseline category scope of a security-only run: the
// categories the security sensor owns, unioned by the adapter (ADR 0019). It is
// passed to the unified baseline diff/prune so items from a sensor that did not
// run are never marked gone. The provider is irrelevant to OwnedCategories, so a
// default resolution is enough.
func securityScope(language string) map[string]bool {
	return scannedCategories(security.New(providerForLanguage(language, nil)))
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
	p := providerForLanguage(req.Language, nil)
	if cm, ok := p.(interface {
		CoverageManifest() coverage.Manifest
	}); ok {
		return CoverageResponse{Manifest: cm.CoverageManifest()}, nil
	}
	return CoverageResponse{}, fmt.Errorf("no coverage manifest for language %q", req.Language)
}
