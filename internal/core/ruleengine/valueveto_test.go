package ruleengine_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/ruleengine"
)

// SPEC (issue #148, ADR 0087) — `metavariable-value-veto`, a constraint that
// DROPS a match when the value bound to a metavariable proves, by its own shape,
// that it is something the rule is not about.
//
//	metavariable-value-veto:
//	  $VALUE: not-a-credential
//
// WHAT IT RECEIVES. The source text bound to the metavariable, quotes included.
// It is decoded as a JavaScript string literal first, and only a literal that
// decodes is handed to the predicate (not-a-credential is
// valueshape.NotACredential, the function the Go provider calls). A value that
// does not decode — a concatenation, a legacy octal escape, a lone surrogate —
// proves nothing, and the match is KEPT.
//
// WHEN IT RUNS. At bind time, like metavariable-regex and metavariable-name, so
// it steers the object-subset search: in `{secretFile: "lib/config.ts", apiKey:
// "s3cr3t"}` the first pair is rejected while it is being bound and the search
// goes on to the real credential. A filter applied after the match would have
// dropped the whole object, a false all-clear.
//
// WHAT FAILS AT COMPILE. An unknown predicate; a key that is not a
// metavariable; a metavariable some base alternative never binds; and a
// metavariable that stands, in some base alternative, where a whole string
// literal cannot (inside a string, a template or a regex; a shorthand property,
// a member name or a declaration name). Each one would otherwise compile and
// then either never veto or make the rule silent.

// vetoForms is one TypeScript shape per alternative the rule below declares,
// mirroring the six SEC-001 alternatives. name and lit are spliced in as source.
var vetoForms = []struct {
	form string
	src  func(name, lit string) string
}{
	{"const", func(n, l string) string { return "const " + n + " = " + l + ";" }},
	{"let", func(n, l string) string { return "let " + n + " = " + l + ";" }},
	{"var", func(n, l string) string { return "var " + n + " = " + l + ";" }},
	{"typed const", func(n, l string) string { return "const " + n + ": string = " + l + ";" }},
	{"typed let", func(n, l string) string { return "let " + n + ": string = " + l + ";" }},
	{"object property", func(n, l string) string {
		return "const cfg = { retries: 3, " + n + ": " + l + ", region: 1 };"
	}},
}

// vetoRule is SEC-001's shape with the veto: every alternative binds $VALUE.
func vetoRule() ruleengine.Rule {
	return ruleengine.Rule{
		ID: "T-VETO", Message: "m", Severity: "high", Dimension: "security",
		PatternEither: []string{
			"const $NAME = $VALUE",
			"let $NAME = $VALUE",
			"({...$REST, $NAME: $VALUE})",
			"var $NAME = $VALUE",
			"const $NAME: $T = $VALUE",
			"let $NAME: $T = $VALUE",
		},
		MetavariableName:      map[string]string{"$NAME": "credential"},
		MetavariableRegex:     map[string]string{"$VALUE": `^["'].+["']$`},
		MetavariableValueVeto: map[string]string{"$VALUE": "not-a-credential"},
	}
}

func compileVeto(t *testing.T) []ruleengine.CompiledRule {
	t.Helper()
	c, err := ruleengine.Compile([]ruleengine.Rule{vetoRule()}, tsParse)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return c
}

func vetoMatches(t *testing.T, compiled []ruleengine.CompiledRule, src string) int {
	t.Helper()
	n, err := tsParse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return len(ruleengine.Match(compiled, n, "f.ts"))
}

// TestValueVetoIsQuietAtEveryForm: a value that proves it is a file path is
// dropped at each of the six forms, and the same form with a value that proves
// nothing fires. The control is what makes the silence mean something.
func TestValueVetoIsQuietAtEveryForm(t *testing.T) {
	compiled := compileVeto(t)
	for _, f := range vetoForms {
		t.Run(f.form, func(t *testing.T) {
			if got := vetoMatches(t, compiled, f.src("apiKey", `"s3cr3t"`)); got != 1 {
				t.Fatalf("control: %s with \"s3cr3t\" -> %d findings, want 1 — the form does not reach the rule", f.form, got)
			}
			if got := vetoMatches(t, compiled, f.src("secretFile", `"lib/config.ts"`)); got != 0 {
				t.Errorf("%s with a file path -> %d findings, want 0 — the value proves it is not a credential", f.form, got)
			}
		})
	}
}

