package golang_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/valueshape"
)

// SEC-001's value veto (issue #148, ADR 0086).
//
// The name gate (ADR 0075) decides WHICH identifiers are credentials and is not
// touched here. These tests pin the second question, asked only after the name
// matched: does the literal's own shape prove it is something else? When
// internal/core/valueshape says yes, the provider emits nothing.
//
// Every fixture is real Go source driven through the provider's real go/ast
// path, at each of the three AST shapes that can reach SEC-001. A hand-built
// node could hold a value the parser never produces — a literal that is already
// unquoted, for one — and would keep a test green while protecting nothing.

// sec001Site is one AST shape that reaches SEC-001's name gate. lit is Go
// SOURCE for a string literal, quotes included.
type sec001Site struct {
	shape string
	src   func(name, lit string) string
}

// sec001Sites enumerates every shape security.go dispatches to a secret check:
// *ast.AssignStmt, *ast.ValueSpec and *ast.KeyValueExpr. A veto wired into two
// of the three would leave the third affirming, so each rule below runs at all
// of them.
var sec001Sites = []sec001Site{
	{
		shape: "AssignStmt",
		src: func(name, lit string) string {
			return "package p\n\nfunc f() { " + name + " := " + lit + "; _ = " + name + " }\n"
		},
	},
	{
		shape: "ValueSpec",
		src: func(name, lit string) string {
			return "package p\n\nconst " + name + " = " + lit + "\n"
		},
	},
	{
		shape: "KeyValueExpr",
		src: func(name, lit string) string {
			return "package p\n\ntype Cfg struct{ " + name + " string }\n\n" +
				"func f() Cfg { return Cfg{" + name + ": " + lit + "} }\n"
		},
	},
}

// gatedName is a name the gate accepts at every site. It is the ADR 0075
// constraint's own identifier, so the positive control below is that
// constraint, not a lookalike.
const gatedName = "signingKey"

func sec001Of(t *testing.T, src string) []findings.Finding {
	t.Helper()
	var out []findings.Finding
	for _, f := range analyzeSec(t, "x.go", src) {
		if f.ID == "SEC-001" {
			out = append(out, f)
		}
	}
	return out
}

// requireSiteReachesTheGate is the positive probe every quiet assertion below
// depends on: the same site, with a value no guard can prove anything about,
// must fire. Without it "no finding" would also be what a template that never
// reaches the name gate produces.
func requireSiteReachesTheGate(t *testing.T, site sec001Site) {
	t.Helper()
	if got := sec001Of(t, site.src(gatedName, strconv.Quote("s3cr3t"))); len(got) != 1 {
		t.Fatalf("vacuum: %s with a plain short literal emitted %d SEC-001, want 1 — "+
			"the template does not reach the name gate, so a quiet result proves nothing", site.shape, len(got))
	}
}

// vetoedForms holds one literal per valueshape.Form.
var vetoedForms = map[valueshape.Form]string{
	valueshape.FormURL:        "https://api.example.com/v1/users",
	valueshape.FormURN:        "urn:ietf:params:oauth:grant-type:jwt-bearer",
	valueshape.FormEnvVarName: "STRIPE_API_KEY",
	valueshape.FormFilePath:   "lib/config.ts",
}

// TestSEC001ValueVetoIsQuietForEveryFormAtEverySite is the rule itself: a
// literal that proves it is a URL, a URN, an environment-variable name or a
// file path emits NOTHING — no SEC-001, and no demoted finding in its place.
func TestSEC001ValueVetoIsQuietForEveryFormAtEverySite(t *testing.T) {
	for _, form := range valueshape.Forms() {
		value, ok := vetoedForms[form]
		if !ok {
			t.Errorf("valueshape declares form %q and no provider fixture exercises it", form)
			continue
		}
		// The fixture is verified by its CONTENT: it must be the form it is
		// filed under, or this row would pass for an unrelated reason.
		if got, vetoed := valueshape.NotACredential(value); !vetoed || got != form {
			t.Errorf("fixture %q is filed under %q but valueshape says (%q, %v)", value, form, got, vetoed)
			continue
		}
		for _, site := range sec001Sites {
			t.Run(string(form)+"/"+site.shape, func(t *testing.T) {
				requireSiteReachesTheGate(t, site)
				fs := analyzeSec(t, "x.go", site.src(gatedName, strconv.Quote(value)))
				if len(fs) != 0 {
					t.Errorf("%q proves it is a %s and the provider still emitted: %+v", value, form, fs)
				}
			})
		}
	}
}

