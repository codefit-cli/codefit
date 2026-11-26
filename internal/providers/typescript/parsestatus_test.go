package typescript_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/ruleengine"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
	"github.com/codefit-cli/codefit/rules"
)

// These tests drive the REAL provider over the REAL parser (ADR 0089). Every
// status asserted below is what AnalyzeFile computes from the tree it parsed;
// none is a hand-built value the production path could not produce.

// tsFixtures are every .ts/.tsx file under testdata, parser forms included,
// with the positive control that the large real files are among them.
func tsFixtures(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir("testdata", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && (strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".tsx")) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking testdata: %v", err)
	}
	for _, want := range []string{"real_vscode_strings.ts", "real_excalidraw_actions.tsx", "broken_missing_initializer.ts"} {
		found := false
		for _, f := range files {
			found = found || filepath.Base(f) == want
		}
		if !found {
			t.Fatalf("the fixture set does not include %s (%d files) — it would prove less than it claims", want, len(files))
		}
	}
	return files
}

func readSource(t *testing.T, path string) providers.SourceFile {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return providers.SourceFile{Path: filepath.ToSlash(path), Content: content}
}

func analyzeFile(t *testing.T, src providers.SourceFile) providers.FileAnalysis {
	t.Helper()
	a, err := typescript.New().AnalyzeFile(src)
	if err != nil {
		t.Fatalf("AnalyzeFile(%s): %v", src.Path, err)
	}
	return a
}

// everyRuleHelper is a project authorization helper the built-in set does not
// know. The equivalence test registers it, so the combined call must thread the
// project's helpers exactly as the separate surface call does.
const everyRuleHelper = "assertTeamMember"

// everyRuleSource is one Next.js route handler file shaped so that EVERY
// TypeScript security rule fires and EVERY surface category appears, each line
// reusing the shape the rule's own tests drive. Its credential literal is the
// same non-key-format value the other tests in this file use, so the
// repository's self-audit (a Go-provider run over this tree, whose layer 1
// matches only known API-key formats) gains no site from it.
const everyRuleSource = `import { createHash } from "crypto";

const apiKey = "s3cr3t";
const sessionToken = Math.random().toString(36);
const digest = md5(payload);
const weak = createHash("sha1");
const evaluated = eval(userInput);
const concatenated = { __html: "<b>" + name + "</b>" };
const interpolated = { __html: ` + "`<b>${name}</b>`" + ` };
db.query("SELECT * FROM t WHERE id = " + id);
db.query(` + "`SELECT * FROM t WHERE id = ${id}`" + `);

export async function GET(req: Request, { params }: { params: { id: string } }) {
  await ` + everyRuleHelper + `("orders:read");
  for (const lineId of lineIds) {
    await prisma.line.findUnique({ where: { id: lineId } });
  }
  return Response.json(await prisma.order.findUnique({ where: { id: params.id } }));
}
`

