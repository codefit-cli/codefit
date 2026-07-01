package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the parsed representation of .codefit.yaml. Fields mirror the PRD
// schema; this is a skeleton, so zero values are acceptable until the loader
// and validators are implemented.
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
// weights finding severity by location (RF-11).
type Project struct {
	Name            string          `yaml:"name"`
	Language        string          `yaml:"language"`
	Framework       string          `yaml:"framework"`
	Description     string          `yaml:"description"`
	PathCriticality PathCriticality `yaml:"path_criticality"`
}

// PathCriticality classifies directories so the engine can raise or lower a
// finding's severity by where it lives. A secret in a test is noise; in
// production it is critical (RF-11). Language providers supply sensible
// defaults via LanguageProvider.DefaultPathCriticality.
type PathCriticality struct {
	Production []string `yaml:"production"`
	Test       []string `yaml:"test"`
	Example    []string `yaml:"example"`
}

// Database declares the DB paradigm and schema sources for the DB sensor.
type Database struct {
	Paradigm    string   `yaml:"paradigm"` // oltp | olap | mixed (auto by default)
	Type        string   `yaml:"type"`
	SchemaPaths []string `yaml:"schema_paths"`
	ORM         string   `yaml:"orm"`
}

// Sensors toggles and tunes each sensor. Skeleton: only the enable flags and a
// couple of representative knobs are modeled.
type Sensors struct {
	Security   SensorToggle `yaml:"security"`
	Review     SensorToggle `yaml:"review"`
	DB         SensorToggle `yaml:"db"`
	Complexity SensorToggle `yaml:"complexity"`
	Practices  SensorToggle `yaml:"practices"`
	Tests      SensorToggle `yaml:"tests"`
}

// SensorToggle is the minimal per-sensor config common to every sensor. Enabled
// is a *bool to carry a THREE-STATE meaning: nil (unset in .codefit.yaml) lets a
// sensor apply its own default (the DB sensor treats unset as on — opt-out); an
// explicit true/false overrides it. A plain bool could not tell "unset" from
// "explicitly false".
type SensorToggle struct {
	Enabled *bool `yaml:"enabled,omitempty"`
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

// Baseline configures the adoption baseline (RF-10): when enabled only new
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
