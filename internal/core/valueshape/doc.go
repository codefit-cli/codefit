// Package valueshape decides one question: does this string LITERAL's own
// shape prove it is something other than a credential?
//
// It is a leaf. It imports no codefit package except internal/core/namematch,
// and nothing outside the standard library, because both providers' SEC-001
// consume it (Go directly; TypeScript through a rule-engine operator, T4) and
// neither may pull the other in through it.
//
// # What it does
//
// SEC-001 (both providers) affirms "this looks like a hardcoded credential"
// at Confidence 1.0 once a variable's NAME establishes it (ADR 0075,
// internal/core/namematch): the value itself is checked for nothing beyond
// "non-empty string". That is too little. `const secretFile = "lib/config.ts"`
// is a real false affirmation, reproduced on main@76dbaec over MCP.
//
// NotACredential(value) closes the VALUE side of that gap, narrowly. It is a
// veto, never a classifier: given the un-quoted content of a string literal,
// it reports (Form, true) only when the shape is positive proof the value is
// a URL, a URN, an environment-variable name, or a file path — and ("", false)
// in every other case, meaning "this establishes nothing; the name gate's
// affirmation stands." The four Forms are enumerated by Forms(), in the fixed
// order NotACredential checks them: URL, URN, env-var-name, file-path — the
// first that proves wins, and the returned Form is itself the evidence that
// eventually reaches a finding's message.
//
// # Edge cases
//
// The empty string returns ("", false): it establishes nothing about its own
// shape. Every guard resolves an internal parse ambiguity — a URL whose
// fragment or query fails url.ParseQuery, most concretely — toward NOT
// vetoing, because a blind form is worse than one line of noise a developer
// has to read (see NotACredential's own doc comment for the guard-by-guard
// detail, and CLAUDE.md's TDD/mutation section for why "when in doubt, do not
// veto" is this project's standing rule rather than a one-off judgment call
// here).
//
// # What this does NOT see, and the census behind it
//
// Census, 2026-09-28, real sensor over MCP (not a static probe: SEC-001's
// name gate driven through the actual AST/regex path, because a hand-rolled
// enumeration cannot reproduce a tokenizer's real boundaries — the same
// discipline ADR 0075 already applied to itself).
//
// Go: 320 SEC-001 name-gate findings over the Go standard library (net/http,
// crypto, database/sql, cmd/go), golang.org/x/oauth2, golang.org/x/sys,
// modelcontextprotocol/go-sdk, gotreesitter, and codefit's own tree. Bucketed
// by the shape of the VALUE, not the finding's severity:
//
//	URL                              103
//	short test literal                65
//	URN                                46
//	short identifier                  36
//	SCREAMING_SNAKE env-var name       35
//	hex/base64 blob                    14
//	contains '/'                       10  (includes AWS's own published
//	                                        example secret access key — the
//	                                        reason no bare '/' veto exists
//	                                        anywhere in this package)
//	text with spaces                   4
//	header name                        2
//	placeholder                        2
//	path with extension                1
//	other                              2
//
// TypeScript: 52 SEC-001 name-gate findings over three real projects (1,545
// files total; project identities withheld per this repository's dogfood
// doctrine — counts and forms are publishable, locations are not). URL, URN,
// env-var-name, and path-with-extension: 0 occurrences. Text with spaces: 2.
// The remainder is short literals and identifiers, mostly under test paths.
//
// The bucket table above is a HUMAN categorization of the 320 values, made to
// decide which four buckets this package should close — it is not what
// NotACredential itself returns. Measured directly against the real
// predicate, over that same 320-value corpus, the VETOED-BY-FORM counts are:
//
//	FormURL           102
//	FormURN            46
//	FormEnvVarName     18
//	FormFilePath        1
//	-----------------------
//	total vetoed      167 / 320  (52.2%)
//
// The gap between a bucket count and its predicate count is expected and not
// a discrepancy to chase: the bucket table records what a human judged the
// VALUE to look like, while the predicate additionally enforces every guard
// in NotACredential's doc comment (no userinfo, no credential-named query
// key, at least one underscore, a closed extension list, and so on). Adding
// the token-like guard below moves the URL count from 102 to 100 (167 total
// to 165): the two URLs it demotes to keep-firing are occurrences of
// `https://login.live.com/oauth20_token.srf`, whose path segment
// `oauth20_token.srf` is itself token-like (18 characters, letters and a
// digit) despite being an OAuth endpoint NAME, not a secret. That is accepted
// noise, not a defect: this package has no way to distinguish "a token-shaped
// endpoint name" from "a token", and the alternative — narrowing the guard
// until it stops catching that — is exactly the kind of narrowing this
// package's own doctrine (see WHAT THIS DOES NOT SEE below) refuses to make
// without new measurement in the other direction.
//
// Four buckets above are exactly the four Forms this package closes: URL,
// URN, SCREAMING_SNAKE env-var name, path with extension. The rest are
// DELIBERATELY left firing, with the reason each was rejected as a guard:
//
//   - short test literal / short identifier (`test_client_secret`,
//     `progressToken`) — by SHAPE, indistinguishable from a real short secret
//     such as `s3cr3t`, which ADR 0075 requires to keep firing (no length
//     floor). This package has no notion of "test" or "identifier-like" to
//     tell them apart, and inventing one here would be exactly the kind of
//     speculative widening ADR 0075's own vocabulary work refused.
//   - hex/base64 blob, and specifically a JWT or an STS-style token — never
//     vetoed. A JWT's dots would pass FormFilePath's charset; what stops it
//     is the CLOSED extension list, which a base64url segment never matches.
//     '+', '/' and '=' — base64's own alphabet — are why FormFilePath's
//     charset excludes '+' and '=' outright, and why plain '/' alone (also
//     the path separator) was never treated as sufficient proof.
//   - contains '/' — never a bare veto condition on its own. The measured
//     reason is concrete: AWS's own published example secret access key,
//     `wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY`, contains '/' (and '+'). A
//     '/'-only guard would have silenced the exact string AWS publishes to
//     demonstrate what a real key looks like.
//   - text with spaces — a prose-veto guard was PROPOSED and DROPPED
//     2026-09-28, before it was ever implemented: a passphrase is a secret
//     WITH spaces ("correct horse battery staple"), and the measured yield
//     for vetoing prose was 4/320 Go findings plus 2/52 TypeScript findings —
//     noise this project accepts rather than risk silencing a real
//     passphrase. Prose keeps firing; `master secret` is the concrete,
//     catalogued example of the accepted noise.
//   - header name (`X-Api-Key`) — never vetoed: mixed case fails
//     FormEnvVarName's all-uppercase requirement, and no extension fails
//     FormFilePath.
//   - placeholder — a name like `YOUR_CLIENT_SECRET` IS vetoed here, but only
//     because it happens to also be SCREAMING_SNAKE-shaped; a placeholder
//     that is not (`<your-secret-here>`) is not covered by any guard in this
//     package and keeps firing.
//   - path with extension — the one Go bucket this package DOES close, and
//     the bucket that motivated this feature (`lib/config.ts`).
//   - "other" (Go: 2, uncategorized) — not investigated further; the census
//     is a sample against real code, not an exhaustive taxonomy, and a
//     residual "other" bucket this small does not justify a fifth guard
//     without its own measured evidence.
//
// One further declared limit, inside FormEnvVarName itself: a value made only
// of uppercase letters, digits and underscores, WITH at least one underscore,
// IS vetoed. `AKIAIOSFODNN7EXAMPLE` (AWS access key ID shape) and
// `JBSWY3DPEHPK3PXP` (base32 TOTP secret shape) both keep firing precisely
// because neither carries an underscore — the guard requires one specifically
// so it does not swallow either of those real secret shapes, and that
// trade-off is the boundary of what FormEnvVarName can prove, not an
// oversight to be tightened later without new measurement.
//
// # The URL token-like guard, and its own declared limit
//
// A URL that clears every other FormURL guard can still carry its secret in
// plain sight, in a position no credential-named-key check was ever meant to
// see: a Slack incoming-webhook URL, a Discord webhook URL, an S3 presigned
// URL, an Azure SAS URL, and a Telegram bot-token URL each embed the secret
// itself in a PATH SEGMENT or a QUERY/FRAGMENT VALUE whose KEY (`sig`,
// `X-Amz-Signature`, or no key at all for a bare path segment) namematch's
// credential vocabulary does not recognise. Measured directly (the same real
// predicate as above): all five of those shapes were VETOED as FormURL before
// this guard existed — a false affirmation this package would otherwise still
// be shipping.
//
// The guard: FormURL does NOT veto when any path segment (`u.Path` split on
// "/"), any decoded query value, or any decoded fragment value (both via
// url.ParseQuery, since a fragment is where OAuth's implicit flow puts a
// token) is token-like — see isTokenLike. Token-like requires ALL of: at
// least 16 bytes, at least one ASCII letter, AND at least one ASCII digit.
// Both letter and digit are required, deliberately: a value with only one of
// the two is a declared, measured limit, not an oversight —
//
//   - a long ALL-LETTER segment (`openid-configuration`, from
//     `https://accounts.google.com/.well-known/openid-configuration`) is a
//     descriptive endpoint name, not a secret, and must stay vetoed;
//   - a long ALL-DIGIT segment (`12345678901234567890`, a numeric id) is an
//     identifier, not a secret, and must stay vetoed;
//   - a segment carrying both letters and digits but SHORTER than 16 bytes
//     (`v2.0`, `token`, from
//     `https://login.microsoftonline.com/common/oauth2/v2.0/token`) is too
//     short to be distinguished from an ordinary path component, and must
//     stay vetoed.
//
// So the declared limit is exactly this: a secret-bearing URL whose token
// part is SHORTER than 16 bytes, or carries no ASCII digit at all, is still
// vetoed as FormURL — the guard has no shape to act on in either case. This
// trades a rare short-or-all-letter embedded secret for never mistaking a
// long identifier or a long descriptive path segment for one, mirroring the
// same trade-off FormEnvVarName already makes with its own underscore
// requirement, above.
package valueshape