// TestAnalyzeFile_EqualsTheTwoSeparateCalls: the combined call is an
// optimisation of AnalyzeSecurity + AnalyzeSurface, never a different analysis.
// Over every TypeScript fixture, errored ones included, and over
// everyRuleSource, a provider configured WITH a project authz helper returns
// exactly the same findings and the same surface, in the same order, from both.
//
// The comparison only controls what the inputs exercise, so two positive
// controls guard it: the inputs must fire every rule id the real loader loads
// and produce every surface category the provider declares — derived, not
// typed, so a rule or category added later that no input exercises fails here
// instead of leaving the combined call free to drop it — and the registered
// helper must change the surface, or dropping the helpers from the combined call
// would compare equal.
func TestAnalyzeFile_EqualsTheTwoSeparateCalls(t *testing.T) {
	p := typescript.New(typescript.WithAuthzHelpers([]string{everyRuleHelper}))
	inline := providers.SourceFile{Path: "app/orders/[id]/route.ts", Content: []byte(everyRuleSource)}
	// The same bytes under a path with upper-case letters. Next.js route-file
	// detection compares the base name case-sensitively, so "Route.ts" is NOT a
	// route file: its surface differs from the lower-case path's (positive
	// control 4 below), and every finding and surface item reports the path as
	// given. A combined call that altered the path's case would not compare equal.
	mixedCase := providers.SourceFile{Path: mixedCasePath, Content: []byte(everyRuleSource)}
	sources := []providers.SourceFile{inline, mixedCase}
	for _, path := range tsFixtures(t) {
		sources = append(sources, readSource(t, path))
	}

	ruleIDs := map[string]bool{}
	categories := map[surface.Category]bool{}
	for _, src := range sources {
		combined, err := p.AnalyzeFile(src)
		if err != nil {
			t.Fatalf("AnalyzeFile(%s): %v", src.Path, err)
		}
		sec, err := p.AnalyzeSecurity(src)
		if err != nil {
			t.Fatalf("AnalyzeSecurity(%s): %v", src.Path, err)
		}
		surf, err := p.AnalyzeSurface(src)
		if err != nil {
			t.Fatalf("AnalyzeSurface(%s): %v", src.Path, err)
		}
		if !reflect.DeepEqual(combined.Findings, sec) {
			t.Errorf("%s: AnalyzeFile findings differ from AnalyzeSecurity.\ncombined: %+v\nseparate: %+v",
				src.Path, combined.Findings, sec)
		}
		if !reflect.DeepEqual(combined.Surface, surf) {
			t.Errorf("%s: AnalyzeFile surface differs from AnalyzeSurface.\ncombined: %+v\nseparate: %+v",
				src.Path, combined.Surface, surf)
		}
		for _, f := range sec {
			ruleIDs[f.ID] = true
		}
		for _, it := range surf {
			categories[surface.Category(it.Category)] = true
		}
	}

	// Positive control 1: every rule the real loader loads fired somewhere.
	raw, err := ruleengine.LoadFS(rules.FS, "typescript/security")
	if err != nil {
		t.Fatalf("loading the typescript security rules: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("the real loader returned no typescript security rules — the control would be vacuous")
	}
	for _, r := range raw {
		if !ruleIDs[r.ID] {
			t.Errorf("no input fires %s — the equivalence cannot see the combined call drop it; "+
				"extend everyRuleSource with the shape that rule's own tests use", r.ID)
		}
	}
	// Positive control 2: every surface category the provider declares appeared,
	// and nothing outside the declaration did.
	declared := map[surface.Category]bool{}
	for _, c := range p.Capability().Surface {
		declared[c] = true
		if !categories[c] {
			t.Errorf("no input produces %q surface — the equivalence cannot see the combined call drop "+
				"that query; extend everyRuleSource", c)
		}
	}
	for c := range categories {
		if !declared[c] {
			t.Errorf("the inputs produced %q surface, which the provider's Capability does not declare", c)
		}
	}
	// Positive control 3: the registered helper changes the result — the authz
	// item's known_authz_detected fact flips from false to true — so a combined
	// call that ignored the project's helpers would not compare equal.
	plain, err := typescript.New().AnalyzeSurface(inline)
	if err != nil {
		t.Fatalf("AnalyzeSurface without helpers: %v", err)
	}
	withHelper, err := p.AnalyzeSurface(inline)
	if err != nil {
		t.Fatalf("AnalyzeSurface with helpers: %v", err)
	}
	if !authzDetected(plain, false) || !authzDetected(withHelper, true) {
		t.Errorf("registering %s does not flip known_authz_detected on everyRuleSource's authz item — "+
			"the equivalence cannot see the combined call drop the project's authz helpers.\n"+
			"without: %+v\nwith: %+v", everyRuleHelper, plain, withHelper)
	}
	// Positive control 4: the result depends on the path's case, so the
	// mixed-case input really checks that the combined call passes it unchanged.
	mixed, err := p.AnalyzeSurface(mixedCase)
	if err != nil {
		t.Fatalf("AnalyzeSurface(%s): %v", mixedCasePath, err)
	}
	lowered, err := p.AnalyzeSurface(providers.SourceFile{Path: strings.ToLower(mixedCasePath), Content: mixedCase.Content})
	if err != nil {
		t.Fatalf("AnalyzeSurface(lower-cased): %v", err)
	}
	if len(mixed) == len(lowered) {
		t.Errorf("%s and its lower-cased path give %d surface items each — the input no longer depends on "+
			"the path's case, so it cannot catch a combined call that changes it", mixedCasePath, len(mixed))
	}
}

// mixedCasePath is a route-shaped path whose base name differs from a Next.js
// route file only by case.
const mixedCasePath = "app/Orders/[id]/Route.ts"

// authzDetected reports whether items hold an authz item whose
// known_authz_detected fact equals want.
func authzDetected(items []findings.SurfaceItem, want bool) bool {
	for _, it := range items {
		if it.Category == string(surface.CategoryAuthz) && it.StructuralFacts["known_authz_detected"] == want {
			return true
		}
	}
	return false
}

// TestAnalyzeFile_ValidAndRealCodeReportComplete is the negative fixture of the
// detection: every valid parser-form row, in every extension it runs in, and
// every real or representative fixture outside parser_forms (the vendored real
// files included) must report a complete parse. A check that reported
// incomplete for everything would pass every positive row below and make the
// status meaningless; this is what catches it.
func TestAnalyzeFile_ValidAndRealCodeReportComplete(t *testing.T) {
	var paths []string
	for _, form := range validForms {
		for _, ext := range form.exts {
			paths = append(paths, filepath.Join(formsDir, form.file+ext))
		}
	}
	for _, path := range tsFixtures(t) {
		if !strings.Contains(filepath.ToSlash(path), "parser_forms/") {
			paths = append(paths, path)
		}
	}
	if len(paths) < 40 {
		t.Fatalf("only %d clean fixtures — fewer than the measured set", len(paths))
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			got := analyzeFile(t, readSource(t, path)).Parse
			if got != findings.ParseComplete() {
				t.Errorf("%s parses clean on the pinned parser, but AnalyzeFile reports %+v — "+
					"a valid file reported as not fully read", path, got)
			}
		})
	}
}

