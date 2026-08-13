package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/codefit-cli/codefit/internal/config"
)

// repoCodefitYAML is the project's own .codefit.yaml (self-dogfooding config),
// two levels up from internal/config.
const repoCodefitYAML = "../../.codefit.yaml"

func TestLoadParsesProjectConfig(t *testing.T) {
	cfg, err := config.Load(repoCodefitYAML)
	if err != nil {
		t.Fatalf("Load(%q) returned error: %v", repoCodefitYAML, err)
	}
	if cfg.Version != "1" {
		t.Errorf("Version = %q, want %q", cfg.Version, "1")
	}
	if cfg.Project.Name != "codefit" {
		t.Errorf("Project.Name = %q, want %q", cfg.Project.Name, "codefit")
	}
	if cfg.Project.Language != "go" {
		t.Errorf("Project.Language = %q, want %q", cfg.Project.Language, "go")
	}
	if !slices.Contains(cfg.Project.PathCriticality.Production, "internal/**") {
		t.Errorf("PathCriticality.Production = %v, want it to contain %q",
			cfg.Project.PathCriticality.Production, "internal/**")
	}
	if !slices.Contains(cfg.Project.PathCriticality.Test, "**/*_test.go") {
		t.Errorf("PathCriticality.Test = %v, want it to contain %q",
			cfg.Project.PathCriticality.Test, "**/*_test.go")
	}
}

func TestLoadMissingFileErrors(t *testing.T) {
	_, err := config.Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("Load on a missing file returned nil error, want an error")
	}
}

// LoadOptional must distinguish the three states the scan path conflated:
// ABSENT (use defaults, no error), PRESENT-BUT-INVALID (error, stop), VALID (load).

func TestLoadOptionalAbsentReturnsNilNoError(t *testing.T) {
	cfg, err := config.LoadOptional(filepath.Join(t.TempDir(), ".codefit.yaml"))
	if err != nil {
		t.Fatalf("LoadOptional on an absent file must NOT error, got: %v", err)
	}
	if cfg != nil {
		t.Errorf("LoadOptional on an absent file must return nil cfg (use defaults), got %+v", cfg)
	}
}

func TestLoadOptionalInvalidErrorsLoudly(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".codefit.yaml")
	// framework "nextjs" is not in the allow-list ("next" is): the exact bug that
	// silently disabled path_criticality on Bitácora.
	writeYAML(t, path, "version: \"1\"\nproject:\n  name: \"x\"\n  language: \"typescript\"\n  framework: \"nextjs\"\n")

	cfg, err := config.LoadOptional(path)
	if err == nil {
		t.Fatal("LoadOptional on a PRESENT but invalid config must error, got nil")
	}
	if cfg != nil {
		t.Errorf("invalid config must not return a usable cfg, got %+v", cfg)
	}
	msg := err.Error()
	for _, want := range []string{"framework", "nextjs", "next"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message must name the field + offending + allowed values; missing %q in: %s", want, msg)
		}
	}
}

func TestLoadOptionalValidLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".codefit.yaml")
	writeYAML(t, path, "version: \"1\"\nproject:\n  name: \"x\"\n  language: \"typescript\"\n  framework: \"next\"\n")

	cfg, err := config.LoadOptional(path)
	if err != nil {
		t.Fatalf("LoadOptional on a valid config errored: %v", err)
	}
	if cfg == nil || cfg.Project.Framework != "next" {
		t.Errorf("valid config must load, got %+v", cfg)
	}
}

// TestSecuritySensorInlineRoundTrip is the SPIKE that gates every downstream
// task of RF-10 `test_severity`. `SecuritySensor` embeds `SensorToggle` with
// `yaml:",inline"` so that ONE source keeps `enabled` (and its three-state
// *bool) while the security sensor gains a key the other five sensors must not
// silently accept. That layout is designed-in but nothing in this repository
// proved gopkg.in/yaml.v3 honours `,inline` on an EMBEDDED struct in both
// directions, and an unproven serializer under a config type is exactly the
// silent-drift class codefit exists to catch.
//
// It proves four things at once:
//   - decode: `enabled` and `test_severity` sit at the SAME level and both land;
//   - three-state survival: `test_severity` alone must NOT invent an Enabled;
//   - the reverse: `enabled` alone must leave TestSeverity empty (= unset);
//   - encode: marshalling emits both keys flat, never nested under a
//     `sensortoggle` key — a nested emission would make any config codefit
//     writes back unreadable by itself.
func TestSecuritySensorInlineRoundTrip(t *testing.T) {
	const head = "version: \"1\"\nproject:\n  name: \"x\"\n  language: \"go\"\nsensors:\n  security:\n"

	t.Run("both keys decode at the same level", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".codefit.yaml")
		writeYAML(t, path, head+"    enabled: true\n    test_severity: \"keep\"\n")

		cfg, err := config.Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		sec := cfg.Sensors.Security
		if sec.Enabled == nil || !*sec.Enabled {
			t.Errorf("Sensors.Security.Enabled = %v, want an explicit true — ,inline did not carry the embedded field", sec.Enabled)
		}
		if sec.TestSeverity != config.TestSeverityKeep {
			t.Errorf("Sensors.Security.TestSeverity = %q, want %q", sec.TestSeverity, config.TestSeverityKeep)
		}
	})

	t.Run("test_severity alone leaves Enabled unset", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".codefit.yaml")
		writeYAML(t, path, head+"    test_severity: \"downgrade\"\n")

		cfg, err := config.Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Sensors.Security.Enabled != nil {
			t.Errorf("Enabled = %v, want nil — the three-state must survive ,inline", *cfg.Sensors.Security.Enabled)
		}
		if cfg.Sensors.Security.TestSeverity != config.TestSeverityDowngrade {
			t.Errorf("TestSeverity = %q, want %q", cfg.Sensors.Security.TestSeverity, config.TestSeverityDowngrade)
		}
	})

	t.Run("enabled alone leaves test_severity unset", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".codefit.yaml")
		writeYAML(t, path, head+"    enabled: false\n")

		cfg, err := config.Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Sensors.Security.Enabled == nil || *cfg.Sensors.Security.Enabled {
			t.Errorf("Enabled = %v, want an explicit false", cfg.Sensors.Security.Enabled)
		}
		if cfg.Sensors.Security.TestSeverity != "" {
			t.Errorf("TestSeverity = %q, want \"\" (unset)", cfg.Sensors.Security.TestSeverity)
		}
	})

	t.Run("encode emits both keys flat", func(t *testing.T) {
		on := true
		out, err := yaml.Marshal(config.SecuritySensor{
			SensorToggle: config.SensorToggle{Enabled: &on},
			TestSeverity: config.TestSeverityInfo,
		})
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		got := string(out)
		for _, want := range []string{"enabled: true", "test_severity: info"} {
			if !strings.Contains(got, want) {
				t.Errorf("encoded SecuritySensor is missing %q; got:\n%s", want, got)
			}
		}
		if strings.Contains(strings.ToLower(got), "sensortoggle") {
			t.Errorf("encoded SecuritySensor nested the embedded struct instead of inlining it; got:\n%s", got)
		}
	})
}

func writeYAML(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
