package typescript_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// These tests are a gate for the TypeScript parser dependency (ADR 0088). The
// parser, github.com/odvcencio/gotreesitter, is error-recovering: given a form
// its grammar does not accept, it returns a tree whose root may be an ERROR node,
// and every rule and surface query then sees a different tree. A file that trips
// on one ordinary line can stop reporting everything after it (issue #189).
// Upstream behaviour across versions is measured NON-monotonic — a form fixed in
// one release breaks again in a later one.
//
// NECESSARY, NOT SUFFICIENT. A bump that fails a row here is rejected. A bump
// that passes every row is NOT thereby accepted: these rows hold only the forms
// that were reduced to a synthetic fixture, and real code holds forms nobody has
// reduced yet. Accepting a bump also requires re-running the census over real
// TypeScript projects and comparing findings and surface items by identity, as
// ADR 0088 did; that census is not automated in this repository. Which
// regressions the rows catch, by version, is in the guards column below and in
// ADR 0088; what they do not catch is written there too.
//
// Every fixture under testdata/parser_forms is synthetic: written here, one
// syntactic form per file, never copied from a real project. Each holds the
// form on its first line and a hardcoded credential AFTER it, so a row proves
// that the rest of the file is still read, not only that the form parses.

// trailingFinding is the line every fixture carries after its form. SEC-001 fires
// on it through the provider's AST rules; a fixture whose tree is broken by the
// form above it loses it.
const trailingFinding = `const apiKey = "s3cr3t";`

// formsDir is where the parser-form fixtures live.
var formsDir = filepath.Join("testdata", "parser_forms")

// Extensions a row runs in.
var (
	bothExts = []string{".ts", ".tsx"}
	tsxOnly  = []string{".tsx"}
)

// validForms are forms TypeScript accepts that must parse with no error. marker
// is a substring the fixture must contain, so a fixture edited away from its
// form fails here instead of passing for the wrong reason. guards names the
// gotreesitter versions on which the row was OBSERVED to fail — the regression
// it exists to catch; a row with no such version pins a boundary instead.
var validForms = []struct {
	file, marker string
	exts         []string
	guards       string
	why          string
}{
	{"typed_arrow_parameter", "(n: number) =>", bothExts, "v0.20.2",
		"a type-annotated arrow parameter; the largest class of the census. The arrow sits inside another " +
			"arrow's body because v0.20.2 accepted the bare top-level form in .tsx and rejected it only there " +
			"— a top-level fixture would have been green on the old parser in .tsx and proved nothing"},
	{"typed_arrow_return_type", "): number =>", bothExts, "v0.20.2",
		"a typed arrow parameter followed by a return-type annotation"},
	{"function_default_literal", `(path = "lib/a.ts")`, bothExts, "v0.20.2",
		"a function-declaration parameter whose default is a literal"},
	{"arrow_default", "(flag = true) =>", bothExts, "v0.20.2", "an arrow parameter with a default value"},
	{"import_type", `import("./handlers").Handler`, bothExts, "v0.20.2", `an import("…") type in a type annotation`},
	{"arrow_array_destructured", "([a, b], [c, d]) =>", bothExts, "v0.20.2", "array-destructured arrow parameters"},
	{"jsx_text_equals_sign", "<p>total = gross - tax</p>", tsxOnly, "v0.43.0, v0.43.1, v0.47.0, v0.50.0, v0.53.0",
		"an `=` in JSX text. Those versions turn it into ERROR nodes under a program root; in a real file the " +
			"error spread over the whole file. v0.20.2 and v0.54.0 parse it clean"},
	{"tagged_template_type_name", "sql<Row>`", bothExts, "",
		"boundary of residual (a): a tagged template whose type argument is one type name parses clean"},
	{"tagged_template_tuple_of_names", "sql<[A, B]>`", bothExts, "",
		"boundary of residual (a): a tuple of type names as the type argument parses clean"},
	{"tagged_template_union", "sql<A | B>`", bothExts, "",
		"boundary of residual (a): a union of type names as the type argument parses clean"},
	{"jsx_text_paren_after_attribute", `<span className="hint">(optional)</span>`, tsxOnly, "",
		"boundary of residual (c): JSX text starting with `(` parses clean when its opening tag has an attribute"},
	{"jsx_text_paren_on_own_line", "\n      (optional)\n", tsxOnly, "",
		"boundary of residual (c): JSX text starting with `(` parses clean on its own line"},
	{"jsx_text_paren_nested_in_parent", "<div><span>(optional)</span></div>", tsxOnly, "",
		"boundary of residual (c): the same attribute-less element parses clean when it is nested directly " +
			"inside another JSX element; only an element standing where an expression stands errors"},
	// The .tsx forms v0.58.0 rejects and v0.54.0 parses clean are NOT here: three
	// census files show them, each error 18 to 34 bytes, and none was reduced to a
	// synthetic fixture. Line-level reduction keeps a structure of more than a
	// hundred lines, and renaming its identifiers changes how v0.54.0 parses it,
	// so no row can stand in for them. These rows therefore do not reject
	// v0.58.0; the census does (ADR 0088).
}

