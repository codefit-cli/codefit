package security_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/scope"
	"github.com/codefit-cli/codefit/internal/providers/golang"
	"github.com/codefit-cli/codefit/internal/sensors/security"
)

// leakingSource is a file that ALWAYS yields a deterministic SEC-001: an
// Anthropic-shaped key on a source line. It drives the REAL layer-1 regex, so a
// file the walk audits is provably a file that produced a finding — no
// hand-built SensorResult is involved anywhere in these tests.
const leakingSource = "package main\n\nconst key = \"sk-ant-abcdefgh12345678\"\n"

// threeFileProject writes three Go files that each leak a credential, so under a
// full audit the sensor emits exactly three SEC-001 findings, one per file.
func threeFileProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, rel := range []string{"a.go", "b.go", "sub/c.go"} {
		writeFile(t, root, rel, leakingSource)
	}
	return root
}

func runScoped(t *testing.T, root string, ctx auditctx.AuditContext) findings.SensorResult {
	t.Helper()
	ctx.ProjectRoot = root
	ctx.Language = "go"
	if ctx.Config == nil {
		ctx.Config = &config.Config{Project: config.Project{Language: "go"}}
	}
	res, err := security.New(golang.New()).Run(ctx)
	if err != nil {
		t.Fatalf("sensor Run: %v", err)
	}
	return res
}

func filesOf(fs []findings.Finding) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range fs {
		if !seen[f.File] {
			seen[f.File] = true
			out = append(out, f.File)
		}
	}
	sort.Strings(out)
	return out
}

// A narrowed scope means the walk ANALYSES only the scoped files. Proven through
// the real pyramid: only the scoped file's credential is found.
func TestRun_NarrowedScope_AuditsOnlyScopedFiles(t *testing.T) {
	root := threeFileProject(t)

	res := runScoped(t, root, auditctx.AuditContext{Scope: scope.Of([]string{"b.go"})})

	if got, want := filesOf(res.Findings), []string{"b.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("findings came from %v, want only %v — the walk analysed out-of-scope files", got, want)
	}
	if got, want := res.AuditedFiles, []string{"b.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("AuditedFiles = %v, want %v", got, want)
	}
	// auditable_total still counts the WHOLE project: a partial audit must be able
	// to say 1 of 3, which it cannot do if the walk stops counting what it skips.
	if res.AuditableTotal != 3 {
		t.Errorf("AuditableTotal = %d, want 3 (every auditable file, scoped or not)", res.AuditableTotal)
	}
}

// A subdirectory path in the scope resolves the same way, and a Windows-spelled
// request matches a file the walk found with a slash separator.
func TestRun_NarrowedScope_SubdirAndSeparatorSpelling(t *testing.T) {
	root := threeFileProject(t)

	res := runScoped(t, root, auditctx.AuditContext{Scope: scope.Of([]string{`sub\c.go`})})

	if got, want := filesOf(res.Findings), []string{"sub/c.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("findings came from %v, want only %v", got, want)
	}
}

// THE fail-safe for the walk: an AuditContext built without a Scope must audit
// EVERYTHING. The zero-value Scope includes nothing (that is what makes it safe
// for the baseline prune guard), so a walk that asked Includes() instead of
// Narrows() would silently audit no file at all and report score 100 — a false
// all-clear, the exact class of lie codefit exists to catch.
func TestRun_UnsetScope_AuditsEverything(t *testing.T) {
	root := threeFileProject(t)

	res := runScoped(t, root, auditctx.AuditContext{}) // Scope deliberately unset

	if got, want := filesOf(res.Findings), []string{"a.go", "b.go", "sub/c.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("unset scope audited %v, want every file %v", got, want)
	}
	if res.AuditableTotal != 3 || len(res.AuditedFiles) != 3 {
		t.Errorf("unset scope: audited %d of %d, want 3 of 3", len(res.AuditedFiles), res.AuditableTotal)
	}
}

// An explicit Full() scope is the same thing, said out loud.
func TestRun_FullScope_AuditsEverything(t *testing.T) {
	root := threeFileProject(t)

	res := runScoped(t, root, auditctx.AuditContext{Scope: scope.Full()})

	if got, want := filesOf(res.Findings), []string{"a.go", "b.go", "sub/c.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("full scope audited %v, want every file %v", got, want)
	}
}

// A scoped path the walk never reaches (deleted, wrong extension, skipped dir)
// simply does not appear in AuditedFiles — that difference is what lets the MCP
// layer report `unmatched` instead of passing off "never looked" as "clean".
func TestRun_ScopedPathTheWalkNeverReaches_IsNotAudited(t *testing.T) {
	root := threeFileProject(t)
	writeFile(t, root, "node_modules/dep/d.go", leakingSource) // skipped dir
	writeFile(t, root, "notes.txt", leakingSource)             // wrong extension

	res := runScoped(t, root, auditctx.AuditContext{Scope: scope.Of([]string{
		"a.go", "deleted.go", "notes.txt", "node_modules/dep/d.go",
	})})

	if got, want := res.AuditedFiles, []string{"a.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("AuditedFiles = %v, want %v — only the path the walk actually reached", got, want)
	}
}
