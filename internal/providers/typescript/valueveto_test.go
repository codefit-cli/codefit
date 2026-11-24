package typescript_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/valueshape"
)

// SEC-001's value veto in TypeScript (issue #148, ADR 0086, ADR 0087).
//
// The name gate (ADR 0085) decides WHICH identifiers are credentials and is not
// touched here. These tests pin the second question, asked only after the name
// matched: does the literal's own shape prove it is something else? The rule
// asks through `metavariable-value-veto: {$VALUE: not-a-credential}`, which
// decodes the literal as JavaScript and calls internal/core/valueshape — the
// same function the Go provider calls (internal/providers/golang/valueveto_test.go
// is the mirror of this file).
//
// Every fixture is real TypeScript source driven through the provider's real
// AnalyzeSecurity: the embedded rules, the real parser, the real engine.

// tsSite is one TypeScript shape SEC-001 declares. lit is TypeScript SOURCE for
// a string literal, quotes included.
type tsSite struct {
	shape string
	src   func(name, lit string) string
}

// tsSites enumerates every alternative rules/typescript/security/secrets.yaml
// declares for SEC-001. A veto that reached five of the six would leave the
// sixth affirming, so each rule below runs at all of them.
var tsSites = []tsSite{
	{"const", func(n, l string) string { return "const " + n + " = " + l + ";\n" }},
	{"let", func(n, l string) string { return "let " + n + " = " + l + ";\n" }},
	{"var", func(n, l string) string { return "var " + n + " = " + l + ";\n" }},
	{"typed const", func(n, l string) string { return "const " + n + ": string = " + l + ";\n" }},
	{"typed let", func(n, l string) string { return "let " + n + ": string = " + l + ";\n" }},
	{"object property", func(n, l string) string {
		return "export const cfg = { region: \"eu\", " + n + ": " + l + ", retries: 3 };\n"
	}},
}

// tsGatedName is a name the gate accepts at every site, the same identifier the
// Go mirror uses.
const tsGatedName = "signingKey"

// dq writes value as a double-quoted TypeScript literal. It refuses a value it
// would have to escape, so no fixture is silently rewritten into another form.
func dq(t *testing.T, value string) string {
	t.Helper()
	if strings.ContainsAny(value, "\"\\\n\r") {
		t.Fatalf("fixture %q needs escaping; write its literal by hand", value)
	}
	return `"` + value + `"`
}

// sq is dq with single quotes.
func sq(t *testing.T, value string) string {
	t.Helper()
	if strings.ContainsAny(value, "'\\\n\r") {
		t.Fatalf("fixture %q needs escaping; write its literal by hand", value)
	}
	return `'` + value + `'`
}

func tsSEC001Of(t *testing.T, src string) []findings.Finding {
	t.Helper()
	var out []findings.Finding
	for _, f := range analyze(t, "app/x/config.ts", src) {
		if f.ID == "SEC-001" {
			out = append(out, f)
		}
	}
	return out
}

// requireTSSiteReachesTheGate is the positive probe every quiet assertion below
// depends on: the same site, with a value no guard can prove anything about,
// must fire. Without it "no finding" is also what a template that never reaches
// the rule produces.
func requireTSSiteReachesTheGate(t *testing.T, site tsSite) {
	t.Helper()
	if got := tsSEC001Of(t, site.src(tsGatedName, `"s3cr3t"`)); len(got) != 1 {
		t.Fatalf("vacuum: %s with a plain short literal emitted %d SEC-001, want 1 — "+
			"the template does not reach the rule, so a quiet result proves nothing", site.shape, len(got))
	}
}

// tsVetoedForms holds one literal per valueshape.Form.
var tsVetoedForms = map[valueshape.Form]string{
	valueshape.FormURL:        "https://api.example.com/v1/users",
	valueshape.FormURN:        "urn:ietf:params:oauth:grant-type:jwt-bearer",
	valueshape.FormEnvVarName: "PAYMENT_API_KEY",
	valueshape.FormFilePath:   "lib/config.ts",
}

