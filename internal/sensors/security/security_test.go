package security_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/providers/golang"
	"github.com/codefit-cli/codefit/internal/sensors/security"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runSensor(t *testing.T, root string) findings.SensorResult {
	t.Helper()
	cfg := &config.Config{Project: config.Project{
		Language: "go",
		PathCriticality: config.PathCriticality{
			Production: []string{"**/*.go"},
			Test:       []string{"**/*_test.go"},
			Example:    []string{"examples/**"},
		},
	}}
	ctx := auditctx.AuditContext{ProjectRoot: root, Language: "go", Config: cfg, NoLLM: true}
	res, err := security.New(golang.New()).Run(ctx)
	if err != nil {
		t.Fatalf("sensor Run: %v", err)
	}
	return res
}

func find(fs []findings.Finding, fileSuffix, id string) *findings.Finding {
	for i := range fs {
		if fs[i].ID == id && strings.HasSuffix(fs[i].File, fileSuffix) {
			return &fs[i]
		}
	}
	return nil
}

func TestSensorIdentity(t *testing.T) {
	s := security.New(golang.New())
	if s.Name() != "security" || s.Dimension() != findings.DimensionSecurity {
		t.Errorf("identity = %q/%q", s.Name(), s.Dimension())
	}
}

func TestSensorFindsASTSecurityIssues(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "auth.go", `package x
import "crypto/md5"
func f() {
	apiKey := "super-secret-value-1234"
	_ = apiKey
	_ = md5.New()
}`)
	res := runSensor(t, root)

	if f := find(res.Findings, "auth.go", "SEC-001"); f == nil || f.Severity != findings.SeverityHigh {
		t.Errorf("want SEC-001 high in auth.go, got %+v", f)
	}
	if f := find(res.Findings, "auth.go", "SEC-052"); f == nil || f.Severity != findings.SeverityMedium {
		t.Errorf("want SEC-052 medium in auth.go, got %+v", f)
	}
}

func TestSensorLayer1DetectsAPIKeyPattern(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "config.go", `package x
var k = "sk-ant-api03-AbCdEf1234567890"
`)
	res := runSensor(t, root)
	f := find(res.Findings, "config.go", "SEC-001")
	if f == nil || f.Severity != findings.SeverityCritical {
		t.Errorf("want a critical SEC-001 from the API-key pattern, got %+v", f)
	}
}

func TestSensorDowngradesInTestFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "config_test.go", `package x
var k = "sk-ant-api03-AbCdEf1234567890"
`)
	res := runSensor(t, root)
	f := find(res.Findings, "config_test.go", "SEC-001")
	if f == nil {
		t.Fatal("expected a finding in the test file")
	}
	if f.Severity == findings.SeverityCritical {
		t.Error("critical finding in a test file should be downgraded")
	}
	if f.Severity != findings.SeverityHigh {
		t.Errorf("critical should downgrade to high in tests, got %q", f.Severity)
	}
}

func TestSensorExampleFilesAreInfo(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "examples/demo.go", `package x
var k = "sk-ant-api03-AbCdEf1234567890"
`)
	res := runSensor(t, root)
	f := find(res.Findings, "demo.go", "SEC-001")
	if f == nil || f.Severity != findings.SeverityInfo {
		t.Errorf("findings in examples should be info, got %+v", f)
	}
}
