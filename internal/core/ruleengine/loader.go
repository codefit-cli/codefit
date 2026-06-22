package ruleengine

import (
	"fmt"
	"io/fs"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/codefit-cli/codefit/internal/core/findings"
)

// LoadFS reads and validates every *.yaml rule file under dir in fsys and
// returns the rules in a stable, file-sorted order. fsys is supplied by the
// caller (the embedded rules/ FS in production, an in-memory FS in tests), so
// the engine never reaches the real filesystem itself.
//
// Validation is strict and located: a malformed file or a structurally invalid
// rule fails with an error naming the file (and the rule id when known), so a
// broken rule can never enter the engine silently — a silent rule is a silent
// vulnerability.
func LoadFS(fsys fs.FS, dir string) ([]Rule, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("reading rules dir %q: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if n := e.Name(); len(n) > 5 && n[len(n)-5:] == ".yaml" {
			names = append(names, n)
		}
	}
	sort.Strings(names) // deterministic load order regardless of FS iteration

	var out []Rule
	for _, name := range names {
		path := dir + "/" + name
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", name, err)
		}
		var doc struct {
			Rules []Rule `yaml:"rules"`
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("%s: parsing YAML: %w", name, err)
		}
		for _, r := range doc.Rules {
			if err := validateRule(r); err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			out = append(out, r)
		}
	}
	return out, nil
}

var validSeverity = map[findings.Severity]bool{
	findings.SeverityCritical: true,
	findings.SeverityHigh:     true,
	findings.SeverityMedium:   true,
	findings.SeverityLow:      true,
	findings.SeverityInfo:     true,
}

var validDimension = map[findings.Dimension]bool{
	findings.DimensionSecurity:   true,
	findings.DimensionReview:     true,
	findings.DimensionDB:         true,
	findings.DimensionComplexity: true,
	findings.DimensionPractices:  true,
	findings.DimensionTests:      true,
}

// validateRule enforces the rule contract before compilation. The id is checked
// first so every later message can name the offending rule.
func validateRule(r Rule) error {
	if r.ID == "" {
		return fmt.Errorf("rule with empty id (message %q): every rule needs a stable id", r.Message)
	}
	if r.Message == "" {
		return fmt.Errorf("rule %s: message is required (it is shown to the developer)", r.ID)
	}
	if !validSeverity[r.Severity] {
		return fmt.Errorf("rule %s: invalid severity %q (want critical|high|medium|low|info)", r.ID, r.Severity)
	}
	if !validDimension[r.Dimension] {
		return fmt.Errorf("rule %s: invalid dimension %q (want security|review|db|complexity|practices|tests)", r.ID, r.Dimension)
	}
	if r.Pattern == "" && len(r.PatternEither) == 0 {
		return fmt.Errorf("rule %s: needs a pattern or pattern-either (a rule without a base pattern matches nothing)", r.ID)
	}
	return nil
}
