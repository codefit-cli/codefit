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

// TestLoadRejectsUnknownTestSeverity — an unrecognised
// sensors.security.test_severity must STOP the load, not be quietly resolved to
// the default. Silently accepting "silence" would leave the developer believing
// test findings were suppressed while they were still being reported: a config
// that parses and lies is worse than one that fails.
//
// The message must name the key, the offending value and all three modes, and
// carry path:line so the developer can jump to it — the same shape as framework
// and paradigm.
func TestLoadRejectsUnknownTestSeverity(t *testing.T) {
	body := `version: "1"
project:
  name: "demo"
  language: "go"
sensors:
  security:
    test_severity: "silence"
`
	cfg, err := config.Load(writeConfig(t, body))
	if err == nil {
		t.Fatal("Load accepted an unknown sensors.security.test_severity, want error")
	}
	if cfg != nil {
		t.Errorf("an invalid config must not return a usable *Config, got %+v", cfg)
	}
	msg := err.Error()
	for _, want := range []string{"test_severity", "silence", "info", "downgrade", "keep"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error must name the key, the bad value and the three allowed modes; missing %q in: %s", want, msg)
		}
	}
	// Located error: test_severity is on line 7 of the body.
	if !strings.Contains(msg, ":7:") {
		t.Errorf("error %q should point to line 7 (the test_severity key)", msg)
	}
}

// TestLoadAcceptsEveryTestSeverityMode triangulates the enum arm: all three
// PRD-named modes load, and an unset key loads too. codefit never refuses a
// mode the PRD sanctions — including "keep", whose consequence it warns about
// at materialisation instead (developer autonomy: codefit informs, the
// developer decides).
func TestLoadAcceptsEveryTestSeverityMode(t *testing.T) {
	for _, mode := range []string{"info", "downgrade", "keep"} {
		t.Run(mode, func(t *testing.T) {
			body := `version: "1"
project:
  name: "demo"
  language: "go"
sensors:
  security:
    test_severity: "` + mode + `"
`
			cfg, err := config.Load(writeConfig(t, body))
			if err != nil {
				t.Fatalf("Load rejected the PRD-named mode %q: %v", mode, err)
			}
			if got := cfg.TestSeverityMode(); got != mode {
				t.Errorf("TestSeverityMode() = %q, want %q", got, mode)
			}
		})
	}
	t.Run("unset", func(t *testing.T) {
		cfg, err := config.Load(writeConfig(t, validBody))
		if err != nil {
			t.Fatalf("Load rejected a config with no test_severity: %v", err)
		}
		if got := cfg.TestSeverityMode(); got != config.TestSeverityInfo {
			t.Errorf("an absent test_severity must resolve to %q, got %q", config.TestSeverityInfo, got)
		}
	})
}
