package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the parsed representation of .codefit.yaml. Fields mirror the PRD
// schema. Zero values are acceptable throughout: .codefit.yaml is optional and
// every consumer defaults, so an absent key is a real state, not an unfinished
// one. Load validates what IS present.
type Config struct {
	Version  string   `yaml:"version"`
	Project  Project  `yaml:"project"`
	Database Database `yaml:"database"`
	Sensors  Sensors  `yaml:"sensors"`
	Report   Report   `yaml:"report"`
	Cache    Cache    `yaml:"cache"`
	Baseline Baseline `yaml:"baseline"`
	MCP      MCP      `yaml:"mcp"`
	Ignore   Ignore   `yaml:"ignore"`
}

// Project holds project identity and the path-criticality classification that
// weights finding severity by location (RF-10).
type Project struct {
	Name            string          `yaml:"name"`
	Language        string          `yaml:"language"`
	Framework       string          `yaml:"framework"`
	Description     string          `yaml:"description"`
	PathCriticality PathCriticality `yaml:"path_criticality"`
}

// PathCriticality classifies directories so the engine can raise or lower a
// finding's severity by where it lives. A secret in a test is noise; in
// production it is critical (RF-10). Language providers supply sensible
// defaults via LanguageProvider.DefaultPathCriticality.
type PathCriticality struct {
	Production []string `yaml:"production"`
	Test       []string `yaml:"test"`
	Example    []string `yaml:"example"`
}

// Database declares the DB paradigm and schema sources for the DB sensor.
type Database struct {
	Paradigm    string   `yaml:"paradigm"` // oltp | olap | mixed | auto (default)
	Type        string   `yaml:"type"`
	SchemaPaths []string `yaml:"schema_paths"`
	ORM         string   `yaml:"orm"`
}

// Sensors toggles and tunes each sensor. Only the enable flags and a couple of
// representative knobs are modeled — the tuning surface grows with the sensors
// that need it, rather than being declared ahead of them.
type Sensors struct {
	Security   SecuritySensor `yaml:"security"`
	Review     SensorToggle   `yaml:"review"`
	DB         SensorToggle   `yaml:"db"`
	Complexity SensorToggle   `yaml:"complexity"`
	Practices  SensorToggle   `yaml:"practices"`
	Tests      SensorToggle   `yaml:"tests"`
}

// SensorToggle is the minimal per-sensor config common to every sensor. Enabled
// is a *bool to carry a THREE-STATE meaning: nil (unset in .codefit.yaml) lets a
// sensor apply its own default (the DB sensor treats unset as on — opt-out); an
// explicit true/false overrides it. A plain bool could not tell "unset" from
// "explicitly false".
//
// It carries ONLY what every sensor honours. Adding a sensor-specific knob here
// is the tempting one-line edit that makes five other sensors silently ACCEPT a
// key none of them reads — dead config that parses, validates and does nothing,
// exactly the class of defect codefit audits for. TestSeverity therefore lives
// on SecuritySensor, and sensortoggle_lock_test.go fails the build if that ever
// stops being true.
type SensorToggle struct {
	Enabled *bool `yaml:"enabled,omitempty"`
}

// LanguageUndetected is the value `codefit init` writes into project.language
// when no marker file under the project root resolved an auditable language
// provider.
//
// It is a statement about codefit's DETECTION, never about the project. A Java
// or Rust repository has a language; codefit simply registers no provider that
// can audit its code, so it says so in the one place every later reader looks
// instead of refusing to write a config at all. Rendering it as "this project
// has no language" would be a lie.
//
// It lives here, beside allowedLanguages, for two reasons. internal/scaffold
// already imports internal/config, so housing it there would invert the
// dependency; and internal/providers/registry is the wrong home because the
// sentinel is precisely the ABSENCE of a registered entry — a constant there
// invites someone to add a matching fake table row.
//
// Every consumer of Project.Language must read it as "resolve no provider", not
// as a language name. No production sensor or handler reads the field today
// (validation is its only consumer), so the sentinel resolves nothing anywhere
// by construction — internal/mcp's Lock A asserts exactly that.
const LanguageUndetected = "undetected"

