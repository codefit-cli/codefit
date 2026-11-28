package valueshape_test

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/valueshape"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/golang"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// The cross-provider table for SEC-001's VALUE axis (issue #148, ADR 0086,
// ADR 0087). It is the sibling of TestCrossProviderNameGate in
// internal/core/namematch/crossprovider_test.go, which binds the NAME axis.
//
// Both providers ask valueshape.NotACredential, but they do not hand it the same
// thing in every case: each decodes its own literal grammar first, and each
// reaches SEC-001 from different syntax. So the shared function alone does not
// make the two verdicts equal. This table does, the same way the name table
// does: every row carries the verdict of BOTH providers, measured through the
// REAL provider at every site it declares, and a divergence is admissible only
// when it is written, with the place in the repository that already declares
// it.
//
// An external test package in core may build real providers as fixtures (as
// internal/core/dbrules/names_test.go and internal/core/ruleengine/compilegate_test.go
// do): no production package of core imports a provider, and that is what the
// layering check reads.
//
// SCOPE, stated so the table cannot over-claim: this binds the VALUE verdict of
// SEC-001's layer 2 (the name gate and the value veto). Layer 1, the regex
// shapes of known key formats, is not exercised: no value below has one.

// verdict is what one provider does with one spelling at one site.
type verdict int

const (
	quiet verdict = iota
	fires
	// noSpelling: the spelling is not valid source in that language, so the
	// provider cannot be asked. Measured from the provider's own parse error.
	noSpelling
)

func (v verdict) String() string {
	switch v {
	case quiet:
		return "quiet"
	case fires:
		return "fires"
	case noSpelling:
		return "no spelling"
	}
	return fmt.Sprintf("verdict(%d)", int(v))
}

// site is one syntactic shape that reaches SEC-001. lit is SOURCE for a string
// literal in that language, quotes included.
type site struct {
	shape string
	src   func(name, lit string) string
}

// goSites is copied from sec001Sites in
// internal/providers/golang/valueveto_test.go: the three AST shapes security.go
// dispatches to a secret check (checkSecretAssign, checkSecretValueSpec,
// checkSecretKeyValue). Test helpers cannot be imported across packages.
var goSites = []site{
	{"AssignStmt", func(n, l string) string {
		return "package p\n\nfunc f() { " + n + " := " + l + "; _ = " + n + " }\n"
	}},
	{"ValueSpec", func(n, l string) string {
		return "package p\n\nconst " + n + " = " + l + "\n"
	}},
	{"KeyValueExpr", func(n, l string) string {
		return "package p\n\ntype Cfg struct{ " + n + " string }\n\n" +
			"func f() Cfg { return Cfg{" + n + ": " + l + "} }\n"
	}},
}

// tsSites is copied from tsSites in
// internal/providers/typescript/valueveto_test.go: the six alternatives
// rules/typescript/security/secrets.yaml declares for SEC-001.
var tsSites = []site{
	{"const", func(n, l string) string { return "const " + n + " = " + l + ";\n" }},
	{"let", func(n, l string) string { return "let " + n + " = " + l + ";\n" }},
	{"var", func(n, l string) string { return "var " + n + " = " + l + ";\n" }},
	{"typed const", func(n, l string) string { return "const " + n + ": string = " + l + ";\n" }},
	{"typed let", func(n, l string) string { return "let " + n + ": string = " + l + ";\n" }},
	{"object property", func(n, l string) string {
		return "export const cfg = { region: \"eu\", " + n + ": " + l + ", retries: 3 };\n"
	}},
}

// gatedName is a name both name gates accept at every site: the ADR 0075
// constraint's own identifier, as in both providers' valueveto tests.
const gatedName = "signingKey"

// dq writes value as a double-quoted literal, valid in Go and TypeScript alike.
// Copied from dq in internal/providers/typescript/valueveto_test.go: it refuses
// a value it would have to escape, so no fixture is silently rewritten.
func dq(t *testing.T, value string) string {
	t.Helper()
	if strings.ContainsAny(value, "\"\\\n\r") {
		t.Fatalf("fixture %q needs escaping; write its literal by hand", value)
	}
	return `"` + value + `"`
}

// goVerdict drives the real Go provider (go/ast) over one site.
func goVerdict(t *testing.T, s site, name, lit string) verdict {
	t.Helper()
	src := s.src(name, lit)
	fs, err := golang.New().AnalyzeSecurity(providers.SourceFile{Path: "x.go", Content: []byte(src)})
	if err != nil {
		return noSpelling
	}
	return countSEC001(t, "go/"+s.shape, src, fs)
}

