package golang_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/golang"
)

func analyzeSec(t *testing.T, path, src string) []findings.Finding {
	t.Helper()
	fs, err := golang.New().AnalyzeSecurity(providers.SourceFile{Path: path, Content: []byte(src)})
	if err != nil {
		t.Fatalf("AnalyzeSecurity error: %v", err)
	}
	return fs
}

func analyzePractices(t *testing.T, path, src string) []findings.Finding {
	t.Helper()
	fs, err := golang.New().AnalyzePractices(providers.SourceFile{Path: path, Content: []byte(src)})
	if err != nil {
		t.Fatalf("AnalyzePractices error: %v", err)
	}
	return fs
}

func hasID(fs []findings.Finding, id string) bool {
	for _, f := range fs {
		if f.ID == id {
			return true
		}
	}
	return false
}

func firstWithID(t *testing.T, fs []findings.Finding, id string) findings.Finding {
	t.Helper()
	for _, f := range fs {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("no finding with id %s in %+v", id, fs)
	return findings.Finding{}
}

func TestIdentity(t *testing.T) {
	p := golang.New()
	if p.Language() != "go" {
		t.Errorf("Language = %q, want go", p.Language())
	}
	if len(p.FileExtensions()) == 0 || p.FileExtensions()[0] != ".go" {
		t.Errorf("FileExtensions = %v, want [.go]", p.FileExtensions())
	}
	if len(p.Frameworks()) == 0 {
		t.Error("Frameworks should not be empty")
	}
	pc := p.DefaultPathCriticality()
	if len(pc.Production) == 0 || len(pc.Test) == 0 {
		t.Errorf("DefaultPathCriticality incomplete: %+v", pc)
	}
}

func TestParseErrorIsReturned(t *testing.T) {
	_, err := golang.New().AnalyzeSecurity(providers.SourceFile{Path: "bad.go", Content: []byte("this is not go")})
	if err == nil {
		t.Error("AnalyzeSecurity should return an error on unparseable source")
	}
}

// security ------------------------------------------------------------------

func TestHardcodedSecretAssign(t *testing.T) {
	src := `package x
func f() {
	apiKey := "super-secret-value-123"
	_ = apiKey
}`
	if !hasID(analyzeSec(t, "x.go", src), "SEC-001") {
		t.Error("should flag a hardcoded secret assigned to apiKey")
	}
}

func TestHardcodedSecretStructLiteral(t *testing.T) {
	src := `package x
type C struct{ Password string }
var c = C{Password: "hunter2horse"}`
	if !hasID(analyzeSec(t, "x.go", src), "SEC-001") {
		t.Error("should flag a hardcoded secret in a struct literal")
	}
}

func TestHardcodedSecretIgnoresEmptyAndNonSecretNames(t *testing.T) {
	src := `package x
func f() {
	name := "alice"
	token := ""
	_, _ = name, token
}`
	if hasID(analyzeSec(t, "x.go", src), "SEC-001") {
		t.Error("empty string and non-secret names should not flag")
	}
}

func TestWeakHashMD5(t *testing.T) {
	src := `package x
import "crypto/md5"
func f() { _ = md5.New() }`
	if !hasID(analyzeSec(t, "x.go", src), "SEC-052") {
		t.Error("should flag md5 for security use")
	}
}

func TestSQLInjectionConcat(t *testing.T) {
	src := `package x
import "database/sql"
func f(db *sql.DB, id string) { db.Query("SELECT * FROM users WHERE id = " + id) }`
	if !hasID(analyzeSec(t, "x.go", src), "SEC-010") {
		t.Error("should flag SQL built by string concatenation")
	}
}

func TestSQLParameterizedIsClean(t *testing.T) {
	src := `package x
import "database/sql"
func f(db *sql.DB, id string) { db.Query("SELECT * FROM users WHERE id = $1", id) }`
	if hasID(analyzeSec(t, "x.go", src), "SEC-010") {
		t.Error("parameterized query should not flag")
	}
}

func TestCommandInjectionConcat(t *testing.T) {
	src := `package x
import "os/exec"
func f(user string) { _ = exec.Command("sh", "-c", "echo " + user) }`
	if !hasID(analyzeSec(t, "x.go", src), "SEC-013") {
		t.Error("should flag exec.Command built by concatenation")
	}
}

func TestMathRandForSecurity(t *testing.T) {
	src := `package x
import "math/rand"
func f() { token := rand.Int(); _ = token }`
	if !hasID(analyzeSec(t, "x.go", src), "SEC-050") {
		t.Error("should flag math/rand assigned to a security-named var")
	}
}

func TestGetenvInline(t *testing.T) {
	src := `package x
import "os"
func conn(s string) {}
func f() { conn(os.Getenv("DB_URL")) }`
	if !hasID(analyzeSec(t, "x.go", src), "SEC-040") {
		t.Error("should flag os.Getenv used inline without a default")
	}
}

func TestCleanSecurityFile(t *testing.T) {
	src := `package x
import "crypto/rand"
func f() ([]byte, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	return b, err
}`
	if fs := analyzeSec(t, "x.go", src); len(fs) != 0 {
		t.Errorf("clean file should have no security findings, got %+v", fs)
	}
}

// practices -----------------------------------------------------------------
//
// Every rule kept by this dimension gets two fixtures: one that has the thing
// its message names (must fire), and one that has the same *shape* without the
// claimed property (must not fire). That pair is the direct test of the rule
// that a message states only what its code established.

// PRAC-001 — discarded return value ------------------------------------------

func TestPracticeDiscardedReturnValue(t *testing.T) {
	src := `package x
import "strconv"
func f(s string) { v, _ := strconv.Atoi(s); _ = v }`
	if !hasID(analyzePractices(t, "x.go", src), "PRAC-001") {
		t.Error("should flag a call's last return value discarded into _")
	}
}

// The rule has no type information, so it cannot know the discarded value is an
// error. No field may claim it is one, and none may hedge at Confidence 1.0.
func TestPracticeDiscardedReturnValueClaimsOnlyWhatItChecked(t *testing.T) {
	src := `package x
import "strconv"
func f(s string) { v, _ := strconv.Atoi(s); _ = v }`
	f := firstWithID(t, analyzePractices(t, "x.go", src), "PRAC-001")

	if f.Title != "Discarded return value" {
		t.Errorf("Title = %q, want %q", f.Title, "Discarded return value")
	}
	for name, field := range map[string]string{
		"Title": f.Title, "Description": f.Description, "Suggestion": f.Suggestion,
	} {
		if strings.Contains(strings.ToLower(field), "possibly") {
			t.Errorf("%s hedges at Confidence 1.0: %q", name, field)
		}
	}
	// Title and Description are the assertion surface; the suggestion may name
	// the error case as the common reason to look, but must not assert it.
	for name, field := range map[string]string{"Title": f.Title, "Description": f.Description} {
		if strings.Contains(strings.ToLower(field), "error") {
			t.Errorf("%s asserts the discarded value is an error, which the rule never checked: %q", name, field)
		}
	}
	if f.Confidence != 1.0 {
		t.Errorf("Confidence = %v, want 1.0", f.Confidence)
	}
}

// Same shape (a blank on the left), no call on the right: nothing is discarded
// from a call, so the rule must stay silent.
func TestPracticeDiscardedReturnValueIgnoresNonCallRHS(t *testing.T) {
	src := `package x
func f(m map[string]int, k string) { v, _ := m[k]; _ = v }`
	if hasID(analyzePractices(t, "x.go", src), "PRAC-001") {
		t.Error("a comma-ok map index is not a discarded call result")
	}
}

func TestPracticeDiscardedReturnValueIgnoresSingleBlank(t *testing.T) {
	src := `package x
func g() int { return 1 }
func f() { _ = g() }`
	if hasID(analyzePractices(t, "x.go", src), "PRAC-001") {
		t.Error("a single-LHS _ = call() discards the only value, not a trailing one")
	}
}

// PRAC-002 — defer governed by a loop ----------------------------------------

func TestPracticeDeferInLoop(t *testing.T) {
	src := `package x
import "os"
func f(paths []string) {
	for _, p := range paths {
		file, _ := os.Open(p)
		defer file.Close()
	}
}`
	if !hasID(analyzePractices(t, "x.go", src), "PRAC-002") {
		t.Error("should flag defer inside a loop")
	}
}

func TestPracticeDeferInClosureInLoopIsClean(t *testing.T) {
	src := `package x
func f(paths []string) {
	for range paths {
		go func() {
			defer recover()
		}()
	}
}`
	if hasID(analyzePractices(t, "x.go", src), "PRAC-002") {
		t.Error("defer inside a closure (new func scope) within a loop should not flag")
	}
}

// The shape without the claimed property: a defer that no loop governs.
func TestPracticeDeferWithoutLoopIsClean(t *testing.T) {
	src := `package x
import "os"
func f(p string) {
	file, _ := os.Open(p)
	defer file.Close()
}`
	if hasID(analyzePractices(t, "x.go", src), "PRAC-002") {
		t.Error("a defer with no enclosing loop must not flag")
	}
}

// PRAC-003 — empty interface -------------------------------------------------

func TestPracticeEmptyInterfaceFires(t *testing.T) {
	for name, src := range map[string]string{
		"variable/interface{}":  "package x\nfunc f() { var v interface{}; _ = v }",
		"variable/any":          "package x\nfunc f() { var v any; _ = v }",
		"package var/any":       "package x\nvar V any",
		"struct field/any":      "package x\ntype S struct{ F any }",
		"parameter/interface{}": "package x\nfunc f(v interface{}) {}",
		"parameter/any":         "package x\nfunc f(v any) {}",
		"result/any":            "package x\nfunc f() any { return nil }",
		"slice element/any":     "package x\nvar V []any",
		"map value/any":         "package x\nvar V map[string]any",
	} {
		if !hasID(analyzePractices(t, "x.go", src), "PRAC-003") {
			t.Errorf("%s: empty interface in an ordinary type position should flag", name)
		}
	}
}

// The idiomatic positions: a generic constraint and a variadic sink are where
// `any` is unavoidable, so the empty interface there discards nothing the
// author could have kept.
func TestPracticeEmptyInterfaceSkipsIdiomaticPositions(t *testing.T) {
	for name, src := range map[string]string{
		"type param constraint/any":          "package x\nfunc F[T any](v T) {}",
		"type param constraint/interface{}":  "package x\nfunc F[T interface{}](v T) {}",
		"generic type constraint/any":        "package x\ntype S[T any] struct{ V T }",
		"generic type constraint/iface{}":    "package x\ntype S[T interface{}] struct{ V T }",
		"variadic parameter/any":             "package x\nfunc G(args ...any) {}",
		"variadic parameter/interface{}":     "package x\nfunc G(args ...interface{}) {}",
		"variadic method param/interface{}":  "package x\ntype T struct{}\nfunc (T) G(args ...interface{}) {}",
		"generic func with variadic and any": "package x\nfunc F[T any](args ...any) {}",
	} {
		if hasID(analyzePractices(t, "x.go", src), "PRAC-003") {
			t.Errorf("%s: `any` is idiomatic and unavoidable here, must not flag", name)
		}
	}
}

// A non-empty interface is a named contract, not a discard.
func TestPracticeNonEmptyInterfaceIsClean(t *testing.T) {
	src := `package x
type Reader interface{ Read(p []byte) (int, error) }`
	if hasID(analyzePractices(t, "x.go", src), "PRAC-003") {
		t.Error("a non-empty interface must not flag")
	}
}

// An `any` identifier outside a type position is not a declared empty
// interface: a conversion is an expression, and the rule's message is about a
// type that discards information, not about a call.
func TestPracticeAnyOutsideATypePositionIsClean(t *testing.T) {
	src := `package x
func f(v int) { _ = any(v) }`
	if hasID(analyzePractices(t, "x.go", src), "PRAC-003") {
		t.Error("an `any` conversion is not an empty-interface type declaration")
	}
}

// `any` is a predeclared identifier, not a keyword. If the file redeclares it,
// the rule can no longer claim the type is empty, so it stays silent — the
// `interface{}` spelling, which cannot be redeclared, still fires.
func TestPracticeAnyIdentifierIgnoredWhenFileRedeclaresIt(t *testing.T) {
	src := `package x
type any = int
var V any
var W interface{}`
	fs := analyzePractices(t, "x.go", src)
	count := 0
	for _, f := range fs {
		if f.ID == "PRAC-003" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("got %d PRAC-003, want exactly 1 (the interface{} spelling only): %+v", count, fs)
	}
}

// PRAC-005 — panic in library code -------------------------------------------

func TestPracticePanicInLibraryCode(t *testing.T) {
	src := `package x
func f() { panic("boom") }`
	if !hasID(analyzePractices(t, "x.go", src), "PRAC-005") {
		t.Error("should flag panic in a library package")
	}
}

// The message says library code should return errors instead. `package main`
// is not library code, so the claim does not hold there and the rule is silent.
func TestPracticePanicInMainIsClean(t *testing.T) {
	src := `package main
func main() { panic("boom") }`
	if hasID(analyzePractices(t, "x.go", src), "PRAC-005") {
		t.Error("panic in package main is not library code and must not flag")
	}
}

// R2: no rule carries its own notion of a test file. The filename is the
// sensor's business (path criticality), never the rule's, so the rule fires the
// same way regardless of the suffix.
func TestPracticePanicRuleDoesNotConsultTheFilename(t *testing.T) {
	src := `package x
func f() { panic("boom") }`
	if !hasID(analyzePractices(t, "x_test.go", src), "PRAC-005") {
		t.Error("the rule must not decide test-ness from the path; criticality is the sensor's job")
	}
}

// PRAC-004 — dropped ---------------------------------------------------------

// PRAC-004 claimed a goroutine was started "without a visible WaitGroup or
// channel to synchronize it" while checking only that a `go` statement existed.
// It was dropped rather than softened; nothing may emit it again.
func TestPracticeUnsynchronizedGoroutineRuleIsGone(t *testing.T) {
	for name, src := range map[string]string{
		"bare goroutine": "package x\nfunc f() { go func(){}() }",
		"goroutine with a WaitGroup": `package x
import "sync"
func f() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done() }()
	wg.Wait()
}`,
	} {
		if hasID(analyzePractices(t, "x.go", src), "PRAC-004") {
			t.Errorf("%s: PRAC-004 was dropped as unsound and must never be emitted", name)
		}
	}
}