// TestValueVetoSteersTheObjectSearch is the reason the veto runs at bind time.
// The vetoed pair comes FIRST, so a post-match filter would bind it, drop the
// object and never reach the credential behind it.
func TestValueVetoSteersTheObjectSearch(t *testing.T) {
	compiled := compileVeto(t)
	cases := []struct {
		src  string
		want int
		why  string
	}{
		{`const cfg = { secretFile: "lib/config.ts", apiKey: "s3cr3t" };`, 1,
			"the vetoed pair first: the search must go on to apiKey"},
		{`const cfg = { apiKey: "s3cr3t", secretFile: "lib/config.ts" };`, 1,
			"the credential first: found before the vetoed pair is tried"},
		{`const cfg = { secretFile: "lib/config.ts", tokenUrl: "https://auth.example.com/oauth/token" };`, 0,
			"every credential-named pair is vetoed, so the object proves nothing"},
	}
	for _, tc := range cases {
		if got := vetoMatches(t, compiled, tc.src); got != tc.want {
			t.Errorf("%s -> %d findings, want %d — %s", tc.src, got, tc.want, tc.why)
		}
	}
}

// TestValueVetoDecodesBeforeItDecides pins that the predicate reads the string
// the literal DENOTES, not the characters between its quotes.
func TestValueVetoDecodesBeforeItDecides(t *testing.T) {
	compiled := compileVeto(t)
	cases := []struct {
		lit  string
		want int
		why  string
	}{
		{`'lib/config.ts'`, 0, "a single-quoted literal decodes like a double-quoted one"},
		{`"https:\/\/api.example.com\/v1\/users"`, 0,
			"`\\/` is an identity escape: the literal denotes a plain URL. Trimming the quotes instead " +
				"hands the predicate `https:\\/\\/…`, which has no scheme, and the match fires"},
		{`"lib\u002fconfig.ts"`, 0, "a \\u escape denotes the '/' of a file path"},
		{`"https://api.example.com/v1?api_key\x3dabc"`, 1,
			"the literal denotes `?api_key=abc`, a credential-named parameter carrying a value. Trimming " +
				"the quotes leaves `api_key\\x3dabc`, a parameter with no value, and would veto it"},
		{`"https://api.example.com" + "/v1/users"`, 1,
			"a concatenation is not ONE literal: it does not decode, proves nothing, and keeps firing"},
		{`"lib/config\1.ts"`, 1, "a legacy octal escape does not decode, so it proves nothing"},
		{`"https://api.example.com/\uD800"`, 1, "a lone surrogate does not decode, so it proves nothing"},
	}
	for _, tc := range cases {
		// A row whose reason speaks of an escape must carry one: see the guard
		// in TestDecodeJSStringLiteral for how such a row lost its backslash.
		if mentionsAnEscape(tc.why) && !strings.Contains(tc.lit, `\`) {
			t.Errorf("row %s speaks of an escape but carries no backslash — it pins plain text", tc.lit)
			continue
		}
		src := "const apiKey = " + tc.lit + ";"
		if got := vetoMatches(t, compiled, src); got != tc.want {
			t.Errorf("%s -> %d findings, want %d — %s", src, got, tc.want, tc.why)
		}
	}
}

// mentionsAnEscape reports whether a row's stated reason claims it exercises a
// backslash escape (an escape class, a surrogate or a line continuation). Such
// a row must carry a backslash in its literal, or it pins nothing it claims.
func mentionsAnEscape(reason string) bool {
	r := strings.ToLower(reason)
	return strings.Contains(r, "escape") || strings.Contains(r, "surrogate") || strings.Contains(r, "continuation")
}

// TestValueVetoComposesWithNameAndRegex: the name vocabulary, the regex and the
// veto all hold at once. If one silently won, the rule would affirm on part of
// its evidence or veto on part of it.
func TestValueVetoComposesWithNameAndRegex(t *testing.T) {
	compiled := compileVeto(t)
	for _, tc := range []struct {
		src  string
		want int
		why  string
	}{
		{`const apiKey = "s3cr3t";`, 1, "name, regex and veto all let it through"},
		{`const apiKey = readKey();`, 0, "the regex fails: not a quoted literal"},
		{`const tokenizer = "s3cr3t";`, 0, "the name fails: tokenizer is not a credential"},
		{`const apiKey = "lib/config.ts";`, 0, "the veto fails: a file path"},
		{"const apiKey = `lib/config.ts`;", 0, "the regex fails first: a template literal never reaches the veto"},
	} {
		if got := vetoMatches(t, compiled, tc.src); got != tc.want {
			t.Errorf("%s -> %d findings, want %d — %s", tc.src, got, tc.want, tc.why)
		}
	}
}

// TestValueVetoCompileErrors: every way a veto could compile and then do nothing
// is a located compile error instead.
func TestValueVetoCompileErrors(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(r *ruleengine.Rule)
		wantInErr []string
	}{
		{
			name:      "unknown predicate",
			mutate:    func(r *ruleengine.Rule) { r.MetavariableValueVeto = map[string]string{"$VALUE": "not-a-thing"} },
			wantInErr: []string{"T-VETO", "not-a-thing", "not-a-credential"},
		},
		{
			name:      "key is not a metavariable",
			mutate:    func(r *ruleengine.Rule) { r.MetavariableValueVeto = map[string]string{"VALUE": "not-a-credential"} },
			wantInErr: []string{"T-VETO", `"VALUE"`},
		},
		{
			name:      "key no alternative binds",
			mutate:    func(r *ruleengine.Rule) { r.MetavariableValueVeto = map[string]string{"$OTHER": "not-a-credential"} },
			wantInErr: []string{"T-VETO", "$OTHER"},
		},
		{
			name: "key one alternative does not bind",
			mutate: func(r *ruleengine.Rule) {
				r.PatternEither = append(r.PatternEither, "readSecret($NAME)")
			},
			wantInErr: []string{"T-VETO", "$VALUE", "readSecret($NAME)"},
		},
		{
			name: "key is the object ellipsis marker, which never binds",
			mutate: func(r *ruleengine.Rule) {
				r.PatternEither = []string{"({...$REST, $NAME: $VALUE})"}
				r.MetavariableValueVeto = map[string]string{"$REST": "not-a-credential"}
			},
			wantInErr: []string{"T-VETO", "$REST"},
		},
		{
			name: "key unbound by a single pattern",
			mutate: func(r *ruleengine.Rule) {
				r.PatternEither = nil
				r.Pattern = "readSecret($NAME)"
				r.MetavariableRegex = nil
			},
			wantInErr: []string{"T-VETO", "$VALUE"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := vetoRule()
			tc.mutate(&r)
			_, err := ruleengine.Compile([]ruleengine.Rule{r}, tsParse)
			if err == nil {
				t.Fatalf("Compile accepted it; the veto would never run, or the rule would go silent")
			}
			for _, want := range tc.wantInErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q — it must locate the failure", err.Error(), want)
				}
			}
		})
	}
	// The control: the unmutated rule compiles, so the rows above fail for
	// their own reason and not because vetoRule is broken.
	if _, err := ruleengine.Compile([]ruleengine.Rule{vetoRule()}, tsParse); err != nil {
		t.Fatalf("control: the valid veto rule does not compile: %v", err)
	}
}

// vetoOnly is a rule carrying ONLY a value veto: no regex, no name constraint.
func vetoOnly(pattern, mv string) ruleengine.Rule {
	return ruleengine.Rule{
		ID: "T-POS", Message: "m", Severity: "high", Dimension: "security",
		Pattern:               pattern,
		MetavariableValueVeto: map[string]string{mv: "not-a-credential"},
	}
}

// TestValueVetoRejectsAMetavariableThatCannotHoldALiteral (B1). A metavariable
// written INSIDE a string pattern binds the string's contents, without quotes;
// the decoder refuses unquoted text, so such a veto would compile and then
// never fire. The same is true wherever the metavariable stands in a place a
// whole string literal cannot: inside a template or a regex, a shorthand
// property, a member name, a declaration name. Compile proves the opposite for
// every accepted veto — written as a string literal, that spot still parses,
// still matches, and binds the literal itself — and rejects the rest, naming
// the rule, the metavariable and the pattern.
func TestValueVetoRejectsAMetavariableThatCannotHoldALiteral(t *testing.T) {
	rejected := []struct {
		pattern, mv, why string
	}{
		{`f("$V")`, "$V", "B1 verbatim: inside a double-quoted string it binds the contents"},
		{`f('$V')`, "$V", "inside a single-quoted string"},
		{"f(`$V`)", "$V", "inside a template, a string fragment again"},
		{`f(/$V/)`, "$V", "inside a regex literal it binds the regex pattern text"},
		{`({"$K": 1})`, "$K", "inside a quoted property key"},
		{`({$V})`, "$V", "a shorthand property can only be an identifier"},
		{`foo.$V`, "$V", "a member name can only be an identifier"},
		{`const $NAME = $VALUE`, "$NAME", "a declaration name can only be an identifier or a pattern"},
		{`f('$V', $V)`, "$V", "the first occurrence binds string contents, so the veto never decodes, " +
			"although the second occurrence could hold a literal"},
	}
	for _, tc := range rejected {
		t.Run("rejects "+tc.pattern, func(t *testing.T) {
			_, err := ruleengine.Compile([]ruleengine.Rule{vetoOnly(tc.pattern, tc.mv)}, tsParse)
			if err == nil {
				t.Fatalf("Compile accepted a veto that can never fire — %s", tc.why)
			}
			for _, want := range []string{"T-POS", tc.mv, tc.pattern, "where a string literal"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err.Error(), want)
				}
			}
		})
	}

	// Where a whole string literal CAN stand, the veto compiles and fires.
	accepted := []struct {
		pattern, mv, vetoed, kept string
	}{
		{`f($V)`, "$V", `f("lib/a.ts");`, `f("s3cr3t");`},
		{"f(`${$V}`)", "$V", "f(`${\"lib/a.ts\"}`);", "f(`${\"s3cr3t\"}`);"},
		{`({$K: 1})`, "$K", `const o = {"lib/a.ts": 1};`, `const o = {"s3cr3t": 1};`},
		{`f(...$V)`, "$V", `f(..."lib/a.ts");`, `f(..."s3cr3t");`},
		{`const x: $T = 1`, "$T", `const x: "lib/a.ts" = 1;`, `const x: "s3cr3t" = 1;`},
	}
	for _, tc := range accepted {
		t.Run("accepts "+tc.pattern, func(t *testing.T) {
			compiled, err := ruleengine.Compile([]ruleengine.Rule{vetoOnly(tc.pattern, tc.mv)}, tsParse)
			if err != nil {
				t.Fatalf("Compile rejected a veto on a spot that holds a literal: %v", err)
			}
			if got := vetoMatches(t, compiled, tc.kept); got != 1 {
				t.Errorf("control %s -> %d findings, want 1", tc.kept, got)
			}
			if got := vetoMatches(t, compiled, tc.vetoed); got != 0 {
				t.Errorf("%s -> %d findings, want 0 — the veto must fire here", tc.vetoed, got)
			}
		})
	}
}

// TestValueVetoOnADefaultParameterIsRejectedToday pins a CURRENT behaviour, not
// a rule of the language: a whole string literal CAN stand as a default
// parameter value (`function f(p = "lib/a.ts") {}` is valid TypeScript), yet a
// veto on that spot is refused at compile time.
//
// The cause is the parser, not the compile proof's logic. The proof writes a
// string literal where the metavariable stands and requires the result to
// parse; the TypeScript grammar this engine embeds reports a string-literal
// default parameter as a syntax error, in a pattern and in a source file alike,
// so the proof fails and the rule is rejected. The same pattern without a veto
// compiles, and matches an identifier default.
//
// The rejection fails in the safe direction: a rule author gets a located
// compile error, never a veto that silently does nothing. It is pinned so that
// a parser or compile-proof change that starts ACCEPTING this spot is seen and
// proven to veto there (the accepted table above is where such a row would go),
// instead of slipping through unexamined. No shipped rule puts a veto here.
func TestValueVetoOnADefaultParameterIsRejectedToday(t *testing.T) {
	const pattern = `function $F($P = $V) {}`

	// The cause, observed: the parser flags a string-literal default parameter.
	src, err := tsParse(`function f(p = "lib/a.ts") {}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !src.HasError() {
		t.Fatal("the TypeScript parser now accepts a string-literal default parameter; " +
			"this pin's stated cause is gone, so re-examine whether the veto below still fails to compile, and why")
	}

	// The control: without the veto the pattern compiles and reaches the spot,
	// so the rejection below is the veto's alone.
	plain := ruleengine.Rule{ID: "T-POS", Message: "m", Severity: "high", Dimension: "security", Pattern: pattern}
	compiled, err := ruleengine.Compile([]ruleengine.Rule{plain}, tsParse)
	if err != nil {
		t.Fatalf("control: the pattern without a veto does not compile: %v", err)
	}
	if got := vetoMatches(t, compiled, `function f(p = x) {}`); got != 1 {
		t.Fatalf("control: the pattern without a veto matched an identifier default %d times, want 1", got)
	}

	_, err = ruleengine.Compile([]ruleengine.Rule{vetoOnly(pattern, "$V")}, tsParse)
	if err == nil {
		t.Fatal("Compile now ACCEPTS a veto on a default-parameter value; prove it vetoes there " +
			"(a row in the accepted table of TestValueVetoRejectsAMetavariableThatCannotHoldALiteral), " +
			"then retire this pin and the rules/README.md sentence that describes it")
	}
	for _, want := range []string{"T-POS", "$V", pattern, "where a string literal", "does not parse"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err.Error(), want)
		}
	}
}

// TestValueVetoOnlyRule: a rule whose ONLY constraint is the veto compiles and
// runs. The constraints map is sized from every constraint kind; leaving the
// veto out of that count would write to a nil map at compile time.
func TestValueVetoOnlyRule(t *testing.T) {
	compiled, err := ruleengine.Compile([]ruleengine.Rule{vetoOnly(`readKey($V)`, "$V")}, tsParse)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if got := vetoMatches(t, compiled, `readKey("s3cr3t");`); got != 1 {
		t.Errorf("veto-only rule on a plain value -> %d findings, want 1", got)
	}
	if got := vetoMatches(t, compiled, `readKey("lib/config.ts");`); got != 0 {
		t.Errorf("veto-only rule on a file path -> %d findings, want 0", got)
	}
}

// TestValueVetoAbsentChangesNothing: a rule without the operator matches exactly
// as before, file path included. The veto is opt-in per rule.
func TestValueVetoAbsentChangesNothing(t *testing.T) {
	r := vetoRule()
	r.MetavariableValueVeto = nil
	compiled, err := ruleengine.Compile([]ruleengine.Rule{r}, tsParse)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if got := vetoMatches(t, compiled, `const secretFile = "lib/config.ts";`); got != 1 {
		t.Errorf("without the veto a file path under a credential name -> %d findings, want 1", got)
	}
}

// TestDecodeJSStringLiteral pins the decoder's grammar, one section per escape
// class, including every input it must REFUSE. A refusal is the safe answer: the
// veto then proves nothing and the match is kept.
func TestDecodeJSStringLiteral(t *testing.T) {
	type row struct {
		class string // the grammar class the row exercises; set by section
		src   string
		want  string
		ok    bool
	}
	good := func(src, want string) row { return row{src: src, want: want, ok: true} }
	bad := func(src string) row { return row{src: src} }
	var cases []row
	section := func(class string, rows ...row) {
		for _, r := range rows {
			r.class = class
			cases = append(cases, r)
		}
	}
	// CLASSES THAT NAME AN ESCAPE. A row filed under one of these must reach the
	// decoder WITH a backslash in it. Without this guard a row can claim to pin
	// an escape while carrying only the character the escape denotes: the
	// escape decoded before it ever reached the decoder (an interpreted Go
	// string, or the text decoded on its way into this file), and the row tests
	// plain text under an escape's name. That happened to three rows here.
	escapeClasses := map[string]bool{
		"single-character escape": true,
		`\0 and decimal escapes`:  true,
		`\xHH`:                    true,
		`\uHHHH`:                  true,
		`\u{H…}`:                  true,
		"surrogate pair":          true,
		"line continuation":       true,
		"identity escape":         true,
	}
	// The other classes, named so a misspelled section cannot slip past the
	// guard by being in neither set.
	plainClasses := map[string]bool{"plain": true, "not one literal": true}
	section("plain",
		good(`"abc"`, "abc"),
		good(`'abc'`, "abc"),
		good(`""`, ""),
		good(`"it's"`, "it's"),
		good(`'say "x"'`, `say "x"`),
		good(`"é"`, "é"),
	)

	section("single-character escape",
		good(`"a\nb"`, "a\nb"),
		good(`"a\rb"`, "a\rb"),
		good(`"a\tb"`, "a\tb"),
		good(`"a\bb"`, "a\bb"),
		good(`"a\fb"`, "a\fb"),
		good(`"a\vb"`, "a\vb"),
	)

	section(`\0 and decimal escapes`,
		good(`"\0"`, "\x00"),
		good(`"\0a"`, "\x00a"),
		bad(`"\00"`),
		bad(`"\01"`),
		bad(`"\1"`),
		bad(`"\7"`),
		bad(`"\8"`),
		bad(`"\9"`),
	)

	section(`\xHH`,
		good(`"\x41"`, "A"),
		good(`"\x3d"`, "="),
		good(`"\xe9"`, "é"),
		bad(`"\x4"`),
		bad(`"\xZZ"`),
	)

	section(`\uHHHH`,
		good(`"\u0041"`, "A"),
		good(`"\u002F"`, "/"),
		good(`"\u00e9"`, "é"),
		good(`"a\u0041b"`, "aAb"),
		good(`"\uD7FF"`, "\uD7FF"), // the last code unit below the surrogate range
		good(`"\uE000"`, "\uE000"), // the first code unit above it
		bad(`"\u004"`),
		bad(`"\u00G1"`),
	)

	section(`\u{H…}`,
		good(`"\u{41}"`, "A"),
		good(`"\u{1F600}"`, "\U0001F600"),
		good(`"\u{000041}"`, "A"),
		bad(`"\u{}"`),
		bad(`"\u{110000}"`),
		bad(`"\u{41"`),
		bad(`"\u{D800}"`),
		bad(`"\u{100000041}"`),
		bad(`"\u{FFFFFFFFF}"`),
	)

	section("surrogate pair",
		good(`"\uD83D\uDE00"`, "\U0001F600"),
		good(`"\uD800\uDC00"`, "\U00010000"), // the lowest pair
		good(`"\uDBFF\uDFFF"`, "\U0010FFFF"), // the highest pair
		good(`"a\uD83D\uDE00b"`, "a\U0001F600b"),
		bad(`"\uD83D"`),         // a lone high surrogate at the end
		bad(`"\uD83Dx"`),        // a high surrogate, then a plain character
		bad(`"\uD83Dabcdefgh"`), // a high surrogate, then six plain characters
		bad(`"\uD83D\u0041"`),   // a high surrogate, then a \u escape that is not a low one
		bad(`"\uD83D\uDBFF"`),   // ... just below the low range: another high surrogate
		bad(`"\uD83D\uE000"`),   // ... just above the low range
		bad(`"\uD83D\uDE0"`),    // ... a malformed \u escape
		bad(`"\uD83D\u{DE00}"`), // the low half as \u{…}, which is refused as a surrogate
		bad(`"\uDE00"`),         // a lone low surrogate
		bad(`"\uDE00\uD83D"`),   // a pair written in the wrong order
	)

	section("line continuation",
		good("\"a\\\nb\"", "ab"),
		good("\"a\\\r\nb\"", "ab"),
		good("\"a\\\rb\"", "ab"),
		good("\"a\\\u2028b\"", "ab"), // U+2028 LINE SEPARATOR
		good("\"a\\\u2029b\"", "ab"), // U+2029 PARAGRAPH SEPARATOR
	)

	section("identity escape",
		good(`"\/"`, "/"),
		good(`"\'"`, "'"),
		good(`'\''`, "'"),
		good(`"\""`, `"`),
		good(`"\\"`, `\`),
		good(`"\q"`, "q"),
		good(`"\é"`, "é"),
	)

	section("not one literal",
		bad(``),
		bad(`"`),
		bad(`abc`),
		bad("`abc`"),
		bad(`"abc`),
		bad(`"abc'`),
		bad(`"a"b"`),
		bad(`"a" + "b"`),
		bad(`"abc\"`),
		bad("\"a\nb\""),
		bad("\"a\rb\""),
	)
	seen := map[string]bool{}
	for _, tc := range cases {
		if !escapeClasses[tc.class] && !plainClasses[tc.class] {
			t.Errorf("row %q is filed under %q, a class neither set declares", tc.src, tc.class)
		}
		if seen[tc.src] {
			t.Errorf("row %q appears twice: one of the two was meant to pin something else", tc.src)
		}
		seen[tc.src] = true
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q", tc.src), func(t *testing.T) {
			if escapeClasses[tc.class] && !strings.Contains(tc.src, `\`) {
				t.Fatalf("row %q is filed under %s but carries no backslash: the decoder never sees "+
					"the escape the row claims to pin, only the text it denotes", tc.src, tc.class)
			}
			got, ok := ruleengine.DecodeJSStringLiteral(tc.src)
			if ok != tc.ok || got != tc.want {
				t.Errorf("decode(%s) = (%q, %v), want (%q, %v)", tc.src, got, ok, tc.want, tc.ok)
			}
		})
	}
}
