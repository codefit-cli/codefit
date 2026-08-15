package golang_test

import (
	"strings"
	"testing"
)

// Every fixture here is REAL Go source text driven through the provider's real
// go/ast parser. None is a hand-built *ast struct: a hand-built fixture locks a
// reality the production path cannot produce, which is how a test stays green
// while protecting nothing.

// TestSEC001DelimiterSeparatedCredential is the case that makes the arm
// deletion safe rather than a regression.
//
// Arm A did a raw substring match for "apikey". lower("API_KEY") is "api_key",
// which does NOT contain "apikey" — so before this change API_KEY fired ONLY
// through the bare-"key"-plus-long-value arm that also reported enum constants.
// Deleting that arm without adjacent-pair joining would have bought the
// false-positive fix with a real, undeclared false negative.
func TestSEC001DelimiterSeparatedCredential(t *testing.T) {
	for _, src := range []string{
		"package p\n\nconst API_KEY = \"sk-live-abcdefghijklmno\"\n",
		"package p\n\nfunc f() { api_key := \"sk-live-abcdefghijklmno\"; _ = api_key }\n",
		"package p\n\nfunc f() { apiKey := \"super-secret-value-123\"; _ = apiKey }\n",
	} {
		if !hasID(analyzeSec(t, "x.go", src), "SEC-001") {
			t.Errorf("SEC-001 not emitted for:\n%s", src)
		}
	}
}

// TestSEC001EnumConstantIsQuiet is the false affirmation this change removes.
//
// The fixture is the MEASURED shape, not a lookalike: a grouped const (…) block
// declaring a named non-builtin type, copied in form from the two production
// sites the AST census found (internal/core/surface/surface.go and
// internal/core/paradigm/schemagate.go). Both fired at Confidence 1.0 saying
// "looks like a hardcoded credential" about a category name.
//
// The length guard that was supposed to prevent this was inverted in practice:
// these values are past 16 bytes BECAUSE they are descriptive, while a real
// credential has no length floor at all.
func TestSEC001EnumConstantIsQuiet(t *testing.T) {
	const src = `package surface

// Category is a named non-builtin type, exactly as at the measured sites.
type Category string

const (
	CategoryAuthz                     Category = "authz"
	CategoryDWDimensionNoSurrogateKey Category = "dw-dimension-no-surrogate-key"
	CategoryDWFactMissingForeignKey   Category = "dw-fact-missing-foreign-key"
)
`
	if fs := analyzeSec(t, "internal/core/surface/surface.go", src); hasID(fs, "SEC-001") {
		t.Errorf("SEC-001 emitted for a category enum — the affirmation is false: %+v", fs)
	}
}

// TestSEC001SignalNamesEnumIsQuiet is the second measured production site: a
// grouped var block of descriptive names, not an enum of a named type. Both
// shapes are pinned because they failed for the same reason but do not look
// alike, and a fixture that resembles only one of them would leave the other
// unprotected.
func TestSEC001SignalNamesEnumIsQuiet(t *testing.T) {
	const src = `package paradigm

const (
	SignalSurrogateKeyNames = "surrogate-key-names-present"
	SignalNaturalKeyOnly    = "natural-key-only-detected"
)
`
	if fs := analyzeSec(t, "internal/core/paradigm/schemagate.go", src); hasID(fs, "SEC-001") {
		t.Errorf("SEC-001 emitted for a signal-name constant: %+v", fs)
	}
}

// TestSEC001CoverageGoLacked pins the names that fire only because the
// vocabulary replaced the substring arms. None of them matched Arm A: "pwd",
// "privatekey", "refreshtoken" and "accesstoken" were not in the old set, and
// the old bare-"key" arm caught privateKey only by accident of value length.
func TestSEC001CoverageGoLacked(t *testing.T) {
	cases := map[string]string{
		"privateKey":    "package p\n\nfunc f() { privateKey := \"-----BEGIN RSA-----\"; _ = privateKey }\n",
		"pwd":           "package p\n\nfunc f() { pwd := \"hunter2\"; _ = pwd }\n",
		"refreshToken":  "package p\n\nfunc f() { refreshToken := \"rt_abc\"; _ = refreshToken }\n",
		"accessToken":   "package p\n\nfunc f() { accessToken := \"at_abc\"; _ = accessToken }\n",
		"accessKey":     "package p\n\nfunc f() { accessKey := \"AKIAIOSFODNN7\"; _ = accessKey }\n",
		"SIGNING_KEY":   "package p\n\nconst SIGNING_KEY = \"s3cr3t\"\n",
		"encryptionKey": "package p\n\nfunc f() { encryptionKey := \"ek_abc\"; _ = encryptionKey }\n",
	}
	for name, src := range cases {
		if !hasID(analyzeSec(t, "x.go", src), "SEC-001") {
			t.Errorf("SEC-001 not emitted for %s — a credential spelling was lost", name)
		}
	}
}