// TestSEC001SecretFileNamedConfigPathIsQuiet is the reproduction that opened
// issue #148, verbatim: the name gate matched the component "secret", and the
// value is a path to a TypeScript file.
func TestSEC001SecretFileNamedConfigPathIsQuiet(t *testing.T) {
	const reproduction = "package p\n\nconst secretFile = \"lib/config.ts\"\n"
	if fs := sec001Of(t, reproduction); len(fs) != 0 {
		t.Errorf("SEC-001 affirmed a file path as a hardcoded credential: %+v", fs)
	}
	// The name gate is unchanged: the same name still fires on a value that
	// proves nothing.
	const sameName = "package p\n\nconst secretFile = \"s3cr3t\"\n"
	if fs := sec001Of(t, sameName); len(fs) != 1 {
		t.Errorf("secretFile no longer reaches the name gate: got %d SEC-001, want 1", len(fs))
	}
}

// TestSEC001ValueVetoKeepsFiring is the mandatory negative fixture: every value
// form the veto does NOT silence, each with the reason it must not. A row
// without a reason fails.
//
// The veto is a proof, never a suspicion. Each row here is either a real
// credential shape, or a shape that cannot be told apart from one.
func TestSEC001ValueVetoKeepsFiring(t *testing.T) {
	cases := []struct {
		value  string
		reason string
	}{
		{
			value:  "s3cr3t",
			reason: "ADR 0075: a credential has no length floor, so a short literal proves nothing",
		},
		{
			value:  "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
			reason: "AWS's published example secret access key: it contains '/' and '+', so a bare '/' is never proof of a path",
		},
		{
			value:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			reason: "a hex blob: no scheme, no underscore, no extension — no guard has a shape to prove against",
		},
		{
			value:  "FQoGZXIvYXdzEBQaDG5vdF9hX3NlY3JldA==",
			reason: "a base64 STS-style token: '=' is outside the file-path charset",
		},
		{
			value:  "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
			reason: "a JWT: its dots look like a path's, but its last segment is not a known file extension",
		},
		{
			value:  "JBSWY3DPEHPK3PXP",
			reason: "a base32 TOTP secret: all-caps with no underscore, which the env-var-name form requires",
		},
		{
			value:  "postgres://user:pass@db.example.com/app",
			reason: "a URL WITH userinfo carries the credential in user:pass@",
		},
		{
			value:  "https://api.example.com/v1?api_key=abc123",
			reason: "a URL whose query has a credential-named parameter carrying a value",
		},
		{
			value:  "https://app.example.com/cb#access_token=abc",
			reason: "OAuth's implicit flow puts the credential in the fragment",
		},
		{
			value:  "https://hooks.example.com/services/T0A1B2C3D/B0A1B2C3D/abcDEF123ghiJKL456mnoPQR",
			reason: "a webhook URL: the secret IS a path segment (16+ bytes, letters and digits)",
		},
		{
			value:  "https://bucket.example.com/o?X-Amz-Signature=0f1e2d3c4b5a69788796a5b4c3d2e1f0",
			reason: "a presigned URL: the signature is a token-like query value under a key the name vocabulary does not know",
		},
		// B1: a token-like run in a URL position the guard once skipped. Each
		// fired before the veto existed and must fire with it.
		{
			value:  "https://k9f8e7d6c5b4a3928170.lambda-url.us-east-1.on.aws/",
			reason: "a function URL: the secret is a host label",
		},
		{
			value:  "https://abcdefg1234567hijklmn890opqrs.lambda-url.us-east-2.on.aws/",
			reason: "a function URL whose host label is a 29-byte token",
		},
		{
			value:  "https://k9f8e7d6c5b4a3928170.api.example.com/v1",
			reason: "a token-like host label in front of an ordinary path",
		},
		{
			value:  "https://eo1a2b3c4d5e6f7g8.m.pipedream.net",
			reason: "a request-bin URL: the secret is a 17-byte host label",
		},
		{
			value:  "https://app.example.com/#/invite/9f8e7d6c5b4a39281706",
			reason: "a hash-route invite link: a fragment that is not key=value is inspected whole",
		},
		{
			value:  "https://app.example.com/login#Ab12Cd34Ef56Gh78Ij90",
			reason: "a bare token as the whole fragment",
		},
		{
			value:  "https://app.example.com/#9f8e7d6c5b%264a39281706",
			reason: "an encoded '&' inside a bare fragment: the fragment is split before it is decoded, so the 21-byte token stays whole",
		},
		{
			value:  "https://app.example.com/#9f8e7d6c5b%3D4a39281706",
			reason: "an encoded '=' inside a bare fragment: split before decoding, so it cannot cut the token into a short key and a short value",
		},
		{
			value:  "https://app.example.com/cb#access_token%3Dabc",
			reason: "the credential-named-parameter check reads the decoded fragment, so an encoded '=' still yields access_token=abc",
		},
		{
			value:  "https://cal.example.com/feed.ics?9f8e7d6c5b4a39281706",
			reason: "a calendar feed URL: the secret is a bare query key with no '='",
		},
		{
			value:  "https://cal.example.com/private/feed.ics?Ab12Cd34Ef56Gh78Ij90",
			reason: "a bare query key token after a two-segment path",
		},
		{
			value:  "https://example.com/hook/9f8e7d6c5b%2F4a39281706",
			reason: "an encoded '/' inside one path segment: decoded whole it is a 21-byte token",
		},
		{
			value:  "https://img.example.com/s/qL8%2Fz3%2BxW9v%2Fk2N%3D/photo.jpg",
			reason: "a base64 token percent-encoded into one path segment",
		},
		// A query or fragment url.ParseQuery rejects: no token and no
		// credential name, so the parse failure is the only thing that keeps
		// each of these firing.
		{
			value:  "https://api.example.com/v1/items?page=2;sort=asc",
			reason: "a query url.ParseQuery rejects (a literal ';'): what the guards cannot read is not proof of innocence",
		},
		{
			value:  "https://api.example.com/v1/items?q=%zz",
			reason: "a query with an invalid percent-escape: url.Parse accepts it and url.ParseQuery rejects it",
		},
		{
			value:  "https://app.example.com/#/settings;tab=2",
			reason: "a fragment url.ParseQuery rejects (a literal ';')",
		},
		{
			value:  "https://app.example.com/#a%3Bb",
			reason: "a fragment whose decoded form ('a;b') url.ParseQuery rejects, although its escaped form parses",
		},
		{
			value:  "correct horse battery staple",
			reason: "a passphrase is a secret with spaces; the prose veto was dropped, so prose keeps firing as declared noise",
		},
		{
			value:  "test_client_secret",
			reason: "a short test literal: by shape indistinguishable from a real short secret",
		},
		{
			value:  "progressToken",
			reason: "a short identifier: by shape indistinguishable from a real short secret",
		},
		{
			value:  "/etc/secrets/db",
			reason: "a path with no extension: the file-path form anchors on a closed extension list and never guesses from structure",
		},
		{
			value:  "secret.notanext",
			reason: "a dotted value whose trailing segment is not a known file extension",
		},
		{
			value:  "X-Api-Key",
			reason: "a header name: mixed case fails env-var-name and there is no extension",
		},
		{
			value:  "<your-secret-here>",
			reason: "a placeholder that is not SCREAMING_SNAKE: no form covers it",
		},
		{
			value:  "changeme",
			reason: "a lowercase placeholder: by shape indistinguishable from a real short secret",
		},
		{
			value:  "TODO",
			reason: "an all-caps placeholder with no underscore: env-var-name requires one, so it keeps firing",
		},
		{
			value:  "<token>",
			reason: "an angle-bracket placeholder: no form covers it",
		},
		{
			value:  "__SECRET__",
			reason: "env-var-name needs an upper-case letter first and allows no leading, trailing or doubled underscore",
		},
	}

	for _, tc := range cases {
		if tc.reason == "" {
			t.Errorf("negative fixture %q carries no reason — every keeps-firing row must say why", tc.value)
			continue
		}
		// Verified by content: if valueshape ever starts vetoing this value the
		// failure names the predicate, not the provider.
		if form, vetoed := valueshape.NotACredential(tc.value); vetoed {
			t.Errorf("fixture %q is filed as keeps-firing and valueshape vetoes it as %q", tc.value, form)
			continue
		}
		for _, site := range sec001Sites {
			t.Run(tc.value+"/"+site.shape, func(t *testing.T) {
				fs := sec001Of(t, site.src(gatedName, strconv.Quote(tc.value)))
				if len(fs) != 1 {
					t.Fatalf("got %d SEC-001, want 1 — %s", len(fs), tc.reason)
				}
				f := fs[0]
				if f.Confidence != 1.0 {
					t.Errorf("Confidence = %v, want 1.0 — what still fires is not demoted", f.Confidence)
				}
				if f.Severity != findings.SeverityHigh {
					t.Errorf("Severity = %q, want %q", f.Severity, findings.SeverityHigh)
				}
				if !strings.Contains(f.Description, "'"+gatedName+"'") {
					t.Errorf("finding does not name the identifier: %q", f.Description)
				}
			})
		}
	}
}