// TestTSSEC001ValueVetoIsQuietForEveryFormAtEverySite is the rule itself: a
// literal that proves it is a URL, a URN, an environment-variable name or a file
// path emits NOTHING, in either quote style, at each of the six sites.
func TestTSSEC001ValueVetoIsQuietForEveryFormAtEverySite(t *testing.T) {
	for _, form := range valueshape.Forms() {
		value, ok := tsVetoedForms[form]
		if !ok {
			t.Errorf("valueshape declares form %q and no TypeScript fixture exercises it", form)
			continue
		}
		if got, vetoed := valueshape.NotACredential(value); !vetoed || got != form {
			t.Fatalf("fixture %q is filed under %q but valueshape says (%q, %v)", value, form, got, vetoed)
		}
		for _, site := range tsSites {
			for quote, lit := range map[string]string{"double": dq(t, value), "single": sq(t, value)} {
				t.Run(string(form)+"/"+site.shape+"/"+quote, func(t *testing.T) {
					requireTSSiteReachesTheGate(t, site)
					fs := analyze(t, "app/x/config.ts", site.src(tsGatedName, lit))
					if len(fs) != 0 {
						t.Errorf("%s proves it is a %s and the provider still emitted: %+v", lit, form, fs)
					}
				})
			}
		}
	}
}

// TestTSSEC001SecretFileNamedConfigPathIsQuiet is the reproduction that opened
// issue #148, verbatim, in the language it was reported in.
func TestTSSEC001SecretFileNamedConfigPathIsQuiet(t *testing.T) {
	if fs := tsSEC001Of(t, `const secretFile = "lib/config.ts";`+"\n"); len(fs) != 0 {
		t.Errorf("SEC-001 affirmed a file path as a hardcoded credential: %+v", fs)
	}
	// The name gate is unchanged: the same name still fires on a value that
	// proves nothing.
	if fs := tsSEC001Of(t, `const secretFile = "s3cr3t";`+"\n"); len(fs) != 1 {
		t.Errorf("secretFile no longer reaches the name gate: got %d SEC-001, want 1", len(fs))
	}
}