// Test-path severity modes (RF-10). They govern what happens to a security
// finding whose file is classified "test" by path_criticality:
//
//   - TestSeverityInfo forces it to info. It is the DEFAULT, and the mode the
//     PRD chose: a secret in a test does not weigh what a secret in production
//     weighs, so it costs the security score nothing (severity penalty 0).
//   - TestSeverityDowngrade lowers it exactly one level (critical→high, …). It
//     was codefit's hardcoded behaviour before this key existed.
//   - TestSeverityKeep applies NO adjustment at all: the natural severity
//     survives. It is the only one of the three that can leave a critical
//     security finding standing on a test path, which means it is also the only
//     one that can make scoring.IsBlocked true from a test file. That is a
//     deliberate, PRD-named choice, not an accident — see SecuritySensor.
const (
	TestSeverityInfo      = "info"
	TestSeverityDowngrade = "downgrade"
	TestSeverityKeep      = "keep"
)

// SecuritySensor is the security sensor's config: the shared toggle plus the
// knobs only this sensor honours.
//
// SensorToggle is embedded with `yaml:",inline"` so `enabled` and
// `test_severity` sit at the same level in .codefit.yaml while `enabled` keeps
// ONE definition (and its three-state *bool) shared with every other sensor.
//
// TestSeverity selects how a test-classified path re-weights a security
// finding: "" (unset) means TestSeverityInfo — resolve it through
// (*Config).TestSeverityMode(), never by reading this field raw.
//
// CONSEQUENCE OF "keep", stated where the key is declared: under
// TestSeverityKeep a critical security finding in a test file stays critical,
// so it keeps RequiresConsent and scoring.IsBlocked reports the project as
// blocked. codefit does not refuse the mode — it is one of the three the PRD
// names, and the developer decides — but the consequence is not hidden either:
// the security sensor emits one warning per run when keep actually leaves a
// finding at critical on a test path.
type SecuritySensor struct {
	SensorToggle `yaml:",inline"`
	TestSeverity string `yaml:"test_severity,omitempty"`
}

// Report configures output format and the per-dimension score weights (which
// must sum to 100).
type Report struct {
	Output       string         `yaml:"output"`
	OutFile      string         `yaml:"out_file"`
	IncludeInfo  bool           `yaml:"include_info"`
	ScoreWeights map[string]int `yaml:"score_weights"`
}

// Cache configures the content-hash finding cache.
type Cache struct {
	Enabled bool   `yaml:"enabled"`
	Dir     string `yaml:"dir"`
}

// Baseline configures the adoption baseline (RF-08): when enabled only new
// findings are reported.
type Baseline struct {
	Enabled bool   `yaml:"enabled"`
	File    string `yaml:"file"`
}

// MCP configures the MCP server and which tools it exposes.
type MCP struct {
	Enabled     bool     `yaml:"enabled"`
	ExposeTools []string `yaml:"expose_tools"`
}

// Ignore holds path globs and finding suppressions. Critical security
// suppressions require an embedded ConsentRecord (validated later).
type Ignore struct {
	Paths    []string        `yaml:"paths"`
	Findings []IgnoreFinding `yaml:"findings"`
}

// IgnoreFinding suppresses a finding by ID. For critical security findings the
// consent fields are mandatory.
type IgnoreFinding struct {
	ID         string `yaml:"id"`
	Reason     string `yaml:"reason"`
	AcceptedBy string `yaml:"accepted_by"`
	AcceptedAt string `yaml:"accepted_at"`
}

// LoadOptional loads a .codefit.yaml only when it exists, distinguishing the
// three states callers must not conflate:
//
//   - ABSENT  → (nil, nil): no config is fine, the caller uses defaults.
//   - PRESENT but INVALID → (nil, error): codefit must REFUSE to run silently
//     with a broken config. Swallowing this is the very anti-pattern codefit
//     exists to catch — a false "all good" that hides a real problem (e.g. an
//     invalid framework silently disabling path_criticality).
//   - VALID   → (cfg, nil): loaded normally.
//
// The returned error is the located, field-level message from Load/validate
// (e.g. `invalid framework "nextjs" (allowed: …)`), useful enough to fix.
func LoadOptional(path string) (*Config, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("checking config %q: %w", path, err)
	}
	return Load(path)
}

// Load reads, parses and validates a .codefit.yaml file from path. Validation
// errors are located (path:line) so the user can jump to the offending line.
//
// It does not yet apply defaulting or merge with the global user config; those
// are layered on by the caller.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %q: %w", path, err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parsing config %q: %w", path, err)
	}
	var cfg Config
	if err := root.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decoding config %q: %w", path, err)
	}
	if err := validate(&cfg, &root, path); err != nil {
		return nil, err
	}
	return &cfg, nil
}
