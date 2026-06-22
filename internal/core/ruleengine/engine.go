package ruleengine

import (
	"fmt"
	"regexp"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/syntax"
)

// CompiledRule is a Rule whose pattern strings have been parsed to syntax.Node
// (once, at load time) and whose metavariable regexes are compiled. The
// ruleengine never parses source itself — Compile receives a parse function
// from the active provider, so the engine stays parser-agnostic.
type CompiledRule struct {
	rule      Rule
	pattern   syntax.Node            // the unwrapped node for `pattern`
	metavarRe map[string]*regexp.Regexp
}

// Compile parses each rule's pattern(s) via parse (a provider's parser, which
// emits syntax.Node) and compiles the metavariable regexes.
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
		if len(r.MetavariableRegex) > 0 {
			cr.metavarRe = make(map[string]*regexp.Regexp, len(r.MetavariableRegex))
			for mv, expr := range r.MetavariableRegex {
				re, err := regexp.Compile(expr)
				if err != nil {
					return nil, fmt.Errorf("rule %s: metavariable-regex %q: %w", r.ID, expr, err)
				}
				cr.metavarRe[mv] = re
			}
		}
		out = append(out, cr)
	}
	return out, nil
}

// compilePattern parses a pattern string and unwraps it to the meaningful node
// (a pattern like "foo($X)" parses as program > expression_statement >
// call_expression; we match the call, not the wrappers).
func compilePattern(src string, parse func(string) (syntax.Node, error)) (syntax.Node, error) {
	root, err := parse(src)
	if err != nil {
		return nil, err
	}
	return unwrap(root), nil
}

// unwrap descends through single-child program/expression_statement wrappers to
// the significant pattern node.
func unwrap(n syntax.Node) syntax.Node {
	for n != nil && (n.Type() == "program" || n.Type() == "expression_statement") && n.NamedChildCount() == 1 {
		n = n.NamedChild(0)
	}
	return n
}

// Match runs the compiled rules over the tree rooted at root and returns the
// findings, located by file and line. file is the project-relative path.
func Match(rules []CompiledRule, root syntax.Node, file string) []findings.Finding {
	var out []findings.Finding
	for _, cr := range rules {
		walk(root, func(node syntax.Node) {
			if binds, ok := evalRule(cr, node); ok {
				out = append(out, cr.finding(node, file, binds))
			}
		})
	}
	return out
}

// evalRule reports whether a rule matches at node, returning the bindings.
func evalRule(cr CompiledRule, node syntax.Node) (map[string]string, bool) {
	if cr.pattern == nil {
		return nil, false
	}
	binds := map[string]string{}
	if !matchNode(cr.pattern, node, binds) {
		return nil, false
	}
	// metavariable-regex: each named metavariable's bound text must match.
	for mv, re := range cr.metavarRe {
		text, ok := binds[mv]
		if !ok || !re.MatchString(text) {
			return nil, false
		}
	}
	return binds, true
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
