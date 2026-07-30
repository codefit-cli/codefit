package security_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/providers/golang"
	"github.com/codefit-cli/codefit/internal/sensors/security"
)

// Go fail-loud cross-provider debt lock (db-model-completeness-contract,
// design SS10, resolved decision #4: "a skipped test is a TODO, not a
// control"). ONE syntactically invalid .go file aborts the ENTIRE sensor
// walk — a stricter, opposite-direction failure mode from the TS parser's
// per-node HasError() (declared, unconsumed, locked separately in
// internal/core/syntax/hasError_debt_test.go). Asserts TODAY's actual
// behavior against the real production code path (security.Sensor.Run ->
// filepath.WalkDir -> scanFile -> golang parse), not a description of it.
//
// Citations re-verified against this working tree before writing (per
// architect direction 2026-07-30): security.go's scanFile error check
// (`fileErr != nil { return fileErr }`, ~:91-93) aborts filepath.WalkDir;
// walkErr (~:100-101) returns findings.SensorResult{Error: walkErr.Error()}
// with nil Findings/Surface and a non-nil error.
func TestSecurity_OneInvalidGoFile_AbortsWholeWalk(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A valid file first (so the walk has something to find before it hits
	// the invalid one — order is directory-entry order, not deliberately
	// controlled here, and the assertion below does not depend on it).
	write("valid.go", "package main\n\nfunc main() {}\n")
	// Deliberately malformed Go: an unbalanced brace.
	write("broken.go", "package main\n\nfunc broken() {\n")

	cfg := &config.Config{Project: config.Project{Language: "go"}}
	ctx := auditctx.AuditContext{ProjectRoot: root, Language: "go", Config: cfg, NoLLM: true}

	res, err := security.New(golang.New()).Run(ctx)
	if err == nil {
		t.Fatal("Run() returned a nil error over a tree containing one syntactically invalid .go file — " +
			"want the walk to abort. If this changed because the sensor now tolerates a bad file and " +
			"continues, delete this lock and update the coverage manifest in the same change")
	}
	if len(res.Findings) != 0 {
		t.Errorf("Findings = %v, want none — the walk must abort BEFORE returning partial results", res.Findings)
	}
	if len(res.Surface) != 0 {
		t.Errorf("Surface = %v, want none — the walk must abort BEFORE returning partial results", res.Surface)
	}
	if res.Error == "" {
		t.Error("SensorResult.Error is empty, want the walk's parse error message")
	}
}
