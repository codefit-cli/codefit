package valueshape

import (
	"regexp"
	"strings"
	"testing"
)

// TestForms locks the exhaustive list: every const exactly once, in the
// declared order.
func TestForms(t *testing.T) {
	got := Forms()
	want := []Form{FormURL, FormURN, FormEnvVarName, FormFilePath}
	if len(got) != len(want) {
		t.Fatalf("Forms() = %v, want %v", got, want)
	}
	seen := make(map[Form]bool, len(got))
	for i, f := range got {
		if f != want[i] {
			t.Errorf("Forms()[%d] = %q, want %q", i, f, want[i])
		}
		if seen[f] {
			t.Errorf("Forms() lists %q more than once", f)
		}
		seen[f] = true
	}
}

// TestNotACredential_Vetoed is the positive table: every value here proves,
// by its own shape, that it is something other than a credential.
func TestNotACredential_Vetoed(t *testing.T) {
	cases := []struct {
		value string
		form  Form
	}{
		{`https://api.amazon.com/auth/o2/token`, FormURL},
		{`http://localhost:8080/callback`, FormURL},
		{`https://example.com/cb?state=xyz`, FormURL},
		{`https://api.example.com/?token=`, FormURL},
		{`urn:ietf:params:oauth:token-type:id_token`, FormURN},
		{`URN:example:a`, FormURN},
		{`CLIENT_SECRET`, FormEnvVarName},
		{`YOUR_CLIENT_SECRET`, FormEnvVarName},
		{`AWS_SECRET_ACCESS_KEY`, FormEnvVarName},
		{`lib/config.ts`, FormFilePath},
		{`certs/server.key`, FormFilePath},
		{`.env`, FormFilePath},
		{`config/app.YAML`, FormFilePath},
		{`~/.ssh/id_rsa.pub`, FormFilePath},
		// The three rows below pin the token-like guard's thresholds from
		// both sides, so a later change to tokenLikeMinLength or to the
		// letter/digit requirement cannot silently widen or narrow it.
		{`https://accounts.google.com/.well-known/openid-configuration`, FormURL}, // long segment, letters only, no digit — must stay vetoed
		{`https://login.microsoftonline.com/common/oauth2/v2.0/token`, FormURL},   // segments carry digits but none reach the length floor — must stay vetoed
		{`https://api.example.com/v1/users/12345678901234567890`, FormURL},        // a long all-digit segment is an id, not a secret — must stay vetoed
		// Negative fixture for the positions B1 added to the token-like guard
		// (host label, bare/keyed query key, non-pair fragment, encoded
		// separator). Each must STAY vetoed after the guard inspects them; the
		// comment is the reason.
		{`https://authorizationserver.example.com/token`, FormURL},              // a 19-byte host label of letters only: a descriptive name, no digit
		{`https://ec2-12-34-56-78.compute-1.amazonaws.com/`, FormURL},           // host labels mix letters and digits but none reaches 16 bytes
		{`https://api.example.com:8443/v1/users`, FormURL},                      // the port is not a host label
		{`https://[2001:db8::8a2e:370:7334]:8443/v1`, FormURL},                  // an IPv6 literal host: 23 bytes of hex and colons, skipped as an address
		{`http://[fe80::1ff:fe23:4567:890a%25eth0]/status`, FormURL},            // an IPv6 literal with a zone: still an address, still skipped
		{`https://docs.example.com/guide#section-2`, FormURL},                   // a fragment anchor with a digit, under 16 bytes
		{`https://app.example.com/#/settings/notifications`, FormURL},           // a hash route of letters only
		{`https://api.example.com/v1/items?debug`, FormURL},                     // a bare query flag: short, letters only
		{`https://api.example.com/v1/items?page=2&include_archived=1`, FormURL}, // query keys are inspected; these are names without a digit
		{`https://api.example.com/api/v2/resources`, FormURL},                   // a version-like path: every segment short
		{`https://example.com/files/report%2Fq3.pdf`, FormURL},                  // an encoded separator inside a short segment: decoded whole, still 13 bytes
	}

	firedForm := make(map[Form]bool, len(Forms()))

	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			form, ok := NotACredential(tc.value)
			if !ok {
				t.Fatalf("NotACredential(%q) = (%q, false), want (%q, true)", tc.value, form, tc.form)
			}
			if form != tc.form {
				t.Errorf("NotACredential(%q) form = %q, want %q", tc.value, form, tc.form)
			}
			firedForm[form] = true
		})
	}

	for _, f := range Forms() {
		if !firedForm[f] {
			t.Errorf("no VETOED row exercises %q — a Form that can never fire is a dead branch", f)
		}
	}
}

