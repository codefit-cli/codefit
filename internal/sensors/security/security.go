// Package security is the universal security sensor. It orchestrates the
// filtering pyramid — a language-agnostic regex layer for obvious secrets and
// the active provider's AST analysis (deterministic findings plus mapped
// surface) — and adjusts each finding's severity by the file's path criticality
// (RF-11). codefit runs no LLM layer: the surface is returned to the agent,
// which reasons over it with its own model.
package security

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/scoring"
	"github.com/codefit-cli/codefit/internal/providers"
)

// Sensor runs the security dimension against a project, driven by a language
// provider for the AST layer.
type Sensor struct {
	provider providers.LanguageProvider
}

// New builds a security sensor backed by a language provider.
func New(p providers.LanguageProvider) *Sensor { return &Sensor{provider: p} }

func (*Sensor) Name() string                  { return "security" }
func (*Sensor) Dimension() findings.Dimension { return findings.DimensionSecurity }

// skipDirs are directories never walked.
var skipDirs = map[string]bool{
	".git": true, "vendor": true, "node_modules": true,
	"dist": true, "bin": true, ".codefit": true,
}

// Run walks the project's source files and returns the security findings.
func (s *Sensor) Run(ctx auditctx.AuditContext) (findings.SensorResult, error) {
	start := time.Now()
	exts := s.provider.FileExtensions()

	var all []findings.Finding
	var surface []findings.SurfaceItem
	walkErr := filepath.WalkDir(ctx.ProjectRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !slices.Contains(exts, filepath.Ext(path)) {
			return nil
		}
		rel, relErr := filepath.Rel(ctx.ProjectRoot, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)

		fileFindings, fileSurface, fileErr := s.scanFile(rel, path)
		if fileErr != nil {
			return fileErr
		}
		applyCriticality(ctx.Config, rel, fileFindings)
		all = append(all, fileFindings...)
		surface = append(surface, fileSurface...)
		return nil
	})
	if walkErr != nil {
		return findings.SensorResult{Sensor: s.Name(), Error: walkErr.Error()}, walkErr
	}

	return findings.SensorResult{
		Sensor:     s.Name(),
		Score:      scoring.DimensionScore(all),
		Findings:   all,
		Surface:    surface,
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}

// scanFile runs the pyramid layers over a single file: deterministic findings
// (regex + provider AST) and the mapped surface for the agent to reason about.
// codefit never runs a layer-3 LLM — the surface is returned to the agent.
func (s *Sensor) scanFile(rel, abs string) ([]findings.Finding, []findings.SurfaceItem, error) {
	content, err := os.ReadFile(abs)
	if err != nil {
		return nil, nil, err
	}
	src := providers.SourceFile{Path: rel, Content: content}
	var out []findings.Finding

	// Layer 1: language-agnostic regex for obvious secrets.
	out = append(out, layer1Secrets(rel, content)...)

	// Layer 2: provider AST analysis — deterministic findings + mapped surface.
	astFindings, err := s.provider.AnalyzeSecurity(src)
	if err != nil {
		return nil, nil, err
	}
	out = append(out, astFindings...)

	surface, err := s.provider.AnalyzeSurface(src)
	if err != nil {
		return nil, nil, err
	}
	return out, surface, nil
}

// apiKeyPatterns matches well-known credential shapes (layer 1).
var apiKeyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-ant-[A-Za-z0-9-]{8,}`),      // Anthropic
	regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`),          // OpenAI
	regexp.MustCompile(`ghp_[A-Za-z0-9]{20,}`),         // GitHub PAT
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`), // Slack
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),             // AWS access key id
}

// layer1Secrets scans raw text for credential-shaped strings.
func layer1Secrets(rel string, content []byte) []findings.Finding {
	var out []findings.Finding
	for _, line := range nonEmptyLines(content) {
		for _, re := range apiKeyPatterns {
			if re.MatchString(line.text) {
				out = append(out, findings.Finding{
					ID:              "SEC-001",
					Dimension:       findings.DimensionSecurity,
					Severity:        findings.SeverityCritical,
					File:            rel,
					Line:            line.number,
					Title:           "Possible hardcoded API key",
					Description:     "A string matching a known API-key format is present in the source.",
					Suggestion:      "Move the secret to an environment variable or the OS keychain.",
					Confidence:      1.0,
					RequiresConsent: true,
				})
				break // one finding per line is enough
			}
		}
	}
	return out
}

type sourceLine struct {
	number int
	text   string
}

func nonEmptyLines(content []byte) []sourceLine {
	var out []sourceLine
	for i, t := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(t) != "" {
			out = append(out, sourceLine{number: i + 1, text: t})
		}
	}
	return out
}

// applyCriticality adjusts each finding's severity by the file's path
// criticality: test downgrades one level, example forces info (RF-11).
func applyCriticality(cfg *config.Config, rel string, fs []findings.Finding) {
	if cfg == nil {
		return
	}
	criticality := cfg.PathCriticalityFor(rel)
	for i := range fs {
		fs[i].Severity = adjustSeverity(fs[i].Severity, criticality)
		fs[i].RequiresConsent = fs[i].Dimension == findings.DimensionSecurity &&
			fs[i].Severity == findings.SeverityCritical
	}
}

func adjustSeverity(sev findings.Severity, criticality string) findings.Severity {
	switch criticality {
	case config.CriticalityTest:
		return downgrade(sev)
	case config.CriticalityExample:
		return findings.SeverityInfo
	default:
		return sev
	}
}

func downgrade(sev findings.Severity) findings.Severity {
	switch sev {
	case findings.SeverityCritical:
		return findings.SeverityHigh
	case findings.SeverityHigh:
		return findings.SeverityMedium
	case findings.SeverityMedium:
		return findings.SeverityLow
	case findings.SeverityLow:
		return findings.SeverityInfo
	default:
		return findings.SeverityInfo
	}
}