// tsVerdict drives the real TypeScript provider through AnalyzeFile, the call
// the security sensor makes. Control 7 lives here: a file the parser did not
// read whole could be quiet because the parser stopped, not because of the
// veto, and the row would pass for the wrong reason.
func tsVerdict(t *testing.T, s site, name, lit string) verdict {
	t.Helper()
	src := s.src(name, lit)
	got, err := typescript.New().AnalyzeFile(providers.SourceFile{Path: "app/x/config.ts", Content: []byte(src)})
	if err != nil {
		t.Fatalf("typescript/%s: AnalyzeFile: %v\n%s", s.shape, err, src)
	}
	if got.Parse != findings.ParseComplete() {
		t.Errorf("C7: typescript/%s did not parse whole (%+v), so its verdict proves nothing:\n%s",
			s.shape, got.Parse, src)
	}
	return countSEC001(t, "typescript/"+s.shape, src, got.Findings)
}

func countSEC001(t *testing.T, where, src string, fs []findings.Finding) verdict {
	t.Helper()
	n := 0
	for _, f := range fs {
		if f.ID == "SEC-001" {
			n++
		}
	}
	switch n {
	case 0:
		return quiet
	case 1:
		return fires
	}
	t.Fatalf("%s: %d SEC-001 for one name/value pair, want 0 or 1:\n%s", where, n, src)
	return quiet
}

// measure returns the one verdict a provider gives at all of its sites, and an
// error naming the sites when they disagree (part of Control 1: a veto wired
// into some sites and not others is a divergence inside one provider).
func measure(t *testing.T, provider string, sites []site, ask func(*testing.T, site, string, string) verdict,
	name, lit string) (verdict, error) {
	t.Helper()
	var first verdict
	var each []string
	same := true
	for i, s := range sites {
		v := ask(t, s, name, lit)
		each = append(each, s.shape+"="+v.String())
		if i == 0 {
			first = v
		} else if v != first {
			same = false
		}
	}
	if !same {
		return first, fmt.Errorf("C1: %s sites disagree for %s: %s", provider, lit, strings.Join(each, ", "))
	}
	return first, nil
}

// requireProbes are the vacuum probes, on both sides and at every site: a value
// no guard can prove anything about must fire (else every firing row's column
// is a constant), and a plain path must be quiet (else the veto is not running
// and every quiet row's column is a constant). They report and let the table
// run, so a broken provider shows every row it moves.
func requireProbes(t *testing.T) {
	t.Helper()
	for _, s := range goSites {
		if v := goVerdict(t, s, gatedName, `"s3cr3t"`); v != fires {
			t.Errorf("vacuum: go/%s is %s on \"s3cr3t\", want fires", s.shape, v)
		}
		if v := goVerdict(t, s, gatedName, `"lib/config.ts"`); v != quiet {
			t.Errorf("vacuum: go/%s is %s on \"lib/config.ts\", want quiet", s.shape, v)
		}
	}
	for _, s := range tsSites {
		if v := tsVerdict(t, s, gatedName, `"s3cr3t"`); v != fires {
			t.Errorf("vacuum: typescript/%s is %s on \"s3cr3t\", want fires", s.shape, v)
		}
		if v := tsVerdict(t, s, gatedName, `"lib/config.ts"`); v != quiet {
			t.Errorf("vacuum: typescript/%s is %s on \"lib/config.ts\", want quiet", s.shape, v)
		}
	}
}

func boolVerdict(b bool) verdict {
	if b {
		return fires
	}
	return quiet
}

// valueRow is one denoted value, written the same way in both languages (a
// plain double-quoted literal), so any difference between the columns comes from
// the providers and not from the spelling. That axis is TestCrossProviderLiteralGrammar.
type valueRow struct {
	value string
	// ident overrides gatedName for the rows whose identifier is the point.
	ident string
	// form is what NotACredential must say about value; "" means it vetoes
	// nothing. Checked by content, so a row cannot be filed under a form it
	// does not have.
	form             valueshape.Form
	goFires, tsFires bool
	// why is REQUIRED on every row that fires (Control 5).
	why string
	// divergence is REQUIRED when goFires != tsFires and forbidden otherwise.
	divergence string
}