// TestNotACredential_KeepsFiring is the mandatory negative fixture: values
// that must NOT be vetoed, each carrying a written reason. A row with an
// empty reason fails the test — see the loop below.
func TestNotACredential_KeepsFiring(t *testing.T) {
	cases := []struct {
		value  string
		reason string
	}{
		{
			value:  `s3cr3t`,
			reason: "ADR 0075: no length floor — a short literal is not, by itself, proof of anything",
		},
		{
			value:  `wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY`,
			reason: "AWS's own published example secret access key: carries '/' and '+', neither of which the file-path charset admits",
		},
		{
			value:  `AKIAIOSFODNN7EXAMPLE`,
			reason: "SCREAMING_SNAKE with no underscore — env-var-name requires at least one, so a real AWS access key ID keeps firing",
		},
		{
			value:  `JBSWY3DPEHPK3PXP`,
			reason: "base32 TOTP secret: uppercase and digits with no underscore, same gap as the AWS key ID",
		},
		{
			value:  `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`,
			reason: "64-hex string: no dot, no underscore, no scheme — no guard has a shape to prove against",
		},
		{
			value:  `eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U`,
			reason: "a JWT: the dots pass the file-path charset, but the trailing segment is not in the closed extension list",
		},
		{
			value:  `FQoGZXIvYXdzEBQaDG5vdF9hX3NlY3JldA==`,
			reason: "a base64 STS-like token ending in '==' — '=' is outside the file-path charset",
		},
		{
			value:  `postgres://user:pass@db.example.com/app`,
			reason: "a URL WITH userinfo carries a credential in user:pass@ — must keep firing",
		},
		{
			value:  `https://api.example.com/v1?api_key=abc123`,
			reason: "a query parameter whose key matches the credential vocabulary with a non-empty value",
		},
		{
			value:  `https://app.example.com/cb#access_token=abc`,
			reason: "OAuth implicit flow puts the credential in the fragment, not the query",
		},
		{
			value:  `http://`,
			reason: "no host — url.Parse succeeds but proves nothing",
		},
		{
			value:  `urn:`,
			reason: "incomplete URN — no namespace identifier",
		},
		{
			value:  `correct horse battery staple`,
			reason: "a passphrase is a secret with spaces — the prose veto was dropped 2026-09-28 (see doc.go)",
		},
		{
			value:  `master secret`,
			reason: "prose — known noise (4/320 Go + 2/52 TS), kept firing rather than risk silencing a real passphrase",
		},
		{
			value:  `test_client_secret`,
			reason: "a short identifier-shaped literal, indistinguishable by shape from a real secret",
		},
		{
			value:  `progressToken`,
			reason: "a short identifier-shaped literal, indistinguishable by shape from a real secret",
		},
		{
			value:  `lib/config`,
			reason: "a file path with no extension — the file-path guard requires one, to stay anchored on a closed list",
		},
		{
			value:  `/etc/secrets/db`,
			reason: "a file path with no extension",
		},
		{
			value:  `CLIENT_SECRET=abc`,
			reason: "'=' is outside the env-var-name charset — this is a NAME=VALUE pair, not a bare name",
		},
		{
			value:  `Bearer abc123`,
			reason: "whitespace disqualifies every guard — prose keeps firing",
		},
		{
			value:  `secret.notanext`,
			reason: "has a dot, but the trailing segment is not in the closed extension list",
		},
		{
			value:  `X-Api-Key`,
			reason: "a header name: mixed case disqualifies env-var-name, no dot disqualifies file-path",
		},
		{
			value:  `https://hooks.slack.com/services/T0A1B2C3D/B0A1B2C3D/abcDEF123ghiJKL456mnoPQR`,
			reason: "a Slack incoming-webhook URL: the secret IS the last path segment (token-like: >=16 chars, letters and digits)",
		},
		{
			value:  `https://discord.com/api/webhooks/123456789012345678/AbCdEf1234567890GhIjKlMnOp`,
			reason: "a Discord webhook URL: the secret is a token-like path segment, even though the segment before it is a long all-digit webhook id",
		},
		{
			value:  `https://bucket.s3.amazonaws.com/o?X-Amz-Signature=0f1e2d3c4b5a69788796a5b4c3d2e1f0`,
			reason: "an S3 presigned URL: the signature lives in a query VALUE whose KEY ('X-Amz-Signature') is not in the credential vocabulary, so only the token-like check catches it",
		},
		{
			value:  `https://acct.blob.core.windows.net/c/b?sv=2021-08-06&sig=AbC123dEf456GhI789jKl%3D`,
			reason: "an Azure SAS URL: 'sig' is not a credential-vocabulary key, so only the token-like check on the decoded query value catches it",
		},
		{
			value:  `https://api.telegram.org/bot123456789:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsaw/sendMessage`,
			reason: "a Telegram bot-token URL: the token lives in a path segment together with the literal 'bot' prefix",
		},
		// B1: the four positions the token-like guard did not inspect. Every
		// row here was vetoed as FormURL before the guard read it.
		{
			value:  `https://k9f8e7d6c5b4a3928170.lambda-url.us-east-1.on.aws/`,
			reason: "a function URL: the secret is the first HOST LABEL (20 bytes, letters and digits)",
		},
		{
			value:  `https://abcdefg1234567hijklmn890opqrs.lambda-url.us-east-2.on.aws/`,
			reason: "a function URL whose host label is a 29-byte token",
		},
		{
			value:  `https://k9f8e7d6c5b4a3928170.api.example.com`,
			reason: "a token-like HOST LABEL with no path at all",
		},
		{
			value:  `https://k9f8e7d6c5b4a3928170.api.example.com/v1`,
			reason: "a token-like HOST LABEL in front of an ordinary path",
		},
		{
			value:  `https://k9f8e7d6c5b4a3928170.example.com:8443/v1`,
			reason: "a token-like HOST LABEL with a port: the port is stripped, the label is still read",
		},
		{
			value:  `https://eo1a2b3c4d5e6f7g8.m.pipedream.net`,
			reason: "a request-bin URL: the secret is a 17-byte HOST LABEL",
		},
		{
			value:  `https://app.example.com/#/invite/9f8e7d6c5b4a39281706`,
			reason: "a hash-route invite link: the fragment is not key=value, so it reads as one valueless key, inspected whole",
		},
		{
			value:  `https://app.example.com/login#9f8e7d6c5b4a39281706`,
			reason: "a bare token as the whole FRAGMENT",
		},
		{
			value:  `https://app.example.com/login#Ab12Cd34Ef56Gh78Ij90`,
			reason: "a bare token as the whole FRAGMENT, mixed case",
		},
		{
			value:  `https://app.example.com/#9f8e7d6c5b%264a39281706`,
			reason: "an encoded '&' inside a bare FRAGMENT: the fragment is split before it is decoded, so the 21-byte token stays whole, exactly as it does in the query",
		},
		{
			value:  `https://app.example.com/#9f8e7d6c5b%3D4a39281706`,
			reason: "an encoded '=' inside a bare FRAGMENT: split before decoding, so it cannot cut the 21-byte token into a short key and a short value",
		},
		{
			value:  `https://app.example.com/cb#access_token%3Dabc`,
			reason: "the credential-named-parameter check reads the DECODED fragment, so an encoded '=' still yields access_token=abc — the token-like fix does not touch this check",
		},
		{
			value:  `https://cal.example.com/feed.ics?9f8e7d6c5b4a39281706`,
			reason: "a calendar feed URL: the secret is a BARE QUERY KEY with no '='",
		},
		{
			value:  `https://cal.example.com/private/feed.ics?Ab12Cd34Ef56Gh78Ij90`,
			reason: "a BARE QUERY KEY token after a two-segment path",
		},
		{
			value:  `https://api.example.com/v1/items?page=2&9f8e7d6c5b4a39281706`,
			reason: "a BARE QUERY KEY token after an ordinary pair",
		},
		{
			value:  `https://api.example.com/v1/items?9f8e7d6c5b4a39281706=1`,
			reason: "a token in the KEY position of a pair that carries a value",
		},
		{
			value:  `https://example.com/hook/9f8e7d6c5b%2F4a39281706`,
			reason: "an encoded '/' inside ONE path segment: decoded whole it is 21 bytes; split after decoding it would be two short pieces",
		},
		{
			value:  `https://img.example.com/s/qL8%2Fz3%2BxW9v%2Fk2N%3D/photo.jpg`,
			reason: "a base64 token percent-encoded into one path segment: decoded whole it is 16 bytes with letters and digits",
		},
		{
			value:  `cache:key.json`,
			reason: "':' is outside the file-path charset even without '//' — the charset guard, not just the scheme-and-authority check, must reject it despite the valid .json extension",
		},
		{
			value:  `secret.jsonx`,
			reason: "the closed extension list is anchored to the TRUE trailing extension — 'jsonx' is not itself listed, even though it contains '.json' as a substring",
		},
		// A query or fragment that url.ParseQuery rejects is evidence the
		// guards cannot read. None of these carries a token or a credential
		// name: the parse failure is the only thing that keeps them firing.
		{
			value:  `https://api.example.com/v1/items?page=2;sort=asc`,
			reason: "a query url.ParseQuery rejects (a literal ';'): what the guards cannot read is not proof of innocence",
		},
		{
			value:  `https://api.example.com/v1/items?q=%zz`,
			reason: "a query with an invalid percent-escape: url.Parse accepts it and url.ParseQuery rejects it",
		},
		{
			value:  `https://app.example.com/#/settings;tab=2`,
			reason: "a fragment url.ParseQuery rejects (a literal ';'), in its escaped form and in its decoded form alike",
		},
		{
			value:  `https://app.example.com/#a%3Bb`,
			reason: "a fragment whose escaped form parses but whose decoded form ('a;b') does not: the credential-name check reads the decoded form, and that parse failure alone keeps this firing",
		},
		{
			value:  ``,
			reason: "the empty string establishes nothing",
		},
	}

	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			if tc.reason == "" {
				t.Fatalf("negative fixture %q carries no reason — every keeps-firing row must say why", tc.value)
			}
			if form, ok := NotACredential(tc.value); ok {
				t.Errorf("NotACredential(%q) = (%q, true), want (_, false): %s", tc.value, form, tc.reason)
			}
		})
	}
}

