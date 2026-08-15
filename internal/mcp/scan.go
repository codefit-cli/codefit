package mcp

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/coverage"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/scope"
	"github.com/codefit-cli/codefit/internal/core/scoring"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/sensors/security"
)

// ScanRequest is the input to codefit-scan-security: a project root, a language,
// and optionally the files to narrow the audit to.
type ScanRequest struct {
	Root     string `json:"root"`
	Language string `json:"language"`
	// ChangedFiles narrows the audit to these project-relative paths (layer 0 of
	// the filtering pyramid). codefit does not ask git which files changed — it
	// has no power over the user's git, and the calling agent already knows what
	// it touched. Absent or empty means a FULL audit, never "audit nothing", and
	// a narrowed run declares itself in the response's scope block.
	ChangedFiles []string `json:"changed_files,omitempty"`
}

// ScanResponse is the deterministic + surface result over the project (the §11
// contract): flat findings and surface, the dimension score, and whether the
// project is blocked (an unconsented critical security finding).
// Scope declares how much of the project this result describes. It is ALWAYS
// present, so `blocked: false` is never read as a wider claim than it is.
type ScanResponse struct {
	Findings []findings.Finding     `json:"findings"`
	Surface  []findings.SurfaceItem `json:"surface"`
	Score    int                    `json:"score"`
	Blocked  bool                   `json:"blocked"`
	Scope    ScopeBlock             `json:"scope"`
	// SurfaceCoverage declares which of surface.ProviderCategories this
	// language's provider mapped and which it did not (R2,
	// docs/specs/declared-partial-language-exposure.md): a project whose
	// provider maps a narrow slice of the vocabulary (Go today: authz only)
	// must never report surface_items with no statement that the rest was
	// never searched for. ALWAYS present — HandleScanSecurity errors before
	// reaching this point when no provider resolves, so a returned response
	// always has one.
	SurfaceCoverage surface.CoverageStatement `json:"surface_coverage"`
}

// HandleScanSecurity runs the security sensor over the project and returns the
// flat findings + surface. A thin adapter: it resolves the provider, runs the
// sensor (the deterministic rules + the surface queries, already wired there),
// and returns its result — no detection logic lives here.
func HandleScanSecurity(req ScanRequest) (ScanResponse, error) {
	scp := scope.Of(req.ChangedFiles)
	res, err := runSecurity(req.Root, req.Language, recognizedHelpers(req.Root, req.Language), scp)
	if err != nil {
		return ScanResponse{}, err
	}
	block := scopeBlockFor(scp, res.AuditedFiles, res.AuditableTotal)
	if err := block.Validate(); err != nil {
		return ScanResponse{}, err
	}
	return ScanResponse{
		Findings:        res.Findings,
		Surface:         res.Surface,
		Score:           res.Score,
		Blocked:         scoring.IsBlocked(res.Findings),
		Scope:           block,
		SurfaceCoverage: surfaceCoverageFor(req.Language),
	}, nil
}

// surfaceCoverageFor computes the surface coverage statement for the
// language's resolved provider (R2): which of surface.ProviderCategories it
// mapped, and which it did not. Returns the zero CoverageStatement when no
// provider resolves — every production caller only reaches this after a
// provider has already been proven to resolve (runSecurity itself errors
// otherwise), so the zero case is unreachable in practice, not silently
// papered over.
func surfaceCoverageFor(language string) surface.CoverageStatement {
	p := providerForLanguage(language, nil)
	if p == nil {
		return surface.CoverageStatement{}
	}
	return surface.DeriveCoverage(p.Capability().Surface)
}

// runSecurity resolves the provider (recognizing the project's registered authz
// helpers) and runs the security sensor over the project root — the shared body of
// codefit-scan-security, -scan-all, and -scan-endpoint.
// scp is layer 0: the files this pass may analyse. The walk still counts every
// auditable file, so the caller can say how much of the project it left out.
func runSecurity(root, language string, helpers []string, scp scope.Scope) (findings.SensorResult, error) {
	provider := providerForLanguage(language, helpers)
	if provider == nil {
		return findings.SensorResult{}, fmt.Errorf("unsupported language %q", language)
	}
	// A missing config is fine (nil → defaults); a PRESENT but invalid one is a
	// hard error — scanning silently with no path_criticality would be a false
	// "all good", the very anti-pattern codefit exists to catch.
	cfg, err := config.LoadOptional(filepath.Join(root, ".codefit.yaml"))
	if err != nil {
		return findings.SensorResult{}, fmt.Errorf("loading project config: %w", err)
	}
	ctx := auditctx.AuditContext{ProjectRoot: root, Language: language, Config: cfg, Scope: scp}
	res, err := security.New(provider).Run(ctx)
	if err != nil {
		return findings.SensorResult{}, fmt.Errorf("security scan: %w", err)
	}
	return res, nil
}

