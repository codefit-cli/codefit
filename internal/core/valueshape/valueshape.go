package valueshape

import (
	"net/netip"
	"net/url"
	"regexp"
	"strings"

	"github.com/codefit-cli/codefit/internal/core/namematch"
)

// Form names the shape a string literal proved it has.
type Form string

const (
	FormURL        Form = "url"
	FormURN        Form = "urn"
	FormEnvVarName Form = "env-var-name"
	FormFilePath   Form = "file-path"
)

// Forms returns every Form, in a fixed order (for exhaustiveness checks and
// parity tables). The order matches the order NotACredential checks guards
// in, which is itself load-bearing: see the doc comment there.
func Forms() []Form {
	return []Form{FormURL, FormURN, FormEnvVarName, FormFilePath}
}

// DeclaredLimit is the served text of what the value veto does and does not
// see, in both directions, for a provider manifest to cite BY REFERENCE instead
// of restating it by hand. It names no provider: each manifest says who applies
// the veto and how its literal reaches NotACredential, then appends this.
//
// Both providers cite it: the TypeScript manifest (ts.hardcoded-secrets in
// internal/providers/typescript/coverage.go) and the Go capability
// (limitValueVeto in internal/providers/golang/security.go). Neither keeps a
// copy of its own, so there is one text to keep true. Every statement here is
// pinned through the real providers by their valueveto tests, the four
// placeholders included.
const DeclaredLimit = "SEC-001 stays silent when the string literal's own shape proves it is " +
	"not a credential (ADR 0086, internal/core/valueshape; forms url, urn, env-var-name, " +
	"file-path): a URL with a scheme and a host, no userinfo, no credential-named query or " +
	"fragment parameter carrying a value, and nothing token-like (16+ bytes with a letter and " +
	"a digit) in any host label (an IP-literal host is skipped whole, never read as labels), " +
	"decoded path segment, query key or value, or fragment key or value (each position is " +
	"split before it is decoded, so an encoded slash, ampersand or equals sign cannot cut a " +
	"token, and a fragment that is not key=value is read whole); a urn: URN; a SCREAMING_SNAKE " +
	"name with at least one underscore; a path ending in a known file extension. Such a " +
	"literal is not reported at all. " +
	"Declared, not fixed, in both directions. NOT REPORTED although it may be a secret: a " +
	"value that matches the environment-variable-name pattern (an ASCII upper-case letter " +
	"first, then ASCII upper-case letters and digits, at least one underscore, none leading, trailing or " +
	"doubled), whatever it holds; a bare file name with a known extension and no " +
	"separator (Hunter2024.db); and a URL whose embedded token is shorter than 16 bytes, lacks " +
	"either a letter or a digit, or is cut by the URL's own literal separators (dots between " +
	"host labels, slashes between path segments, & or = in a query or fragment) into pieces " +
	"each shorter than 16 bytes. STILL REPORTED although it is often not a secret, because its " +
	"shape is indistinguishable from one: short test literals, short identifiers, prose, paths " +
	"without a known extension, header names, and placeholders that do not match that " +
	"pattern: changeme, <token>, TODO, __SECRET__."

// urlSchemePattern is the RFC 3986 scheme grammar, checked BEFORE handing the
// value to url.Parse. url.Parse alone accepts far too much — a bare relative
// path such as "lib/config.ts" parses without error and with no scheme — so
// this gate is what makes FormURL mean "this value carries a scheme", not
// merely "url.Parse didn't complain".
var urlSchemePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`)

// urnPattern is RFC 8141's `urn:<NID>:<NSS>` shape, case-insensitive: the
// namespace identifier is 1-32 characters of [a-z0-9-] starting with an
// alphanumeric, and the namespace-specific string is anything non-whitespace.
var urnPattern = regexp.MustCompile(`(?i)^urn:[a-z0-9][a-z0-9-]{0,31}:\S+$`)

// envVarNamePattern requires at least one underscore, on purpose: it is what
// keeps an AWS access key ID and base32 TOTP secrets like
// `JBSWY3DPEHPK3PXP` firing. Both are SCREAMING-CASE-shaped, and both are
// real secrets this project has measured — see doc.go.
var envVarNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9]*(_[A-Z0-9]+)+$`)

