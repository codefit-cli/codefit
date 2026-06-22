package typescript_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// analyze runs the provider's real AnalyzeSecurity (loads the embedded rules,
// compiles them, matches the source) — the full chain a contributor exercises.
func analyze(t *testing.T, path, content string) []findings.Finding {
	t.Helper()
	got, err := typescript.New().AnalyzeSecurity(providers.SourceFile{Path: path, Content: []byte(content)})
	if err != nil {
		t.Fatalf("AnalyzeSecurity(%s): %v", path, err)
	}
	return got
}

// hasRule reports whether any finding carries the given rule id.
func hasRule(finds []findings.Finding, id string) bool {
	for _, f := range finds {
		if f.ID == id {
			return true
		}
	}
	return false
}

// Each security category is tested on BOTH sides of its contract: it must fire
// on the vulnerable fixture AND stay silent on the clean one. The clean side is
// where < 1% false positives is won or lost.

func TestSecurity_WeakCrypto(t *testing.T) {
	vuln := analyze(t, "crypto_vuln.ts", `
import { createHash } from "crypto";
function fingerprint(data: string) {
  const a = md5(data);
  const b = sha1(data);
  const c = createHash("md5").update(data).digest("hex");
  return a + b + c;
}
`)
	if !hasRule(vuln, "SEC-052") {
		t.Errorf("weak crypto (md5/sha1/createHash('md5')) must fire SEC-052, got %+v", vuln)
	}

	if !hasRule(vuln, "SEC-053") {
		t.Errorf("createHash('md5') must fire SEC-053, got %+v", vuln)
	}

	clean := analyze(t, "crypto_clean.ts", `
import { createHash } from "crypto";
function fingerprint(data: string) {
  const a = sha256(data);
  const b = createHash("sha256").update(data).digest("hex");
  return a + b;
}
`)
	if hasRule(clean, "SEC-052") || hasRule(clean, "SEC-053") {
		t.Errorf("sha256 / createHash('sha256') is strong and must NOT fire weak-crypto rules, got %+v", clean)
	}
}

func TestSecurity_HardcodedSecret(t *testing.T) {
	// A credential-named variable assigned a static string literal is a hardcoded
	// secret — conclusive over the declarator subtree (ADR 0004).
	vuln := analyze(t, "secret_vuln.ts", `
const apiKey = "sk-live-abc123def456";
const password = 'hunter2';
const authToken = "Bearer eyJhbGciOi";
`)
	if !hasRule(vuln, "SEC-001") {
		t.Errorf("credential-named var assigned a string literal must fire SEC-001, got %+v", vuln)
	}

	// The four classic false positives of secret detection, all of which MUST
	// stay silent — this is what separates a usable rule from a noisy one:
	clean := analyze(t, "secret_clean.ts", `
const apiKey = getApiKey();                       // value is a call, not a literal
const secret = "";                                 // empty string, nothing to leak
const token = process.env.TOKEN;                   // value is a member access
const userName = "bob";                            // name is not credential-like
const password = `+"`${process.env.PASSWORD}`"+`;   // template literal: dynamic, not hardcoded
`)
	if hasRule(clean, "SEC-001") {
		t.Errorf("none of the clean secret cases (call, empty, member, non-credential name, interpolated template) may fire SEC-001, got %+v", clean)
	}
}

func TestSecurity_InsecureRandom(t *testing.T) {
	// Math.random() is not cryptographically secure; using it for a security
	// value (token, nonce, salt…) is the vulnerability. The variable name is the
	// local signal that the value is security-sensitive (ADR 0004: conclusive
	// over the declarator subtree, no need to follow data elsewhere).
	vuln := analyze(t, "rng_vuln.ts", `
const sessionToken = Math.random();
const csrfToken = Math.random().toString(36);
`)
	if !hasRule(vuln, "SEC-058") {
		t.Errorf("Math.random() assigned to a security-named var must fire SEC-058, got %+v", vuln)
	}

	// A non-security use of Math.random() (a ratio, an animation, a sample) must
	// stay silent — this is where naive RNG rules become noise.
	clean := analyze(t, "rng_clean.ts", `
const ratio = Math.random();
const jitter = Math.random() * 100;
const sampleIndex = Math.floor(Math.random() * items.length);
`)
	if hasRule(clean, "SEC-058") {
		t.Errorf("non-security Math.random() must NOT fire SEC-058, got %+v", clean)
	}
}
