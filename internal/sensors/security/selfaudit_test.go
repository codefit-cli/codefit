package security_test

import (
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/scoring"
	"github.com/codefit-cli/codefit/internal/providers/golang"
	"github.com/codefit-cli/codefit/internal/sensors/security"
)

// TestSelfAudit is codefit's dogfooding gate, migrated from the
// `codefit scan --no-llm --fail-on critical` CI step to a Go integration test
// (MCP-first: there is no audit CLI anymore). It runs the real security sensor,
// driven by the real Go provider, over codefit's own source tree — exercising
// the sensor and provider against actual code, not merely compiling them.
//
// It asserts two things:
//   - the sensor actually ran over real files (it must find the known test
//     fixtures), so a silently-broken sensor fails this test;
//   - the repo is not "blocked": no critical security finding lacks consent
//     (the known fixtures live in _test.go and are downgraded by
//     path_criticality, so they never reach critical).
func TestSelfAudit(t *testing.T) {
	repoRoot, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(repoRoot, ".codefit.yaml"))
	if err != nil {
		t.Fatalf("loading repo .codefit.yaml: %v", err)
	}

	ctx := auditctx.AuditContext{
		ProjectRoot: repoRoot,
		Language:    "go",
		Config:      cfg,
	}
	res, err := security.New(golang.New()).Run(ctx)
	if err != nil {
		t.Fatalf("self-audit run: %v", err)
	}

	// Exercised, not just compiled: the sensor must have walked real source and
	// detected the known credential-shaped fixtures. Zero findings would mean
	// the sensor never actually ran over the tree.
	if len(res.Findings) == 0 {
		t.Fatal("self-audit produced no findings — the security sensor did not exercise the source tree")
	}

	// Dogfooding gate: codefit's own code must not block (no unconsented
	// critical security findings).
	if scoring.IsBlocked(res.Findings) {
		for _, f := range res.Findings {
			if f.Dimension == "security" && f.Severity == "critical" && f.Suppressed == nil && !f.Baselined {
				t.Errorf("self-audit BLOCKED by %s at %s:%d — %s", f.ID, f.File, f.Line, f.Title)
			}
		}
	}
}
