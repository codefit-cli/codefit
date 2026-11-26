package typescript_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
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

// TestAnalyzeFile_EqualsTheTwoSeparateCalls: the combined call is an
// optimisation of AnalyzeSecurity + AnalyzeSurface, never a different analysis.
// Over every TypeScript fixture, errored ones included, it returns exactly the
// same findings and the same surface, in the same order.
func TestAnalyzeFile_EqualsTheTwoSeparateCalls(t *testing.T) {
	p := typescript.New()
	totalFindings, totalSurface := 0, 0
	for _, path := range tsFixtures(t) {
		src := readSource(t, path)
		combined := analyzeFile(t, src)

		sec, err := p.AnalyzeSecurity(src)
		if err != nil {
			t.Fatalf("AnalyzeSecurity(%s): %v", path, err)
		}
		surf, err := p.AnalyzeSurface(src)
		if err != nil {
			t.Fatalf("AnalyzeSurface(%s): %v", path, err)
		}
		if !reflect.DeepEqual(combined.Findings, sec) {
			t.Errorf("%s: AnalyzeFile findings differ from AnalyzeSecurity.\ncombined: %+v\nseparate: %+v",
				path, combined.Findings, sec)
		}
		if !reflect.DeepEqual(combined.Surface, surf) {
			t.Errorf("%s: AnalyzeFile surface differs from AnalyzeSurface.\ncombined: %+v\nseparate: %+v",
				path, combined.Surface, surf)
		}
		totalFindings += len(sec)
		totalSurface += len(surf)
	}
	// Positive control: an equality over empty results proves nothing.
	if totalFindings == 0 || totalSurface == 0 {
		t.Fatalf("the fixtures produced %d findings and %d surface items — the comparison is vacuous",
			totalFindings, totalSurface)
	}
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
