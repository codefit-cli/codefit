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

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/syntax"
	"github.com/codefit-cli/codefit/internal/providers"
)

// Provider implements providers.LanguageProvider for TypeScript and TSX.
type Provider struct{}

// New returns a TypeScript language provider.
func New() *Provider { return &Provider{} }

// compile-time check that Provider satisfies the contract.
var _ providers.LanguageProvider = (*Provider)(nil)

func (*Provider) Language() string { return "typescript" }

func (*Provider) Frameworks() []string {
	return []string{"react", "next", "express", "nestjs", "node"}
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

// --- Rules / surface: STUB (implemented in Prompts 1.2 / 1.3) ---

// AnalyzeSecurity is a stub; the deterministic security rules arrive in Prompt 1.2.
func (*Provider) AnalyzeSecurity(providers.SourceFile) ([]findings.Finding, error) {
	return nil, nil
}

// AnalyzePractices is a stub; the best-practice rules arrive in Prompt 1.2.
func (*Provider) AnalyzePractices(providers.SourceFile) ([]findings.Finding, error) {
	return nil, nil
}

// AnalyzeSurface is a stub; surface mapping arrives in Prompt 1.3.
func (*Provider) AnalyzeSurface(providers.SourceFile) ([]findings.SurfaceItem, error) {
	return nil, nil
}
