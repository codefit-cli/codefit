package valueshape

import "testing"

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
		{
			value:  `cache:key.json`,
			reason: "':' is outside the file-path charset even without '//' — the charset guard, not just the scheme-and-authority check, must reject it despite the valid .json extension",
		},
		{
			value:  `secret.jsonx`,
			reason: "the closed extension list is anchored to the TRUE trailing extension — 'jsonx' is not itself listed, even though it contains '.json' as a substring",
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