// TestSEC001ValueVetoDeclaredBlindSpots locks the cost of the veto, so it is a
// declared limit and not a belief: these values are vetoed although each COULD
// be a real secret. capability.go and COVERAGE.md say so; if one of these rows
// starts firing, those declarations became false and must change in the same
// commit.
func TestSEC001ValueVetoDeclaredBlindSpots(t *testing.T) {
	cases := []struct {
		value string
		form  valueshape.Form
		limit string
	}{
		{
			value: "MY_S3CR3T_VALUE",
			form:  valueshape.FormEnvVarName,
			limit: "an all-caps secret containing an underscore has the shape of an environment-variable name",
		},
		{
			value: "https://api.example.com/hook/abc123",
			form:  valueshape.FormURL,
			limit: "a token shorter than 16 bytes inside a URL gives the token-like guard nothing to act on",
		},
		{
			value: "https://api.example.com/hook/abcdefghijklmnopqrstuvwx",
			form:  valueshape.FormURL,
			limit: "a URL-embedded token with no digit is read as a descriptive path segment",
		},
		{
			value: "https://api.example.com/v1/k/AbCd%2FEfGh%2BIjKl%2FMnOp%2FQrSt",
			form:  valueshape.FormURL,
			limit: "an encoded token whose decoded form has no digit: the hex of '%2F' is encoding, not content",
		},
		{
			value: "https://api.example.com/hook/9f8e7d6c/5b4a39281706",
			form:  valueshape.FormURL,
			limit: "a token cut by the URL's own separators into pieces each shorter than 16 bytes",
		},
		{
			value: "https://app.example.com/#9f8e7d6c5b&4a39281706",
			form:  valueshape.FormURL,
			limit: "a token cut by a literal '&' in a fragment into two keys each shorter than 16 bytes",
		},
		{
			value: "https://cal.example.com/feed.ics?9f8e7d6c5b&4a39281706",
			form:  valueshape.FormURL,
			limit: "a token cut by a literal '&' in a query into two keys each shorter than 16 bytes",
		},
		{
			value: "Hunter2024.db",
			form:  valueshape.FormFilePath,
			limit: "a bare word.ext with a listed extension and no separator has the shape of a file name",
		},
	}
	for _, tc := range cases {
		if got, vetoed := valueshape.NotACredential(tc.value); !vetoed || got != tc.form {
			t.Errorf("fixture %q is filed under %q but valueshape says (%q, %v)", tc.value, tc.form, got, vetoed)
			continue
		}
		for _, site := range sec001Sites {
			t.Run(tc.value+"/"+site.shape, func(t *testing.T) {
				requireSiteReachesTheGate(t, site)
				if fs := sec001Of(t, site.src(gatedName, strconv.Quote(tc.value))); len(fs) != 0 {
					t.Errorf("SEC-001 fired — then the declared limit is false (%s): %+v", tc.limit, fs)
				}
			})
		}
	}
}

