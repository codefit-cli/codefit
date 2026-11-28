package mcp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/scope"
	"github.com/codefit-cli/codefit/internal/sensors/security"
)

// The SEC-001 field count (issue #148). The value veto changed what SEC-001
// reports in both providers; table tests prove the rule on fixtures, and only a
// run over real projects says what it does to the reports a user actually gets.
// The dogfood-tagged TestDogfoodSEC001FieldCounts (dogfood_sec001_test.go) does
// that run. The counting itself lives HERE, in an untagged file, so that the
// ordinary `go test ./...` drives it on every change: a dogfood test is compiled
// only when somebody asks for it, and a counter nobody runs can rot into counting
// nothing while the field numbers it printed keep being quoted.
//
// What it runs is the REAL security sensor with the REAL providers, resolved the
// way the MCP adapter resolves them (providerForLanguage), over every language
// codefit-scan-security admits (SupportedLanguageNames, derived from the
// registry). It deliberately does NOT go through runSecurity or buildScanAll:
// both load the project's own .codefit.yaml, and one that enables the finding
// cache with a relative directory makes the sensor write that directory inside
// the project root. Here the configuration is built in memory and has no cache,
// so a run only reads the project.

// sec001FieldCount is what one provider did over one project. Every exported
// field is a count or a duration: a field run's output is pasted into public
// places, and a count is publishable where a location is not. The findings
// themselves are kept unexported for the lock test, which owns its fixture, and
// are never printed by the field run.
type sec001FieldCount struct {
	Language            string
	FilesAudited        int
	ParseStatusReported bool
	FilesNotParsedWhole int
	SEC001              int
	DurationMs          int64
	sec001              []findings.Finding
}

// summary renders the counts, and nothing else, as one line.
func (c sec001FieldCount) summary() string {
	notWhole := "not reported"
	if c.ParseStatusReported {
		notWhole = strconv.Itoa(c.FilesNotParsedWhole)
	}
	return fmt.Sprintf("%-10s files audited %5d | not parsed whole %-12s | SEC-001 %4d | %6d ms",
		c.Language, c.FilesAudited, notWhole, c.SEC001, c.DurationMs)
}

// sec001FieldError is a sensor failure in one language's run. Its message can
// name a file (a parse error does), so a field run reports only the language.
type sec001FieldError struct {
	language string
	err      error
}

func (e *sec001FieldError) Error() string {
	return e.language + " security sensor: " + e.err.Error()
}

func (e *sec001FieldError) Unwrap() error { return e.err }

// sec001FieldCounts runs the security sensor once per language over root and
// counts what each provider audited and how many SEC-001 it reported, layer 1
// and layer 2 together (the sensor merges them by file, line and rule id). A
// provider that audits no file is still returned, with zeros; deciding that a
// zero means "not this project's language" is the caller's job.
func sec001FieldCounts(root string) ([]sec001FieldCount, error) {
	var out []sec001FieldCount
	for _, lang := range SupportedLanguageNames() {
		p := providerForLanguage(lang, nil)
		if p == nil {
			return nil, &sec001FieldError{language: lang, err: errors.New("no security provider resolves")}
		}
		cfg := &config.Config{Project: config.Project{
			Language:        lang,
			PathCriticality: p.DefaultPathCriticality(),
		}}
		res, err := security.New(p).Run(auditctx.AuditContext{
			ProjectRoot: root, Language: lang, Config: cfg, Scope: scope.Full(),
		})
		if err != nil {
			return nil, &sec001FieldError{language: lang, err: err}
		}
		c := sec001FieldCount{
			Language:            lang,
			FilesAudited:        len(res.AuditedFiles),
			ParseStatusReported: res.ParseStatusReported,
			FilesNotParsedWhole: len(res.ParseIncomplete),
			DurationMs:          res.DurationMs,
		}
		for _, f := range res.Findings {
			if f.ID == "SEC-001" {
				c.SEC001++
				c.sec001 = append(c.sec001, f)
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// sec001FieldControl is the planted project: in each language, one secret
// SEC-001 must report and one value the value veto must keep quiet, under the
// same credential name. want is the one finding each language owes, by file and
// line; a provider missing from the run, an extra provider, a second finding or
// a finding on the vetoed line all fail.
var sec001FieldControl = struct {
	files map[string]string
	want  map[string]struct {
		file string
		line int
	}
}{
	files: map[string]string{
		"app/config.ts": "const signingKey = \"s3cr3t\";\n" +
			"const secretFile = \"lib/config.ts\";\n",
		"cmd/app/main.go": "package main\n\n" +
			"const signingKey = \"s3cr3t\"\n\n" +
			"const secretFile = \"lib/config.ts\"\n\n" +
			"func main() {}\n",
	},
	want: map[string]struct {
		file string
		line int
	}{
		"typescript": {"app/config.ts", 1},
		"go":         {"cmd/app/main.go", 3},
	},
}

// requireSEC001FieldControl plants the control project in a temporary
// directory, counts it, and fails unless every language reports exactly its one
// secret. The lock below runs it on every `go test ./...`; the field run runs it
// first, so the numbers it prints come from a counter shown alive in that run.
func requireSEC001FieldControl(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	for rel, src := range sec001FieldControl.files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	counts, err := sec001FieldCounts(root)
	if err != nil {
		t.Fatalf("counting the planted control: %v", err)
	}
	seen := make(map[string]bool)
	for _, c := range counts {
		seen[c.Language] = true
		want, ok := sec001FieldControl.want[c.Language]
		if !ok {
			t.Errorf("the count ran a language the control plants nothing for: %s — add a file and a want for it", c.Language)
			continue
		}
		if c.FilesAudited != 1 {
			t.Errorf("%s audited %d files of the control, want 1", c.Language, c.FilesAudited)
		}
		if c.ParseStatusReported && c.FilesNotParsedWhole != 0 {
			t.Errorf("%s reports %d control files not parsed whole, want 0", c.Language, c.FilesNotParsedWhole)
		}
		if c.SEC001 != 1 || len(c.sec001) != 1 {
			t.Errorf("%s counted %d SEC-001 on the control, want 1 (the secret, not the vetoed path): %+v",
				c.Language, c.SEC001, c.sec001)
			continue
		}
		if got := c.sec001[0]; got.File != want.file || got.Line != want.line {
			t.Errorf("%s reported SEC-001 at %s:%d, want %s:%d (the secret's line)",
				c.Language, got.File, got.Line, want.file, want.line)
		}
		// The line the field run prints carries counts and nothing that locates
		// a finding: no file, no directory, no matched text.
		if line := c.summary(); strings.Contains(line, want.file) || strings.Contains(line, "/") ||
			strings.Contains(line, "s3cr3t") || !strings.Contains(line, "SEC-001    1") {
			t.Errorf("%s: the printed summary must hold counts only, and this count: %q", c.Language, line)
		}
	}
	for lang := range sec001FieldControl.want {
		if !seen[lang] {
			t.Errorf("the count did not run %s: a field table without it would say nothing about it", lang)
		}
	}
}

// TestSEC001FieldCountsSeeBothProviders is the lock on the field counter: over
// the planted control, both providers run, each audits its one file, each
// reports exactly the secret while the value veto keeps the path quiet, and the
// line the field run prints holds counts only.
func TestSEC001FieldCountsSeeBothProviders(t *testing.T) {
	requireSEC001FieldControl(t)
}