// TestParseFailureDoesNotVetoInEitherGuard pins each URL guard's OWN answer to
// a query or fragment it cannot parse. Both must answer in the direction that
// does not veto, each on its own. Through NotACredential the two cannot be told
// apart for the three keeps-firing rows whose query or fragment fails to parse
// in both its escaped and its decoded form (`?page=2;sort=asc`, `?q=%zz`,
// `#/settings;tab=2`): those rows fail only when BOTH guards are wrong. The
// `#a%3Bb` row is caught by carriesCredential alone, because its escaped
// fragment parses and its decoded one does not. queryHasTokenLike on its own is
// observable only through this test.
func TestParseFailureDoesNotVetoInEitherGuard(t *testing.T) {
	unparseable := []string{
		"page=2;sort=asc", // a literal ';'
		"q=%zz",           // an invalid percent-escape
		"/settings;tab=2", // a hash route with a literal ';'
	}
	for _, raw := range unparseable {
		t.Run(raw, func(t *testing.T) {
			if !carriesCredential(raw) {
				t.Errorf("carriesCredential(%q) = false, want true: an unparseable query is not proof it carries no credential", raw)
			}
			if !queryHasTokenLike(raw) {
				t.Errorf("queryHasTokenLike(%q) = false, want true: an unparseable query is not proof it carries no token", raw)
			}
		})
	}
}