// valueRows: the four forms; the union of the keeps-firing rows of
// internal/providers/golang/valueveto_test.go and
// internal/providers/typescript/valueveto_test.go (each is cited by the file that
// first held it); the issue #148 spellings; and the declared blind spots both
// files record.
var valueRows = []valueRow{
	// --- The four forms: vetoed in both. ---
	{value: "https://api.example.com/v1/users", form: valueshape.FormURL},
	{value: "urn:ietf:params:oauth:grant-type:jwt-bearer", form: valueshape.FormURN},
	{value: "STRIPE_API_KEY", form: valueshape.FormEnvVarName},
	{value: "PAYMENT_API_KEY", form: valueshape.FormEnvVarName},
	{value: "lib/config.ts", form: valueshape.FormFilePath},
	// The reproduction that opened issue #148, verbatim.
	{value: "lib/config.ts", ident: "secretFile", form: valueshape.FormFilePath},

	// --- Issue #148's acceptance: the exact spelling keeps firing. ---
	{value: "s3cr3t", ident: "SIGNING_KEY", goFires: true, tsFires: true,
		why: "issue #148 acceptance, verbatim: SIGNING_KEY = \"s3cr3t\" still fires (ADR 0075: no length floor)"},

	// --- Keeps firing in both (the negative fixture of both providers). ---
	{value: "s3cr3t", goFires: true, tsFires: true, why: "ADR 0075: a credential has no length floor"},
	{value: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", goFires: true, tsFires: true,
		why: "go: AWS's published example secret key, with '/' and '+': a bare '/' is never proof of a path"},
	{value: "Kx9/vQ2+mR7pL4wZ8nT3yB6cF1hJ5dG0sA", goFires: true, tsFires: true,
		why: "typescript: a base64-shaped secret with '/' and '+'"},
	{value: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", goFires: true, tsFires: true,
		why: "both: a hex blob, no scheme, no underscore, no extension"},
	{value: "FQoGZXIvYXdzEBQaDG5vdF9hX3NlY3JldA==", goFires: true, tsFires: true,
		why: "go: a base64 STS-style token; '=' is outside the file-path charset"},
	{value: "c29tZS1ub3QtcmVhbC12YWx1ZQ==", goFires: true, tsFires: true,
		why: "typescript: a base64 token; '=' is outside the file-path charset"},
	{value: "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U", goFires: true, tsFires: true,
		why: "go: a JWT; its last dotted segment is not a known file extension"},
	{value: "aGVsbG8xMjM.d29ybGQ0NTY.c2lnbmF0dXJlNzg5", goFires: true, tsFires: true,
		why: "typescript: a dotted token; its last segment is not a known file extension"},
	{value: "JBSWY3DPEHPK3PXP", goFires: true, tsFires: true,
		why: "go: a base32 secret, all-caps with no underscore, which env-var-name requires"},
	{value: "MFRGGZDFMZTWQ2LK", goFires: true, tsFires: true,
		why: "typescript: a base32 secret, all-caps with no underscore"},
	{value: "postgres://user:pass@db.example.com/app", goFires: true, tsFires: true,
		why: "both: a URL with userinfo carries the credential in user:pass@"},
	{value: "https://api.example.com/v1?api_key=abc123", goFires: true, tsFires: true,
		why: "both: a credential-named query parameter carrying a value"},
	{value: "https://app.example.com/cb#access_token=abc", goFires: true, tsFires: true,
		why: "both: OAuth's implicit flow puts the credential in the fragment"},
	{value: "https://hooks.example.com/services/T0A1B2C3D/B0A1B2C3D/abcDEF123ghiJKL456mnoPQR", goFires: true, tsFires: true,
		why: "go: a webhook URL whose secret is a token-like path segment"},
	{value: "https://hooks.example.com/services/Ab12Cd34Ef56Gh78Ij90", goFires: true, tsFires: true,
		why: "typescript: a webhook URL whose secret is a token-like path segment"},
	{value: "https://bucket.example.com/o?X-Amz-Signature=0f1e2d3c4b5a69788796a5b4c3d2e1f0", goFires: true, tsFires: true,
		why: "go: a presigned URL; a token-like query value under a key the vocabulary does not know"},
	{value: "https://files.example.com/o?sig=0f1e2d3c4b5a69788796a5b4c3d2e1f0", goFires: true, tsFires: true,
		why: "typescript: a signed URL; a token-like query value"},
	{value: "https://k9f8e7d6c5b4a3928170.lambda-url.us-east-1.on.aws/", goFires: true, tsFires: true,
		why: "go: a function URL whose secret is a host label"},
	{value: "https://abcdefg1234567hijklmn890opqrs.lambda-url.us-east-2.on.aws/", goFires: true, tsFires: true,
		why: "go: a function URL whose host label is a 29-byte token"},
	{value: "https://k9f8e7d6c5b4a3928170.api.example.com/v1", goFires: true, tsFires: true,
		why: "go: a token-like host label in front of an ordinary path"},
	{value: "https://k9f8e7d6c5b4a3928170.fn.example.com/", goFires: true, tsFires: true,
		why: "typescript: a function URL whose secret is a host label"},
	{value: "https://eo1a2b3c4d5e6f7g8.m.pipedream.net", goFires: true, tsFires: true,
		why: "go: a request-bin URL whose secret is a 17-byte host label"},
	{value: "https://app.example.com/#/invite/9f8e7d6c5b4a39281706", goFires: true, tsFires: true,
		why: "both: a fragment that is not key=value is read whole"},
	{value: "https://app.example.com/login#Ab12Cd34Ef56Gh78Ij90", goFires: true, tsFires: true,
		why: "go: a bare token as the whole fragment"},
	{value: "https://app.example.com/#9f8e7d6c5b%264a39281706", goFires: true, tsFires: true,
		why: "go: an encoded '&' in a bare fragment; split before decoding, the token stays whole"},
	{value: "https://app.example.com/#9f8e7d6c5b%3D4a39281706", goFires: true, tsFires: true,
		why: "go: an encoded '=' in a bare fragment; split before decoding"},
	{value: "https://app.example.com/cb#access_token%3Dabc", goFires: true, tsFires: true,
		why: "go: the decoded fragment still yields access_token=abc"},
	{value: "https://cal.example.com/feed.ics?9f8e7d6c5b4a39281706", goFires: true, tsFires: true,
		why: "both: a calendar feed whose secret is a bare query key"},
	{value: "https://cal.example.com/private/feed.ics?Ab12Cd34Ef56Gh78Ij90", goFires: true, tsFires: true,
		why: "go: a bare query key token after a two-segment path"},
	{value: "https://example.com/hook/9f8e7d6c5b%2F4a39281706", goFires: true, tsFires: true,
		why: "both: an encoded '/' inside one path segment; decoded whole it is a 21-byte token"},
	{value: "https://img.example.com/s/qL8%2Fz3%2BxW9v%2Fk2N%3D/photo.jpg", goFires: true, tsFires: true,
		why: "go: a base64 token percent-encoded into one path segment"},
	{value: "https://api.example.com/v1/items?page=2;sort=asc", goFires: true, tsFires: true,
		why: "both: a query url.ParseQuery rejects is not proof of innocence"},
	{value: "https://api.example.com/v1/items?q=%zz", goFires: true, tsFires: true,
		why: "go: an invalid percent-escape url.ParseQuery rejects"},
	{value: "https://app.example.com/#/settings;tab=2", goFires: true, tsFires: true,
		why: "go: a fragment url.ParseQuery rejects"},
	{value: "https://app.example.com/#a%3Bb", goFires: true, tsFires: true,
		why: "go: a fragment whose decoded form url.ParseQuery rejects"},
	{value: "correct horse battery staple", goFires: true, tsFires: true,
		why: "both: a passphrase is a secret with spaces; prose keeps firing as declared noise"},
	{value: "test_client_secret", goFires: true, tsFires: true, why: "both: a short test literal"},
	{value: "progressToken", goFires: true, tsFires: true, why: "both: a short identifier"},
	{value: "/etc/secrets/db", goFires: true, tsFires: true,
		why: "both: a path with no extension; file-path anchors on a closed extension list"},
	{value: "secret.notanext", goFires: true, tsFires: true,
		why: "both: a trailing segment that is not a known extension"},
	{value: "X-Api-Key", goFires: true, tsFires: true,
		why: "both: a header name; mixed case fails env-var-name"},
	{value: "<your-secret-here>", goFires: true, tsFires: true, why: "both: a placeholder no form covers"},

	// --- The placeholders DeclaredLimit names as STILL REPORTED (Control 6). ---
	{value: "changeme", goFires: true, tsFires: true, why: "a lowercase placeholder, indistinguishable from a short secret"},
	{value: "<token>", goFires: true, tsFires: true, why: "an angle-bracket placeholder no form covers"},
	{value: "TODO", goFires: true, tsFires: true, why: "all-caps with no underscore; env-var-name requires one"},
	{value: "__SECRET__", goFires: true, tsFires: true,
		why: "env-var-name needs a letter first and allows no leading, trailing or doubled underscore"},

	// --- Declared blind spots: vetoed in both, although each could be a secret
	// (DeclaredLimit's NOT REPORTED sentence). ---
	{value: "MY_S3CR3T_VALUE", form: valueshape.FormEnvVarName},
	{value: "https://api.example.com/hook/abc123", form: valueshape.FormURL},
	{value: "https://api.example.com/hook/abcdefghijklmnopqrstuvwx", form: valueshape.FormURL},
	{value: "https://api.example.com/v1/k/AbCd%2FEfGh%2BIjKl%2FMnOp%2FQrSt", form: valueshape.FormURL},
	{value: "https://api.example.com/hook/9f8e7d6c/5b4a39281706", form: valueshape.FormURL},
	{value: "https://app.example.com/#9f8e7d6c5b&4a39281706", form: valueshape.FormURL},
	{value: "https://cal.example.com/feed.ics?9f8e7d6c5b&4a39281706", form: valueshape.FormURL},
	{value: "Hunter2024.db", form: valueshape.FormFilePath},
}