// tokenLikeMinLength is the length threshold isTokenLike anchors on. It is
// pinned to the byte, from both sides, by two rows that sit exactly on the
// boundary:
//
//   - Below it: the VETOED row `https://ec2-12-34-56-78.compute-1.amazonaws.com/`
//     in TestNotACredential_Vetoed, whose longest host label is 15 bytes. At 15
//     that row starts firing.
//   - At it: the keeps-firing row `https://img.example.com/s/qL8%2Fz3%2BxW9v%2Fk2N%3D/photo.jpg`
//     in TestNotACredential_KeepsFiring (and in the Go provider's
//     TestSEC001ValueVetoKeepsFiring), whose decoded path segment is a
//     16-byte base64 token. At 17 that row is vetoed.
//
// Every other row that depends on the threshold sits further from it, so
// moving the constant by more than one byte fails more rows; how many depends
// on the tables, not on this comment, and is not counted here.
const tokenLikeMinLength = 16

// isTokenLike reports whether s has the shape of an opaque secret embedded in
// a URL: at least tokenLikeMinLength bytes, containing at least one ASCII
// letter AND at least one ASCII digit. Both are required, and each on its own
// is measured to be too permissive: a long letters-only path segment such as
// "openid-configuration" is prose, not a secret (it must NOT trip this), and
// a long all-digit segment such as a numeric webhook or user id is an
// identifier, not a secret (it must NOT trip this either). Only their
// conjunction — the shape of a real generated token — trips it.
func isTokenLike(s string) bool {
	if len(s) < tokenLikeMinLength {
		return false
	}
	var hasLetter, hasDigit bool
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z':
			hasLetter = true
		case c >= '0' && c <= '9':
			hasDigit = true
		}
		if hasLetter && hasDigit {
			return true
		}
	}
	return false
}

// carriesTokenLike reports whether u — a URL that has already passed every
// other FormURL guard — has the secret itself sitting in plain sight: any
// HOST LABEL, any PATH SEGMENT, any QUERY KEY or VALUE, or any FRAGMENT KEY or
// VALUE that is token-like. This is what a credential-named-key check alone
// cannot see: webhook, presigned, SAS, bot-token, function and feed URLs each
// carry the secret in a position whose KEY (or lack of one) namematch's
// credential vocabulary was never meant to recognise. doc.go states, position
// by position, what is read and what is not.
//
// Every position starts from its ESCAPED form, so it is split on its literal
// separators first and decoded once per piece: an encoded separator stays
// inside its piece and cannot cut a token. The fragment is read through
// u.EscapedFragment(), not u.Fragment, for exactly that reason — u.Fragment is
// already decoded, and url.ParseQuery would decode and split it a second time.
func carriesTokenLike(u *url.URL) bool {
	return hostHasTokenLikeLabel(u.Hostname()) ||
		pathHasTokenLikeSegment(u.EscapedPath()) ||
		queryHasTokenLike(u.RawQuery) ||
		queryHasTokenLike(u.EscapedFragment())
}

// hostHasTokenLikeLabel reports whether any DNS label of host (already
// stripped of its port and brackets by url.URL.Hostname) is token-like. An IP
// literal is not a name and is skipped whole: an IPv6 address is long and
// mixes hex letters and digits without being a secret.
func hostHasTokenLikeLabel(host string) bool {
	if _, err := netip.ParseAddr(host); err == nil {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if isTokenLike(label) {
			return true
		}
	}
	return false
}

// pathHasTokenLikeSegment reports whether any segment of escapedPath is
// token-like once decoded. It splits BEFORE decoding, so an encoded "/" (%2F)
// stays inside its segment and cannot cut a token into pieces shorter than
// tokenLikeMinLength. A segment that does not decode is read as written.
//
// Every piece the decoded path splits into is a substring of one decoded
// segment, so this sees everything a split of the decoded path saw.
func pathHasTokenLikeSegment(escapedPath string) bool {
	for _, segment := range strings.Split(escapedPath, "/") {
		decoded, err := url.PathUnescape(segment)
		if err != nil {
			decoded = segment
		}
		if isTokenLike(decoded) {
			return true
		}
	}
	return false
}

// queryHasTokenLike reports whether any DECODED key or value in raw — an
// ESCAPED query string or fragment, parsed as a query string, which is also
// how a URL fragment is read — is token-like. raw must not be decoded already:
// url.ParseQuery splits on the literal "&" and "=" and then decodes each piece,
// and a pre-decoded string would turn an encoded separator into a real one.
//
// Keys are read as well as values. A bare key (`?<token>`) and a fragment that
// is not a list of pairs (`#/invite/<token>`, `#<token>`) both parse as one key
// with an empty value, and are read WHOLE: any piece of them that is token-like
// makes the whole token-like. A key that carries a value is read too, because
// nothing stops a token from sitting there, and the cost is noise, not
// blindness.
//
// A raw string that fails to parse is reported as carrying a token-like part:
// what this function cannot read is not proof that no token is there, so it
// answers in the direction that does not veto. It answers so on its own.
// carriesCredential makes the same choice for its own input, but it reads the
// DECODED fragment where this reads the escaped one, so neither function's
// answer may rest on the other's.
func queryHasTokenLike(raw string) bool {
	if raw == "" {
		return false
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return true
	}
	for key, vals := range values {
		if isTokenLike(key) {
			return true
		}
		for _, v := range vals {
			if isTokenLike(v) {
				return true
			}
		}
	}
	return false
}

