package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/scope"
)

// leakyTS is a handler that ALWAYS yields a deterministic SEC-001 through the
// real layer-1 regex, so "this file was audited" is proven by a finding the
// production path produced, not by a hand-built result.
const leakyTS = "export const key = \"sk-ant-abcdefgh12345678\"\n\nexport async function GET() {\n  return await prisma.account.findMany({ where: { status: \"x\" } })\n}\n"

func writeTS(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// scopedProject is a typescript project with three leaking handlers, a Prisma
// schema, and a .codefit.yaml wiring the DB dimension to that schema.
func scopedProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeTS(t, root, "src/a.ts", leakyTS)
	writeTS(t, root, "src/b.ts", leakyTS)
	writeTS(t, root, "src/c.ts", leakyTS)
	writeTS(t, root, "prisma/schema.prisma", `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model Account {
  id     Int    @id
  status String
}
`)
	writeTS(t, root, ".codefit.yaml", `version: "1"
project:
  name: t
  language: typescript
  framework: next
database:
  orm: prisma
  type: postgresql
  paradigm: oltp
  schema_paths:
    - prisma/schema.prisma
`)
	return root
}

func filesOfFindings(fs []findings.Finding) map[string]bool {
	out := map[string]bool{}
	for _, f := range fs {
		out[f.File] = true
	}
	return out
}

// codefit-scan-security narrows to changed_files and DECLARES it (R1).
func TestScanSecurity_ChangedFiles_NarrowsAndDeclares(t *testing.T) {
	root := scopedProject(t)

	res, err := HandleScanSecurity(ScanRequest{Root: root, Language: "typescript", ChangedFiles: []string{"src/b.ts"}})
	if err != nil {
		t.Fatal(err)
	}

	if got := filesOfFindings(res.Findings); len(got) != 1 || !got["src/b.ts"] {
		t.Errorf("findings came from %v, want only src/b.ts", got)
	}
	if res.Scope.Mode != ScopeModePartial {
		t.Errorf("mode = %q, want %q", res.Scope.Mode, ScopeModePartial)
	}
	if res.Scope.Requested != 1 || res.Scope.Audited != 1 {
		t.Errorf("requested/audited = %d/%d, want 1/1", res.Scope.Requested, res.Scope.Audited)
	}
	if res.Scope.AuditableTotal != 3 {
		t.Errorf("auditable_total = %d, want 3 — narrowing must not shrink the denominator", res.Scope.AuditableTotal)
	}
	if res.Scope.Note == "" {
		t.Error("a partial scan MUST carry a non-empty note (R1)")
	}
	// R2: blocked:false from a partial scan is a narrower claim, and the note must
	// say so rather than let the agent read it as "no critical anywhere".
	if !strings.Contains(res.Scope.Note, "blocked") {
		t.Errorf("the partial note must qualify `blocked` (R2), got %q", res.Scope.Note)
	}
}

// A full scan still emits the block, so a consumer never infers the mode from an
// absence — with an EMPTY note, because there is nothing to caveat.
func TestScanSecurity_NoChangedFiles_IsFullAndSilent(t *testing.T) {
	root := scopedProject(t)

	res, err := HandleScanSecurity(ScanRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}

	if res.Scope.Mode != ScopeModeFull {
		t.Errorf("mode = %q, want %q", res.Scope.Mode, ScopeModeFull)
	}
	if res.Scope.Note != "" {
		t.Errorf("a full scan's note must be empty, got %q", res.Scope.Note)
	}
	if res.Scope.Audited != 3 || res.Scope.AuditableTotal != 3 {
		t.Errorf("full scan audited %d of %d, want 3 of 3", res.Scope.Audited, res.Scope.AuditableTotal)
	}
	if len(res.Scope.Unmatched) != 0 {
		t.Errorf("a full scan has nothing unmatched, got %v", res.Scope.Unmatched)
	}
}

