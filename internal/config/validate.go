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