// wantReason is the measured reason for each still-an-error row, on the pinned
// parser (gotreesitter v0.54.0). Every row's form is on line 1, so every line is
// 1; TestAnalyzeFile_ReportsTheFirstAffectedLine pins lines past the first.
//
// Measured root of every row: `program`. No committed fixture parses to an
// ERROR root on v0.54.0, and neither did 68 synthetic inputs tried for it; the
// only ERROR root measured is a parse stopped by the memory budget, which the
// stop outranks (see the security sensor's budget test).
var wantReason = map[string]string{
	"broken_missing_initializer.ts":                findings.ParseReasonErrorNode,
	"broken_missing_initializer.tsx":               findings.ParseReasonErrorNode,
	"broken_unclosed_condition.ts":                 findings.ParseReasonMissingNode,
	"broken_unclosed_condition.tsx":                findings.ParseReasonMissingNode,
	"tagged_template_type_arguments.ts":            findings.ParseReasonMissingNode,
	"tagged_template_type_arguments.tsx":           findings.ParseReasonMissingNode,
	"tagged_template_array_type.ts":                findings.ParseReasonMissingNode,
	"tagged_template_array_type.tsx":               findings.ParseReasonMissingNode,
	"tagged_template_object_type.ts":               findings.ParseReasonMissingNode,
	"tagged_template_object_type.tsx":              findings.ParseReasonMissingNode,
	"tagged_template_generic_type.ts":              findings.ParseReasonMissingNode,
	"tagged_template_generic_type.tsx":             findings.ParseReasonMissingNode,
	"tagged_template_two_type_arguments.ts":        findings.ParseReasonMissingNode,
	"tagged_template_two_type_arguments.tsx":       findings.ParseReasonMissingNode,
	"tagged_template_array_type_without_await.ts":  findings.ParseReasonErrorNode,
	"tagged_template_array_type_without_await.tsx": findings.ParseReasonErrorNode,
	"jsx_text_leading_paren.tsx":                   findings.ParseReasonErrorNode,
	"jsx_text_leading_paren_in_expression.tsx":     findings.ParseReasonErrorNode,
	"jsx_text_lone_paren.tsx":                      findings.ParseReasonErrorNode,
	"jsx_attribute_bare_ampersand.tsx":             findings.ParseReasonErrorNode,
}

// TestAnalyzeFile_StillAnErrorRowsReportIncompleteWithTheirReason: every form
// the pinned parser still rejects — genuinely broken code and the declared
// residual alike — is reported incomplete, with the reason its tree shows. This
// covers the measured shapes of S11 that a fixture can reach: an ERROR node
// under a `program` root (so a healthy-looking root proves nothing), and a
// zero-width MISSING node with no ERROR node at all (the tagged-template
// residual after `await`).
func TestAnalyzeFile_StillAnErrorRowsReportIncompleteWithTheirReason(t *testing.T) {
	if len(wantReason) != len(stillAnError) {
		t.Fatalf("wantReason has %d rows, stillAnError has %d — every row needs its measured reason",
			len(wantReason), len(stillAnError))
	}
	for _, row := range stillAnError {
		t.Run(row.file, func(t *testing.T) {
			reason, ok := wantReason[row.file]
			if !ok {
				t.Fatalf("no measured reason for %s", row.file)
			}
			got := analyzeFile(t, readSource(t, filepath.Join(formsDir, row.file))).Parse
			if want := findings.ParseIncomplete(reason, 1); got != want {
				t.Errorf("%s (%s): status %+v, want %+v", row.file, row.why, got, want)
			}
		})
	}
}