// TestSEC001ShortValueStillFires is the true-positive half of the inverted
// guard. The deleted arm required 16+ bytes of value, so an 8-character
// password in a bare-"key"-named variable was REJECTED while a 29-byte
// descriptive category name was accepted. Value length is no longer consulted
// at all.
func TestSEC001ShortValueStillFires(t *testing.T) {
	const src = "package p\n\nfunc f() { apiKey := \"abc\"; _ = apiKey }\n"
	if !hasID(analyzeSec(t, "x.go", src), "SEC-001") {
		t.Error("SEC-001 not emitted for a short credential value — value length is not a credential property")
	}
}

// TestSEC001SubstringArtifactsAreQuiet pins the names that fired only because
// the old matcher used strings.Contains.
func TestSEC001SubstringArtifactsAreQuiet(t *testing.T) {
	cases := map[string]string{
		"tokenizer": "package p\n\nfunc f() { tokenizer := \"some-long-value-here\"; _ = tokenizer }\n",
		"keyboard":  "package p\n\nfunc f() { keyboard := \"some-long-value-here\"; _ = keyboard }\n",
		"monkeyId":  "package p\n\nfunc f() { monkeyId := \"some-long-value-here\"; _ = monkeyId }\n",
		"textKey":   "package p\n\nfunc f() { textKey := \"some-long-value-here\"; _ = textKey }\n",
	}
	for name, src := range cases {
		if fs := analyzeSec(t, "x.go", src); hasID(fs, "SEC-001") {
			t.Errorf("SEC-001 emitted for %s — a substring artifact, not a credential: %+v", name, fs)
		}
	}
}

// TestSEC001PIIIsNotACredential holds the vocabulary split at the provider
// boundary. DB-053 treats ssn/cvv as sensitive and should; SEC-001 says "this
// looks like a hardcoded CREDENTIAL", which an SSN is not.
func TestSEC001PIIIsNotACredential(t *testing.T) {
	for _, src := range []string{
		"package p\n\nfunc f() { ssn := \"123-45-6789\"; _ = ssn }\n",
		"package p\n\nfunc f() { creditCard := \"4111111111111111\"; _ = creditCard }\n",
	} {
		if fs := analyzeSec(t, "x.go", src); hasID(fs, "SEC-001") {
			t.Errorf("SEC-001 emitted for PII — SEC-001 affirms credentials only: %+v", fs)
		}
	}
}

// TestSEC001StructLiteralStillFires keeps the *ast.KeyValueExpr path alive.
// This shape is the reason the census had to be an AST enumeration: no static
// probe can see a composite-literal field as a name/value pair.
func TestSEC001StructLiteralStillFires(t *testing.T) {
	const src = `package p

type Cfg struct{ Password string }

func f() Cfg { return Cfg{Password: "hunter2hunter2"} }
`
	f := firstWithID(t, analyzeSec(t, "x.go", src), "SEC-001")
	if !strings.Contains(f.Description, "Password") {
		t.Errorf("finding does not name the field: %q", f.Description)
	}
}

// TestSEC001MultiNameValueSpecStillFires covers the other shape a static probe
// cannot enumerate: several names declared in one *ast.ValueSpec.
func TestSEC001MultiNameValueSpecStillFires(t *testing.T) {
	const src = "package p\n\nvar host, apiKey = \"localhost\", \"sk-live-abcdefghij\"\n"
	f := firstWithID(t, analyzeSec(t, "x.go", src), "SEC-001")
	if !strings.Contains(f.Description, "apiKey") {
		t.Errorf("the wrong name was reported: %q", f.Description)
	}
}

