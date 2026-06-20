package golang_test

import (
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
	if p.ReviewPromptContext() == "" {
		t.Error("ReviewPromptContext should describe the Go ecosystem")
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

func TestPracticeIgnoredError(t *testing.T) {
	src := `package x
import "strconv"
func f(s string) { v, _ := strconv.Atoi(s); _ = v }`
	if !hasID(analyzePractices(t, "x.go", src), "PRAC-001") {
		t.Error("should flag ignored error")
	}
}

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

func TestPracticePanicInProduction(t *testing.T) {
	src := `package x
func f() { panic("boom") }`
	if !hasID(analyzePractices(t, "x.go", src), "PRAC-005") {
		t.Error("should flag panic in production code")
	}
}

func TestPracticePanicAllowedInTests(t *testing.T) {
	src := `package x
func f() { panic("boom") }`
	if hasID(analyzePractices(t, "x_test.go", src), "PRAC-005") {
		t.Error("panic in a _test.go file should not be flagged")
	}
}

func TestPracticeGoroutine(t *testing.T) {
	src := `package x
func f() { go func(){}() }`
	if !hasID(analyzePractices(t, "x.go", src), "PRAC-004") {
		t.Error("should flag an unsynchronized goroutine")
	}
}

func TestPracticeEmptyInterface(t *testing.T) {
	src := `package x
func f(v interface{}) {}`
	if !hasID(analyzePractices(t, "x.go", src), "PRAC-003") {
		t.Error("should flag empty interface{} usage")
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
