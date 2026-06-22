package providers

import (
	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/core/findings"
)

// SourceFile is the input to a provider's analysis: a project-relative path and
// the file's raw content.
type SourceFile struct {
	Path    string
	Content []byte
}

// LanguageProvider is the contract every supported language implements. The
// core depends only on this interface, never on a concrete language — which is
// what lets codefit scale to new languages without changing the engine.
//
// The provider owns its parser (go/ast for Go, tree-sitter for TS/Java/Python
// later) and exposes analysis that returns findings, so the interface stays
// parser-agnostic (see ADR 0001).
type LanguageProvider interface {
	// Identity.
	Language() string         // "go", "typescript", "java", "python"
	Frameworks() []string     // recognized frameworks
	FileExtensions() []string // e.g. [".go"], [".ts", ".tsx"]

	// DefaultPathCriticality returns sensible production/test/example defaults
	// for this ecosystem (RF-11), overridable in .codefit.yaml.
	DefaultPathCriticality() config.PathCriticality

	// AnalyzeSecurity runs the provider's language-specific static security
	// analysis (the AST layer of the pyramid) and returns deterministic
	// findings with their natural, pre-path-criticality severity.
	AnalyzeSecurity(src SourceFile) ([]findings.Finding, error)

	// AnalyzePractices runs the provider's best-practice checks.
	AnalyzePractices(src SourceFile) ([]findings.Finding, error)

	// AnalyzeSurface maps the auditable structural surface of a file (PRD
	// section 10): it enumerates, per category, every structure the agent
	// should reason about (e.g. HTTP handlers to verify authorization on). It
	// does not judge whether an item is vulnerable.
	//
	// Provisional: this parser-agnostic, provider-owns-analysis shape (ADR 0001)
	// is revisited in Fase 1 against the real TypeScript provider, where a
	// declarative SurfaceQuery model may replace it.
	AnalyzeSurface(src SourceFile) ([]findings.SurfaceItem, error)
}