// securityScope is the baseline category scope of a security-only run: the
// categories the security sensor owns, unioned by the adapter (ADR 0019). It is
// passed to the unified baseline diff/prune so items from a sensor that did not
// run are never marked gone. The provider is irrelevant to OwnedCategories, so a
// default resolution is enough.
func securityScope(language string) map[string]bool {
	return scannedCategories(security.New(providerForLanguage(language, nil)))
}

// CoverageRequest is the input to codefit-coverage.
type CoverageRequest struct {
	Language string `json:"language"`
	// Detail names the entries whose full prose the caller wants. Many ids in
	// one call: an agent that has to make one round trip per declared limit
	// stops asking. Omit it and the answer is the index alone.
	Detail []string `json:"detail,omitempty"`
}

// CoverageResponse carries the coverage index: every entry codefit declares for
// the language, as an id, a one-line claim, its answer class, and whether it has
// more prose to give. The prose itself is served only when asked for by id.
type CoverageResponse struct {
	Language string `json:"language"`
	// Index holds EVERY entry the manifest carries, always. The response budget
	// authorizes withholding for scan-all; for coverage it authorizes nothing,
	// so an over-budget index is reported as over budget and stays complete.
	Index []coverage.IndexEntry `json:"index"`
	// Entries and Bytes let the caller check the two facts it would otherwise
	// have to take on trust: how many entries arrived, and how large the index
	// was on the wire.
	Entries int `json:"entries"`
	Bytes   int `json:"bytes"`
	// Withheld is always zero, and WithheldNote says so in words. "No mention of
	// truncation" and "nothing was truncated" are not the same bytes, and an
	// agent cannot tell a complete answer from a quiet one without being told.
	Withheld     int    `json:"withheld"`
	WithheldNote string `json:"withheld_note"`
	// OverBudget reports that the index exceeded the response budget. Nothing is
	// dropped when it does — the instruction is to shorten a claim.
	OverBudget bool   `json:"over_budget,omitempty"`
	BudgetNote string `json:"budget_note,omitempty"`
	// Detail carries the full prose of the requested entries, byte for byte as
	// authored.
	Detail []coverage.Entry `json:"detail,omitempty"`
	// Unrecognized names every requested id that matched no entry. Naming it is
	// the point: an empty success would say the entry has nothing to declare,
	// which is a different and false answer from "there is no such entry".
	Unrecognized     []string `json:"unrecognized,omitempty"`
	UnrecognizedNote string   `json:"unrecognized_note,omitempty"`
	// Derived is true when the answer was COMPUTED from the provider's
	// Capability() rather than served from a hand-written CoverageManifest()
	// (R1, docs/specs/declared-partial-language-exposure.md): the derived
	// answer is the FLOOR every registered, exposed language gets; a
	// hand-written prose manifest (TypeScript today) stays authoritative and
	// is never replaced by this.
	Derived bool `json:"derived"`
}

// HandleCoverage returns the coverage manifest for the language. A provider
// with a hand-written CoverageManifest() serves it unchanged (Derived: false)
// — the prose manifest stays authoritative. A registered, resolvable provider
// with NO prose manifest still gets a truthful, DERIVED answer from its
// Capability() (Derived: true) — "no coverage manifest for language X" is the
// wrong answer to "what do you cover", not the absence of an error. Only a
// language that resolves NO provider at all (unregistered, or registered but
// not exposed for security scanning) still errors: there is nothing to
// derive from.
func HandleCoverage(req CoverageRequest) (CoverageResponse, error) {
	p := providerForLanguage(req.Language, nil)
	if p == nil {
		return CoverageResponse{}, fmt.Errorf("no coverage manifest for language %q", req.Language)
	}
	if cm, ok := p.(interface {
		CoverageManifest() coverage.Manifest
	}); ok {
		return coverageResponse(cm.CoverageManifest(), false, req.Detail), nil
	}
	return coverageResponse(deriveManifest(p), true, req.Detail), nil
}

const (
	coverageWithheldNote = "Nothing was withheld: every entry this manifest holds is named in the index. " +
		"The response budget authorizes withholding for scan-all; for coverage it authorizes nothing."
	coverageOverBudgetNote = "This index is over the response budget and is still complete. " +
		"Shorten a claim, never drop an entry."
	coverageUnrecognizedNote = "These ids matched no entry in this language's manifest. " +
		"They are named rather than skipped: an empty result would say the entry has nothing to declare, " +
		"which is a different answer from there being no such entry. Read the index for the ids that exist."
)

