package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
)

// writeConfig writes a .codefit.yaml with the given body to a temp dir and
// returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, ".codefit.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}
	return p
}

const validBody = `version: "1"
project:
  name: "demo"
  language: "go"
`

func TestLoadAcceptsValidConfig(t *testing.T) {
	_, err := config.Load(writeConfig(t, validBody))
	if err != nil {
		t.Fatalf("Load on valid config errored: %v", err)
	}
}

func TestLoadRejectsUnknownLanguage(t *testing.T) {
	body := `version: "1"
project:
  name: "demo"
  language: "rust"
`
	_, err := config.Load(writeConfig(t, body))
	if err == nil {
		t.Fatal("Load accepted an unknown language, want error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "language") || !strings.Contains(msg, "rust") {
		t.Errorf("error %q should mention the field and the bad value", msg)
	}
	// Located error: the bad value is on line 4 of the body.
	if !strings.Contains(msg, ":4:") {
		t.Errorf("error %q should point to line 4", msg)
	}
}

func TestLoadRejectsMissingName(t *testing.T) {
	body := `version: "1"
project:
  language: "go"
`
	_, err := config.Load(writeConfig(t, body))
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("Load should reject a missing project name, got: %v", err)
	}
}

func TestLoadRejectsUnknownParadigm(t *testing.T) {
	body := `version: "1"
project:
  name: "demo"
  language: "go"
database:
  paradigm: "graph"
`
	_, err := config.Load(writeConfig(t, body))
	if err == nil || !strings.Contains(err.Error(), "paradigm") {
		t.Fatalf("Load should reject an unknown db paradigm, got: %v", err)
	}
}

func TestLoadAcceptsAutoParadigm(t *testing.T) {
	body := `version: "1"
project:
  name: "demo"
  language: "go"
database:
  paradigm: "auto"
`
	_, err := config.Load(writeConfig(t, body))
	if err != nil {
		t.Fatalf("Load should accept database.paradigm=auto, got: %v", err)
	}
}

func TestLoadAcceptsEmptyParadigm(t *testing.T) {
	// Empty (unset) paradigm is existing behavior: validate.go only checks
	// non-empty values against the allow-list.
	_, err := config.Load(writeConfig(t, validBody))
	if err != nil {
		t.Fatalf("Load should accept an unset database.paradigm, got: %v", err)
	}
}

func TestLoadAcceptsSQLServerDBType(t *testing.T) {
	body := `version: "1"
project:
  name: "demo"
  language: "go"
database:
  type: "sqlserver"
`
	_, err := config.Load(writeConfig(t, body))
	if err != nil {
		t.Fatalf("Load should accept database.type=sqlserver, got: %v", err)
	}
}

func TestLoadRejectsUnknownDBType(t *testing.T) {
	body := `version: "1"
project:
  name: "demo"
  language: "go"
database:
  type: "oracle"
`
	_, err := config.Load(writeConfig(t, body))
	if err == nil {
		t.Fatal("Load accepted an unknown database type, want error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "database type") || !strings.Contains(msg, "oracle") {
		t.Errorf("error %q should mention the field and the bad value", msg)
	}
}

func TestLoadRejectsUnknownFramework(t *testing.T) {
	body := `version: "1"
project:
  name: "demo"
  language: "typescript"
  framework: "angularjs"
`
	_, err := config.Load(writeConfig(t, body))
	if err == nil || !strings.Contains(err.Error(), "framework") {
		t.Fatalf("Load should reject an unknown framework, got: %v", err)
	}
}