// An empty changed_files list is FULL, never "audit nothing".
func TestScanSecurity_EmptyChangedFiles_IsFull(t *testing.T) {
	root := scopedProject(t)

	res, err := HandleScanSecurity(ScanRequest{Root: root, Language: "typescript", ChangedFiles: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Scope.Mode != ScopeModeFull || res.Scope.Audited != 3 {
		t.Errorf("empty changed_files: mode=%q audited=%d, want full/3", res.Scope.Mode, res.Scope.Audited)
	}
}

// Test contract item 6 (R3): a requested path the walk never reaches lands in
// `unmatched`. Without it, an agent that passes three wrong paths gets "0
// findings" and reads it as clean — the difference between "audited and clean"
// and "never looked".
func TestScanSecurity_RequestedPathNeverReached_IsUnmatched(t *testing.T) {
	root := scopedProject(t)

	res, err := HandleScanSecurity(ScanRequest{Root: root, Language: "typescript", ChangedFiles: []string{
		"src/a.ts", "src/deleted.ts", "README.md",
	}})
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"README.md", "src/deleted.ts"}; !reflect.DeepEqual(res.Scope.Unmatched, want) {
		t.Errorf("unmatched = %v, want %v", res.Scope.Unmatched, want)
	}
	if res.Scope.Requested != 3 || res.Scope.Audited != 1 {
		t.Errorf("requested/audited = %d/%d, want 3/1", res.Scope.Requested, res.Scope.Audited)
	}
	if !strings.Contains(res.Scope.Note, "unmatched") {
		t.Errorf("the note must point at unmatched when some requested path was never reached, got %q", res.Scope.Note)
	}
}

// Every requested path missing is the worst case and must NOT read as clean.
func TestScanSecurity_AllRequestedPathsMissing_SaysNothingWasAudited(t *testing.T) {
	root := scopedProject(t)

	res, err := HandleScanSecurity(ScanRequest{Root: root, Language: "typescript", ChangedFiles: []string{"nope/x.ts"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 0 {
		t.Fatalf("expected no findings, got %d", len(res.Findings))
	}
	if res.Scope.Audited != 0 || len(res.Scope.Unmatched) != 1 {
		t.Errorf("audited=%d unmatched=%v, want 0 and one entry", res.Scope.Audited, res.Scope.Unmatched)
	}
	if !strings.Contains(res.Scope.Note, "0 of 3") {
		t.Errorf("the note must state that NOTHING was audited, got %q", res.Scope.Note)
	}
}

// Test contract item 5, both directions. The invariant is enforced in
// PRODUCTION, not merely asserted in a test: an honesty violation is a loud
// error, never a silently unlabelled partial result.
func TestScopeBlock_Validate_NoteInvariant(t *testing.T) {
	if err := (ScopeBlock{Mode: ScopeModePartial, Note: ""}).Validate(); err == nil {
		t.Error("mode:partial with an EMPTY note must fail — an undeclared partial audit is a lying auditor")
	}
	if err := (ScopeBlock{Mode: ScopeModeFull, Note: "some caveat"}).Validate(); err == nil {
		t.Error("mode:full with a NON-EMPTY note must fail — a full audit has nothing to caveat")
	}
	if err := (ScopeBlock{Mode: ScopeModePartial, Note: "declared"}).Validate(); err != nil {
		t.Errorf("a declared partial audit is valid, got %v", err)
	}
	if err := (ScopeBlock{Mode: ScopeModeFull}).Validate(); err != nil {
		t.Errorf("a silent full audit is valid, got %v", err)
	}
	if err := (ScopeBlock{Mode: "sorta"}).Validate(); err == nil {
		t.Error("an unknown mode must fail")
	}
}

// Test contract item 8: a partial scan whose scope holds no configured schema
// path reports the DB dimension as null (NOT MEASURED) — never 100.
func TestScanAll_PartialScopeWithoutSchemaPath_DBIsNotMeasured(t *testing.T) {
	root := scopedProject(t)

	res, err := HandleScanAll(ScanAllRequest{Root: root, Language: "typescript", ChangedFiles: []string{"src/a.ts"}})
	if err != nil {
		t.Fatal(err)
	}

	got, present := res.Score.ByDimension[findings.DimensionDB]
	if !present {
		t.Fatal("by_dimension must carry the db key even when unmeasured")
	}
	if got != nil {
		t.Errorf("db score = %d, want null (not measured): no configured schema path was in scope", *got)
	}
	if res.DB != nil {
		t.Errorf("no DB section when the dimension did not run, got %+v", res.DB)
	}
}

// The other direction: with the schema path in scope the dimension DOES run and
// is measured — so the null above is the scope talking, not the dimension being
// broken.
func TestScanAll_PartialScopeWithSchemaPath_DBIsMeasured(t *testing.T) {
	root := scopedProject(t)

	res, err := HandleScanAll(ScanAllRequest{Root: root, Language: "typescript", ChangedFiles: []string{"prisma/schema.prisma"}})
	if err != nil {
		t.Fatal(err)
	}

	got := res.Score.ByDimension[findings.DimensionDB]
	if got == nil {
		t.Fatal("db score = null, want measured: the configured schema path WAS in scope")
	}
	if res.DB == nil || !res.DB.Measured {
		t.Errorf("expected a measured DB section, got %+v", res.DB)
	}
	// The schema file it read counts as audited, so it is not reported unmatched.
	if len(res.Scope.Unmatched) != 0 {
		t.Errorf("unmatched = %v, want empty — the DB dimension audited the requested schema", res.Scope.Unmatched)
	}
}

// A full scan-all keeps every dimension it had before: the scope block is
// additive, not a behaviour change.
func TestScanAll_FullScan_UnchangedDimensionsAndSilentScope(t *testing.T) {
	root := scopedProject(t)

	res, err := HandleScanAll(ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Score.ByDimension[findings.DimensionDB] == nil {
		t.Error("a full scan measures db (schema_paths configured)")
	}
	if res.Scope.Mode != ScopeModeFull || res.Scope.Note != "" {
		t.Errorf("full scan-all scope = %+v, want mode full with an empty note", res.Scope)
	}
}

// R5, the deliberate asymmetry: scanning may be cheap and partial; FORGETTING
// may not. Exactly two tools accept changed_files, and codefit-baseline-prune
// and codefit-scan-db are not among them. The check reads the JSON schema the
// server actually publishes — the thing an agent sees — so a field added to a
// params struct cannot quietly appear on a tool that must not have it.
func TestOnlyScanSecurityAndScanAllAcceptChangedFiles(t *testing.T) {
	allowed := map[string]bool{
		string(ToolScanSecurity): true,
		string(ToolScanAll):      true,
	}
	ctx := context.Background()
	st, ct := mcpsdk.NewInMemoryTransports()
	go func() { _ = NewServer().Run(ctx, st) }()
	session, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "changed-files-check"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	lt, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	for _, tool := range lt.Tools {
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshalling %s input schema: %v", tool.Name, err)
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("decoding %s input schema: %v", tool.Name, err)
		}
		_, has := schema.Properties["changed_files"]
		if has && !allowed[tool.Name] {
			t.Errorf("tool %q publishes changed_files — pruning audit memory always requires a FULL audit (R5)", tool.Name)
		}
		if has {
			seen[tool.Name] = true
		}
	}
	// Positive probe: the allowlist is not vacuously satisfied by nobody having
	// the field at all.
	for name := range allowed {
		if !seen[name] {
			t.Errorf("tool %q does NOT publish changed_files — the scope is unreachable from the agent", name)
		}
	}
}

// R5 end to end through the REAL prune handler: a partial scan-all must not turn
// an untouched file's baseline item into something prune deletes.
func TestScanAll_PartialScope_DoesNotPruneUntouchedFiles(t *testing.T) {
	root := scopedProject(t)

	// 1. Full scan-all records every file's items in the committed baseline.
	if _, err := HandleScanAll(ScanAllRequest{Root: root, Language: "typescript"}); err != nil {
		t.Fatal(err)
	}
	before := baselineFiles(t, root)
	for _, want := range []string{"src/a.ts", "src/b.ts", "src/c.ts"} {
		if !before[want] {
			t.Fatalf("setup: the baseline must track %s, got %v", want, before)
		}
	}

	// 2. Resolve b.ts (its findings disappear) but scan ONLY b.ts.
	writeTS(t, root, "src/b.ts", "export async function GET() {\n  return 1\n}\n")
	res, err := HandleScanAll(ScanAllRequest{Root: root, Language: "typescript", ChangedFiles: []string{"src/b.ts"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range res.Baseline.GoneCandidates {
		if g.File != "src/b.ts" {
			t.Errorf("a partial scan proposed pruning %s in %s — it never opened that file", g.Fingerprint, g.File)
		}
	}

	// 3. The prune (always full, by contract) must still find a.ts and c.ts.
	if _, err := HandleBaselinePrune(BaselinePruneRequest{Root: root, Language: "typescript"}); err != nil {
		t.Fatal(err)
	}
	after := baselineFiles(t, root)
	for _, want := range []string{"src/a.ts", "src/c.ts"} {
		if !after[want] {
			t.Errorf("the partial scan destroyed the audit memory of %s (baseline now %v)", want, after)
		}
	}
}

func baselineFiles(t *testing.T, root string) map[string]bool {
	t.Helper()
	res, err := HandleBaselineList(BaselineListRequest{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, it := range res.Items {
		out[it.File] = true
	}
	return out
}

// The scope block's own canonicalization: a Windows-spelled request matches the
// slash-spelled path the walk reports, so it is audited rather than unmatched.
func TestScanSecurity_WindowsSpelledRequest_IsAudited(t *testing.T) {
	root := scopedProject(t)

	res, err := HandleScanSecurity(ScanRequest{Root: root, Language: "typescript", ChangedFiles: []string{`src\a.ts`}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Scope.Audited != 1 || len(res.Scope.Unmatched) != 0 {
		t.Errorf("audited=%d unmatched=%v, want 1 and none", res.Scope.Audited, res.Scope.Unmatched)
	}
	if got := scope.Canon(`src\a.ts`); got != "src/a.ts" {
		t.Fatalf("probe: Canon = %q", got)
	}
}