// readForm reads a fixture and fails unless it holds the declared marker and the
// later secret, in that order: a fixture is verified by its content, not by its
// name.
func readForm(t *testing.T, name, marker string) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(formsDir, name))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	at := bytes.Index(content, []byte(marker))
	if at < 0 {
		t.Fatalf("fixture %s does not contain its form %q — it no longer tests what its name says", name, marker)
	}
	secretAt := bytes.Index(content, []byte(trailingFinding))
	if secretAt < 0 {
		t.Fatalf("fixture %s does not carry the later secret %q", name, trailingFinding)
	}
	if secretAt < at {
		t.Fatalf("fixture %s holds the secret BEFORE its form; it would not prove the rest of the file is read", name)
	}
	return content
}

// secretLine is the 1-based line of the later secret in content.
func secretLine(content []byte) int {
	return bytes.Count(content[:bytes.Index(content, []byte(trailingFinding))], []byte("\n")) + 1
}

// reportsLaterSecret reports whether SEC-001 fired on the later secret's line.
func reportsLaterSecret(finds []findings.Finding, line int) bool {
	for _, f := range finds {
		if f.ID == "SEC-001" && f.Line == line {
			return true
		}
	}
	return false
}

// orNone renders an empty guards column.
func orNone(guards string) string {
	if guards == "" {
		return "no version — a boundary row"
	}
	return guards
}

// TestParserForms_ValidFormsParseAndKeepTheLaterFinding: each form parses with
// no error through the real provider, in each extension its row names, and the
// finding after it is still reported by the real AnalyzeSecurity.
func TestParserForms_ValidFormsParseAndKeepTheLaterFinding(t *testing.T) {
	p := typescript.New()
	for _, form := range validForms {
		for _, ext := range form.exts {
			name := form.file + ext
			t.Run(name, func(t *testing.T) {
				content := readForm(t, name, form.marker)
				src := providers.SourceFile{Path: name, Content: content}

				root, err := p.Parse(src)
				if err != nil {
					t.Fatalf("Parse: %v", err)
				}
				if root.HasError() {
					t.Errorf("%s parses WITH an error (root %q): the parser rejects %s (observed failing before "+
						"on: %s). Do not bump the parser to this version; see ADR 0088", name, root.Type(), form.why,
						orNone(form.guards))
				}

				finds, err := p.AnalyzeSecurity(src)
				if err != nil {
					t.Fatalf("AnalyzeSecurity: %v", err)
				}
				if line := secretLine(content); !reportsLaterSecret(finds, line) {
					t.Errorf("%s: the credential on line %d, after %s, is not reported (got %+v) — "+
						"the parse error silenced the rest of the file (issue #189)", name, line, form.why, finds)
				}
			})
		}
	}
}