// TestNotACredential_DeclaredBlindSpots locks what the veto still hides after
// the token-like guard inspects every URL position: each value here COULD be a
// real secret and IS vetoed. doc.go declares every row; if one starts firing,
// the declaration became false and must change in the same commit.
func TestNotACredential_DeclaredBlindSpots(t *testing.T) {
	cases := []struct {
		value string
		form  Form
		limit string
	}{
		{`https://abc123def456.example.com/`, FormURL, "a token-like host label shorter than 16 bytes"},
		{`https://api.example.com/hook/abc123`, FormURL, "a path token shorter than 16 bytes"},
		{`https://app.example.com/#/invite/abc123`, FormURL, "a fragment token shorter than 16 bytes"},
		{`https://api.example.com/hook/abcdefghijklmnopqrstuvwx`, FormURL, "a 24-byte path token with no digit"},
		{`https://api.example.com/v1/k/AbCd%2FEfGh%2BIjKl%2FMnOp%2FQrSt`, FormURL, "an encoded token whose DECODED form has no digit — the hex of '%2F' is encoding, not content"},
		{`https://api.example.com/hook/9f8e7d6c/5b4a39281706`, FormURL, "a token cut by a literal '/' into two segments each shorter than 16 bytes"},
		{`https://9f8e7d6c.5b4a39281706.example.com/`, FormURL, "a token cut by '.' into two host labels each shorter than 16 bytes"},
		{`https://app.example.com/#9f8e7d6c5b&4a39281706`, FormURL, "a token cut by a literal '&' in a fragment into two keys each shorter than 16 bytes"},
		{`https://app.example.com/#9f8e7d6c5b=4a39281706`, FormURL, "a token cut by a literal '=' in a fragment into a key and a value each shorter than 16 bytes"},
		{`https://cal.example.com/feed.ics?9f8e7d6c5b&4a39281706`, FormURL, "a token cut by a literal '&' in a query into two keys each shorter than 16 bytes"},
		{`https://app.example.com/#1234567890123456%2541`, FormURL, "a fragment is decoded once: '%2541' denotes the three characters '%41', not the letter 'A', so this token has no letter"},
		{`Hunter2024.db`, FormFilePath, "a bare word.ext with a listed extension and no separator has the shape of a file name"},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			if tc.limit == "" {
				t.Fatalf("blind-spot fixture %q carries no declared limit", tc.value)
			}
			form, ok := NotACredential(tc.value)
			if !ok || form != tc.form {
				t.Errorf("NotACredential(%q) = (%q, %v), want (%q, true) — then the declared limit is false: %s",
					tc.value, form, ok, tc.form, tc.limit)
			}
		})
	}
}

