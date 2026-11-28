package typescript_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// SEC-001 and the JavaScript line continuation (issue #148, ADR 0087).
//
// A backslash immediately followed by a line terminator, inside a quoted string,
// is a line continuation: it denotes nothing, so "s3cr" + backslash + LF + "3t"
// is the string s3cr3t. The value decoder behind metavariable-value-veto always
// handled it. The rule did not: its $VALUE regex was ^["'].+["']$, and in RE2 the
// dot does not match a line feed, so a literal holding one never matched, never
// reached the decoder, and SEC-001 was silent on it WHATEVER it held. That was a
// false all-clear, and the documentation said the opposite.
//
// The bytes are the whole point of these fixtures and are easy to lose on the way
// into a file (an editor, a tool or a copy can turn the pair into a real line
// break or drop the backslash). So they are spelled as \x escapes of this Go
// source, and every row is checked for the exact byte pair before it runs.
const (
	backslash = "\x5c"
	lf        = "\x0a"
	crlf      = "\x0d\x0a"
	cr        = "\x0d"
)

// continued writes a TypeScript string literal whose text is head, a line
// continuation ending in ending, then tail, between two quote characters.
func continued(quote, head, ending, tail string) string {
	return quote + head + backslash + ending + tail + quote
}

// continuationRow is one literal that holds a line continuation.
type continuationRow struct {
	name string
	lit  string // TypeScript source, quotes included
	// ending is the line terminator after the backslash: lf or crlf. The byte
	// guard requires the literal to hold backslash+ending.
	ending string
	want   int
	reason string
}

// analyzeWhole drives the real provider through AnalyzeFile, the call the
// security sensor makes, and requires the file to have been parsed whole: a
// literal the parser stopped on could be quiet because the parser stopped, not
// because of the rule.
func analyzeWhole(t *testing.T, src string) []findings.Finding {
	t.Helper()
	got, err := typescript.New().AnalyzeFile(providers.SourceFile{Path: "app/x/config.ts", Content: []byte(src)})
	if err != nil {
		t.Fatalf("AnalyzeFile: %v\n%s", err, src)
	}
	if got.Parse != findings.ParseComplete() {
		t.Fatalf("the file was not parsed whole (%+v), so its verdict proves nothing:\n%q", got.Parse, src)
	}
	var out []findings.Finding
	for _, f := range got.Findings {
		if f.ID == "SEC-001" {
			out = append(out, f)
		}
	}
	return out
}

// requireContinuationBytes is the byte guard: a row about a line continuation
// must hold the backslash immediately followed by its line terminator.
func requireContinuationBytes(t *testing.T, name, lit, ending string) bool {
	t.Helper()
	switch ending {
	case lf:
		if !strings.Contains(lit, backslash+lf) || strings.Contains(lit, backslash+crlf) {
			t.Errorf("%s: the literal does not hold the byte pair 0x5C 0x0A, so it pins no LF line continuation: %q", name, lit)
			return false
		}
	case crlf:
		if !strings.Contains(lit, backslash+crlf) {
			t.Errorf("%s: the literal does not hold the bytes 0x5C 0x0D 0x0A, so it pins no CR LF line continuation: %q", name, lit)
			return false
		}
	default:
		t.Errorf("%s: unknown line ending %q", name, ending)
		return false
	}
	return true
}