// filePathCharset is deliberately narrower than "printable ASCII": it exists
// to keep base64 ('+', '/', '=') and JWT/STS tokens off this guard. '/' alone
// is admitted because it is also the ordinary path separator, but '+' and '='
// are not, and that asymmetry is what lets a file path and a base64 blob share
// '/' without sharing a verdict.
var filePathCharset = regexp.MustCompile(`^[A-Za-z0-9_./~@-]+$`)

// filePathExtensions is the CLOSED list FormFilePath anchors on. It exists so
// that a long dot-separated blob — a JWT is three base64url segments joined by
// dots — cannot be mistaken for a file path merely for containing a dot: the
// segment after the last dot must be a real, known extension, not merely
// "short enough to look like one".
var filePathExtensions = map[string]bool{
	"ts": true, "tsx": true, "js": true, "jsx": true, "mjs": true, "cjs": true,
	"go": true, "py": true, "rb": true, "java": true, "kt": true, "php": true,
	"cs": true, "rs": true, "swift": true,
	"json": true, "yaml": true, "yml": true, "toml": true, "ini": true,
	"conf": true, "cfg": true, "env": true, "xml": true,
	"html": true, "htm": true, "css": true, "scss": true, "md": true, "txt": true,
	"csv": true, "sql": true, "db": true, "sqlite": true, "log": true, "lock": true,
	"pem": true, "key": true, "crt": true, "cer": true, "der": true,
	"p12": true, "pfx": true, "jks": true, "keystore": true, "pub": true,
}

