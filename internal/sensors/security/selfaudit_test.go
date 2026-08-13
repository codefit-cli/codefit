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
//     (the known fixtures live in _test.go, which path_criticality classifies
//     as test, so RF-10 re-weights them below critical).
//
// That second guarantee is MODE-DEPENDENT since RF-10 (see the precondition
// below), which is why the wording is no longer "downgraded": under the default
// the fixtures are forced to info, not lowered one level.
func TestSelfAudit(t *testing.T) {
	repoRoot, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(repoRoot, ".codefit.yaml"))
	if err != nil {
		t.Fatalf("loading repo .codefit.yaml: %v", err)
	}

	// PRECONDITION, not an assertion about the code under test. The dogfooding
	// gate below only holds while test-path findings are re-weighted BELOW
	// critical — true under test_severity "info" (the default, and what this
	// repo's .codefit.yaml declares by saying nothing) and under "downgrade",
	// false under "keep".
	//
	// Someone who sets keep in .codefit.yaml has changed a project policy, not
	// broken the sensor. Without this check the suite would report a dogfooding
	// REGRESSION — "self-audit BLOCKED by SEC-001 …" — and send the next reader
	// hunting through the security sensor for a bug that is not there. It fails
	// naming the actual cause instead.
	if mode := cfg.TestSeverityMode(); mode == config.TestSeverityKeep {
		t.Fatalf("precondition not met: this repository's .codefit.yaml sets "+
			"sensors.security.test_severity=%q, so credential-shaped fixtures in *_test.go stay "+
			"critical and the project blocks itself BY CONFIGURATION. This is not a sensor "+
			"regression. Remove the key (default %q) or set %q to restore the dogfooding gate.",
			mode, config.TestSeverityInfo, config.TestSeverityDowngrade)
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