// TestCrossProviderValueVerdicts runs the controls that make a value-axis drift
// between the providers fail instead of ship.
func TestCrossProviderValueVerdicts(t *testing.T) {
	requireProbes(t)

	quietForms := map[valueshape.Form]bool{}
	rowsByValue := map[string][]valueRow{}
	for _, r := range valueRows {
		ident := r.ident
		if ident == "" {
			ident = gatedName
		}
		rowsByValue[r.value] = append(rowsByValue[r.value], r)

		// The fixture is verified by its CONTENT: it must be the form it is
		// filed under, or the row passes for an unrelated reason.
		if got, vetoed := valueshape.NotACredential(r.value); got != r.form || vetoed != (r.form != "") {
			t.Errorf("%q is filed under form %q but NotACredential says (%q, %v)", r.value, r.form, got, vetoed)
		}

		// Control 1 — verdict fidelity, at every site of both providers.
		lit := dq(t, r.value)
		goGot, err := measure(t, "go", goSites, goVerdict, ident, lit)
		if err != nil {
			t.Error(err)
		}
		if goGot != boolVerdict(r.goFires) {
			t.Errorf("C1: %s = %s: go is %s, table declares %s", ident, lit, goGot, boolVerdict(r.goFires))
		}
		tsGot, err := measure(t, "typescript", tsSites, tsVerdict, ident, lit)
		if err != nil {
			t.Error(err)
		}
		if tsGot != boolVerdict(r.tsFires) {
			t.Errorf("C1: %s = %s: typescript is %s, table declares %s", ident, lit, tsGot, boolVerdict(r.tsFires))
		}

		// Control 2 — agreement or a written reason.
		if r.goFires != r.tsFires && r.divergence == "" {
			t.Errorf("C2: %q: go fires=%v, typescript fires=%v, and no divergence is written", r.value, r.goFires, r.tsFires)
		}
		// Control 4 — no ghost reasons.
		if r.goFires == r.tsFires && r.divergence != "" {
			t.Errorf("C4: %q: the providers agree (fires=%v) but a divergence is still recorded: %s",
				r.value, r.goFires, r.divergence)
		}
		// Control 5 — what fires says why.
		if (r.goFires || r.tsFires) && r.why == "" {
			t.Errorf("C5: %q fires and carries no reason: every keeps-firing row must say why", r.value)
		}
		if r.form != "" && !r.goFires && !r.tsFires {
			quietForms[r.form] = true
		}
	}

	// Control 3 — no form without a verdict in both columns.
	for _, form := range valueshape.Forms() {
		if !quietForms[form] {
			t.Errorf("C3: form %q is exercised by no row quiet in both providers — a form cannot ship "+
				"without its cross-provider verdict", form)
		}
	}

	// Control 6 — every placeholder the served text promises keeps firing has a
	// row firing in both columns. The list is read from DeclaredLimit, not
	// copied, so a placeholder added there needs a row here.
	lists := valueshape.StillReportedPlaceholders.FindAllStringSubmatch(valueshape.DeclaredLimit, -1)
	if len(lists) != 1 {
		t.Fatalf("C6: DeclaredLimit carries %d placeholder lists, want exactly 1", len(lists))
	}
	placeholders := strings.Split(lists[0][1], ", ")
	if len(placeholders) < 4 {
		t.Fatalf("C6: the placeholder list parsed into %d names, want at least 4: %q", len(placeholders), placeholders)
	}
	for _, p := range placeholders {
		found := false
		for _, r := range rowsByValue[p] {
			if r.goFires && r.tsFires {
				found = true
			}
		}
		if !found {
			t.Errorf("C6: DeclaredLimit says %q is STILL REPORTED and no row fires on it in both providers", p)
		}
	}
}

