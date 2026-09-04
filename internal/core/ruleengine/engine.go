package ruleengine

import (
	"fmt"
	"github.com/codefit-cli/codefit/internal/core/namematch"
	"regexp"
	"sort"
	"strings"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/syntax"
)

// CompiledRule is a Rule whose pattern strings have been parsed to syntax.Node
// (once, at load time) and whose metavariable regexes are compiled. The
// ruleengine never parses source itself — Compile receives a parse function
// from the active provider, so the engine stays parser-agnostic.
type CompiledRule struct {
	rule    Rule
	pattern syntax.Node   // the unwrapped node for `pattern`
	either  []syntax.Node // `pattern-either` (OR of base patterns)
	not     syntax.Node   // `pattern-not` (exclusion)
	inside  syntax.Node   // `pattern-inside` (the match must be within this)
	// constraints are the per-metavariable gates from metavariable-regex and
	// metavariable-name, merged. See [metavarname] in matcher.go.
	constraints constraints
}

// Compile parses each rule's pattern(s) via parse (a provider's parser, which
// emits syntax.Node) and compiles the metavariable regexes.
//
// Compilation is strict and located, the same contract [LoadFS] enforces on the
// YAML: a pattern that does not parse fails the whole compile with an error
// naming the rule id, the operator, and the offending pattern text. It is not
// enough to trust parse's error — see [compilePattern] for why a nil error does
// not mean the pattern parsed.
func Compile(rules []Rule, parse func(src string) (syntax.Node, error)) ([]CompiledRule, error) {
	out := make([]CompiledRule, 0, len(rules))
	for _, r := range rules {
		cr := CompiledRule{rule: r}
		if r.Pattern != "" {
			node, err := compilePattern(r.Pattern, parse)
			if err != nil {
				return nil, fmt.Errorf("rule %s: %w", r.ID, err)
			}
			cr.pattern = node
		}
		for _, p := range r.PatternEither {
			node, err := compilePattern(p, parse)
			if err != nil {
				return nil, fmt.Errorf("rule %s: pattern-either: %w", r.ID, err)
			}
			cr.either = append(cr.either, node)
		}
		if r.PatternNot != "" {
			node, err := compilePattern(r.PatternNot, parse)
			if err != nil {
				return nil, fmt.Errorf("rule %s: pattern-not: %w", r.ID, err)
			}
			cr.not = node
		}
		if r.PatternInside != "" {
			node, err := compilePattern(r.PatternInside, parse)
			if err != nil {
				return nil, fmt.Errorf("rule %s: pattern-inside: %w", r.ID, err)
			}
			cr.inside = node
		}
		if n := len(r.MetavariableRegex) + len(r.MetavariableName); n > 0 {
			cr.constraints = make(constraints, n)
		}
		for mv, expr := range r.MetavariableRegex {
			re, err := regexp.Compile(expr)
			if err != nil {
				return nil, fmt.Errorf("rule %s: metavariable-regex %q: %w", r.ID, expr, err)
			}
			c := cr.constraints[mv]
			c.re = re
			cr.constraints[mv] = c
		}
		for mv, vocab := range r.MetavariableName {
			build, known := metavarVocabularies[vocab]
			if !known {
				return nil, fmt.Errorf("rule %s: metavariable-name %s: unknown vocabulary %q (known: %s) — "+
					"a rule that named an unknown vocabulary would compile cleanly and then match "+
					"nothing, and nobody would ever be told",
					r.ID, mv, vocab, strings.Join(knownVocabularies(), ", "))
			}
			c := cr.constraints[mv]
			c.set = build()
			cr.constraints[mv] = c
		}
		out = append(out, cr)
	}
	return out, nil
}

// compilePattern parses a pattern string and unwraps it to the meaningful node
// (a pattern like "foo($X)" parses as program > expression_statement >
// call_expression; we match the call, not the wrappers).
//
// A nil error from parse is NOT proof the pattern parsed. tree-sitter — the
// parser behind every syntax.Node today — is error-RECOVERING: given a pattern
// it cannot parse it returns a TREE containing ERROR nodes rather than failing,
// so err is nil and the pattern looks fine. The compiled rule then matches
// nothing, forever, in silence. syntax.Node.HasError is the signal that reports
// this, and consulting it here is the gate that makes loader.go's promise true
// for the compile step as well: a broken rule can never enter the engine
// silently, because a silent rule is a silent vulnerability.
func compilePattern(src string, parse func(string) (syntax.Node, error)) (syntax.Node, error) {
	root, err := parse(src)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, fmt.Errorf("pattern %q: the parser returned no tree and no error", src)
	}
	if root.HasError() {
		return nil, fmt.Errorf("pattern %q does not parse (the parsed tree contains a syntax error): "+
			"the parser recovers instead of failing, so this pattern would compile cleanly and then "+
			"match nothing — fix the pattern's syntax", src)
	}
	return unwrap(root), nil
}

