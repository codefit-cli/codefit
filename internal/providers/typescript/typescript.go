// Package typescript is the TypeScript/TSX LanguageProvider. It parses with
// gotreesitter (pure Go, no CGO — see ADR 0002) and adapts its AST to the
// core's parser-agnostic syntax.Node (ADR 0003).
//
// This phase (Prompt 1.1) implements identity + parsing only. The
// deterministic rules and surface mapping (AnalyzeSecurity/Practices/Surface)
// are stubs here; they arrive in Prompts 1.2/1.3.
package typescript

import (
	"fmt"
	"strings"
	"sync"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/ruleengine"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/core/syntax"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/rules"
)

// Provider implements providers.LanguageProvider for TypeScript and TSX.
// authzHelpers holds the project's registered custom authz helpers (in addition to
// the built-in NextAuth-style set), so surface recognition reflects per-project
// knowledge without the agent re-reasoning (ADR 0013). Empty for the stateless
// file-level surface tools; populated by the project-scan path from the baseline.
type Provider struct {
	authzHelpers map[string]bool
}

// Option configures a Provider at construction.
type Option func(*Provider)

// WithAuthzHelpers registers project-specific authorization helper names the
// provider recognizes in addition to the built-in set. The names come from the
// committed baseline (a human-approved decision), never from guessing.
func WithAuthzHelpers(names []string) Option {
	return func(p *Provider) {
		if len(names) == 0 {
			return
		}
		if p.authzHelpers == nil {
			p.authzHelpers = make(map[string]bool, len(names))
		}
		for _, n := range names {
			p.authzHelpers[n] = true
		}
	}
}

// New returns a TypeScript language provider. With no options it recognizes only
// the built-in authz helpers (the stateless default).
func New(opts ...Option) *Provider {
	p := &Provider{}
	for _, o := range opts {
		o(p)
	}
	return p
}

// compile-time check that Provider satisfies the contract.
var _ providers.LanguageProvider = (*Provider)(nil)

func (*Provider) Language() string { return "typescript" }

func (*Provider) Frameworks() []string {
	return []string{"react", "next", "express", "fastify", "nestjs", "node"}
}

func (*Provider) FileExtensions() []string { return []string{".ts", ".tsx"} }

func (*Provider) DefaultPathCriticality() config.PathCriticality {
	return config.PathCriticality{
		Production: []string{"src/**"},
		Test:       []string{"**/*.test.ts", "**/*.test.tsx", "**/*.spec.ts", "**/*.spec.tsx"},
		Example:    []string{"examples/**", "docs/**"},
	}
}

// Parse parses a TypeScript (.ts) or TSX (.tsx) file and returns the root of the
// parser-agnostic AST. The concrete gotreesitter tree is hidden behind
// syntax.Node so the core never depends on the parser.
func (*Provider) Parse(src providers.SourceFile) (syntax.Node, error) {
	lang := grammars.TypescriptLanguage()
	if strings.HasSuffix(src.Path, ".tsx") {
		lang = grammars.TsxLanguage()
	}
	tree, err := ts.NewParser(lang).Parse(src.Content)
	if err != nil {
		return nil, fmt.Errorf("parsing %q: %w", src.Path, err)
	}
	return tsNode{n: tree.RootNode(), lang: lang, src: src.Content}, nil
}

// --- Security rules (Prompt 1.2) ---

// securityRules compiles the embedded TypeScript security rules exactly once for
// the process. Compilation parses each pattern with this provider, so the rules
// and the analysed code share one parser; a malformed rule fails the whole load
// loudly (it is a packaging bug, not a per-file condition).
var (
	secRulesOnce sync.Once
	secRules     []ruleengine.CompiledRule
	secRulesErr  error
)

func securityRules() ([]ruleengine.CompiledRule, error) {
	secRulesOnce.Do(func() {
		raw, err := ruleengine.LoadFS(rules.FS, "typescript/security")
		if err != nil {
			secRulesErr = fmt.Errorf("loading typescript security rules: %w", err)
			return
		}
		compiled, err := ruleengine.Compile(raw, parsePattern)
		if err != nil {
			secRulesErr = fmt.Errorf("compiling typescript security rules: %w", err)
			return
		}
		secRules = compiled
	})
	return secRules, secRulesErr
}

// parsePattern parses a rule's pattern string into a syntax.Node. Patterns are
// TypeScript expressions; the .ts grammar is used (JSX patterns, needed only by
// the XSS rule, are handled when that rule lands).
func parsePattern(src string) (syntax.Node, error) {
	return (&Provider{}).Parse(providers.SourceFile{Path: "pattern.ts", Content: []byte(src)})
}

// AnalyzeSecurity runs the embedded deterministic security rules over src and
// returns the findings. The provider is a thin adapter: it loads/compiles the
// rules (once), parses the file, and hands both to the language-agnostic
// ruleengine — no detection logic lives here.
func (p *Provider) AnalyzeSecurity(src providers.SourceFile) ([]findings.Finding, error) {
	compiled, err := securityRules()
	if err != nil {
		return nil, err
	}
	root, err := p.Parse(src)
	if err != nil {
		return nil, err
	}
	return ruleengine.Match(compiled, root, src.Path), nil
}

// AnalyzePractices is a stub; the best-practice rules arrive in Prompt 1.2.
func (*Provider) AnalyzePractices(providers.SourceFile) ([]findings.Finding, error) {
	return nil, nil
}

// AnalyzeSurface maps the auditable structural surface of a TypeScript file by
// running the provider's surface queries through the agnostic framework, which
// stamps the stable ids. Today only IDOR (Next App Router) is wired; authz and
// over-fetching are added as their queries land.
func (p *Provider) AnalyzeSurface(src providers.SourceFile) ([]findings.SurfaceItem, error) {
	root, err := p.Parse(src)
	if err != nil {
		return nil, err
	}
	return surface.Run(p.surfaceQueries(), root, src.Path), nil
}

// surfaceQueries are the surface categories the TS provider can enumerate. The
// recognized authz-helper set is threaded into the IDOR and authz queries so the
// known_authz_detected fact reflects this project's registered helpers.
func (p *Provider) surfaceQueries() []surface.Query {
	return []surface.Query{
		idorQuery{recognized: p.authzHelpers},
		authzQuery{recognized: p.authzHelpers},
		overfetchQuery{},
		nplus1Query{},
	}
}