var continuationRows = []continuationRow{
	{"secret, double-quoted, LF", continued(`"`, "s3cr", lf, "3t"), lf, 1,
		"the literal denotes s3cr3t, which proves nothing: it fires, as the one-line spelling does"},
	{"secret, double-quoted, CR LF", continued(`"`, "s3cr", crlf, "3t"), crlf, 1,
		"a CR LF continuation denotes nothing either: the literal is s3cr3t"},
	{"secret, single-quoted, LF", continued(`'`, "s3cr", lf, "3t"), lf, 1,
		"a single-quoted literal is a string too, with the same continuation"},
	{"secret, single-quoted, CR LF", continued(`'`, "s3cr", crlf, "3t"), crlf, 1,
		"single quotes and a CR LF continuation: still s3cr3t"},
	{"credential query parameter, LF", continued(`"`, "https://api.example.com/v1?api_key=", lf, "abc123"), lf, 1,
		"the veto reads the decoded URL, and a credential-named parameter carrying a value keeps firing"},
	{"path, double-quoted, LF", continued(`"`, "lib/con", lf, "fig.ts"), lf, 0,
		"the literal denotes lib/config.ts: the value veto applies to it, the continuation does not bypass it"},
	{"path, double-quoted, CR LF", continued(`"`, "lib/con", crlf, "fig.ts"), crlf, 0,
		"lib/config.ts through a CR LF continuation is still a path the veto silences"},
	{"path, single-quoted, LF", continued(`'`, "lib/con", lf, "fig.ts"), lf, 0,
		"single quotes do not change what the veto reads"},
	{"URL, double-quoted, LF", continued(`"`, "https://api.example.com/", lf, "v1/users"), lf, 0,
		"the literal denotes a plain URL, which the veto silences"},
}

// TestTSSEC001ReadsALiteralWithALineContinuation: a literal holding a line
// continuation reaches the rule and its value veto at all six sites, with LF and
// CR LF line endings, in either quote style. A secret fires; a value the veto
// proves is something else stays quiet.
func TestTSSEC001ReadsALiteralWithALineContinuation(t *testing.T) {
	for _, row := range continuationRows {
		if row.reason == "" {
			t.Errorf("%s: no reason written", row.name)
			continue
		}
		if !requireContinuationBytes(t, row.name, row.lit, row.ending) {
			continue
		}
		for _, site := range tsSites {
			t.Run(row.name+"/"+site.shape, func(t *testing.T) {
				requireTSSiteReachesTheGate(t, site)
				got := analyzeWhole(t, site.src(tsGatedName, row.lit))
				if len(got) != row.want {
					t.Fatalf("got %d SEC-001, want %d — %s", len(got), row.want, row.reason)
				}
				if row.want == 1 && got[0].Confidence != 1.0 {
					t.Errorf("Confidence = %v, want 1.0", got[0].Confidence)
				}
			})
		}
	}
}

// TestTSSEC001OneLineLiteralsKeepTheirVerdict pins what the old $VALUE regex
// already matched, so widening it to admit a line continuation changed nothing
// for them: a one-line quoted secret fires, a one-line vetoed value is quiet, in
// both quote styles, at all six sites. The two continuations whose terminator is
// not a line feed (a lone CR, and U+2028) were matched by the old dot already, and
// keep their verdict too.
func TestTSSEC001OneLineLiteralsKeepTheirVerdict(t *testing.T) {
	cases := []struct {
		lit    string
		want   int
		reason string
	}{
		{`"s3cr3t"`, 1, "a one-line double-quoted secret"},
		{`'s3cr3t'`, 1, "a one-line single-quoted secret"},
		{`"lib/config.ts"`, 0, "a one-line path, vetoed"},
		{`'https://api.example.com/v1/users'`, 0, "a one-line URL, vetoed"},
		{`"PAYMENT_API_KEY"`, 0, "a one-line environment-variable name, vetoed"},
		{continued(`"`, "s3cr", cr, "3t"), 1, "a lone-CR continuation: the old dot matched a CR, and it fired before"},
		{continued(`"`, "lib/con", cr, "fig.ts"), 0, "a lone-CR continuation in a path: vetoed before and after"},
		{continued(`"`, "s3cr", "\u2028", "3t"), 1, "a U+2028 continuation: the old dot matched it, and it fired before"},
	}
	for _, tc := range cases {
		for _, site := range tsSites {
			t.Run(tc.lit+"/"+site.shape, func(t *testing.T) {
				if got := analyzeWhole(t, site.src(tsGatedName, tc.lit)); len(got) != tc.want {
					t.Errorf("got %d SEC-001, want %d — %s", len(got), tc.want, tc.reason)
				}
			})
		}
	}
}

