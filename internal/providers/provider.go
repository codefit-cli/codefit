package providers

import (
	"slices"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// SourceFile is the input to a provider's analysis: a project-relative path and
// the file's raw content.
type SourceFile struct {
	Path    string
	Content []byte
}

// ExcludedRule names a rule id a provider will PERMANENTLY not implement, and
// why. This is a different kind of fact from Declared: Declared says a rule
// IS covered; a permanent drop says it never will be, and silently omitting
// it from a coverage answer is indistinguishable from "not yet" — exactly the
// over-promise this project's coverage manifests exist to prevent (mirrors
// internal/core/dbcoverage's NotCovered() precedent: DB-012 and DW-022 are
// recorded there with their reasons rather than left as an absence). Scoped
// to one rule id rather than free prose, so it can be checked (ValidExclusions)
// instead of only read.
type ExcludedRule struct {
	ID     string // rule id, e.g. "PRAC-004"
	Reason string // why it is permanently not covered
}

// RuleSet is what a provider declares for one deterministic rule family
// (security or practices): the rule IDs it implements, and whether that list
// is derivable from a real rule loader (Enumerable: true, e.g. TypeScript's
// YAML-backed security rules) or a hand-maintained mirror of the provider's
// own Go source (Enumerable: false, e.g. Go's AST-detector rules, which have
// no All()/ID() loader today). Declared is never a count — a count cannot be
// checked against anything; a list of IDs can (Control A, for the
// Enumerable==true case). Excluded names rule ids this family permanently
// will NOT implement, each with why (see ExcludedRule) — checked disjoint
// from Declared by ValidExclusions (C6).
type RuleSet struct {
	Declared   []string // rule IDs, sorted
	Enumerable bool
	Excluded   []ExcludedRule
}

// ValidExclusions reports whether r's Excluded rule ids are disjoint from its
// Declared ones (C6) — a rule id cannot be claimed as both covered and
// permanently excluded; that contradiction would make the declaration
// self-defeating rather than a real fact about the provider.
func (r RuleSet) ValidExclusions() bool {
	for _, ex := range r.Excluded {
		if slices.Contains(r.Declared, ex.ID) {
			return false
		}
	}
	return true
}

// Capability is what a LanguageProvider declares it implements — a fact about
// the provider, independent of which resolvers currently admit it (exposure,
// owned by internal/providers/registry). Surface must be a subset of
// surface.ProviderCategories (C2, checked by ValidSurface); CoverageManifest
// mirrors whether the provider implements the optional CoverageManifest()
// method (C4, checked where the provider is resolved).
type Capability struct {
	Security, Practices RuleSet
	Surface             []surface.Category
	CoverageManifest    bool
}

// ValidSurface reports whether every category in c.Surface is a member of
// surface.ProviderCategories — C2, the guard that keeps a declared Capability
// within the vocabulary D1b locked to the const block in internal/core/surface.
func (c Capability) ValidSurface() bool {
	for _, cat := range c.Surface {
		if !slices.Contains(surface.ProviderCategories, cat) {
			return false
		}
	}
	return true
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

	// Capability declares what this provider implements — its rule IDs per
	// family and its surface category coverage — independent of which
	// resolvers currently expose it (that is internal/providers/registry's
	// Exposure, a separate fact). A provider cannot know which resolvers admit
	// it, so it never declares its own exposure; it only declares what it can
	// do. Every registered provider MUST return a non-zero Capability (C1).
	Capability() Capability

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