// TestSEC001LowercaseConcatenationIsTheDeclaredLimit locks the gap so it is a
// LIMIT and not a belief. Component matching cannot split secretkey, because
// there is no boundary in it to split on. The delimited spellings must still
// fire, or the limit would be describing something much larger than it claims.
func TestSEC001LowercaseConcatenationIsTheDeclaredLimit(t *testing.T) {
	const gap = "package p\n\nfunc f() { secretkey := \"abcdefghijklmnopqrst\"; _ = secretkey }\n"
	if fs := analyzeSec(t, "x.go", gap); hasID(fs, "SEC-001") {
		t.Errorf("secretkey fired — then the limit declared by codefit-coverage is false: %+v", fs)
	}
	for _, src := range []string{
		"package p\n\nfunc f() { secretKey := \"abcdefghijklmnopqrst\"; _ = secretKey }\n",
		"package p\n\nfunc f() { secret_key := \"abcdefghijklmnopqrst\"; _ = secret_key }\n",
		"package p\n\nconst SECRET_KEY = \"abcdefghijklmnopqrst\"\n",
	} {
		if !hasID(analyzeSec(t, "x.go", src), "SEC-001") {
			t.Errorf("a DELIMITED spelling went quiet — the declared limit covers the concatenated spelling only:\n%s", src)
		}
	}
}

// --- SEC-050 -------------------------------------------------------------

// TestSEC050SubstringArtifactIsQuiet is SEC-050's version of the same defect.
// Its name gate had no guard of any kind — not even the length one — so a bare
// "key" substring made monkeyIndex a "security value".
func TestSEC050SubstringArtifactIsQuiet(t *testing.T) {
	const src = `package p

import "math/rand"

func f() int {
	monkeyIndex := rand.Intn(10)
	return monkeyIndex
}
`
	if fs := analyzeSec(t, "x.go", src); hasID(fs, "SEC-050") {
		t.Errorf("SEC-050 emitted for monkeyIndex — \"key\" is a substring there, never a component: %+v", fs)
	}
}

// TestSEC050CryptoMaterialStillFires is the true-positive direction. These
// names are SEC-050's OWN vocabulary: crypto material, not credentials. If
// SEC-050 had been pointed at the credential set instead, nonce/salt/iv would
// have gone silent — a regression disguised as consolidation.
func TestSEC050CryptoMaterialStillFires(t *testing.T) {
	cases := map[string]string{
		"sessionToken": "sessionToken := rand.Int63()",
		"nonce":        "nonce := rand.Int63()",
		"salt":         "salt := rand.Int63()",
		"iv":           "iv := rand.Int63()",
		"sessionId":    "sessionId := rand.Int63()",
		"apiKey":       "apiKey := rand.Int63()",
	}
	for name, stmt := range cases {
		src := "package p\n\nimport \"math/rand\"\n\nfunc f() { " + stmt + "; _ = " + name + " }\n"
		if !hasID(analyzeSec(t, "x.go", src), "SEC-050") {
			t.Errorf("SEC-050 not emitted for %s — crypto material lost its guard", name)
		}
	}
}

// TestSEC050BareKeyComponentStillFires records a deliberate difference from
// SEC-001. A bare "key" COMPONENT is admissible in SEC-050's vocabulary and is
// not in SEC-001's, because SEC-050 additionally requires the value to come
// from a math/rand call — a second condition SEC-001 has no equivalent of.
func TestSEC050BareKeyComponentStillFires(t *testing.T) {
	const src = `package p

import "math/rand"

func f() { sessionKey := rand.Int63(); _ = sessionKey }
`
	if !hasID(analyzeSec(t, "x.go", src), "SEC-050") {
		t.Error("SEC-050 not emitted for sessionKey — a bare key component is in SEC-050's own vocabulary")
	}
}

// TestSEC050IgnoresNonRandomSource keeps the second condition honest: the name
// gate alone must not emit anything.
func TestSEC050IgnoresNonRandomSource(t *testing.T) {
	const src = "package p\n\nfunc f() { sessionToken := 42; _ = sessionToken }\n"
	if fs := analyzeSec(t, "x.go", src); hasID(fs, "SEC-050") {
		t.Errorf("SEC-050 emitted without a math/rand call: %+v", fs)
	}
}