// TestSEC001ValueVetoReadsTheValueNotItsSourceText pins WHAT is handed to the
// predicate: the string the literal denotes, not the characters between its
// quotes. The two differ whenever the source uses an escape.
//
// The first row is the one that matters: in source text an escaped '=' leaves
// the credential-named query parameter with no value, and the URL would be
// vetoed while the program holds `…?api_key=abc`.
//
// The value after the '=' is three bytes on purpose. Read as source text, the
// query is the single key `api_key\x3dabc`, 14 bytes: too short for the
// token-like guard, and a credential-named key with no value, so ONLY decoding
// makes this row fire. A value of 16 or more bytes would make that undecoded key
// token-like and the row would fire with or without decoding, pinning nothing.
func TestSEC001ValueVetoReadsTheValueNotItsSourceText(t *testing.T) {
	const escapedEquals = `"https://api.example.com/v1?api_key\x3dabc"`
	for _, site := range sec001Sites {
		if fs := sec001Of(t, site.src(gatedName, escapedEquals)); len(fs) != 1 {
			t.Errorf("%s: got %d SEC-001, want 1 — the literal denotes a URL carrying api_key=abc",
				site.shape, len(fs))
		}
	}

	quiet := map[string]string{
		"escaped slashes denote a plain URL": `"https:\x2f\x2fapi.example.com/v1/users"`,
		"a raw string literal":               "`lib/config.ts`",
	}
	for name, lit := range quiet {
		for _, site := range sec001Sites {
			requireSiteReachesTheGate(t, site)
			if fs := sec001Of(t, site.src(gatedName, lit)); len(fs) != 0 {
				t.Errorf("%s / %s: SEC-001 fired on a value that proves it is not a credential: %+v",
					name, site.shape, fs)
			}
		}
	}
}