// TestTSSEC001ValueVetoKeepsFiring is the mandatory negative fixture: every
// value the veto does NOT silence, each with the reason it must not. A row
// without a reason fails. Values are neutral on purpose: the token-like guard is
// structural, so a vendor's real key format would add nothing but a fixture a
// secret scanner refuses.
func TestTSSEC001ValueVetoKeepsFiring(t *testing.T) {
	cases := []struct {
		value  string
		reason string
	}{
		{"s3cr3t", "ADR 0075: a credential has no length floor, so a short literal proves nothing"},
		{"Kx9/vQ2+mR7pL4wZ8nT3yB6cF1hJ5dG0sA", "a base64-shaped secret with '/' and '+': a bare '/' is never proof of a path"},
		{"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "a hex blob: no scheme, no underscore, no extension"},
		{"c29tZS1ub3QtcmVhbC12YWx1ZQ==", "a base64 token: '=' is outside the file-path charset"},
		{"aGVsbG8xMjM.d29ybGQ0NTY.c2lnbmF0dXJlNzg5", "a dotted token: its last segment is not a known file extension"},
		{"MFRGGZDFMZTWQ2LK", "a base32 secret: all-caps with no underscore, which env-var-name requires"},
		{"postgres://user:pass@db.example.com/app", "a URL WITH userinfo carries the credential in user:pass@"},
		{"https://api.example.com/v1?api_key=abc123", "a credential-named query parameter carrying a value"},
		{"https://app.example.com/cb#access_token=abc", "OAuth's implicit flow puts the credential in the fragment"},
		{"https://hooks.example.com/services/Ab12Cd34Ef56Gh78Ij90", "a webhook URL: the secret IS a token-like path segment"},
		{"https://files.example.com/o?sig=0f1e2d3c4b5a69788796a5b4c3d2e1f0", "a signed URL: a token-like query value under a key the vocabulary does not know"},
		{"https://k9f8e7d6c5b4a3928170.fn.example.com/", "a function URL: the secret is a host label"},
		{"https://app.example.com/#/invite/9f8e7d6c5b4a39281706", "a hash-route invite link: a fragment that is not key=value is read whole"},
		{"https://cal.example.com/feed.ics?9f8e7d6c5b4a39281706", "a calendar feed: the secret is a bare query key"},
		{"https://example.com/hook/9f8e7d6c5b%2F4a39281706", "an encoded '/' inside one path segment: decoded whole it is a 21-byte token"},
		{"https://api.example.com/v1/items?page=2;sort=asc", "a query url.ParseQuery rejects: what the guards cannot read is not proof of innocence"},
		{"correct horse battery staple", "a passphrase is a secret with spaces; prose keeps firing as declared noise"},
		{"test_client_secret", "a short test literal: by shape indistinguishable from a real short secret"},
		{"progressToken", "a short identifier: by shape indistinguishable from a real short secret"},
		{"/etc/secrets/db", "a path with no extension: the file-path form anchors on a closed extension list"},
		{"secret.notanext", "a dotted value whose trailing segment is not a known file extension"},
		{"X-Api-Key", "a header name: mixed case fails env-var-name and there is no extension"},
		{"<your-secret-here>", "a placeholder that is not SCREAMING_SNAKE: no form covers it"},
		{"changeme", "a lowercase placeholder: by shape indistinguishable from a real short secret"},
		{"TODO", "an all-caps placeholder with no underscore: env-var-name requires one"},
		{"<token>", "an angle-bracket placeholder: no form covers it"},
		{"__SECRET__", "env-var-name needs an upper-case letter first and allows no leading, trailing or doubled underscore"},
	}
	for _, tc := range cases {
		if tc.reason == "" {
			t.Fatalf("negative fixture %q carries no reason — every keeps-firing row must say why", tc.value)
		}
		if form, vetoed := valueshape.NotACredential(tc.value); vetoed {
			t.Fatalf("fixture %q is filed as keeps-firing and valueshape vetoes it as %q", tc.value, form)
		}
		for _, site := range tsSites {
			t.Run(tc.value+"/"+site.shape, func(t *testing.T) {
				fs := tsSEC001Of(t, site.src(tsGatedName, dq(t, tc.value)))
				if len(fs) != 1 {
					t.Fatalf("got %d SEC-001, want 1 — %s", len(fs), tc.reason)
				}
				if fs[0].Confidence != 1.0 {
					t.Errorf("Confidence = %v, want 1.0 — what still fires is not demoted", fs[0].Confidence)
				}
				if fs[0].Severity != findings.SeverityHigh {
					t.Errorf("Severity = %q, want %q", fs[0].Severity, findings.SeverityHigh)
				}
			})
		}
	}
}

// TestTSSEC001ValueVetoReadsTheDecodedValue pins what is handed to the
// predicate: the string the TypeScript literal DENOTES. Each row is written as
// source, escapes included, and runs at every site.
func TestTSSEC001ValueVetoReadsTheDecodedValue(t *testing.T) {
	cases := []struct {
		lit    string
		want   int
		reason string
	}{
		{`"https://api.example.com/v1?api_key\x3dabc"`, 1,
			"the literal denotes ?api_key=abc, a credential-named parameter carrying a value; read " +
				"between the quotes it is a parameter with no value, and would be vetoed"},
		{`"https:\/\/api.example.com\/v1\/users"`, 0, "\\/ is an identity escape: the literal denotes a plain URL"},
		{`'lib\u002fconfig.ts'`, 0, "a \\u escape in a single-quoted literal denotes the '/' of a file path"},
		{`"https://api.example.com" + "/v1/users"`, 1,
			"a concatenation is not one literal: the decoder refuses it, so it proves nothing"},
		{`"lib/config\1.ts"`, 1, "a legacy octal escape is refused by the decoder, so it proves nothing"},
		{`"https://api.example.com/v1/\uD800"`, 1, "a lone surrogate has no UTF-8 form; refused, so it proves nothing"},
	}
	for _, tc := range cases {
		for _, site := range tsSites {
			t.Run(tc.lit+"/"+site.shape, func(t *testing.T) {
				requireTSSiteReachesTheGate(t, site)
				if got := tsSEC001Of(t, site.src(tsGatedName, tc.lit)); len(got) != tc.want {
					t.Errorf("got %d SEC-001, want %d — %s", len(got), tc.want, tc.reason)
				}
			})
		}
	}
}

// TestTSSEC001ValueVetoDeclaredBlindSpots locks the cost of the veto, so it is a
// declared limit and not a belief: these values are vetoed although each COULD
// be a real secret. coverage.go and COVERAGE.md say so; if one starts firing,
// those declarations became false and must change in the same commit.
func TestTSSEC001ValueVetoDeclaredBlindSpots(t *testing.T) {
	cases := []struct {
		value string
		form  valueshape.Form
		limit string
	}{
		{"MY_S3CR3T_VALUE", valueshape.FormEnvVarName, "an all-caps secret containing an underscore has the shape of an environment-variable name"},
		{"https://api.example.com/hook/abc123", valueshape.FormURL, "a token shorter than 16 bytes gives the token-like guard nothing to act on"},
		{"https://api.example.com/hook/abcdefghijklmnopqrstuvwx", valueshape.FormURL, "a URL-embedded token with no digit reads as a descriptive segment"},
		{"https://api.example.com/hook/9f8e7d6c/5b4a39281706", valueshape.FormURL, "a token cut by the URL's own '/' into pieces each shorter than 16 bytes"},
		{"https://app.example.com/#9f8e7d6c5b&4a39281706", valueshape.FormURL, "a token cut by a literal '&' in a fragment"},
		{"https://cal.example.com/feed.ics?9f8e7d6c5b&4a39281706", valueshape.FormURL, "a token cut by a literal '&' in a query"},
		{"Hunter2024.db", valueshape.FormFilePath, "a bare word.ext with a listed extension has the shape of a file name"},
	}
	for _, tc := range cases {
		if got, vetoed := valueshape.NotACredential(tc.value); !vetoed || got != tc.form {
			t.Fatalf("fixture %q is filed under %q but valueshape says (%q, %v)", tc.value, tc.form, got, vetoed)
		}
		for _, site := range tsSites {
			t.Run(tc.value+"/"+site.shape, func(t *testing.T) {
				requireTSSiteReachesTheGate(t, site)
				if fs := tsSEC001Of(t, site.src(tsGatedName, dq(t, tc.value))); len(fs) != 0 {
					t.Errorf("SEC-001 fired — then the declared limit is false (%s): %+v", tc.limit, fs)
				}
			})
		}
	}
}

// TestTSSEC001FormsTheVetoNeverSees is the negative fixture on the SHAPE axis:
// forms in which a credential-named binding holds a string, and SEC-001 is
// silent whatever the value, because no alternative binds $VALUE to one quoted
// literal there. The veto never runs on them; it neither silences nor reports
// them. Each is probed with "s3cr3t", which fires at every declared site, so the
// silence is the shape's, not the value's. If a row starts firing, SEC-001
// widened and the veto now applies there too: move the row and update
// coverage.go in the same commit.
func TestTSSEC001FormsTheVetoNeverSees(t *testing.T) {
	cases := []struct {
		src    string
		reason string
	}{
		{"class C { signingKey = \"s3cr3t\"; }\n",
			"a class field: the declared shape limit; a rule cannot address public_field_definition alone"},
		{"const signingKey = `s3cr3t`;\n",
			"a template literal: excluded by SEC-001's $VALUE regex, since a template may interpolate"},
		{"const signingKey = (\"s3cr3t\");\n",
			"a parenthesized literal: $VALUE binds the parentheses, which the quote regex rejects"},
		{"const signingKey = \"s3cr3t\" as string;\n",
			"an as-cast: $VALUE binds the whole cast expression, which does not end in a quote"},
		{"let signingKey: string;\nsigningKey = \"s3cr3t\";\n",
			"a bare assignment: no alternative declares assignment_expression"},
		{"const { signingKey = \"s3cr3t\" } = readConfig();\n",
			"a destructuring default: no alternative declares an object pattern"},
	}
	for _, tc := range cases {
		if fs := tsSEC001Of(t, tc.src); len(fs) != 0 {
			t.Errorf("SEC-001 fired on a form declared blind — %s:\n%s%+v", tc.reason, tc.src, fs)
		}
	}
	// The probe value is not what keeps them quiet: at a declared site it fires.
	requireTSSiteReachesTheGate(t, tsSites[0])
}

// TestTSSEC001ValueVetoIsPerValue: in one object, the veto belongs to one pair.
// A vetoed pair must not silence its credential neighbour, whichever comes
// first, and a vetoed neighbour must not be needed for the credential to fire.
func TestTSSEC001ValueVetoIsPerValue(t *testing.T) {
	cases := []struct {
		src  string
		want int
		why  string
	}{
		{`export const cfg = { secretFile: "lib/config.ts", signingKey: "s3cr3t" };`, 1,
			"the vetoed pair first: the search must reach the credential behind it"},
		{`export const cfg = { signingKey: "s3cr3t", secretFile: "lib/config.ts" };`, 1,
			"the credential first"},
		{`export const cfg = { secretFile: "lib/config.ts", tokenUrl: "https://auth.example.com/oauth/token" };`, 0,
			"every credential-named pair is vetoed"},
	}
	for _, tc := range cases {
		if fs := tsSEC001Of(t, tc.src+"\n"); len(fs) != tc.want {
			t.Errorf("%s -> %d SEC-001, want %d — %s", tc.src, len(fs), tc.want, tc.why)
		}
	}
}

// TestTSSEC001EmptyLiteralIsStillQuiet keeps the value condition that existed
// before the veto: the $VALUE regex demands at least one character between the
// quotes, so an empty string was never reported, and the veto is not what makes
// it quiet.
func TestTSSEC001EmptyLiteralIsStillQuiet(t *testing.T) {
	for _, site := range tsSites {
		requireTSSiteReachesTheGate(t, site)
		if fs := tsSEC001Of(t, site.src(tsGatedName, `""`)); len(fs) != 0 {
			t.Errorf("%s: SEC-001 fired on an empty literal: %+v", site.shape, fs)
		}
	}
}