// grammarRow is one item of ADR 0087's "Left for the value-axis parity table":
// the same intended value spelled in each language's own literal grammar.
type grammarRow struct {
	item         string
	goLit, tsLit string
	// goDenotes is what strconv.Unquote must decode goLit to; "" when goLit is
	// not one Go literal (a concatenation, or a spelling Go refuses).
	goDenotes      string
	goWant, tsWant verdict
	// escape marks a row about an escape: both spellings must carry a
	// backslash, or text decoded on its way into this file left the row
	// pinning plain characters under an escape's name.
	escape bool
	// continuation is the line terminator of a line-continuation row (lineFeed
	// or carriageReturnLineFeed), "" for every other row. Both spellings must
	// hold bs immediately followed by it: a continuation lost on its way into this
	// file (turned into a real line break, or its backslash dropped) would leave
	// the row pinning another literal under the continuation's name.
	continuation string
	why          string
	divergence   string
}

// bs is one backslash. The four-hex unicode escape is assembled from it, so its
// spelling cannot be decoded away on the way into this file.
const bs = "\\"

// The two line terminators the line-continuation rows put after bs, spelled as
// \x escapes so the bytes (0x0A, and 0x0D 0x0A) are unambiguous in this source.
const (
	lineFeed               = "\x0a"
	carriageReturnLineFeed = "\x0d\x0a"
)