// TestSEC001ValueVetoIsPerValue covers the shapes that carry several name/value
// pairs in one node. The veto belongs to one value: it must not silence its
// neighbour, and a neighbour's plain value must not un-veto it.
func TestSEC001ValueVetoIsPerValue(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "ValueSpec, first value vetoed",
			src:  "package p\n\nvar apiKey, password = \"https://api.example.com/v1/users\", \"hunter2\"\n",
			want: "password",
		},
		{
			name: "ValueSpec, second value vetoed",
			src:  "package p\n\nvar apiKey, password = \"hunter2\", \"lib/config.ts\"\n",
			want: "apiKey",
		},
		{
			name: "AssignStmt, first value vetoed",
			src:  "package p\n\nfunc f() { apiKey, password := \"urn:example:a\", \"hunter2\"; _, _ = apiKey, password }\n",
			want: "password",
		},
		{
			name: "AssignStmt, second value vetoed",
			src:  "package p\n\nfunc f() { apiKey, password := \"hunter2\", \"STRIPE_API_KEY\"; _, _ = apiKey, password }\n",
			want: "apiKey",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := sec001Of(t, tc.src)
			if len(fs) != 1 {
				t.Fatalf("got %d SEC-001, want exactly 1 (for %s): %+v", len(fs), tc.want, fs)
			}
			if !strings.Contains(fs[0].Description, "'"+tc.want+"'") {
				t.Errorf("the wrong name was reported, want %s: %q", tc.want, fs[0].Description)
			}
		})
	}
}

// TestSEC001EmptyLiteralIsStillQuiet keeps the one value condition that existed
// before the veto: an empty string under a credential name was never reported,
// and the veto is not what makes it quiet.
func TestSEC001EmptyLiteralIsStillQuiet(t *testing.T) {
	for _, site := range sec001Sites {
		requireSiteReachesTheGate(t, site)
		if fs := sec001Of(t, site.src(gatedName, `""`)); len(fs) != 0 {
			t.Errorf("%s: SEC-001 fired on an empty literal: %+v", site.shape, fs)
		}
	}
}