// TestAnalyzeFile_ReportsTheFirstAffectedLine pins the line and the fixed order
// of the reasons on inputs whose problem is NOT on line 1, so a check that
// always answered line 1, or answered by position instead of by order, fails.
func TestAnalyzeFile_ReportsTheFirstAffectedLine(t *testing.T) {
	// Measured on the pinned parser. Note that `const x = ;` is NOT used past
	// line 1: on line 1 it leaves an ERROR node (broken_missing_initializer),
	// after other statements the parser recovers with a MISSING node instead.
	cases := []struct {
		name, path, content string
		want                findings.ParseStatus
	}{
		{"an error node on line 3", "late_error.ts",
			"const a = 1;\nconst b = 2;\n}}}}\nconst apiKey = \"s3cr3t\";\n",
			findings.ParseIncomplete(findings.ParseReasonErrorNode, 3)},
		{"a missing node on line 3", "late_missing.ts",
			"const a = 1;\n\nif (a {\n}\n",
			findings.ParseIncomplete(findings.ParseReasonMissingNode, 3)},
		// The order is by REASON, not by position: a MISSING node on line 2 and
		// an ERROR node on line 4 report the error node, because an error node
		// can hide source text and a missing node only stands in for a token.
		{"an error node outranks an earlier missing node", "both.ts",
			"const a = 1;\nif (a {\n}\nconst c = @@@;\n",
			findings.ParseIncomplete(findings.ParseReasonErrorNode, 4)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := analyzeFile(t, providers.SourceFile{Path: c.path, Content: []byte(c.content)}).Parse
			if got != c.want {
				t.Errorf("status %+v, want %+v", got, c.want)
			}
		})
	}
}

// --- the error_flag arm, in a child process ------------------------------------

// The parser reads GOT_C_RECOVERY once per process (its environment knobs are
// loaded once), so the setting can only be changed for a whole process: the test
// re-runs this test binary as a child with it set, the child runs the real
// AnalyzeFile and writes the analysis to a file, and the parent asserts on it.
const (
	errorFlagChildEnv    = "CODEFIT_TEST_TS_ERROR_FLAG_CHILD"
	errorFlagChildOutput = "CODEFIT_TEST_TS_ERROR_FLAG_CHILD_OUTPUT"
	cRecoveryEnv         = "GOT_C_RECOVERY"
)

// cutShortSource reaches the last arm, error_flag, through the real parser. It is
// the shape gotreesitter v0.54.0 documents beside markTruncatedTreeHasError
// (parser_api.go): the GLR frontier dies mid-file (stop reason no_stacks_alive,
// which is NOT a stop ParseStoppedEarly reports) after the live stack reduced to
// a clean `program`, so the parser returns a prefix-only root with no ERROR and
// no MISSING node, and only sets the root's error flag because real source past
// the root's end was dropped.
//
// Measured on v0.54.0 with GOT_C_RECOVERY=0 (the parser's older, resync-based
// recovery): stop reason no_stacks_alive, truncated, the root ends at byte 25 —
// the end of line 2 — of 56, error flag set, no ERROR or MISSING node; the same
// on 30 of 30 runs. Line 3 is a lone backslash followed by the text x01 (a
// literal backslash, not a control byte). Under the default settings the same
// bytes give an ERROR node on line 3 instead, and neither shape was found under
// the default settings in the 68 committed TypeScript files, the generated
// inputs tried for it, or a TypeScript lib.dom.d.ts.
const cutShortSource = "const a = 1;\nconst b = 2;\n\\x01\n" + `const apiKey = "s3cr3t";` + "\n"

