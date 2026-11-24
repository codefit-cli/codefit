package ruleengine_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/ruleengine"
)

// SPEC (issue #148) — `metavariable-value-veto`, a constraint that
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
// WHAT FAILS AT COMPILE. An unknown predicate, a key that is not a metavariable,
// and a metavariable some base alternative never binds. Each one would otherwise
// compile and then either never veto or make the rule silent.

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
		{`"lib/config.ts"`, 0, "a \\u escape denotes the '/' of a file path"},
		{`"https://api.example.com/v1?api_key\x3dabc"`, 1,
			"the literal denotes `?api_key=abc`, a credential-named parameter carrying a value. Trimming " +
				"the quotes leaves `api_key\\x3dabc`, a parameter with no value, and would veto it"},
		{`"https://api.example.com" + "/v1/users"`, 1,
			"a concatenation is not ONE literal: it does not decode, proves nothing, and keeps firing"},
		{`"lib/config\1.ts"`, 1, "a legacy octal escape does not decode, so it proves nothing"},
		{`"https://api.example.com/\uD800"`, 1, "a lone surrogate does not decode, so it proves nothing"},
	}
	for _, tc := range cases {
		src := "const apiKey = " + tc.lit + ";"
		if got := vetoMatches(t, compiled, src); got != tc.want {
			t.Errorf("%s -> %d findings, want %d — %s", src, got, tc.want, tc.why)
		}
	}
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

// TestDecodeJSStringLiteral pins the decoder's grammar, one escape class per
// row, including every input it must REFUSE. A refusal is the safe answer: the
// veto then proves nothing and the match is kept.
func TestDecodeJSStringLiteral(t *testing.T) {
	type row struct {
		src  string
		want string
		ok   bool
	}
	good := func(src, want string) row { return row{src, want, true} }
	bad := func(src string) row { return row{src, "", false} }
	cases := []row{
		good(`"abc"`, "abc"),
		good(`'abc'`, "abc"),
		good(`""`, ""),
		good(`"it's"`, "it's"),
		good(`'say "x"'`, `say "x"`),
		good(`"é"`, "é"),

		good(`"a\nb"`, "a\nb"),
		good(`"a\rb"`, "a\rb"),
		good(`"a\tb"`, "a\tb"),
		good(`"a\bb"`, "a\bb"),
		good(`"a\fb"`, "a\fb"),
		good(`"a\vb"`, "a\vb"),

		good(`"\0"`, "\x00"),
		good(`"\0a"`, "\x00a"),
		bad(`"\00"`),
		bad(`"\01"`),
		bad(`"\1"`),
		bad(`"\7"`),
		bad(`"\8"`),
		bad(`"\9"`),

		good(`"\x41"`, "A"),
		good(`"\x3d"`, "="),
		good(`"\xe9"`, "é"),
		bad(`"\x4"`),
		bad(`"\xZZ"`),

		good(`"A"`, "A"),
		good(`"/"`, "/"),
		bad(`"\u004"`),
		bad(`"\u00G1"`),
		good(`"\u{41}"`, "A"),
		good(`"\u{1F600}"`, "\U0001F600"),
		good(`"\u{000041}"`, "A"),
		bad(`"\u{}"`),
		bad(`"\u{110000}"`),
		bad(`"\u{41"`),
		bad(`"\u{D800}"`),

		good(`"😀"`, "\U0001F600"),
		bad(`"\uD83D"`),
		bad(`"\uD83Dx"`),
		bad(`"\uD83DA"`),
		bad(`"\uDE00"`),

		good("\"a\\\nb\"", "ab"),
		good("\"a\\\r\nb\"", "ab"),
		good("\"a\\\rb\"", "ab"),
		good("\"a\\ b\"", "ab"),
		good("\"a\\ b\"", "ab"),

		good(`"\/"`, "/"),
		good(`"\'"`, "'"),
		good(`'\''`, "'"),
		good(`"\""`, `"`),
		good(`"\\"`, `\`),
		good(`"\q"`, "q"),
		good(`"\é"`, "é"),

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
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q", tc.src), func(t *testing.T) {
			got, ok := ruleengine.DecodeJSStringLiteral(tc.src)
			if ok != tc.ok || got != tc.want {
				t.Errorf("decode(%s) = (%q, %v), want (%q, %v)", tc.src, got, ok, tc.want, tc.ok)
			}
		})
	}
}