// coverageResponse projects a manifest into the answer the agent receives. It is
// the ONLY place that shape is built, so the index and the detail are two views
// of one value rather than two code paths that have to agree.
func coverageResponse(m coverage.Manifest, derived bool, want []string) CoverageResponse {
	index := m.Index()
	resp := CoverageResponse{
		Language:     m.Language,
		Index:        index,
		Entries:      len(index),
		Withheld:     0,
		WithheldNote: coverageWithheldNote,
		Derived:      derived,
	}
	if raw, err := json.Marshal(index); err == nil {
		resp.Bytes = len(raw)
	}
	if resp.Bytes > ResponseBudgetBytes {
		resp.OverBudget = true
		resp.BudgetNote = coverageOverBudgetNote
	}
	if len(want) > 0 {
		resp.Detail, resp.Unrecognized = m.Resolve(want)
		if len(resp.Unrecognized) > 0 {
			resp.UnrecognizedNote = coverageUnrecognizedNote
		}
	}
	return resp
}

// deriveManifest builds the FLOOR coverage answer (R1) from a provider's
// Capability(): the security/practices rule ids it declares, prose for each
// surface category it maps, and prose for each it does NOT — the not-mapped
// half derived from surface.DeriveCoverage against the locked
// surface.ProviderCategories vocabulary, never a literal list of category
// names (test contract item 4).
// withLimits appends every declared limit for id to that rule's OWN
// Deterministic line, rather than collecting limits into a separate field.
//
// The placement is the point. An agent reading "SEC-001 (security rule,
// declared)" must not be able to reach that claim without its caveat: they are
// one string, so no summariser between codefit and the model can keep the claim
// and drop the qualification. A sibling array is exactly that droppable, and a
// coverage answer that over-claims is the failure this repo's source-to-mirror
// chain exists to prevent. See ADR 0075.
func withLimits(line, id string, rs providers.RuleSet) string {
	for _, l := range rs.Limits {
		if l.ID == id {
			line += " — declared limit: " + l.Limit
		}
	}
	return line
}

// surfaceEntryID derives a surface entry's id from the locked
// surface.ProviderCategories vocabulary rather than from a hand-typed list, so a
// category that is renamed cannot leave a stale id behind.
func surfaceEntryID(c surface.Category) string {
	return "surface." + string(c)
}

// ruleEntry builds one declared rule's entry. A rule with no declared limit says
// everything in its claim and carries no detail. A rule WITH one keeps ADR
// 0075's placement: the qualification stays welded to that rule's own entry and
// appears on no other, and because the full limit text is far longer than a
// claim may be, the claim states that a limit exists and the detail carries it.
// An agent cannot read the claim and come away thinking the rule is unqualified.
func ruleEntry(id, family string, rs providers.RuleSet) coverage.Entry {
	line := fmt.Sprintf("%s (%s rule, declared)", id, family)
	full := withLimits(line, id, rs)
	if full == line {
		return coverage.Entry{ID: id, Claim: line}
	}
	return coverage.Entry{
		ID:     id,
		Claim:  line + " — carries a DECLARED LIMIT; read this entry's detail before reading it as complete",
		Detail: full,
	}
}

func deriveManifest(p providers.LanguageProvider) coverage.Manifest {
	cap := p.Capability()
	cs := surface.DeriveCoverage(cap.Surface)

	det := make([]coverage.Entry, 0, len(cap.Security.Declared)+len(cap.Practices.Declared))
	for _, id := range cap.Security.Declared {
		det = append(det, ruleEntry(id, "security", cap.Security))
	}
	for _, id := range cap.Practices.Declared {
		det = append(det, ruleEntry(id, "practices", cap.Practices))
	}

	reasoning := make([]coverage.Entry, 0, len(cs.Mapped))
	for _, c := range cs.Mapped {
		reasoning = append(reasoning, coverage.Entry{
			ID: surfaceEntryID(c),
			Claim: fmt.Sprintf(
				"Surface category %q is mapped for %s: the provider enumerates this structural surface for the agent to reason about.",
				c, p.Language()),
		})
	}

	notCovered := make([]coverage.Entry, 0, len(cs.NotMapped)+len(cap.Security.Excluded)+len(cap.Practices.Excluded))
	for _, c := range cs.NotMapped {
		notCovered = append(notCovered, coverage.Entry{
			ID: surfaceEntryID(c),
			Claim: fmt.Sprintf(
				"Surface category %q is NOT mapped for %s — never searched for, not merely absent from this scan.",
				c, p.Language()),
		})
	}
	for _, ex := range append(append([]providers.ExcludedRule{}, cap.Security.Excluded...), cap.Practices.Excluded...) {
		notCovered = append(notCovered, coverage.Entry{
			ID:    ex.ID,
			Claim: fmt.Sprintf("%s is permanently NOT covered: %s", ex.ID, ex.Reason),
		})
	}

	return coverage.Manifest{
		Language:      p.Language(),
		Deterministic: det,
		Reasoning:     reasoning,
		NotCovered:    notCovered,
	}.WithProse()
}