// TestTSSEC001StringFormsStillUnseen is the negative fixture for this change:
// string forms holding the same secret on which SEC-001 is still silent after the
// $VALUE regex admits a line continuation, each with its reason. The regex admits a
// line feed ONLY right after a backslash, so a line break between two operands is
// still refused; that keeps this change to literals that hold a continuation. If a
// row starts firing, SEC-001 widened: move the row and update coverage.go and
// COVERAGE.md in the same commit.
func TestTSSEC001StringFormsStillUnseen(t *testing.T) {
	cases := []struct {
		src    string
		reason string
	}{
		{"const signingKey = `s3cr" + backslash + lf + "3t`;\n",
			"a template literal with a continuation: the regex admits only a single or double quote"},
		{"const signingKey = `s3cr3t`;\n",
			"a template literal on one line: the regex admits only a single or double quote, with or without a continuation"},
		{"const signingKey = \"s3cr\" +" + lf + "  \"3t\";\n",
			"a concatenation broken across lines: the line feed is outside any literal, and the regex admits one only after a backslash"},
		{"const signingKey = \"s3cr3t\" + suffix;\n",
			"a concatenation ending in a non-literal operand: the bound text does not end in a quote"},
		{"const signingKey = prefix + \"s3cr3t\";\n",
			"a concatenation starting with a non-literal operand: the bound text does not start with a quote"},
	}
	for _, tc := range cases {
		if got := analyzeWhole(t, tc.src); len(got) != 0 {
			t.Errorf("SEC-001 fired on a form declared unseen — %s:\n%q\n%+v", tc.reason, tc.src, got)
		}
	}
	// The probe value is not what keeps them quiet: at a declared site it fires.
	requireTSSiteReachesTheGate(t, tsSites[0])
}

// TestTSSEC001ContinuationEdgesAsMeasured pins two verdicts the independent
// verifier of the line-continuation fix measured (advisories A1 and A2,
// 2026-11-28). Neither is a bug the rule should lose; both are consequences of
// admitting a line feed right after a backslash, written down so the
// documentation can say exactly where the line runs.
func TestTSSEC001ContinuationEdgesAsMeasured(t *testing.T) {
	cases := []struct {
		name string
		src  string
		// pair: the source must hold a backslash right before a line feed
		// (0x5C 0x0A), the bytes the row is about.
		pair   bool
		want   int
		reason string
	}{
		// A1. The backslash that admits the line feed need not be inside a
		// literal: a line comment can end in one. Then the bound text runs from
		// the first quote to the last across the break, the regex admits it, the
		// decoder refuses it as not one literal, and it fires like a one-line
		// concatenation of two literals.
		{"concatenation across lines after a comment ending in a backslash",
			"const signingKey = \"s3cr\" + // split" + backslash + lf + "  \"3t\";\n", true, 1,
			"the comment's backslash sits right before the line feed, so the regex admits the break; not one literal, so it keeps firing"},
		// The same concatenation without that backslash stays unseen (the
		// negative fixture above): the comment is what makes the difference.
		{"concatenation across lines after a comment with no backslash",
			"const signingKey = \"s3cr\" + // split" + lf + "  \"3t\";\n", false, 0,
			"the line feed follows no backslash, so the regex refuses the break"},
		// A2. A literal holding nothing but a continuation denotes the empty
		// string. It is reported, while the empty literal on one line is not:
		// the regex needs at least one step between the quotes, and the
		// backslash with its line feed is one.
		{"a literal holding only a line continuation",
			"const signingKey = \"" + backslash + lf + "\";\n", true, 1,
			"the backslash and line feed satisfy the regex's middle; the value denotes the empty string, which proves nothing, so it fires"},
		{"an empty literal on one line",
			"const signingKey = \"\";\n", false, 0,
			"nothing stands between the quotes, so the regex refuses it"},
	}
	for _, tc := range cases {
		if tc.reason == "" {
			t.Errorf("%s: no reason written", tc.name)
			continue
		}
		if strings.Contains(tc.src, backslash+lf) != tc.pair {
			t.Errorf("%s: holding the byte pair 0x5C 0x0A is %v, the row says %v: %q",
				tc.name, !tc.pair, tc.pair, tc.src)
			continue
		}
		if got := analyzeWhole(t, tc.src); len(got) != tc.want {
			t.Errorf("%s: got %d SEC-001, want %d — %s:\n%q", tc.name, len(got), tc.want, tc.reason, tc.src)
		}
	}
	requireTSSiteReachesTheGate(t, tsSites[0])
}