// stillAnError are forms the pinned parser still reports as a syntax error. The
// broken_* rows are genuinely invalid code and must stay errors under any
// parser. The others are VALID TypeScript the pinned parser rejects: the
// residual of ADR 0088, declared in the coverage manifest
// (ts.parser-residual-forms).
//
// When the parser starts accepting a residual row, that is NOT by itself a reason
// to accept the version: v0.58.0 accepts every residual (a) row below and breaks
// .tsx forms elsewhere. Move the row to validForms only for a version that
// passes every other row AND the census over real projects (ADR 0088).
//
// laterSecretKept records, for each row, whether the finding after the error is
// still reported on the pinned parser — whether the error is local or silences
// the rest of the file. It is pinned in both directions, because the residual's
// declared cost depends on it.
var stillAnError = []struct {
	file, marker    string
	valid           bool
	laterSecretKept bool
	why             string
}{
	{"broken_missing_initializer.ts", "const x = ;", false, true, "an initializer is missing"},
	{"broken_missing_initializer.tsx", "const x = ;", false, true, "an initializer is missing"},
	{"broken_unclosed_condition.ts", "if (x {", false, true, "the condition's parenthesis is never closed"},
	{"broken_unclosed_condition.tsx", "if (x {", false, true, "the condition's parenthesis is never closed"},
	{"tagged_template_type_arguments.ts", "sql<[{ id: number }]>`", true, true,
		"residual (a): a tagged template whose explicit type argument is " +
			"a tuple holding an object type; after `await` the parser leaves a MISSING `!` and no ERROR node"},
	{"tagged_template_type_arguments.tsx", "sql<[{ id: number }]>`", true, true,
		"residual (a): a tagged template whose explicit type argument is " +
			"a tuple holding an object type; after `await` the parser leaves a MISSING `!` and no ERROR node"},
	{"tagged_template_array_type.ts", "sql<Row[]>`", true, true,
		"residual (a): a tagged template whose explicit type argument is " +
			"an array type; after `await` the parser leaves a MISSING `!`"},
	{"tagged_template_array_type.tsx", "sql<Row[]>`", true, true,
		"residual (a): a tagged template whose explicit type argument is " +
			"an array type; after `await` the parser leaves a MISSING `!`"},
	{"tagged_template_object_type.ts", "sql<{ id: number }>`", true, true,
		"residual (a): a tagged template whose explicit type argument is " +
			"an object type; after `await` the parser leaves a MISSING `!`"},
	{"tagged_template_object_type.tsx", "sql<{ id: number }>`", true, true,
		"residual (a): a tagged template whose explicit type argument is " +
			"an object type; after `await` the parser leaves a MISSING `!`"},
	{"tagged_template_generic_type.ts", "sql<Array<Row>>`", true, true,
		"residual (a): a tagged template whose explicit type argument is " +
			"a generic type; after `await` the parser leaves a MISSING `!`. v0.20.2 parsed this one clean"},
	{"tagged_template_generic_type.tsx", "sql<Array<Row>>`", true, true,
		"residual (a): a tagged template whose explicit type argument is " +
			"a generic type; after `await` the parser leaves a MISSING `!`. v0.20.2 parsed this one clean"},
	{"tagged_template_two_type_arguments.ts", "sql<A, B>`", true, true,
		"residual (a): a tagged template whose explicit type argument is " +
			"two type arguments; after `await` the parser leaves a MISSING `!`"},
	{"tagged_template_two_type_arguments.tsx", "sql<A, B>`", true, true,
		"residual (a): a tagged template whose explicit type argument is " +
			"two type arguments; after `await` the parser leaves a MISSING `!`"},
	{"tagged_template_array_type_without_await.ts", "= sql<Row[]>`", true, true,
		"residual (a): a tagged template whose explicit type argument is " +
			"an array type, with no `await` before the call; the parser leaves an ERROR node instead of a MISSING `!`"},
	{"tagged_template_array_type_without_await.tsx", "= sql<Row[]>`", true, true,
		"residual (a): a tagged template whose explicit type argument is " +
			"an array type, with no `await` before the call; the parser leaves an ERROR node instead of a MISSING `!`"},
	{"jsx_text_leading_paren.tsx", "<span>(optional)</span>", true, false,
		"residual (c): JSX text that starts with `(` right after an opening tag with no attribute, on the same " +
			"line, in an element that is not nested inside another JSX element or fragment (here after `=>`). " +
			"New relative to v0.20.2, which parsed it clean; v0.53.0 and v0.58.0 reject it too. NOT " +
			"local: the ERROR node runs into the start of the next statement, and the finding on the next line is " +
			"LOST"},
	{"jsx_text_leading_paren_in_expression.tsx", "<span>(optional)</span>}", true, false,
		"residual (c) inside a {…} expression container, which is expression position although a parent " +
			"element surrounds it; the ERROR node spans the whole file and the later " +
			"finding is LOST"},
	{"jsx_text_lone_paren.tsx", "<span>(</span>", true, false,
		"residual (c) with a lone `(` as the text; the ERROR node spans the whole file and the later finding " +
			"is LOST"},
	{"jsx_attribute_bare_ampersand.tsx", `?family=A&display=swap"`, true, true,
		"residual (b): a bare `&` followed by a name, with no closing `;`, inside a quoted JSX attribute value " +
			"(a query string in an href). The grammar reads it as an unterminated character reference and " +
			"leaves an ERROR node over the reference (its width is that of the text, `&b=2` gives 4 bytes). " +
			"v0.20.2 parsed this form clean (v0.53.0 rejects it too). `&amp;`, `& b` and the same text " +
			"as a JSX child parse clean"},
}

