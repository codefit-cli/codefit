package security_test

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/findings"
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

// sec001Site identifies one SEC-001 emission site WITHOUT its line number.
// Line is deliberately excluded: it churns on every unrelated edit above the
// finding, which would make this control a maintenance tax that gets deleted
// rather than a gate that gets read.
type sec001Site struct {
	File        string
	Description string
}

// wantSEC001 is the EXACT SEC-001 census over codefit's own tree, transcribed
// from a real go/ast run of the security sensor (never typed from memory). The
// value is how many times that (File, Description) pair fires, so a change
// WITHIN one file is caught too — a set alone would hide 3 fires collapsing to 1.
//
// Two distinct detectors emit SEC-001 and both are pinned here:
//
//   - "A string matching a known API-key format …" — layer 1, a regex over raw
//     text (credential SHAPE), security.go's layer1Secrets. It never looks at a
//     name and is untouched by name-gate work.
//   - "The value assigned to 'X' looks like a hardcoded credential." — layer 2,
//     the Go provider's AST name gate (credential NAME), golang/parse.go's
//     looksSecret. This is the half a name-vocabulary change moves.
//
// This pin exists because TestSelfAudit above asserts only len(Findings) != 0
// and !IsBlocked: it prints no findings and asserts no count, so DELETING a
// detector entirely left it green. The census is the missing direction.
var wantSEC001 = map[sec001Site]int{
	{"internal/core/cache/cache_test.go", "A string matching a known API-key format is present in the source."}:                          3,
	{"internal/core/findings/fingerprint_test.go", "A string matching a known API-key format is present in the source."}:                 1,
	{"internal/core/paradigm/schemagate.go", "The value assigned to 'SignalSurrogateKeyNames' looks like a hardcoded credential."}:       1,
	{"internal/core/surface/surface.go", "The value assigned to 'CategoryDWDimensionNoSurrogateKey' looks like a hardcoded credential."}: 1,
	{"internal/mcp/baseline_loop_test.go", "A string matching a known API-key format is present in the source."}:                         2,
	{"internal/mcp/baseline_loop_test.go", "The value assigned to 'secretFile' looks like a hardcoded credential."}:                      1,
	{"internal/mcp/changedfiles_test.go", "A string matching a known API-key format is present in the source."}:                          1,
	{"internal/mcp/score_test.go", "A string matching a known API-key format is present in the source."}:                                 2,
	{"internal/scaffold/generate_test.go", "The value assigned to 'skillNoSuchKeyClaim' looks like a hardcoded credential."}:             1,
	{"internal/sensors/db/schemagate_note_test.go", "The value assigned to 'starWithSurrogateKeys' looks like a hardcoded credential."}:  1,
	{"internal/sensors/security/cache_test.go", "A string matching a known API-key format is present in the source."}:                    5,
	{"internal/sensors/security/keepwarn_test.go", "A string matching a known API-key format is present in the source."}:                 2,
	{"internal/sensors/security/scope_test.go", "A string matching a known API-key format is present in the source."}:                    1,
	{"internal/sensors/security/security_test.go", "A string matching a known API-key format is present in the source."}:                 5,
}

// TestSelfAuditSEC001Census is the enumeration control TestSelfAudit lacks. It
// fails in BOTH directions: a false affirmation that reappears shows up as an
// unexpected site, and a true positive that disappears shows up as a missing
// one. Neither direction is guarded by an existence-only assertion.
func TestSelfAuditSEC001Census(t *testing.T) {
	res := runSelfAudit(t)

	// Vacuum guards. An empty census is indistinguishable from a sensor that
	// never ran, and an all-SEC-001 result would mean the rest of the security
	// dimension silently died — both would let the comparison below pass by
	// asserting nothing about a tree that was never analysed.
	if res.AuditableTotal == 0 || len(res.AuditedFiles) == 0 {
		t.Fatalf("vacuum: the sensor analysed no files (auditable=%d audited=%d)",
			res.AuditableTotal, len(res.AuditedFiles))
	}
	got := map[sec001Site]int{}
	var layer1, layer2 int
	for _, f := range res.Findings {
		if f.ID != "SEC-001" {
			continue
		}
		got[sec001Site{f.File, f.Description}]++
		switch {
		case strings.HasPrefix(f.Description, "A string matching"):
			layer1++
		case strings.HasPrefix(f.Description, "The value assigned to"):
			layer2++
		}
	}

	// Both SEC-001 detectors must be represented. This is the vacuum guard that
	// actually holds here: codefit's own tree emits SEC-001 and NOTHING else
	// (measured 27 of 27), so "some other rule id is present" would be a false
	// assertion, and asserting only len(got) != 0 would stay green if the whole
	// Go provider silently stopped being wired in — layer 1 alone would carry it.
	if layer1 == 0 {
		t.Fatal("vacuum: no layer-1 (regex shape) SEC-001 at all — security.go's layer1Secrets is not running")
	}
	if layer2 == 0 {
		t.Fatal("vacuum: no layer-2 (AST name) SEC-001 at all — the Go provider's name gate is not running")
	}

	for site, want := range wantSEC001 {
		if got[site] != want {
			t.Errorf("SEC-001 census: %s | %s — got %d, want %d",
				site.File, site.Description, got[site], want)
		}
	}
	var extra []sec001Site
	for site := range got {
		if _, pinned := wantSEC001[site]; !pinned {
			extra = append(extra, site)
		}
	}
	sort.Slice(extra, func(i, j int) bool {
		if extra[i].File != extra[j].File {
			return extra[i].File < extra[j].File
		}
		return extra[i].Description < extra[j].Description
	})
	for _, site := range extra {
		t.Errorf("SEC-001 census: UNPINNED site %s | %s (%d fires) — a new emission the pin never approved",
			site.File, site.Description, got[site])
	}
}

// runSelfAudit runs the real security sensor over codefit's own tree.
func runSelfAudit(t *testing.T) findings.SensorResult {
	t.Helper()
	repoRoot, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(repoRoot, ".codefit.yaml"))
	if err != nil {
		t.Fatalf("loading repo .codefit.yaml: %v", err)
	}
	res, err := security.New(golang.New()).Run(auditctx.AuditContext{
		ProjectRoot: repoRoot,
		Language:    "go",
		Config:      cfg,
	})
	if err != nil {
		t.Fatalf("self-audit run: %v", err)
	}
	return res
}
