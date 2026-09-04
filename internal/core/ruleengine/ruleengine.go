package ruleengine

import "github.com/codefit-cli/codefit/internal/core/findings"

// Rule is a deterministic detection rule in the supported Semgrep-format subset
// (PRD section 17). A rule carries its finding metadata plus one or more pattern
// operators; the operators are mutually composable (patterns = AND of its
// members, PatternEither = OR, etc.).
type Rule struct {
	ID         string             `yaml:"id"`
	Message    string             `yaml:"message"`
	Suggestion string             `yaml:"suggestion"`
	Severity   findings.Severity  `yaml:"severity"`
	Dimension  findings.Dimension `yaml:"dimension"`

	// Operators (core subset). Exactly the set codefit's matcher supports.
	Pattern           string            `yaml:"pattern,omitempty"`
	PatternEither     []string          `yaml:"pattern-either,omitempty"`
	Patterns          []string          `yaml:"patterns,omitempty"`
	PatternNot        string            `yaml:"pattern-not,omitempty"`
	PatternInside     string            `yaml:"pattern-inside,omitempty"`
	MetavariableRegex map[string]string `yaml:"metavariable-regex,omitempty"`
	// MetavariableName constrains a metavariable by NAME COMPONENT against a
	// named vocabulary (today: "credential"), which a regex cannot express —
	// RE2 has no lookbehind, so "token" cannot be anchored to a camelCase
	// boundary. See [metavarname] in matcher.go.
	MetavariableName map[string]string `yaml:"metavariable-name,omitempty"`
}

// Engine matches rules against a parsed file and emits deterministic findings.
//
// NOT IMPLEMENTED by anything today: the working matcher is the package-level
// [Compile]/[Match] pair over [syntax.Node]. This interface is kept for the
// per-language AST adapter it was drawn for, which is why its ast argument is
// still abstract (any).
type Engine interface {
	// Match runs the rules against a parsed AST and returns the findings.
	Match(rules []Rule, ast any) ([]findings.Finding, error)
}
