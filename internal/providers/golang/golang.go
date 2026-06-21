// Package golang is the Go LanguageProvider. It backs its analysis with the
// stdlib go/ast parser (no CGO, no external dependency — see ADR 0001) and is
// codefit's self-audit bootstrap: codefit audits its own Go code from day one.
package golang

import (
	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/providers"
)

// Provider implements providers.LanguageProvider for Go.
type Provider struct{}

// New returns a Go language provider.
func New() *Provider { return &Provider{} }

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
func (*Provider) AnalyzeSurface(src providers.SourceFile) ([]findings.SurfaceItem, error) {
	p, err := parse(src.Path, src.Content)
	if err != nil {
		return nil, err
	}
	return surfaceItems(p), nil
}
