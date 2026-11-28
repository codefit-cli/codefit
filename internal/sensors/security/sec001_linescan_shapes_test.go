package security_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/golang"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
	"github.com/codefit-cli/codefit/internal/sensors/security"
)

// SEC-001 has two producers, and a statement about one is not a statement about
// SEC-001 (issue #148). The name-and-value rule lives in each provider and reads
// a credential-named binding to one string literal; the forms it does not read (a
// concatenation, a parenthesized literal, a conversion, a call, a fallback) are
// declared as "not read by the rule" in both coverage sources. The sensor's line
// scan (layer 1, apiKeyPatterns in security.go) is the other producer: it reports
// SEC-001 on any line that holds a value in one of its known API-key formats,
// whatever the code around it. So a known-format key inside one of those forms IS
// reported, while an ordinary secret in the same form is not. This test drives the
// real SENSOR, where both producers meet, and pins the two halves side by side.
//
// The key-shaped values are assembled at run time from pieces, so no committed
// line holds a complete key-shaped literal: the self-audit, GitHub push
// protection and any other scanner reading this file see only the fragments.

// lineScanKeys holds one value per apiKeyPatterns family, built from fragments
// that do not match on their own.
func lineScanKeys() map[string]string {
	return map[string]string{
		"anthropic":      "sk" + "-ant-" + strings.Repeat("Ab12", 3),
		"openai":         "sk" + "-" + strings.Repeat("Ab12", 6),
		"github pat":     "gh" + "p_" + strings.Repeat("Ab12", 6),
		"slack":          "xo" + "xb-" + strings.Repeat("ab12", 3),
		"aws access key": "AK" + "IA" + strings.Repeat("ABCD2345", 2),
	}
}

// plainValue is an ordinary short secret: the name-and-value rule fires on it
// as one literal, and it has no known API-key format.
const plainValue = "s3cr3t"

// lineScanCase is one form the name-and-value rule does not read. value builds
// the whole value expression around a quoted literal.
type lineScanCase struct {
	form  string
	value func(lit string) string
}

func quoted(s string) string { return `"` + s + `"` }

var goLineScanForms = []lineScanCase{
	{"concatenation", func(l string) string { return quoted(l) + ` + ""` }},
	{"parenthesized literal", func(l string) string { return "(" + quoted(l) + ")" }},
	{"conversion", func(l string) string { return "string(" + quoted(l) + ")" }},
	{"cmp.Or fallback", func(l string) string { return `cmp.Or(os.Getenv("SIGNING_KEY"), ` + quoted(l) + ")" }},
}

var tsLineScanForms = []lineScanCase{
	{"logical or", func(l string) string { return "process.env.SIGNING_KEY || " + quoted(l) }},
	{"nullish coalescing", func(l string) string { return "process.env.SIGNING_KEY ?? " + quoted(l) }},
	{"ternary", func(l string) string { return "isProduction ? process.env.SIGNING_KEY : " + quoted(l) }},
}

// The declaration sites each provider's rule declares; the value sits on one
// line in every template, so the line scan reports it at most once.
var goLineScanSites = map[string]func(name, value string) string{
	"AssignStmt": func(n, v string) string { return "package p\n\nfunc f() { " + n + " := " + v + "; _ = " + n + " }\n" },
	"ValueSpec":  func(n, v string) string { return "package p\n\nvar " + n + " = " + v + "\n" },
	"KeyValueExpr": func(n, v string) string {
		return "package p\n\ntype Cfg struct{ " + n + " string }\n\nfunc f() Cfg { return Cfg{" + n + ": " + v + "} }\n"
	},
}

var tsLineScanSites = map[string]func(name, value string) string{
	"const":       func(n, v string) string { return "const " + n + " = " + v + ";\n" },
	"let":         func(n, v string) string { return "let " + n + " = " + v + ";\n" },
	"var":         func(n, v string) string { return "var " + n + " = " + v + ";\n" },
	"typed const": func(n, v string) string { return "const " + n + ": string = " + v + ";\n" },
	"typed let":   func(n, v string) string { return "let " + n + ": string = " + v + ";\n" },
	"object property": func(n, v string) string {
		return "export const cfg = { region: \"eu\", " + n + ": " + v + ", retries: 3 };\n"
	},
}

