package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Allowed enum values for validated config fields.
var (
	allowedLanguages  = []string{"typescript", "java", "python", "go"}
	allowedFrameworks = []string{"react", "next", "express", "spring", "fastapi", "django"}
	allowedParadigms  = []string{"oltp", "olap", "mixed", "auto"}
	// "sqlserver" (not "mssql") is the resolved config value for T-SQL — matches
	// sqlddl.Dialect.Name and the sqlddl.SQLServer() constructor naming, so the
	// config vocabulary stays consistent with the descriptor naming (design §5,
	// H1 decision).
	allowedDBTypes = []string{"postgresql", "mysql", "sqlserver", "sqlite", "none"}
	// The three RF-10 test-path severity modes. "keep" is deliberately IN the
	// list: refusing a mode the PRD names would be codefit overriding the
	// developer's decision, which the autonomy principle forbids. Its
	// consequence (a critical security finding can survive on a test path and
	// block) is informed at materialisation by the security sensor, not by a
	// validation error here.
	allowedTestSeverities = []string{TestSeverityInfo, TestSeverityDowngrade, TestSeverityKeep}
)

// validate checks required fields and enum values, returning located
// (path:line) errors built from the source YAML node so the user can jump
// straight to the offending line.
func validate(cfg *Config, root *yaml.Node, src string) error {
	var errs []error
	add := func(msg string, keys ...string) {
		errs = append(errs, locatedErr(src, root, msg, keys...))
	}

	if cfg.Project.Name == "" {
		add("project name is required", "project", "name")
	}
	switch {
	case cfg.Project.Language == "":
		add("project language is required", "project", "language")
	case !slices.Contains(allowedLanguages, cfg.Project.Language):
		add(fmt.Sprintf("invalid language %q (allowed: %s)",
			cfg.Project.Language, strings.Join(allowedLanguages, ", ")),
			"project", "language")
	}
	if f := cfg.Project.Framework; f != "" && !slices.Contains(allowedFrameworks, f) {
		add(fmt.Sprintf("invalid framework %q (allowed: %s)",
			f, strings.Join(allowedFrameworks, ", ")),
			"project", "framework")
	}
	if p := cfg.Database.Paradigm; p != "" && !slices.Contains(allowedParadigms, p) {
		add(fmt.Sprintf("invalid database paradigm %q (allowed: %s)",
			p, strings.Join(allowedParadigms, ", ")),
			"database", "paradigm")
	}
	if dt := cfg.Database.Type; dt != "" && !slices.Contains(allowedDBTypes, dt) {
		add(fmt.Sprintf("invalid database type %q (allowed: %s)",
			dt, strings.Join(allowedDBTypes, ", ")),
			"database", "type")
	}
	// An unrecognised test_severity STOPS the load. Resolving it to the default
	// would leave the developer believing test findings were re-weighted the way
	// they asked while codefit did something else — a config that parses and
	// lies. TestSeverityMode's fallback covers only hand-built Configs, which
	// never pass through here.
	if ts := cfg.Sensors.Security.TestSeverity; ts != "" && !slices.Contains(allowedTestSeverities, ts) {
		add(fmt.Sprintf("invalid sensors.security.test_severity %q (allowed: %s)",
			ts, strings.Join(allowedTestSeverities, ", ")),
			"sensors", "security", "test_severity")
	}
	// The sum-to-100 rule stays even though a PARTIAL map is a real,
	// supported case since roadmap P1-2 (scoring.ResolveWeights uses exactly
	// the dimensions the user named, never padded by the defaults). It is
	// deliberately kept, not relaxed to "just be positive":
	//
	//   - scoring.Compute normalizes by the WEIGHT SUM OF THE MEASURED
	//     dimensions, not by a hardcoded 100 — so sum-to-100 is not required
	//     for the arithmetic to be correct. It is required for the NUMBERS
	//     TO MEAN WHAT THEY LOOK LIKE THEY MEAN: {security: 80, db: 20} reads
	//     as an 80/20 split only if 80 and 20 are already percentage points.
	//     Drop the constraint and {security: 5000} becomes "valid", producing
	//     a coherent-looking but meaningless score.
	//   - It gives validation a fixed, unambiguous target instead of an
	//     open-ended one ("must be positive", "must not overflow", ...) that
	//     would need its own new rules for what counts as a "reasonable"
	//     partial sum.
	//   - It matches DefaultWeights()'s own doc comment and its own test
	//     (TestDefaultWeights_SumIsExactly100) — a user's map and codefit's
	//     defaults share one contract, not two.
	//
	// Completeness (does the map name every dimension THIS scan will
	// measure) is deliberately NOT checked here: validation cannot know in
	// advance which dimensions a given project measures (db only runs when
	// schema_paths is configured and in scope), so requiring every one of
	// the six declared dimensions here would force users to weight
	// dimensions their project may never run. That check belongs, and lives,
	// at scan time (scoring.MissingWeights, internal/mcp/scanall.go), where
	// the actual measured set is known and the error can name exactly what
	// is missing.
	if w := cfg.Report.ScoreWeights; len(w) > 0 {
		sum := 0
		for _, v := range w {
			sum += v
		}
		if sum != 100 {
			add(fmt.Sprintf("report.score_weights must sum to 100, got %d", sum),
				"report", "score_weights")
		}
	}
	return errors.Join(errs...)
}

// locatedErr formats an error prefixed with src:line when the key can be
// located in the source node, or src otherwise.
func locatedErr(src string, root *yaml.Node, msg string, keys ...string) error {
	if line := lineOf(root, keys...); line > 0 {
		return fmt.Errorf("%s:%d: %s", src, line, msg)
	}
	return fmt.Errorf("%s: %s", src, msg)
}

// lineOf walks the YAML mapping nodes following keys and returns the source
// line of the last key reached (0 if the path is absent).
func lineOf(root *yaml.Node, keys ...string) int {
	node := root
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	line := 0
	for _, key := range keys {
		if node.Kind != yaml.MappingNode {
			return line
		}
		var next *yaml.Node
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == key {
				line = node.Content[i].Line
				next = node.Content[i+1]
				break
			}
		}
		if next == nil {
			return line
		}
		node = next
	}
	return line
}