// NotACredential reports whether value's OWN shape proves it is something
// other than a credential, and which Form proved it. value is the literal's
// content WITHOUT quotes.
//
// This is a veto, not a classifier: it fires only when the shape is positive
// proof of something else, never on mere suspicion. ("", false) means "the
// value establishes nothing" — the caller (SEC-001, in both providers) keeps
// affirming exactly as it did before this package existed. When a guard's
// evidence is ambiguous — a query string or URL fragment that fails to parse
// — it resolves to NOT vetoing: a blind form that silences a real credential
// is worse than one more line of noise the developer has to read, which is
// this project's standing rule (see CLAUDE.md, "TDD" preamble on mutation, and
// ADR 0075's Arm B post-mortem on the cost of a false affirmation flowing the
// other way).
//
// Guards run in this fixed order and the first that proves wins:
//
//  1. FormURL — value has a scheme (`^[A-Za-z][A-Za-z0-9+.-]*://`),
//     url.Parse succeeds with a non-empty Host, carries no userinfo
//     (`user:pass@` is itself a credential), neither its query string nor its
//     URL fragment (parsed the same way, since OAuth's implicit flow puts the
//     token there) has a key matching namematch's credential vocabulary with a
//     NON-EMPTY value, AND no HOST LABEL (IP literals skipped), PATH SEGMENT,
//     QUERY KEY or VALUE, or FRAGMENT KEY or VALUE is token-like (see
//     isTokenLike; every position is split before it is decoded, so an
//     encoded "/", "&" or "=" cannot cut a token):
//     a webhook, presigned, function or feed URL whose secret sits in the
//     host, the path, a bare query key, a hash route or a query value whose
//     key namematch does not recognise must still be seen.
//  2. FormURN — `urn:<namespace-id>:<namespace-specific-string>`,
//     case-insensitive.
//  3. FormEnvVarName — SCREAMING_SNAKE, requiring at least one underscore.
//  4. FormFilePath — every character in a narrow charset, no "://" anywhere
//     in the value, and a trailing extension from a closed list.
//
// WHAT THIS DOES NOT SEE. This is a deliberately partial veto, not a
// classifier for "everything that is not a credential" — see doc.go for the
// full census this list is measured against, and CLAUDE.md's census/negative-
// fixture doctrine for why an undeclared gap here would be worse than a
// declared one:
//
//   - Short test literals and short identifiers (`test_client_secret`,
//     `progressToken`) are, BY SHAPE, indistinguishable from a real short
//     secret such as `s3cr3t` — which ADR 0075 requires to keep firing, since
//     it has no length floor. Nothing in this package infers "test" from a
//     name; that stays the caller's job, if it does it at all.
//   - Prose (values containing whitespace) does not veto. It did, briefly:
//     the scope this package implements is the version AFTER that guard was
//     dropped 2026-09-28, because "correct horse battery staple" is a real
//     passphrase and this project would rather keep firing on
//     "master secret" (measured noise: 4/320 Go findings, 2/52 TypeScript)
//     than silence one real passphrase.
//   - A file path with no extension (`lib/config`, `/etc/secrets/db`) does not
//     veto — FormFilePath anchors on a closed extension list precisely so it
//     never has to guess from bare structure alone.
//   - Header names (`X-Api-Key`) do not veto: mixed case fails
//     FormEnvVarName, and no extension fails FormFilePath.
//   - Placeholders (`YOUR_CLIENT_SECRET`) DO veto here, as an env-var name —
//     see the census in doc.go for why that is a measured, not a
//     hypothetical, bucket. A placeholder that does not match envVarNamePattern
//     (an ASCII upper-case letter first, at least one underscore, none
//     leading, trailing or doubled) does not veto: `changeme`, `<token>`, `TODO`, and
//     `__SECRET__`, whose underscores are all leading or trailing.
//   - A secret that matches envVarNamePattern (an ASCII upper-case letter
//     first, then ASCII upper-case letters and digits, at least one underscore, none
//     leading, trailing or doubled) IS vetoed by FormEnvVarName. That is a declared
//     limit, not an oversight: an AWS access key ID (no underscore) and
//     `JBSWY3DPEHPK3PXP` (base32, no underscore) both keep firing precisely
//     because they lack the underscore this guard requires.
//   - A secret-bearing URL whose token part is SHORTER than
//     tokenLikeMinLength (16 bytes), lacks an ASCII letter or an ASCII digit,
//     or is cut by the URL's own literal separators ("." between host labels,
//     "/" between path segments, "&" or "=" in a query or fragment) into
//     pieces each shorter than 16 bytes, IS still vetoed by FormURL — in any position,
//     the token-like guard has no evidence to act on. This is a declared
//     limit, not an oversight: it trades a rare short, single-class or
//     pre-cut secret for never mistaking a long identifier (a numeric id) or
//     a long descriptive name (`openid-configuration`) for one.
//   - A bare file name with a listed extension and no separator
//     (`Hunter2024.db`) IS vetoed by FormFilePath: a secret that ends in a
//     dot plus a known extension has the shape of a file name.
func NotACredential(value string) (Form, bool) {
	if value == "" {
		return "", false
	}
	if isURL(value) {
		return FormURL, true
	}
	if urnPattern.MatchString(value) {
		return FormURN, true
	}
	if envVarNamePattern.MatchString(value) {
		return FormEnvVarName, true
	}
	if isFilePath(value) {
		return FormFilePath, true
	}
	return "", false
}

// isURL applies every FormURL guard described in NotACredential's doc
// comment.
func isURL(value string) bool {
	if !urlSchemePattern.MatchString(value) {
		return false
	}
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || u.User != nil {
		return false
	}
	if carriesCredential(u.RawQuery) || carriesCredential(u.Fragment) {
		return false
	}
	if carriesTokenLike(u) {
		return false
	}
	return true
}

// carriesCredential reports whether raw — a URL query string, or a URL
// fragment parsed the same way, since OAuth's implicit flow places the token
// there instead — has a key matching namematch's credential vocabulary with a
// non-empty value.
//
// A raw string that fails to parse as a query is treated as carrying a
// credential, on purpose: NotACredential's "when in doubt, do not veto" rule
// means an ambiguous fragment or query must NOT be read as proof of
// innocence.
func carriesCredential(raw string) bool {
	if raw == "" {
		return false
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return true
	}
	for key, vals := range values {
		if _, ok := namematch.MatchSet(key, namematch.Credential()); !ok {
			continue
		}
		for _, v := range vals {
			if v != "" {
				return true
			}
		}
	}
	return false
}

// isFilePath applies every FormFilePath guard described in NotACredential's
// doc comment.
func isFilePath(value string) bool {
	if strings.Contains(value, "://") {
		return false
	}
	if !filePathCharset.MatchString(value) {
		return false
	}
	ext, ok := trailingExtension(value)
	if !ok {
		return false
	}
	return filePathExtensions[strings.ToLower(ext)]
}

// trailingExtension returns the substring after the LAST '.' in value. It
// reports false when value has no '.', or when the '.' is the final
// character (nothing follows it to be an extension).
func trailingExtension(value string) (string, bool) {
	idx := strings.LastIndex(value, ".")
	if idx == -1 || idx == len(value)-1 {
		return "", false
	}
	return value[idx+1:], true
}