// TestHelperProcessTypeScriptErrorFlag is not a test: it is the child process
// TestAnalyzeFile_CutShortCleanPrefixReportsErrorFlag runs. Without its
// environment variable it does nothing.
func TestHelperProcessTypeScriptErrorFlag(t *testing.T) {
	if os.Getenv(errorFlagChildEnv) != "1" {
		t.Skip("helper process for the error_flag test; runs only as its child")
	}
	a, err := typescript.New().AnalyzeFile(providers.SourceFile{Path: "cut_short.ts", Content: []byte(cutShortSource)})
	if err != nil {
		t.Fatalf("AnalyzeFile: %v", err)
	}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv(errorFlagChildOutput), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestAnalyzeFile_CutShortCleanPrefixReportsErrorFlag pins the last arm through
// the real parser: a root cut short mid-file with only the error flag set is
// reported incomplete with reason error_flag — and its line 0, because no node
// names the place — never complete. The credential on line 4 lies past the root's
// end and is lost, which is exactly why "complete" here would be a false clean.
// The in-process run is the control: under the default settings the same bytes
// take another arm, so the child's result comes from the setting, not the input.
func TestAnalyzeFile_CutShortCleanPrefixReportsErrorFlag(t *testing.T) {
	output := filepath.Join(t.TempDir(), "child.json")
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcessTypeScriptErrorFlag$", "-test.count=1")
	cmd.Env = append(os.Environ(), errorFlagChildEnv+"=1", errorFlagChildOutput+"="+output, cRecoveryEnv+"=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child process failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("the child wrote no result: %v", err)
	}
	var child providers.FileAnalysis
	if err := json.Unmarshal(data, &child); err != nil {
		t.Fatalf("decoding the child result: %v", err)
	}
	if want := findings.ParseIncomplete(findings.ParseReasonErrorFlag, 0); child.Parse != want {
		t.Errorf("with %s=0 the status is %+v, want %+v — a root cut short with only the error flag "+
			"set must read as incomplete", cRecoveryEnv, child.Parse, want)
	}
	for _, f := range child.Findings {
		if f.ID == "SEC-001" && f.Line == 4 {
			t.Errorf("with %s=0 the credential on line 4 IS reported — the root is no longer cut short "+
				"before it, so this input no longer reaches the error_flag shape; re-measure: %+v",
				cRecoveryEnv, child.Findings)
		}
	}

	control := analyzeFile(t, providers.SourceFile{Path: "cut_short.ts", Content: []byte(cutShortSource)}).Parse
	if want := findings.ParseIncomplete(findings.ParseReasonErrorNode, 3); control != want {
		t.Errorf("control: under the default settings the status is %+v, want %+v", control, want)
	}
}

// TestSettingsFingerprint_FollowsEveryParserEnvironmentPrefix (D2): the
// fingerprint changes with every variable under each name prefix gotreesitter
// v0.54.0 reads — GOT_ (57 names, GOT_PARSE_MEMORY_BUDGET_MB among them),
// GOTREESITTER_ (the grammar loader) and GTS_ (the admission and dispatch
// switches) — and with a lower-case spelling, which Windows' environment treats
// as the same name. The probe names are ones the parser does not read, so this
// test cannot change how any other test in the process parses.
func TestSettingsFingerprint_FollowsEveryParserEnvironmentPrefix(t *testing.T) {
	p := typescript.New()
	for _, name := range []string{
		"GOT_CODEFIT_FINGERPRINT_PROBE",
		"GOTREESITTER_CODEFIT_FINGERPRINT_PROBE",
		"GTS_CODEFIT_FINGERPRINT_PROBE",
		"got_codefit_fingerprint_probe_lower",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "1")
			one := p.SettingsFingerprint()
			t.Setenv(name, "2")
			two := p.SettingsFingerprint()
			if one == "" || one == two {
				t.Errorf("fingerprint with %s=1 is %q and with %s=2 is %q — a setting the parser may read "+
					"does not reach the cache key", name, one, name, two)
			}
		})
	}
}

// TestSettingsFingerprint_IgnoresUnrelatedEnvironment: the other direction. A
// fingerprint of the WHOLE environment would be safe but would make the cache
// miss on every shell difference; only the parser's names count.
func TestSettingsFingerprint_IgnoresUnrelatedEnvironment(t *testing.T) {
	p := typescript.New()
	t.Setenv("CODEFIT_UNRELATED_FINGERPRINT_PROBE", "1")
	one := p.SettingsFingerprint()
	t.Setenv("CODEFIT_UNRELATED_FINGERPRINT_PROBE", "2")
	if two := p.SettingsFingerprint(); one != two {
		t.Errorf("an unrelated variable changed the fingerprint (%q then %q)", one, two)
	}
}