// adr0087 prefixes every divergence below. What follows it is a QUOTE of that
// section, checked against the file, so a divergence cannot be admitted by a
// sentence the repository does not hold.
const adr0087 = "ADR 0087, \"Left for the value-axis parity table\": "

// adr0087Section returns that section of the ADR, whitespace collapsed, ending
// where the next section starts (so a dated note appended later cannot be
// what admits a divergence).
func adr0087Section(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../../docs/decisions/0087-typescript-reaches-the-value-veto-through-a-rule-operator.md")
	if err != nil {
		t.Fatalf("reading ADR 0087: %v", err)
	}
	text := string(raw)
	start := strings.Index(text, "## Left for the value-axis parity table")
	end := strings.Index(text, "## Alternatives rejected")
	if start < 0 || end < start {
		t.Fatalf("ADR 0087 no longer has the section the divergences quote (start %d, end %d)", start, end)
	}
	return strings.Join(strings.Fields(text[start:end]), " ")
}

var grammarRows = []grammarRow{
	{
		item: "\\xHH below 0x80, the same escape in both", goLit: `"lib\x2fconfig.ts"`, tsLit: `"lib\x2fconfig.ts"`,
		goDenotes: "lib/config.ts", goWant: quiet, tsWant: quiet, escape: true,
		why: "\\x2f denotes '/' in both grammars: both hand the predicate lib/config.ts",
	},
	{
		item: "\\/ exists only in JavaScript", goLit: `"lib\/config.ts"`, tsLit: `"lib\/config.ts"`,
		goWant: noSpelling, tsWant: quiet, escape: true,
		why:        "an identity escape in JavaScript; Go refuses the escape",
		divergence: adr0087 + "`\\/`, `\\u{…}` and line continuations exist only in JavaScript",
	},
	{
		item: "\\u{…} exists only in JavaScript", goLit: `"lib\u{2f}config.ts"`, tsLit: `"lib\u{2f}config.ts"`,
		goWant: noSpelling, tsWant: quiet, escape: true,
		why:        "the braced escape denotes '/' in JavaScript; Go has no braced form",
		divergence: adr0087 + "`\\/`, `\\u{…}` and line continuations exist only in JavaScript",
	},
	{
		item: "the four-hex unicode escape, the same in both", goLit: `"lib` + bs + `u002fconfig.ts"`, tsLit: `"lib` + bs + `u002fconfig.ts"`,
		goDenotes: "lib/config.ts", goWant: quiet, tsWant: quiet, escape: true,
		why: "both grammars decode four hex digits to '/'",
	},
	// A line continuation: a backslash immediately followed by a line terminator.
	// Go has no spelling for it (an interpreted string literal cannot hold a line
	// break). Until 2026-11-28 TypeScript SEC-001 was quiet on it WHATEVER the
	// value, because the rule's $VALUE regex did not match across a line feed and
	// the literal never reached the decoder; the regex now admits a line feed right
	// after a backslash, so the secret fires and the path is vetoed. The bytes are
	// assembled from bs, lineFeed and carriageReturnLineFeed, and the byte guard
	// in TestCrossProviderLiteralGrammar checks each row for them.
	{
		item: "a line continuation (LF) in a secret exists only in JavaScript", goLit: `"s3cr` + bs + lineFeed + `3t"`,
		tsLit: `"s3cr` + bs + lineFeed + `3t"`, continuation: lineFeed,
		goWant: noSpelling, tsWant: fires, escape: true,
		why:        "the continuation denotes nothing in JavaScript, so the literal is s3cr3t and fires; Go refuses a line break in a string",
		divergence: adr0087 + "`\\/`, `\\u{…}` and line continuations exist only in JavaScript",
	},
	{
		item: "a line continuation (CR LF) in a secret exists only in JavaScript", goLit: `"s3cr` + bs + carriageReturnLineFeed + `3t"`,
		tsLit: `"s3cr` + bs + carriageReturnLineFeed + `3t"`, continuation: carriageReturnLineFeed,
		goWant: noSpelling, tsWant: fires, escape: true,
		why:        "a CR LF continuation denotes nothing either: s3cr3t fires; Go refuses a line break in a string",
		divergence: adr0087 + "`\\/`, `\\u{…}` and line continuations exist only in JavaScript",
	},
	{
		item: "a line continuation (LF) in a path exists only in JavaScript", goLit: `"lib/con` + bs + lineFeed + `fig.ts"`,
		tsLit: `"lib/con` + bs + lineFeed + `fig.ts"`, continuation: lineFeed,
		goWant: noSpelling, tsWant: quiet, escape: true,
		why:        "the literal denotes lib/config.ts in JavaScript and the value veto silences it; Go refuses a line break in a string",
		divergence: adr0087 + "`\\/`, `\\u{…}` and line continuations exist only in JavaScript",
	},
	{
		item: "a line continuation (CR LF) in a path exists only in JavaScript", goLit: `"lib/con` + bs + carriageReturnLineFeed + `fig.ts"`,
		tsLit: `"lib/con` + bs + carriageReturnLineFeed + `fig.ts"`, continuation: carriageReturnLineFeed,
		goWant: noSpelling, tsWant: quiet, escape: true,
		why:        "lib/config.ts through a CR LF continuation is still vetoed in JavaScript; Go refuses a line break in a string",
		divergence: adr0087 + "`\\/`, `\\u{…}` and line continuations exist only in JavaScript",
	},
	{
		item: "octal \\NNN decodes in Go and is refused in JavaScript", goLit: `"lib/config\056ts"`, tsLit: `"lib/config\056ts"`,
		goDenotes: "lib/config.ts", goWant: quiet, tsWant: fires, escape: true,
		why:        "Go decodes \\056 to '.' and vetoes the path; the JavaScript decoder refuses a legacy octal escape, so it proves nothing",
		divergence: adr0087 + "Go's octal decodes where JavaScript's legacy octal is refused, so the same digits veto in Go and keep firing in TypeScript",
	},
	{
		item: "\\xHH above 0x7f: a byte in Go, a code point in JavaScript", goLit: `"lib/caf\xe9.ts"`, tsLit: `"lib/caf\xe9.ts"`,
		goDenotes: "lib/caf\xe9.ts", goWant: fires, tsWant: fires, escape: true,
		why: "the two decode to different strings (byte 0xE9, or U+00E9 in UTF-8), and neither is in the file-path charset",
	},
	// \a and \U exist only in Go; JavaScript reads both as identity escapes (the
	// letter itself). Only the rows where the two verdicts agree are here.
	// Measured 2026-11-26 and NOT IN THE TABLE, because no document writes the
	// verdict difference: "lib/config\a.ts" (go fires: the bell byte leaves the
	// file-path charset; typescript quiet: lib/configa.ts is a path) and
	// "https:\U0000002f\U0000002fapi.example.com/v1/users" (go quiet: a URL;
	// typescript fires: no "://" survives). Pending: to be declared as known
	// differences with the field test of #148, not fixed.
	{
		item: "\\a exists only in Go, in a secret", goLit: `"s3cr\at"`, tsLit: `"s3cr\at"`,
		goDenotes: "s3cr\at", goWant: fires, tsWant: fires, escape: true,
		why: "Go denotes a bell byte, JavaScript the letter a; neither string has a shape that proves anything",
	},
	{
		item: "\\U exists only in Go, in a path", goLit: `"lib\U0000002fconfig.ts"`, tsLit: `"lib\U0000002fconfig.ts"`,
		goDenotes: "lib/config.ts", goWant: quiet, tsWant: quiet, escape: true,
		why: "Go denotes lib/config.ts; JavaScript denotes libU0000002fconfig.ts, a bare file name with a " +
			"known extension, which the file-path form vetoes too (the Hunter2024.db limit): the same verdict by another route",
	},
	{
		item: "a single-quoted path: a string in JavaScript, not a Go literal", goLit: `'lib/config.ts'`, tsLit: `'lib/config.ts'`,
		goWant: noSpelling, tsWant: quiet,
		why:        "JavaScript vetoes the single-quoted path; Go refuses a rune literal of many characters",
		divergence: adr0087 + "A single-quoted JavaScript literal is a string and is vetoed; in Go it is a rune and never reaches `SEC-001`",
	},
	{
		item: "a single-quoted one-character value: a rune in Go", goLit: `'x'`, tsLit: `'x'`,
		goDenotes: "x", goWant: quiet, tsWant: fires,
		why:        "in JavaScript a one-character string under a credential name fires (no length floor); in Go 'x' is a rune",
		divergence: adr0087 + "A single-quoted JavaScript literal is a string and is vetoed; in Go it is a rune and never reaches `SEC-001`",
	},
	{
		item: "a raw (Go) or template (JavaScript) path", goLit: "`lib/config.ts`", tsLit: "`lib/config.ts`",
		goDenotes: "lib/config.ts", goWant: quiet, tsWant: quiet,
		why: "Go's raw string reaches the veto and is vetoed; the template never reaches SEC-001",
	},
	{
		item: "a raw (Go) or template (JavaScript) secret", goLit: "`s3cr3t`", tsLit: "`s3cr3t`",
		goDenotes: "s3cr3t", goWant: fires, tsWant: quiet,
		why:        "Go's raw string reaches SEC-001 and fires; the template never reaches it",
		divergence: adr0087 + "A Go raw string reaches the veto; a JavaScript template never reaches `SEC-001`",
	},
	{
		item: "a concatenation that denotes a path", goLit: `"lib/" + "config.ts"`, tsLit: `"lib/" + "config.ts"`,
		goWant: quiet, tsWant: fires,
		why:        "Go's SEC-001 accepts only a basic literal; TypeScript binds it, its decoder refuses it as not one literal, and it fires",
		divergence: adr0087 + "Go's `SEC-001` accepts only a basic literal, so a concatenation is silent there; in TypeScript it reaches the rule and keeps firing",
	},
	{
		item: "a concatenation that denotes a secret", goLit: `"s3cr3t" + "x"`, tsLit: `"s3cr3t" + "x"`,
		goWant: quiet, tsWant: fires,
		why:        "Go is blind to it whatever it holds; TypeScript fires",
		divergence: adr0087 + "Go's `SEC-001` accepts only a basic literal, so a concatenation is silent there; in TypeScript it reaches the rule and keeps firing",
	},
}