// declaredFormList is the shape DeclaredLimit uses to enumerate its forms:
// "forms a, b, c" — lowercase names joined by ", ".
var declaredFormList = regexp.MustCompile(`forms ([a-z-]+(?:, [a-z-]+)*)`)

// TestDeclaredLimitStatesBothDirectionsForEveryForm locks the served limit text
// a provider manifest cites by reference. It must name every form the
// predicate vetoes, the decision that owns it, and both costs: what it hides
// and what it leaves firing. It names no provider, because more than one
// provider cites it.
func TestDeclaredLimitStatesBothDirectionsForEveryForm(t *testing.T) {
	if DeclaredLimit == "" {
		t.Fatal("DeclaredLimit is empty: every manifest that cites it would serve no limit")
	}
	// Each form must be named AS ITSELF, in the text's own form list. A plain
	// substring check is satisfied by prose that merely contains the letters —
	// "urn" by "a urn: URN", "url" by "a URL" — so a form dropped from the list
	// would still pass. The list is parsed and compared as a set, both ways.
	lists := declaredFormList.FindAllStringSubmatch(DeclaredLimit, -1)
	if len(lists) != 1 {
		t.Fatalf("DeclaredLimit carries %d form lists (\"forms a, b, …\"), want exactly 1:\n%s", len(lists), DeclaredLimit)
	}
	declared := make(map[string]bool)
	for _, name := range strings.Split(lists[0][1], ", ") {
		declared[name] = true
	}
	for _, form := range Forms() {
		if !declared[string(form)] {
			t.Errorf("DeclaredLimit's form list does not name %q, which NotACredential vetoes: %v", form, declared)
		}
		delete(declared, string(form))
	}
	for extra := range declared {
		t.Errorf("DeclaredLimit's form list names %q, which Forms() does not declare", extra)
	}
	for _, marker := range []string{"ADR 0086", "internal/core/valueshape", "is not reported at all",
		"NOT REPORTED", "STILL REPORTED", "16 bytes", "Hunter2024.db", "changeme"} {
		if !strings.Contains(DeclaredLimit, marker) {
			t.Errorf("DeclaredLimit is missing %q:\n%s", marker, DeclaredLimit)
		}
	}
	for _, provider := range []string{"(go)", "(typescript)"} {
		if strings.Contains(DeclaredLimit, provider) {
			t.Errorf("DeclaredLimit names a provider %q; each manifest says who applies it", provider)
		}
	}
}