// sensorSEC001 writes one file into a fresh project and returns the SEC-001
// titles the real sensor reports for it.
func sensorSEC001(t *testing.T, p providers.LanguageProvider, lang, rel, src string) []string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, rel, src)
	res, err := security.New(p).Run(auditctx.AuditContext{
		ProjectRoot: root, Language: lang,
		Config: &config.Config{Project: config.Project{Language: lang, PathCriticality: p.DefaultPathCriticality()}},
	})
	if err != nil {
		t.Fatalf("sensor run: %v\n%s", err, src)
	}
	var titles []string
	for _, f := range res.Findings {
		if f.ID == "SEC-001" {
			titles = append(titles, f.Title)
		}
	}
	return titles
}

// lineScanTitle is the title layer 1 gives its findings; asserting it shows
// which producer reported the key.
const lineScanTitle = "Possible hardcoded API key"

func TestSEC001LineScanReportsKnownFormatKeysInFormsTheRuleDoesNotRead(t *testing.T) {
	type lang struct {
		name     string
		provider providers.LanguageProvider
		rel      string
		forms    []lineScanCase
		sites    map[string]func(name, value string) string
	}
	langs := []lang{
		{"go", golang.New(), "x/config.go", goLineScanForms, goLineScanSites},
		{"typescript", typescript.New(), "app/x/config.ts", tsLineScanForms, tsLineScanSites},
	}
	keys := lineScanKeys()
	if len(keys) != 5 {
		t.Fatalf("%d key fixtures, want one per apiKeyPatterns family (5)", len(keys))
	}
	for _, l := range langs {
		for siteName, site := range l.sites {
			// The site reaches the rule: the plain value as ONE literal fires
			// (through the name-and-value rule, not the line scan).
			if got := sensorSEC001(t, l.provider, l.name, l.rel, site("signingKey", quoted(plainValue))); len(got) != 1 || got[0] == lineScanTitle {
				t.Fatalf("vacuum: %s/%s with one plain literal gave %v, want one rule finding", l.name, siteName, got)
			}
			for _, form := range l.forms {
				t.Run(l.name+"/"+siteName+"/"+form.form, func(t *testing.T) {
					if got := sensorSEC001(t, l.provider, l.name, l.rel, site("signingKey", form.value(plainValue))); len(got) != 0 {
						t.Errorf("an ordinary secret in this form was reported %v: the rule is declared not to read it", got)
					}
					for family, key := range keys {
						got := sensorSEC001(t, l.provider, l.name, l.rel, site("signingKey", form.value(key)))
						if len(got) != 1 || got[0] != lineScanTitle {
							t.Errorf("%s key in this form: got %v, want one line-scan finding (%q)", family, got, lineScanTitle)
						}
					}
				})
			}
		}
	}
}

// TestSEC001ServedTextsDeclareTheLineScan: both coverage sources say the
// name-and-value rule does not read the forms above, so both must also say that
// the line scan still reports a known-format key in them, and name each family's
// prefix. Without it "not read by the rule" reads as "not reported".
func TestSEC001ServedTextsDeclareTheLineScan(t *testing.T) {
	var goText strings.Builder
	for _, l := range golang.New().Capability().Security.Limits {
		if l.ID == "SEC-001" {
			goText.WriteString(l.Limit)
		}
	}
	tsText := ""
	for _, e := range typescript.New().CoverageManifest().Deterministic {
		if e.ID == "ts.hardcoded-secrets" {
			tsText = e.Claim + " " + e.Detail
		}
	}
	for name, text := range map[string]string{"go": goText.String(), "typescript": tsText} {
		if !strings.Contains(text, "line scan") {
			t.Errorf("%s: the served SEC-001 text does not declare the line scan", name)
		}
		for _, prefix := range []string{"sk-ant-", "sk-", "ghp_", "xox", "AKIA"} {
			if !strings.Contains(text, prefix) {
				t.Errorf("%s: the served SEC-001 text does not name the %q family", name, prefix)
			}
		}
	}
}
