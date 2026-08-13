package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bmatcuk/doublestar/v4"
)

// Path-criticality classes (RF-10).
const (
	CriticalityProduction = "production"
	CriticalityTest       = "test"
	CriticalityExample    = "example"
)

// PathCriticalityFor classifies a project-relative file path as "production",
// "test" or "example" by matching it against the configured path_criticality
// globs, or returns "" when nothing matches.
//
// Test and example take precedence over production so that, e.g., a
// *_test.go living under a production directory is correctly treated as test
// (lower severity), not production.
func (c *Config) PathCriticalityFor(file string) string {
	file = filepath.ToSlash(file)
	pc := c.Project.PathCriticality
	switch {
	case matchAny(pc.Test, file):
		return CriticalityTest
	case matchAny(pc.Example, file):
		return CriticalityExample
	case matchAny(pc.Production, file):
		return CriticalityProduction
	default:
		return ""
	}
}

// TestSeverityMode resolves sensors.security.test_severity into the mode the
// security sensor must apply to a test-classified path (RF-10).
//
// It is the SINGLE place the default is decided. Unset ("") means
// TestSeverityInfo — the PRD's default — and so does a nil Config, which is the
// ordinary state of a project with no .codefit.yaml. A value outside the enum
// cannot survive Load (validate rejects it with a located error), so reaching
// this function it can only come from a hand-built Config; the resolver refuses
// to invent a fourth mode and falls back to the default rather than silently
// leaving severities unweighted.
//
// Callers ask for the mode; they never read Sensors.Security.TestSeverity raw.
// A sensor that re-implemented the "" → info rule locally is exactly how two
// defaults drift apart, which is the same reason PathCriticalityFor lives here.
func (c *Config) TestSeverityMode() string {
	if c == nil {
		return TestSeverityInfo
	}
	switch c.Sensors.Security.TestSeverity {
	case TestSeverityDowngrade:
		return TestSeverityDowngrade
	case TestSeverityKeep:
		return TestSeverityKeep
	default:
		return TestSeverityInfo
	}
}

// matchAny reports whether file matches any of the doublestar patterns.
func matchAny(patterns []string, file string) bool {
	for _, p := range patterns {
		if ok, _ := doublestar.Match(filepath.ToSlash(p), file); ok {
			return true
		}
	}
	return false
}

// ExpandGlobs resolves glob patterns (with ** support) to the set of existing
// files under root, project-relative and de-duplicated. It is used to turn
// schema_paths, test_dirs and ignore.paths into concrete file lists.
func ExpandGlobs(root string, patterns []string) ([]string, error) {
	fsys := os.DirFS(root)
	var out []string
	seen := make(map[string]bool)
	for _, p := range patterns {
		p = filepath.ToSlash(p)
		matches, err := doublestar.Glob(fsys, p, doublestar.WithFilesOnly())
		if err != nil {
			return nil, fmt.Errorf("expanding glob %q: %w", p, err)
		}
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	return out, nil
}