// TestCrossProviderLiteralGrammar gives every item of ADR 0087's list a measured
// verdict in both columns, with the same controls as the value table.
func TestCrossProviderLiteralGrammar(t *testing.T) {
	requireProbes(t)
	written := adr0087Section(t)
	for _, r := range grammarRows {
		if r.divergence != "" {
			quote, ok := strings.CutPrefix(r.divergence, adr0087)
			if !ok || !strings.Contains(written, strings.Join(strings.Fields(quote), " ")) {
				t.Errorf("C2: %s: the divergence is not a quote of ADR 0087's parity-table section: %s",
					r.item, r.divergence)
			}
		}
		if r.why == "" {
			t.Errorf("%s: no reason written", r.item)
		}
		if (r.escape || strings.Contains(strings.ToLower(r.why), "escape")) &&
			(!strings.Contains(r.goLit, bs) || !strings.Contains(r.tsLit, bs)) {
			t.Errorf("%s: an escape row whose spelling carries no backslash pins plain text: go %s, ts %s",
				r.item, r.goLit, r.tsLit)
			continue
		}
		// The byte guard for line continuations: a row that names one must say
		// which terminator it holds, and both spellings must hold the backslash
		// immediately followed by it (0x5C 0x0A, or 0x5C 0x0D 0x0A).
		if strings.Contains(r.item, "line continuation") && r.continuation == "" {
			t.Errorf("%s: a line-continuation row must name its line terminator", r.item)
			continue
		}
		if r.continuation != "" {
			want := bs + r.continuation
			if !strings.Contains(r.goLit, want) || !strings.Contains(r.tsLit, want) ||
				(r.continuation == lineFeed && (strings.Contains(r.goLit, bs+carriageReturnLineFeed) ||
					strings.Contains(r.tsLit, bs+carriageReturnLineFeed))) {
				t.Errorf("%s: a line-continuation row does not hold the bytes % x: go %q, ts %q",
					r.item, []byte(want), r.goLit, r.tsLit)
				continue
			}
		}
		// Go's denoted string, checked by the language's own decoder, so the row
		// is about the value it says it is about.
		denoted, uerr := strconv.Unquote(r.goLit)
		switch {
		case r.goDenotes != "" && (uerr != nil || denoted != r.goDenotes):
			t.Errorf("%s: Go %s denotes %q (err %v), the row says %q", r.item, r.goLit, denoted, uerr, r.goDenotes)
		case r.goWant == noSpelling && uerr == nil:
			t.Errorf("%s: the row says Go has no spelling, but strconv.Unquote accepts %s", r.item, r.goLit)
		}
		if r.tsWant == noSpelling {
			t.Errorf("%s: a TypeScript spelling must parse whole (C7); write one that does", r.item)
		}

		goGot, err := measure(t, "go", goSites, goVerdict, gatedName, r.goLit)
		if err != nil {
			t.Error(err)
		}
		if goGot != r.goWant {
			t.Errorf("C1: %s: go is %s on %s, table declares %s", r.item, goGot, r.goLit, r.goWant)
		}
		tsGot, err := measure(t, "typescript", tsSites, tsVerdict, gatedName, r.tsLit)
		if err != nil {
			t.Error(err)
		}
		if tsGot != r.tsWant {
			t.Errorf("C1: %s: typescript is %s on %s, table declares %s", r.item, tsGot, r.tsLit, r.tsWant)
		}
		if r.goWant != r.tsWant && r.divergence == "" {
			t.Errorf("C2: %s: go %s, typescript %s, and no divergence is written", r.item, r.goWant, r.tsWant)
		}
		if r.goWant == r.tsWant && r.divergence != "" {
			t.Errorf("C4: %s: the providers agree (%s) but a divergence is still recorded: %s", r.item, r.goWant, r.divergence)
		}
	}
}
