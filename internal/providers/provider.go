package providers

import (
	"regexp"
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
	// Limits qualify rules this family DOES cover — see RuleLimit.
	Limits []RuleLimit
}

// RuleLimit records that a rule the provider DOES implement covers less than
// its id suggests, and says exactly how much less.
//
// This is a third kind of fact, distinct from both of its neighbours, and the
// distinction is what keeps a coverage answer honest:
//
//	Declared      — "this rule is covered"
//	ExcludedRule  — "this rule will NEVER be covered", and why
//	RuleLimit     — "this rule IS covered, but not over this shape", and why
//
// Reusing ExcludedRule for a limit would have been ADR 0066's lie pointed
// backwards: SEC-001 is covered, so recording it as an exclusion would invent a
// hole that does not exist, exactly as a phantom exclusion invents one that
// never did.
//
// Limit is expected to be cited BY REFERENCE from wherever the gap actually
// lives (a const beside the code that causes it), never copied. A copied string
// is a second place to be wrong; a compile-time reference cannot drift.
type RuleLimit struct {
	ID    string // rule id the limit qualifies; MUST be in the same RuleSet's Declared
	Limit string // what the rule does not cover, and why that is declared rather than fixed
}

// ValidLimits reports whether every limit is GROUNDED — that is, whether its id
// appears in Declared — and names every orphan that is not.
//
// This is the exact INVERSE of ValidExclusions, and deliberately so. An
// excluded id must not be in Declared, because it names a rule that will never
// exist. A limit id must be IN Declared, because it qualifies a rule that does.
// A limit on an undeclared id is a caveat about nothing, and an agent reading a
// coverage answer has no way to tell that from a caveat about something.
func (r RuleSet) ValidLimits() (ok bool, orphan []string) {
	for _, l := range r.Limits {
		if !slices.Contains(r.Declared, l.ID) {
			orphan = append(orphan, l.ID)
		}
	}
	return len(orphan) == 0, orphan
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

// ruleIDShapePattern matches exactly one rule id in the "<PREFIX>-<digits>"
// shape every id in this codebase uses (e.g. "SEC-001", "PRAC-004").
var ruleIDShapePattern = regexp.MustCompile(`^([A-Z]+)-([0-9]+)$`)

// familyIDShape derives the "<PREFIX>-<N digits>" shape shared by every id in
// declared (e.g. ["SEC-001","SEC-010"] -> prefix "SEC", width 3), or
// ok==false if declared is empty, contains an id outside the shape, or the
// ids do not share one uniform prefix and digit width — in which case no
// shape claim can be honestly derived from them.
func familyIDShape(declared []string) (prefix string, width int, ok bool) {
	if len(declared) == 0 {
		return "", 0, false
	}
	for i, id := range declared {
		m := ruleIDShapePattern.FindStringSubmatch(id)
		if m == nil {
			return "", 0, false
		}
		p, digits := m[1], m[2]
		if i == 0 {
			prefix, width = p, len(digits)
			continue
		}
		if p != prefix || len(digits) != width {
			return "", 0, false
		}
	}
	return prefix, width, true
}

// ValidExclusionSource is the phantom-exclusion check (C7) — the gap
// sdd-verify found by mutation: ValidExclusions (C6) only checks that an
// excluded id is not simultaneously Declared, never whether it ever
// corresponded to a real rule. Renaming a real excluded id to a fabricated
// marker (e.g. "PRAC-999-NEVER-EXISTED") left C6, and every other existing
// check, green.
//
// This mirrors ADR 0057's own finding about internal/core/dbcoverage
// (mirrored in reverse here: that ADR's Control B forbids the manifest
// claiming a capability that does not exist; a phantom exclusion is the same
// lie pointed the other way — a hole the manifest claims exists but never
// did) and follows the SAME epistemic split its Control C draws: the check
// is built ONLY where a real rule source exists to check against, and
// declared as impossible-for-now everywhere else, never faked in either
// direction.
//
//   - r.Enumerable == true (e.g. TypeScript's YAML-backed security rules):
//     Control A (typescript/control_a_test.go) already proves Declared is
//     the EXACT set the real rule loader produces, so the "<PREFIX>-<digits>"
//     shape every Declared id shares is itself grounded in that real source.
//     ValidExclusionSource requires every Excluded id to match that same
//     shape, and reports every one that does not.
//   - r.Enumerable == false (e.g. Go's hand-written PRAC/SEC literals, which
//     have no All()/ID() loader): Declared's own shape is unverified, so
//     deriving a pattern from it and calling that "checked against the real
//     rule source" would be exactly the over-promise this check exists to
//     prevent. ValidExclusionSource returns (true, nil) — not applicable, no
//     claim made — rather than silently skip; see ADR 0066.
//
// This is a correspondence check, never an accuracy one (the same
// discipline ADR 0057's dbcoverage Controls B/C draw): it cannot prove an
// excluded id was ever actually built or considered — a fabricated id that
// happens to share the family's shape (e.g. "SEC-999") still passes. It
// catches exactly the defect sdd-verify's mutation produced: an id that does
// not even look like a member of the family.
func (r RuleSet) ValidExclusionSource() (ok bool, phantom []string) {
	if !r.Enumerable || len(r.Excluded) == 0 {
		return true, nil
	}
	prefix, width, derivable := familyIDShape(r.Declared)
	if !derivable {
		return true, nil
	}
	for _, ex := range r.Excluded {
		m := ruleIDShapePattern.FindStringSubmatch(ex.ID)
		if m == nil || m[1] != prefix || len(m[2]) != width {
			phantom = append(phantom, ex.ID)
		}
	}
	return len(phantom) == 0, phantom
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
	// for this ecosystem (RF-10), overridable in .codefit.yaml.
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