// unwrap descends through single-child trivial wrappers to the significant
// pattern node. program and expression_statement wrap any top-level pattern;
// parenthesized_expression wraps a pattern written in parentheses, which lets a
// rule isolate a sub-expression as its pattern (e.g. an object literal:
// "({__html: $A + $B})" unwraps to the object so it matches that object wherever
// it occurs, regardless of the syntax around it). Parentheses are semantically
// transparent — "(expr)" is "expr" — so peeling them never changes meaning.
func unwrap(n syntax.Node) syntax.Node {
	for n != nil && isTrivialWrapper(n.Type()) && n.NamedChildCount() == 1 {
		n = n.NamedChild(0)
	}
	return n
}

func isTrivialWrapper(typ string) bool {
	switch typ {
	case "program", "expression_statement", "parenthesized_expression":
		return true
	default:
		return false
	}
}

// Match runs the compiled rules over the tree rooted at root and returns the
// findings, located by file and line. file is the project-relative path.
func Match(rules []CompiledRule, root syntax.Node, file string) []findings.Finding {
	var out []findings.Finding
	for _, cr := range rules {
		// pattern-inside: precompute the byte ranges of every node matching the
		// inside pattern; a match counts only if its range is contained in one.
		var insideRanges [][2]int
		if cr.inside != nil {
			walk(root, func(n syntax.Node) {
				if matchNode(cr.inside, n, map[string]string{}, nil) {
					insideRanges = append(insideRanges, [2]int{n.StartByte(), n.EndByte()})
				}
			})
		}
		walk(root, func(node syntax.Node) {
			binds, ok := evalRule(cr, node)
			if !ok {
				return
			}
			if cr.inside != nil && !containedInAny(node, insideRanges) {
				return
			}
			out = append(out, cr.finding(node, file, binds))
		})
	}
	return out
}

// containedInAny reports whether node's byte range is contained in any of ranges
// — the pattern-inside test, computed purely from byte offsets so it works
// identically for any parser behind syntax.Node.
func containedInAny(node syntax.Node, ranges [][2]int) bool {
	s, e := node.StartByte(), node.EndByte()
	for _, r := range ranges {
		if r[0] <= s && e <= r[1] {
			return true
		}
	}
	return false
}

// evalRule reports whether a rule matches at node, returning the bindings. The
// base match is `pattern` or, if set, any of `pattern-either` (OR). The match is
// then filtered by `pattern-not` (exclusion) and `metavariable-regex`.
func evalRule(cr CompiledRule, node syntax.Node) (map[string]string, bool) {
	binds, ok := baseMatch(cr, node)
	if !ok {
		return nil, false
	}
	// pattern-not: if the node also matches the excluded pattern, drop it.
	if cr.not != nil && matchNode(cr.not, node, map[string]string{}, nil) {
		return nil, false
	}
	// metavariable-regex: each named metavariable's bound text must match.
	// A metavariable named by a constraint but never bound is a REJECTION, not a
	// pass: the rule asked a question about something the match never produced.
	for mv, c := range cr.constraints {
		text, ok := binds[mv]
		if !ok || !c.ok(text) {
			return nil, false
		}
	}
	return binds, true
}

// baseMatch matches the rule's base pattern (or any pattern-either alternative).
func baseMatch(cr CompiledRule, node syntax.Node) (map[string]string, bool) {
	if cr.pattern != nil {
		binds := map[string]string{}
		if matchNode(cr.pattern, node, binds, cr.constraints) {
			return binds, true
		}
		return nil, false
	}
	for _, alt := range cr.either {
		binds := map[string]string{}
		if matchNode(alt, node, binds, cr.constraints) {
			return binds, true
		}
	}
	return nil, false
}

func (cr CompiledRule) finding(node syntax.Node, file string, _ map[string]string) findings.Finding {
	return findings.Finding{
		ID:          cr.rule.ID,
		Dimension:   cr.rule.Dimension,
		Severity:    cr.rule.Severity,
		File:        file,
		Line:        node.StartLine(),
		Title:       cr.rule.Message,
		Description: cr.rule.Message,
		Suggestion:  cr.rule.Suggestion,
		Confidence:  1.0,
	}
}

// walk visits node and every descendant (pre-order).
func walk(n syntax.Node, visit func(syntax.Node)) {
	if n == nil {
		return
	}
	visit(n)
	for i := 0; i < n.NamedChildCount(); i++ {
		walk(n.NamedChild(i), visit)
	}
}

// metavarVocabularies is the closed set of name vocabularies a rule may ask for.
// It is closed on purpose: an open registry would let a rule name anything and
// discover at runtime that it matches nothing. Each entry is a function rather
// than a value because a vocabulary is built per call and must not be shared
// mutable state between rules.
var metavarVocabularies = map[string]func() map[string]bool{
	"credential": namematch.Credential,
}

func knownVocabularies() []string {
	out := make([]string, 0, len(metavarVocabularies))
	for k := range metavarVocabularies {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
