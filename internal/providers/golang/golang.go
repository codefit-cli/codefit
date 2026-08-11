// Package golang is the Go LanguageProvider. It backs its analysis with the
// stdlib go/ast parser (no CGO, no external dependency — see ADR 0001) and is
// codefit's self-audit bootstrap: codefit audits its own Go code from day one.
package golang

import (
	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/providers"
)

// Provider implements providers.LanguageProvider for Go. authzHelpers holds the
// project's registered custom authz helper names (ADR 0013), threaded in by the
// registry so the "authz" surface item's known_authz_detected fact reflects
// per-project knowledge instead of a built-in vocabulary Go has none of. Empty
// for the stateless file-level surface tools; populated by the project-scan
// path from the baseline.
type Provider struct {
	authzHelpers map[string]bool
}

// Option configures a Provider at construction.
type Option func(*Provider)

// WithAuthzHelpers registers project-specific authorization helper names the
// provider recognizes in a handler body. The names come from the committed
// baseline (a human-approved decision), never from guessing — Go has no
// built-in helper vocabulary (unlike TypeScript's NextAuth-style set): a
// invented name list would be exactly the name-driven over-promise CLAUDE.md
// flags as a known-limit smell.
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

// New returns a Go language provider. With no options it recognizes no authz
// helpers (the stateless default — every existing New() call site keeps
// compiling since Option is variadic).
func New(opts ...Option) *Provider {
	p := &Provider{}
	for _, o := range opts {
		o(p)
	}
	return p
}

// compile-time check that Provider satisfies the contract.
var _ providers.LanguageProvider = (*Provider)(nil)

func (*Provider) Language() string { return "go" }

func (*Provider) Frameworks() []string {
	return []string{"stdlib", "gin", "echo", "chi", "fiber"}
}

func (*Provider) FileExtensions() []string { return []string{".go"} }

func (*Provider) DefaultPathCriticality() config.PathCriticality {
	return config.PathCriticality{
		Production: []string{"**/*.go"},
		Test:       []string{"**/*_test.go"},
	}
}

// AnalyzeSecurity runs the static (go/ast) security checks for Go.
func (*Provider) AnalyzeSecurity(src providers.SourceFile) ([]findings.Finding, error) {
	p, err := parse(src.Path, src.Content)
	if err != nil {
		return nil, err
	}
	return securityChecks(p), nil
}

// AnalyzePractices runs the Go best-practice checks.
func (*Provider) AnalyzePractices(src providers.SourceFile) ([]findings.Finding, error) {
	p, err := parse(src.Path, src.Content)
	if err != nil {
		return nil, err
	}
	return practiceChecks(p), nil
}

// AnalyzeSurface maps the auditable structural surface of a Go file.
func (pr *Provider) AnalyzeSurface(src providers.SourceFile) ([]findings.SurfaceItem, error) {
	p, err := parse(src.Path, src.Content)
	if err != nil {
		return nil, err
	}
	return surfaceItems(p, pr.authzHelpers), nil
}