// TestParserForms_StillAnErrorToday pins what the pinned parser still rejects,
// so a parser that starts accepting broken code, or starts accepting a residual
// form, is seen and examined instead of slipping through.
func TestParserForms_StillAnErrorToday(t *testing.T) {
	p := typescript.New()
	for _, row := range stillAnError {
		t.Run(row.file, func(t *testing.T) {
			content := readForm(t, row.file, row.marker)
			src := providers.SourceFile{Path: row.file, Content: content}

			root, err := p.Parse(src)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !root.HasError() {
				if row.valid {
					t.Errorf("the parser now ACCEPTS %s (%s). That alone does not make this version acceptable "+
						"(v0.58.0 accepts residual (a) and breaks .tsx forms elsewhere): run every other row and the "+
						"census over real projects (ADR 0088) first; only then move the row to validForms and remove "+
						"it from the declared residual in coverage.go, COVERAGE.md and ADR 0088", row.file, row.why)
				} else {
					t.Errorf("the parser now accepts genuinely broken code in %s (%s). Re-examine before "+
						"bumping: an error-free tree for invalid code is a tree codefit cannot trust", row.file, row.why)
				}
			}

			finds, err := p.AnalyzeSecurity(src)
			if err != nil {
				t.Fatalf("AnalyzeSecurity: %v", err)
			}
			line := secretLine(content)
			if got := reportsLaterSecret(finds, line); got != row.laterSecretKept {
				t.Errorf("%s: the credential on line %d after the error reported=%v, pinned %v. "+
					"Re-examine whether this parse error is still local; the declared residual depends on it",
					row.file, line, got, row.laterSecretKept)
			}
		})
	}
}

// parseCeiling bounds the wall-clock time the provider may take to parse every
// TypeScript fixture under testdata, the vendored large real files included.
//
// Why a ceiling at all: gotreesitter v0.21.0 was measured taking 79 to 121
// SECONDS on one ordinary 25 KB file (ADR 0088). A regression of that kind is a
// scan that never returns, and no correctness row above would notice it.
//
// Why this value: on the pinned parser the whole set (63 files) was measured at
// about 0.22 s, and about 1.9 s under -race. Thirty seconds is far above CI noise
// and far below a regression measured in minutes. It is a guard, not a
// benchmark: it says nothing about small slowdowns.
//
// What it does NOT catch, measured: v0.21.0 itself parses this set in about
// 1 s. Its slowdown needs a specific interaction inside that one real file —
// it grew from 1.7 s to 13.9 s as more of the file followed a region of nested
// `as`-casts to function types — and no synthetic file written for this test
// reproduced it. The file is not vendored: it is a foreign project's code. So
// this ceiling catches a regression that slows every parse, not that one.
const parseCeiling = 30 * time.Second

// TestParserForms_ParseTimeCeiling parses every .ts and .tsx fixture once and
// fails when the total exceeds parseCeiling.
func TestParserForms_ParseTimeCeiling(t *testing.T) {
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
	// The positive control: the set is not empty, and it holds the large real
	// files, so a fast result is not the result of parsing nothing.
	for _, want := range []string{"real_vscode_strings.ts", "real_excalidraw_actions.tsx", "typed_arrow_parameter.tsx"} {
		found := false
		for _, f := range files {
			found = found || filepath.Base(f) == want
		}
		if !found {
			t.Fatalf("the timed set does not include %s; it would time less than it claims (%d files)", want, len(files))
		}
	}

	p := typescript.New()
	start := time.Now()
	for _, f := range files {
		content, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		if _, err := p.Parse(providers.SourceFile{Path: f, Content: content}); err != nil {
			t.Fatalf("Parse(%s): %v", f, err)
		}
	}
	if took := time.Since(start); took > parseCeiling {
		t.Errorf("parsing %d fixtures took %v, over the %v ceiling: the parser has a performance "+
			"regression of the kind v0.21.0 had (ADR 0088). Do not bump to this version", len(files), took, parseCeiling)
	}
}
