package mcp

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/core/baseline"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/crossrules"
	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/query"
	"github.com/codefit-cli/codefit/internal/core/report"
	"github.com/codefit-cli/codefit/internal/core/scope"
	"github.com/codefit-cli/codefit/internal/core/scoring"
	"github.com/codefit-cli/codefit/internal/core/sourcetext"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/core/surfaceindex"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/registry"
	"github.com/codefit-cli/codefit/internal/schemasource"
	dbsensor "github.com/codefit-cli/codefit/internal/sensors/db"
)

// ResponseBudgetBytes is the byte budget codefit-scan-all declares for its own
// serialized response.
//
// What is MEASURED (roadmap P0-4): a bisection run against a real MCP client
// (Claude Code, 2026-08-09), driving the v0.2.6 binary over stdio with
// controlled-size responses cut from trimmed copies of a real 317-file
// project. The real ceiling was bracketed, not pinpointed:
//
//	64 097 bytes  ACCEPTED   <- largest observed acceptance
//	74 195 bytes  REJECTED   "exceeds maximum allowed tokens"
//
// 40 000 was CHOSEN inside that bracket, not measured directly: 62% of the
// largest observed acceptance (64 097), leaving room for roughly a 60%
// increase in token density before approaching the rejected end (74 195), and
// it matches an earlier, independent data point — a 40 282-byte response that
// is known to have arrived (2026-08-04, before this bisection existed).
//
// The assumption this number rests on, stated plainly: the client's limit is
// in TOKENS; this budget counts BYTES. The bytes-per-token ratio is
// content-dependent (identifiers, hex digests and deep paths run denser than
// prose), so the margin above is NOT fixed — a byte count under budget can
// still cross a token ceiling the same client would reject. And the
// measurement is of ONE client, ONE date, ONE content shape: other MCP
// clients (Cursor, VSCode, OpenCode) have their own limits, unmeasured here.
//
// Measured consequence of moving this number down (2026-08-09, same real
// project, fresh baseline): payload 39 962 bytes, 19 of 174 endpoints
// withheld (5 actionable, 14 frontier_pending) — at the old 60 000 this same
// project fit entirely with 0 withheld. Real mid-sized projects now see a
// non-zero withheld count where they previously did not; each bucket's
// `count` stays the complete number and codefit-scan-endpoint still fetches
// full detail on request (ADR 0054), but this is a user-visible behaviour
// change, not a free tightening.
//
// See ADR 0062 for the full record, including what this number does NOT fix:
// a byte budget cannot guarantee a token limit. The structural answer — a
// hard cap on entries per bucket, so response size stops being a function of
// project size — is roadmap P0-4's declared follow-up, not this change.
//
// The budget is DECLARED in the response (see BudgetBlock) and enforced by
// withholding the lowest-ranked endpoints, never by truncating the payload: a
// clipped response that reads like a complete one is the one outcome forbidden
// (ADR 0054, same principle as ADR 0048).
const ResponseBudgetBytes = 40_000

// ScanAllRequest is the input to codefit-scan-all: a project root and language.
// codefit walks the project, runs the deterministic sensor and the surface
// queries, and returns the complete per-endpoint picture.
type ScanAllRequest struct {
	Root     string `json:"root"`
	Language string `json:"language"`
	// ChangedFiles narrows the audit to these project-relative paths (layer 0 of
	// the filtering pyramid). codefit does not ask git which files changed — it
	// has no power over the user's git, and the calling agent already knows what
	// it touched. Absent or empty means a FULL audit, never "audit nothing".
	//
	// A narrowed run declares itself in the response's scope block, cannot mark a
	// baseline item in an unopened file as gone, and leaves the DB dimension NOT
	// MEASURED unless a configured schema path is in scope.
	ChangedFiles []string `json:"changed_files,omitempty"`
}

// ScanAllResponse is the agent-first synthesis as an ACTIONABLE summary, not the
// raw item dump. It has three buckets, one per resolution level, all decided by
// facts codefit already computes (ADR 0008):
//
//   - Actionable      — resolved locally AND has a gap: full detail, the agent acts.
//   - ResolvedClean   — resolved locally, NO gap: named + a verification fact;
//     codefit checked and the controls are present.
//   - FrontierPending — not resolved locally (the data left the handler body):
//     named; the agent follows it in the code.
//
// ResolvedClean and FrontierPending are kept DISTINCT on purpose: one affirms
// codefit verified the controls, the other states codefit could not conclude —
// epistemological opposites the agent must distinguish (flattening them would be
// the old frontier-wording error). Full detail of any named endpoint is always one
// codefit-scan-endpoint call away.
// Note on the baseline layer: when a baseline exists, the three buckets are
// FILTERED to what is not yet tracked (new/changed surface, and unaccepted
// deterministic affirmations). So on an unchanged re-scan all three buckets can be
// empty even though Summary.Security.CertainConcerns (computed before filtering) is > 0 —
// the difference is exactly the "known" surface the baseline is silencing.
type ScanAllResponse struct {
	// Summary is the per-dimension count block: one sub-block per audit
	// dimension plus a derived totals, each count declaring which dimension it
	// counted. A null sub-block means that dimension was not measured; see
	// ScanAllSummary for why an unqualified count was a defect.
	Summary ScanAllSummary `json:"summary"`
	// Scope declares how much of the project this response describes: mode full or
	// partial, how many auditable files were in scope, and which requested paths
	// the audit never reached. It is ALWAYS present, so a consumer never infers
	// the mode from an absence and never reads a partial `blocked: false` as the
	// wider claim it is not.
	Scope ScopeBlock `json:"scope"`
	// Score is the per-dimension breakdown plus the weighted global (ADR 0021). It
	// is ALWAYS present: by_dimension carries every weighted dimension, with the
	// unaudited ones (review/complexity/tests, and db when it did not run) as null —
	// an honest statement that the dimension exists but was not measured. The score
	// reflects deterministic AFFIRMATIONS only, not mapped surface.
	Score    scoring.ScoreSummary `json:"score"`
	Baseline BaselineDelta        `json:"baseline"`
	// Budget declares the response's own byte budget and whether anything was
	// withheld to meet it. ALWAYS present, including when nothing was withheld: an
	// agent must be able to tell a complete list from a cut one by reading the
	// response, never by guessing (ADR 0054).
	Budget          BudgetBlock       `json:"budget"`
	Actionable      ActionableSection `json:"actionable"`
	ResolvedClean   ResolvedClean     `json:"resolved_clean"`
	FrontierPending FrontierPending   `json:"frontier_pending"`
	// Security reports whether the security dimension ran (D3b). It is ALWAYS
	// present — deliberately NOT `omitempty`, unlike DB: the db dimension may
	// legitimately not apply to a project (no database configured), but security
	// applies to every project, so an ABSENT section could only mean an older
	// codefit build. Measured=false with a Note is the honest "no provider for
	// this language" state, mirroring DBSection's own Measured/Note shape.
	Security SecuritySection `json:"security"`
	// DB is the parallel database-structure section — the db dimension's findings
	// and surface, baseline-filtered. It is NON-endpoint (a table has no route), so
	// it is its own section, not one of the three endpoint buckets. Nil when the
	// project has no database.schema_paths configured, so a project without a
	// database yields a response byte-identical to before db was wired (ADR 0020).
	DB *DBSection `json:"db,omitempty"`
}

// SecuritySection reports whether the security dimension ran this pass (D3b).
// Measured=false is SOFT, exactly like DBSection: it never fails scan-all, it
// is reported. Note is empty when Measured is true (nothing to caveat) and
// non-empty otherwise, naming why (no provider resolved for the language) and
// what that means for the rest of the response (the schema may still have
// been audited; the code was not).
type SecuritySection struct {
	Measured bool   `json:"measured"`
	Note     string `json:"note,omitempty"`
	// SurfaceCoverage declares which of surface.ProviderCategories this
	// language's provider mapped and which it did not (R2,
	// docs/specs/declared-partial-language-exposure.md) — the "1 of 4"
	// statement a bare `surface_items: 1` never made. Present only when
	// Measured is true: a pointer, so a DB-only pass (Measured=false, no
	// provider resolved) serializes no surface_coverage key at all, rather
	// than a zero-valued statement that could be misread as "nothing
	// unmapped".
	SurfaceCoverage *surface.CoverageStatement `json:"surface_coverage,omitempty"`
	// RecognizedAuthzHelpers names the project-registered authz helper(s)
	// codefit recognized for this language (baseline.RecognizedAuthzHelpers)
	// — the exact names, never re-derived. Present only when Measured is
	// true, mirroring SurfaceCoverage: a pointer, so a DB-only pass
	// serializes no key at all. Unlike SurfaceCoverage, the pointer here must
	// NEVER address a nil slice — baseline.RecognizedAuthzHelpers returns nil
	// on zero matches (it only appends), and a `*[]string` pointing at nil
	// marshals `null`, a THIRD meaning distinct from "absent" and "present
	// and empty". The zero-registration case is present and empty ([]):
	// codefit looked at the baseline and found no registration, which is not
	// the same as never having looked (absent).
	RecognizedAuthzHelpers *[]string `json:"recognized_authz_helpers,omitempty"`
	// RecognizedAuthzHelpersNote is always non-empty whenever
	// RecognizedAuthzHelpers is present — an empty array alone does not tell
	// the agent what to do next (same idiom as
	// coverage.CoverageResponse.WithheldNote). See recognizedAuthzHelpersNote
	// for the wording contract this caption follows: a FACT about codefit's
	// knowledge, never a judgment about the project, and no claim about
	// resolved_clean/actionable (the causal link between the two is verified
	// false — a built-in helper match sets known_authz_detected=true
	// independent of the registered count).
	RecognizedAuthzHelpersNote string `json:"recognized_authz_helpers_note,omitempty"`
}

// DBSection is the database dimension's result inside scan-all. Measured=false with
// a Note is the honest "not audited" state (disabled / no parser / schema read or
// parse failure) — a db failure is SOFT here, reported but never fatal to the
// security result (ADR 0020). Findings are affirmations (e.g. DB-050); Surface is
// the LIGHT index (surfaceindex.Entry, not the full findings.SurfaceItem — design
// D1/D5) of the questions; both are already filtered by the baseline. Full detail
// of any named item is one codefit-scan-db call away with `detail: [ids]`.
type DBSection struct {
	Measured bool                 `json:"measured"`
	Note     string               `json:"note,omitempty"`
	Findings []findings.Finding   `json:"findings,omitempty"`
	Surface  []surfaceindex.Entry `json:"surface,omitempty"`
	// Count is the COMPLETE number of surface items this section classifies —
	// taken from surfaceindex.Index's own return, computed independently of how
	// Surface is built, never from len(Surface) after the fact (design D4/I4:
	// reading it back off the rendered index is the self-referential trap the
	// coverage-chain archive records, obs #1664).
	Count int `json:"count"`
	// Withheld is always 0: there is no ranking axis across db surface's 18
	// disjoint categories (no severity field) to withhold BY — a stated absence
	// of a mechanism, not a principle (design D4). WithheldNote says so in
	// words, deliberately NOT coverage's sentence (a different reason) and NOT
	// the endpoint-bucket pattern (db.surface is never partially rendered).
	Withheld     int    `json:"withheld"`
	WithheldNote string `json:"withheld_note,omitempty"`
	Score        int    `json:"score"`
}

// dbWithheldNote is DBSection's own wording for I4 (design D4): db.surface
// withholds nothing because there is no ranking axis to withhold BY across its
// 18 disjoint categories, not because a fixed manifest authorizes nothing (the
// reason coverageWithheldNote gives) and not because a byte budget cut it (the
// endpoint-bucket reason). Deliberately not reused from either.
const dbWithheldNote = "Nothing was withheld: every item this dimension classified is named in the index. " +
	"There is no ranking axis across db.surface's disjoint categories (no severity field) to withhold BY — " +
	"unlike scan-all's endpoint buckets, this section has no mechanism to cut. Full detail of any named item " +
	"is one " + string(ToolScanDB) + " call away with detail: [ids]."

// BudgetBlock is scan-all's account of its own size. codefit's primary tool has
// to RETURN: a response an MCP client refuses is worth less than a smaller one
// that arrives. So the response declares the budget it is written to, and when
// the endpoint list does not fit it says exactly how many endpoints it is not
// showing and on what ordering it kept the ones it shows.
//
// Withheld=0 still carries a Note, on purpose. "No mention of truncation" and
// "nothing was truncated" must not be the same bytes on the wire: that ambiguity
// is how a clipped response comes to read like a complete one (ADR 0048).
type BudgetBlock struct {
	Bytes    int    `json:"bytes"`
	Withheld int    `json:"withheld"`
	Ordering string `json:"ordering"`
	Note     string `json:"note"`
}

// ActionableSection declares the endpoints codefit resolved locally AND found a
// gap in — the ones the agent acts on. They are NAMED with what it takes to rank
// them (counts, categories, gap kinds, best certainty, whether an affirmation is
// present) and their deterministic concerns in full; the surface question and
// signals text is one codefit-scan-endpoint call away.
//
// Count is the COMPLETE number codefit classified as actionable and never the
// number rendered — Withheld accounts for the difference. Reading Count off
// len(Endpoints) is exactly the bug this section's shape exists to make
// impossible.
type ActionableSection struct {
	Count    int    `json:"count"`
	Withheld int    `json:"withheld"`
	Note     string `json:"note,omitempty"`
	// Endpoints is the rendered prefix of the ranked list: hardest gap kind first
	// (affirmed → access → exposure → efficiency), then by certain-concern count.
	Endpoints []report.ActionableEndpoint `json:"endpoints,omitempty"`
}

// ResolvedClean declares the endpoints codefit resolved locally and found clean
// (controls present, no gap). They are NAMED with a verification fact, not
// detailed. This is an affirmation — codefit looked and it is clean — not a
// generic "not detailed" bucket; that is why it is separate from FrontierPending.
type ResolvedClean struct {
	Count     int                            `json:"count"`
	Withheld  int                            `json:"withheld"`
	Note      string                         `json:"note,omitempty"`
	Endpoints []report.ResolvedCleanEndpoint `json:"endpoints,omitempty"`
}

// FrontierPending declares the endpoints codefit did NOT resolve locally: the data
// left the handler body, so codefit concluded nothing and the agent must follow it
// in the code. They are named (not detailed) with a Note explaining why they are
// not detailed and how to fetch any of them. This is not hiding — it is
// prioritising while declaring the rest.
type FrontierPending struct {
	Count     int                       `json:"count"`
	Withheld  int                       `json:"withheld"`
	Note      string                    `json:"note,omitempty"`
	Endpoints []report.FrontierEndpoint `json:"endpoints,omitempty"`
}

// ScanAllSummary is the at-a-glance count, not a judgment — and every count in
// it DECLARES the dimension it counted.
//
// The flat shape this replaced had four unqualified fields (endpoints,
// deterministic_findings, surface_items, certain_concerns) that were all
// functions of the SECURITY sensor's result while presenting themselves as the
// response's summary. A DB-heavy project therefore read `surface_items: 0` over
// a db.surface holding dozens of items: a security-only prefix of the truth,
// presented unlabelled as the whole (invariant I4 of
// docs/specs/audit-protocol.md), producing the one thing this project exists to
// prevent — a zero that means "nobody looked" (I2).
//
// A sub-block is a POINTER and deliberately NOT omitempty: the key is always on
// the wire, and `null` is the statement "this dimension was not measured". That
// is the shape score.by_dimension already ships (`"db": null` beside
// `"db": 95`); an ABSENT key would be a third state a reader has to guess at.
type ScanAllSummary struct {
	// Security counts the security dimension only. Nil when no provider
	// resolved for the language — never a zeroed block, which would read as
	// "codefit looked at the code and found nothing".
	Security *SecuritySummary `json:"security"`
	// DB counts the database dimension only. Nil when the dimension did not
	// run (no database.schema_paths configured, or narrowed out of scope) and
	// when it ran but could not measure (no parser, unreadable schema) — the
	// DBSection.Measured=false state.
	DB *DBSummary `json:"db"`
	// Totals is DERIVED from the non-nil sub-blocks, never written by hand: a
	// hand-kept total drifts the first time a dimension is added. It carries
	// only COMMENSURABLE units — a deterministic finding and a mapped surface
	// item mean the same thing in both dimensions. Endpoints and schema
	// sources are each dimension's own scale unit and are summed nowhere: a
	// table has no route.
	Totals SummaryTotals `json:"totals"`
	// Note is ALWAYS present, on the BudgetBlock precedent: "no mention" and
	// "nothing to mention" must not be the same bytes on the wire.
	Note string `json:"note"`
}

// SecuritySummary is the security dimension's own counts — verbatim the four
// fields the flat ScanAllSummary carried, so a consumer migrating reads
// summary.security.* for exactly the values it used to read at summary.*.
type SecuritySummary struct {
	Endpoints             int `json:"endpoints"`
	DeterministicFindings int `json:"deterministic_findings"`
	SurfaceItems          int `json:"surface_items"`
	CertainConcerns       int `json:"certain_concerns"`
}

// DBSummary is the database dimension's own counts.
//
// SchemaSources is the dimension's scale unit — the distinct schema sources
// this pass READ, the same census scope's denominator takes (one local, shared;
// two call sites computing one census is how this repo drifts). It is not a new
// measurement, and under a narrowed scope it shrinks with what was read,
// exactly like `scope`.
//
// There is deliberately NO certain_concerns here. The security field of that
// name counts Deterministic PLUS SurfaceConfirmed concerns
// (core/report/aggregate.go's countCertain), where SurfaceConfirmed is set from
// a security-surface structural fact DB items never carry — so it is not a
// certainty-1.0 count, and a DB sibling under the same name would be the exact
// same-name-different-definition defect this shape exists to fix. Adding the
// key later is additive; shipping a differently-defined one now would be
// breaking. See ADR 0069.
type DBSummary struct {
	SchemaSources         int `json:"schema_sources"`
	DeterministicFindings int `json:"deterministic_findings"`
	SurfaceItems          int `json:"surface_items"`
}

// SummaryTotals is the cross-dimension roll-up, over commensurable units only.
// It is a VALUE, not a pointer: totals are always computable (the
// nothing-measurable guard already refused the response where no dimension
// ran), and zeros here are backed by at least one non-null sub-block saying who
// was counted.
type SummaryTotals struct {
	DeterministicFindings int `json:"deterministic_findings"`
	SurfaceItems          int `json:"surface_items"`
}

// summaryNote is the sentence summary always carries. It states the two things
// a reader cannot infer from the numbers: that they are the RAW population
// (pre-baseline-filter, the same population the score is computed over), so a
// non-zero count over empty rendered buckets is the baseline silencing what is
// already tracked rather than a contradiction; and that a null sub-block is
// "not measured" while a zero is "measured, found nothing".
const summaryNote = "counts are per DIMENSION and RAW: taken before the baseline filter, so a count here can " +
	"exceed what the buckets and db.surface list — that difference is the surface the baseline already " +
	"tracks, not a contradiction. A null sub-block means the dimension was NOT measured (nobody looked); a " +
	"zero means it was measured and nothing was found. totals sums only units that mean the same in every " +
	"dimension: never endpoints, never schema sources."

// summarize builds the per-dimension summary from the RAW sensor results.
//
// The signature is the enforcement, not a convention (design D2): it takes
// findings.SensorResult values and no *DBSection at all, so the population
// mistake this change exists to prevent — counting the BASELINE-FILTERED
// dbSection.Findings/Surface instead of the raw dbRes — cannot be written here.
// []findings.Finding does not type-check as a findings.SensorResult, so it is a
// compile error rather than a comment nobody reads.
//
// dbRan is the DB dimension's measured predicate: runDBForScanAll returns
// ran=true only on the same path that returns Measured=true, and every
// not-measured path (no parser, read/parse failure, sensor abstention) returns
// ran=false with an empty result. So `dbRan` IS `dbSection != nil &&
// dbSection.Measured`, without this function having to be handed the section it
// must not read.
//
// dbSources is the shared census (design D5): the distinct schema sources the
// pass read, computed once by the caller and used both here and as the
// DB-only scope denominator.
func summarize(
	secRan bool,
	secRes findings.SensorResult,
	endpoints []report.EndpointReport,
	dbRan bool,
	dbRes findings.SensorResult,
	dbSources []string,
) ScanAllSummary {
	s := ScanAllSummary{Note: summaryNote}
	if secRan {
		certain := 0
		for _, ep := range endpoints {
			certain += ep.CertainConcerns
		}
		s.Security = &SecuritySummary{
			Endpoints:             len(endpoints),
			DeterministicFindings: len(secRes.Findings),
			SurfaceItems:          len(secRes.Surface),
			CertainConcerns:       certain,
		}
	}
	if dbRan {
		s.DB = &DBSummary{
			SchemaSources:         len(dbSources),
			DeterministicFindings: len(dbRes.Findings),
			SurfaceItems:          len(dbRes.Surface),
		}
	}
	// Totals DERIVED from whichever sub-blocks exist (D4), never a literal: a
	// hand-written total is right exactly once and then silently drifts. Only
	// the commensurable pair is projected here; a dimension's own scale unit
	// (endpoints, schema sources) has no projection and therefore cannot reach
	// the roll-up by accident.
	type commensurable struct{ deterministic, surface int }
	var parts []commensurable
	if s.Security != nil {
		parts = append(parts, commensurable{s.Security.DeterministicFindings, s.Security.SurfaceItems})
	}
	if s.DB != nil {
		parts = append(parts, commensurable{s.DB.DeterministicFindings, s.DB.SurfaceItems})
	}
	for _, p := range parts {
		s.Totals.DeterministicFindings += p.deterministic
		s.Totals.SurfaceItems += p.surface
	}
	return s
}

// HandleScanAll runs the full audit over the project and returns the actionable
// summary plus the named frontier-pending list. It reuses the real security sensor
// (the deterministic rules plus the three surface queries already run together
// there), groups the result by endpoint, and partitions by the local-resolution
// fact — it adds no detection, only the aggregation and the split.
func HandleScanAll(req ScanAllRequest) (ScanAllResponse, error) {
	return handleScanAllScoped(req, scope.Of(req.ChangedFiles))
}

// handleScanAllScoped is HandleScanAll with layer 0 made explicit.
func handleScanAllScoped(req ScanAllRequest, scp scope.Scope) (ScanAllResponse, error) {
	return handleScanAllBudgeted(req, scp, filepath.Join(req.Root, baseline.Name), ResponseBudgetBytes)
}

// handleScanAllBudgeted is handleScanAllScoped with its two collaborators made
// arguments instead of constants. Both are SEAMS, not options: production has
// exactly one caller and it passes the project's committed baseline path and the
// declared ResponseBudgetBytes.
//
//   - baselinePath: where the previous baseline is read from and the next one is
//     written to. Parameterised so the dogfood harness can measure a REAL project
//     without writing a byte inside somebody's working clone — and so it measures
//     the first-scan state (nothing tracked yet), which is the state that produced
//     the 313 KB response this budget exists to prevent.
//   - budget: the byte budget the rendered response must fit. Parameterised so a
//     test can drive the withholding path over a real response instead of having
//     to synthesise a project with thousands of endpoints.
func handleScanAllBudgeted(req ScanAllRequest, scp scope.Scope, baselinePath string, budget int) (ScanAllResponse, error) {
	resp, actionable, next, err := buildScanAll(req, scp, baselinePath)
	if err != nil {
		return ScanAllResponse{}, err
	}
	rendered, stillOver := withNamedActionable(resp, actionable, budget)
	// R1 (baseline-write-gate): the baseline is persisted only once the response
	// has passed EVERY check codefit can perform on its own output.
	// scoring.MissingWeights and ScopeBlock.Validate() already gated this point —
	// buildScanAll returns before computing `next` on either failure, so an error
	// from it never reaches here (see TestScanAllWriteGate_BuildScanAllErrorNeverWrites).
	// stillOver is the last and, per the census, the most dangerous of the three:
	// the probability of a response not fitting RISES with the number of
	// findings, so a still-over response must leave the baseline exactly as it
	// found it — the next scan re-observes everything, which is the correct
	// outcome for a reader who never received this one.
	if !stillOver {
		if err := next.Save(baselinePath); err != nil {
			return ScanAllResponse{}, fmt.Errorf("saving baseline: %w", err)
		}
	}
	return rendered, nil
}

// buildScanAll runs the audit and returns the COMPLETE analysis: the response
// with every conclusion already computed — the score, the baseline delta, the
// summary, the scope block, the bucket counts, the DB section — plus the complete
// per-endpoint detail of the actionable bucket, which the caller renders.
//
// The split is the point of this whole change (R4). Everything codefit CONCLUDES
// is decided here, over the whole audit; the rendering that follows can only
// narrow how much of it is spelled out. Nothing downstream of this function may
// be read back into a conclusion — a count taken from the rendered list, a score
// recomputed over what fit, and the response would be lying about a project it
// analysed in full.
//
// The FOURTH return, next, is the baseline diffBaseline computed but did NOT
// persist (R1 of the baseline-write-gate spec): the caller decides whether to
// save it, and only after the budget-fitting step (fitToBudget's stillOver) has
// also passed — a check this function cannot perform, because it runs before
// the response is rendered. next is nil on every error return: an error here
// means the caller must not save anything, and a nil next makes that the only
// possible outcome rather than a caller obligation to remember.
func buildScanAll(req ScanAllRequest, scp scope.Scope, baselinePath string) (ScanAllResponse, []report.EndpointReport, *baseline.Baseline, error) {
	// Load the committed baseline ONCE: it provides both the project's registered
	// authz helpers (recognized during the scan) and the previous items the unified
	// diff is computed against (diffBaseline reuses this same load — no double read).
	prev, err := baseline.Load(baselinePath)
	if err != nil {
		return ScanAllResponse{}, nil, nil, fmt.Errorf("loading baseline: %w", err)
	}

	// Security runs only when a language provider resolves for req.Language — the
	// DB dimension does not need one (its schema parser is picked from the
	// configured schema paths, not the language). secRan is a PREDICATE: the
	// provider itself is discarded here, resolved again inside runSecurity. A nil
	// provider is the only thing made non-fatal; a config-load or sensor error
	// inside runSecurity stays a hard error (D1).
	secRan := providerForLanguage(req.Language, nil) != nil
	// Hoisted once, unconditionally: securitySection needs it below on the
	// same code path whether or not security ran, and reusing this exact
	// slice (rather than a second baseline read) is what keeps the response
	// and the actual matching input in agreement.
	helpers := prev.RecognizedAuthzHelpers(req.Language)
	var secRes findings.SensorResult
	if secRan {
		var err error
		secRes, err = runSecurity(req.Root, req.Language, helpers, scp)
		if err != nil {
			return ScanAllResponse{}, nil, nil, err
		}
	}
	endpoints := report.AggregateEndpoints(secRes.Findings, secRes.Surface)

	// DB runs only when database.schema_paths is configured (the dimension applies
	// to this project). A DB failure is SOFT — reported in its section, never fatal
	// to security (ADR 0020). No schema_paths → no DB section → byte-identical to
	// before db was wired.
	cfg, err := config.LoadOptional(filepath.Join(req.Root, ".codefit.yaml"))
	if err != nil {
		return ScanAllResponse{}, nil, nil, fmt.Errorf("loading project config: %w", err)
	}
	//
	// Under a PARTIAL scope the dimension also needs one of its configured schema
	// paths to be in scope. Otherwise it is NOT MEASURED (R4): running it would
	// score a schema this pass has no reason to believe changed, and reporting 100
	// where the honest answer is null is the difference between "audited and
	// clean" and "never looked".
	var dbRes findings.SensorResult
	var dbSection *DBSection
	dbRan := false
	if cfg != nil && len(cfg.Database.SchemaPaths) > 0 && dbInputsInScope(cfg.Database.SchemaPaths, scp) {
		dbSection, dbRes, dbRan = runDBForScanAll(req.Root, req.Language, cfg, crossrules.All(), scp)
	}

	// The DB census, taken ONCE and shared (design D5): it is both the DB
	// dimension's own scale unit in the summary (schema_sources) and, on a
	// DB-only pass, the scope block's auditable denominator below. Two call
	// sites computing one census is precisely how the two numbers would come to
	// disagree about how much schema this pass read.
	dbSources := distinctCanon(dbRes.AuditedFiles)

	// The summary is computed HERE, not down at the return literal (design D3).
	// filterDBByBaseline below mutates dbSection's Findings/Surface IN PLACE to
	// the baseline-filtered subset; the summary must count the RAW population —
	// the same population Score is computed over — so that a fully-baselined
	// re-scan still reports what the sensors observed instead of collapsing to
	// zero. summarize's signature already makes reading the section impossible;
	// computing before the mutation removes even the window in which a future
	// edit could reintroduce it.
	summary := summarize(secRan, secRes, endpoints, dbRan, dbRes, dbSources)

	// Per-dimension scoring input (ADR 0021): security measured only when it ran,
	// db only when it ran. HOISTED here, above the baseline diff (which SAVES the
	// next baseline), so the nothing-measurable guard right below can refuse
	// BEFORE any write (D5) — not after.
	measured := []findings.Dimension{}
	scored := []findings.Finding{}
	if secRan {
		measured = append(measured, findings.DimensionSecurity)
		scored = append(scored, secRes.Findings...)
	}
	if dbRan {
		measured = append(measured, findings.DimensionDB)
		scored = append(scored, dbRes.Findings...)
	}

	// Nothing measurable: no security provider resolved AND the DB dimension did
	// not run. A response with every dimension null is indistinguishable from an
	// impeccable project to an agent skimming it — the exact defect this change
	// fixes, mirrored at the score level. Refuse instead of scoring an empty
	// `measured` set (scoring.Compute would return Global:0, a false "worst
	// possible" reading — proven unreachable by this guard, see D5).
	if len(measured) == 0 {
		return ScanAllResponse{}, nil, nil, nothingMeasurableError(req.Language, dbSection)
	}

	// One unified baseline diff over both sensors' observations, scoped to the
	// categories of the sensors that ran (ADR 0019), persisted once. scanned
	// starts EMPTY and is only ever added to inside the "if <dim>Ran" block of the
	// dimension that owns the categories (invariant SCANNED-OPT-IN, D2): a
	// forgotten gate can then only fail to ADD categories, never prune something a
	// sensor never looked at. When db ran, the cross rules' categories join the db
	// scope too — their items are part of the db result, so gone-detection/pruning
	// must cover them (ADR 0029).
	scanned := map[string]bool{}
	if secRan {
		for c := range securityScope(req.Language) {
			scanned[c] = true
		}
	}
	if dbRan {
		for _, c := range dbsensor.New(nil).OwnedCategories() {
			scanned[c] = true
		}
		// The code x schema cross is a WHOLE-REPO census: its items anchor to the
		// schema, but the evidence for them is every query filter in the codebase.
		// A narrowed pass sees a shrunken census, so an item can vanish because the
		// query that justified it is in a file this pass never opened — and its
		// schema anchor IS in scope, so the file dimension of the baseline guard
		// would not catch it. The rules still RUN (their items are real, and fewer
		// filters can only produce fewer of them), but their categories stay out of
		// the gone scope, exactly as the DW census rules abstain rather than judge
		// from a shrunken count.
		if !scp.Narrows() {
			for _, c := range crossrules.OwnedCategories() {
				scanned[c] = true
			}
		}
	}
	diff, delta := diffBaseline(prev, observedFrom(secRes, dbRes), scanned, scp)

	// Two presentations of the same diff: endpoints for security, a flat section for db.
	actionable, clean, frontier := report.ClassifyEndpoints(filterEndpointsByBaseline(endpoints, diff))
	if dbSection != nil && dbSection.Measured {
		var filteredSurface []findings.SurfaceItem
		dbSection.Findings, filteredSurface = filterDBByBaseline(dbRes, diff)
		// Count is taken from Index's own second return — computed over the
		// SAME population passed in, independently of how the entries slice is
		// built — never from len(dbSection.Surface) after the fact (D4/I4).
		dbSection.Surface, dbSection.Count = surfaceindex.Index(filteredSurface)
		dbSection.WithheldNote = dbWithheldNote
	}

	// Scoring (ADR 0021), over RAW findings (not baseline-filtered) so the global
	// equals the flat security score exactly when db is absent — no value
	// regression. measured/scored were HOISTED above (see the nothing-measurable
	// guard).
	//
	// weights (roadmap P1-2): cfg.Report.ScoreWeights, when the user set it, IS
	// now the map scoring actually uses — config.Validate already rejected one
	// that does not sum to 100, but nothing ever read it past that point.
	// ResolveWeights falls back to scoring.DefaultWeights() when the user set
	// nothing (cfg == nil or an empty map), so an absent key is byte-identical
	// to before this change.
	var userWeights map[string]int
	if cfg != nil {
		userWeights = cfg.Report.ScoreWeights
	}
	weights := scoring.ResolveWeights(userWeights)

	// The guard below fails loudly if a measured dimension has no weight,
	// never a silently incomplete score. It now has TWO reachable causes,
	// worded differently so the reader knows who must act:
	//   - a user-supplied partial map that does not name every dimension THIS
	//     scan measured (validation only checks the map it was given sums to
	//     100 — it cannot know in advance which dimensions a given project
	//     will measure, since that depends on what is configured/found) — a
	//     CONFIG error, actionable in .codefit.yaml.
	//   - DefaultWeights() itself missing a dimension core/findings declares —
	//     a codefit WIRING bug, never a silently incomplete score (ADR 0021).
	if missing := scoring.MissingWeights(measured, weights); len(missing) > 0 {
		if len(userWeights) > 0 {
			return ScanAllResponse{}, nil, nil, fmt.Errorf(
				"config: report.score_weights in .codefit.yaml has no weight for measured dimension(s) %v — "+
					"add them to score_weights (the map must still sum to 100), or remove score_weights entirely to use codefit's defaults",
				missing)
		}
		return ScanAllResponse{}, nil, nil, fmt.Errorf("codefit internal: measured dimension(s) without a weight: %v", missing)
	}
	score := scoring.Compute(measured, scored, weights)

	// The scope block unions what BOTH dimensions examined: the security walk's
	// files and, when the db dimension ran, the configured schema sources it read.
	// Without the union a requested schema path would be reported unmatched even
	// though the audit read it — the security walk does not open .prisma files.
	//
	// auditableTotal (D3): when security ran, its own walk-based count is the
	// denominator, unchanged. When it did NOT run, the only census this pass
	// actually took is the distinct schema sources the DB dimension read — 0
	// would falsely assert "no auditable files exist"; the residual (how many
	// files a security provider WOULD have counted) is unknowable without a
	// provider, and that caveat lives in Security.Note, not here.
	auditableTotal := secRes.AuditableTotal
	if !secRan {
		auditableTotal = len(dbSources)
	}
	block := scopeBlockFor(scp, append(append([]string{}, secRes.AuditedFiles...), dbRes.AuditedFiles...), auditableTotal)
	if err := block.Validate(); err != nil {
		return ScanAllResponse{}, nil, nil, err
	}

	// Every count below is taken from the COMPLETE classification, never from what
	// the rendering will end up showing (R4).
	resolvedLocally := len(actionable) + len(clean)
	return ScanAllResponse{
		Scope:    block,
		Summary:  summary,
		Score:    score,
		Baseline: delta,
		Actionable: ActionableSection{
			Count: len(actionable),
			Note:  actionableNote(len(actionable), secRan),
			// Endpoints is filled by the rendering step (withNamedActionable), from
			// the complete list returned alongside this response.
		},
		ResolvedClean: ResolvedClean{
			Count:     len(clean),
			Note:      resolvedCleanNote(len(clean), secRan),
			Endpoints: clean,
		},
		FrontierPending: FrontierPending{
			Count:     len(frontier),
			Note:      frontierNote(len(frontier), resolvedLocally, secRan),
			Endpoints: frontier,
		},
		Security: securitySection(secRan, req.Language, helpers),
		DB:       dbSection,
	}, actionable, diff.Next, nil
}

// distinctCanon returns the canonical, deduplicated form of paths (D3's
// denominator for a DB-only pass) — a schema configured twice, or under
// different slash/case spelling, must count once.
func distinctCanon(paths []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range paths {
		c := scope.Canon(p)
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// securitySection builds the ALWAYS-present Security field (D3b). When
// secRan is true there is nothing to caveat (Measured=true, empty Note,
// mirroring how a full ScopeBlock carries no note). When false, the note
// names why (no provider resolved) and what it means for the rest of the
// response: the schema may still have been audited (by the DB dimension)
// while the code was not — this is reachable only after the
// nothing-measurable guard, so a DB-only pass having run is guaranteed here.
func securitySection(secRan bool, language string, helpers []string) SecuritySection {
	if secRan {
		cs := surfaceCoverageFor(language)
		h := nonNilStrings(helpers)
		return SecuritySection{
			Measured:                   true,
			SurfaceCoverage:            &cs,
			RecognizedAuthzHelpers:     &h,
			RecognizedAuthzHelpersNote: recognizedAuthzHelpersNote(helpers),
		}
	}
	return SecuritySection{
		Measured: false,
		Note: "security was NOT audited for this language (no provider) — this is not a clean security " +
			"result; the schema was audited, the code was not.",
	}
}

// nonNilStrings converts a nil slice to an explicit non-nil empty one, never
// mutating or copying a non-nil input. It exists because taking the address
// of a nil []string for a `*[]string` field marshals JSON `null`, not `[]` —
// measured directly (see recognized_authz_helpers' doc comment): the trap is
// live because baseline.RecognizedAuthzHelpers declares `var out []string`
// and only appends, so it returns nil, not an empty slice, on zero matches.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// recognizedAuthzHelpersNote is the ONE function that builds the caption for
// recognized_authz_helpers, shared by codefit-scan-all (securitySection) and
// codefit-scan-security (HandleScanSecurity) so the two responses cannot
// drift in phrasing (spec: "the two handlers MUST share one note-building
// function").
//
// Wording contract, both non-negotiable (issue #155 / #148 precedent):
//  1. The subject is codefit's KNOWLEDGE, never the project's authorization
//     state. The zero case says "codefit recognized no ... helper", never
//     "this project has no authorization" — a project can guard every action
//     through a BUILT-IN helper match (e.g. NextAuth-style getServerSession)
//     that this array does not count at all, so a zero count here is not
//     evidence the project is unguarded.
//  2. No claim about bucket contents (resolved_clean, actionable, or any
//     count). Verified false in general:
//     internal/providers/typescript/idor.go's known_authz_detected gate is
//     `authzHelperSet[name] || recognized[name]` — an OR — so a zero
//     registered-helper run does not force resolved_clean to 0; the field
//     project's own 0-of-176 came from using neither a built-in nor a
//     registered helper, a property of THAT project, not a rule this note
//     may state as general.
func recognizedAuthzHelpersNote(helpers []string) string {
	if len(helpers) == 0 {
		return "codefit recognized no project-registered authorization helper for this language. " +
			"known_authz_detected reflects codefit's built-in helper set only — it says nothing about " +
			"whether this project guards its actions some other way. If a project function IS an authz " +
			"helper, register it with " + string(ToolBaselineRegisterAuthzHelper) + " so codefit recognizes it too."
	}
	return fmt.Sprintf("codefit recognized %d project-registered authorization helper(s) for this language, "+
		"named in recognized_authz_helpers. known_authz_detected reflects codefit's built-in helper set "+
		"plus these.", len(helpers))
}

// endpointOrdering is the one sentence the response uses to state HOW its
// endpoint lists are ranked and, when something had to be withheld, what the
// agent is therefore holding. It is a description of behaviour that already
// exists (AggregateEndpoints' sort and the bucket priority), written once so the
// response and the code cannot drift apart.
const endpointOrdering = "endpoints are ranked hardest gap kind first (affirmed → access → exposure → efficiency), " +
	"then by certain-concern count, then by file and line; when the budget forces a cut, whole buckets are " +
	"withheld lowest-priority first (resolved_clean, then frontier_pending, then actionable) and each bucket " +
	"loses its lowest-ranked entries, so what you are holding is always a PREFIX of that order"

// actionableNote phrases the actionable bucket: these are the endpoints codefit
// resolved locally and found a gap in, NAMED with what it takes to rank them.
// It has to say two things the agent cannot infer: that the concern detail is
// deliberately not here and how to get it, and that the deterministic findings
// that ARE here are complete — otherwise an agent reasonably assumes it is
// looking at a truncated finding too and re-fetches a fact it already has.
//
// secRan distinguishes the two reasons this bucket can be empty (D1 site 15,
// same precedent as BudgetBlock.Note at :138-140): a zero count when security
// DID run is a real "nothing to act on"; a zero count when it did NOT run is
// "codefit never looked" and must say so, or an agent reasonably reads
// silence as "clean".
func actionableNote(count int, secRan bool) string {
	if count == 0 {
		if secRan {
			return ""
		}
		return "0 actionable endpoints, but because security did not run for this language, not because " +
			"nothing was found — no local analysis was performed."
	}
	return fmt.Sprintf("%d endpoint(s) codefit resolved locally and found a gap in — act on these. They are "+
		"NAMED with what it takes to rank them (how many concerns and of which categories, which kinds of gap, "+
		"the highest certainty reached, whether an affirmation is present); the surface question and signals "+
		"text is NOT here. Request %s with an endpoint's file for its full concerns — it re-runs the same "+
		"analysis, so what it returns is exactly what is missing here. Deterministic findings are the "+
		"exception: they are facts codefit already concluded, so they are carried IN FULL in "+
		"deterministic_concerns and never need a second call.", count, ToolScanEndpoint)
}

// resolvedCleanNote phrases the resolved-clean bucket as the affirmation it is:
// codefit checked these locally and the controls are present, no gap. It must not
// read as "ignore" — it is information (these are verified), and the full detail is
// a codefit-scan-endpoint call away. secRan: see actionableNote.
func resolvedCleanNote(count int, secRan bool) string {
	if count == 0 {
		if secRan {
			return ""
		}
		return "0 resolved-clean endpoints, but because security did not run for this language, not because " +
			"nothing was found — no local analysis was performed."
	}
	return fmt.Sprintf("%d endpoint(s) codefit resolved locally and found clean: the controls are present "+
		"(authorization and/or field selection) and no gap was detected in the handler body. They are named "+
		"with the verification fact, not detailed — this is a positive check, not an absence of conclusion. "+
		"Request %s with one of these files for its full detail.", count, ToolScanEndpoint)
}

// frontierNote phrases what the frontier-pending list means, honestly. When there
// are no frontier endpoints it is silent (unless security did not run — see
// secRan below). When some endpoints were resolved and some are frontier, it
// explains they are named-only and how to fetch them. When NOTHING was resolved
// locally (all frontier), it states emphatically that codefit concluded nothing
// locally — this is NOT a clean result, every endpoint requires following the
// data in the code — the same principle as the frontier signal wording: absence
// of actionable items is not "clean". secRan: see actionableNote.
func frontierNote(frontierCount, resolvedLocallyCount int, secRan bool) string {
	if frontierCount == 0 {
		if secRan {
			return ""
		}
		return "0 frontier-pending endpoints, but because security did not run for this language, not because " +
			"nothing was found — no local analysis was performed."
	}
	if resolvedLocallyCount == 0 {
		return fmt.Sprintf("codefit concluded nothing locally: every one of these %d endpoint(s) is "+
			"frontier — the data leaves the handler body, so no local analysis could resolve it. This is "+
			"NOT a clean result; it means all of them require following the data in the code. Request "+
			"%s with any of these files for its full concerns.", frontierCount, ToolScanEndpoint)
	}
	return fmt.Sprintf("%d endpoint(s) are frontier-only: the data leaves the handler body, so codefit "+
		"concluded nothing locally about them — they require following the data in the code. They are named "+
		"here, not detailed (the detail would not save the trip to the code). Request %s with one of these "+
		"files for its full concerns.", frontierCount, ToolScanEndpoint)
}

// ScanEndpointRequest is the input to codefit-scan-endpoint: a project root,
// language, and the file (relative to root, as it appears in scan-all's File
// fields) to re-analyse on demand.
type ScanEndpointRequest struct {
	Root     string `json:"root"`
	Language string `json:"language"`
	File     string `json:"file"`
}

// ScanEndpointResponse carries the full per-endpoint detail for one file:
// every handler in it with all its concerns (signals, reason_to_review, certainty,
// fact fields) — the same concern contract as scan-all. Found is false when the
// file has no auditable concerns (not a route handler, or nothing to enumerate).
type ScanEndpointResponse struct {
	File      string                  `json:"file"`
	Found     bool                    `json:"found"`
	Endpoints []report.EndpointReport `json:"endpoints,omitempty"`
	Note      string                  `json:"note,omitempty"`
}

// HandleScanEndpoint re-analyses a single file on demand and returns its endpoints
// with full detail. STATELESS: it re-runs the static analysis over the project and
// filters to the requested file — it retrieves nothing stored. codefit does not
// keep the surface items waiting to be asked for; it recomputes the request. Static
// analysis is cheap, and re-running the same pipeline guarantees the detail here is
// identical to what scan-all would have shown for that endpoint (ADR 0008).
func HandleScanEndpoint(req ScanEndpointRequest) (ScanEndpointResponse, error) {
	res, err := runSecurity(req.Root, req.Language, recognizedHelpers(req.Root, req.Language), scope.Full())
	if err != nil {
		return ScanEndpointResponse{}, err
	}
	endpoints := report.AggregateEndpoints(res.Findings, res.Surface)
	want := filepath.Clean(req.File)
	var matched []report.EndpointReport
	for _, ep := range endpoints {
		if filepath.Clean(ep.File) == want {
			matched = append(matched, ep)
		}
	}
	if len(matched) == 0 {
		return ScanEndpointResponse{
			File:  req.File,
			Found: false,
			Note: "No auditable concerns for this file: it is not a route handler codefit enumerates, " +
				"or there was nothing to surface. This is a fact about the file, not a clearance.",
		}, nil
	}
	return ScanEndpointResponse{File: req.File, Found: true, Endpoints: matched}, nil
}

// providerForLanguage resolves a provider by language name, filtered to
// entries the registry EXPOSES for security scanning (Entry.Exposure.
// SecurityScan) — the MCP adapter is the single CONSUMER of the registry
// here, never the single source of the mapping itself (that is
// internal/providers/registry, D2/D4). A registered-but-unexposed language
// (go today) resolves nil here exactly as an unregistered one would; the
// registry's Exposure field is what makes that gap deliberate, not an
// omission. The project's registered authz helpers (from the baseline) are
// passed to the provider so surface recognition reflects per-project
// knowledge (ADR 0013).
func providerForLanguage(lang string, authzHelpers []string) providers.LanguageProvider {
	e, ok := registry.ByName(lang)
	if !ok || !e.Exposure.SecurityScan {
		return nil
	}
	return e.New(authzHelpers)
}

// SupportedLanguageNames returns the canonical, deduplicated, sorted set of
// language names codefit-scan-all can resolve a security provider for —
// DERIVED from registry.ExposedForSecurity(), never a hand-written literal.
// This is the single source the nothing-measurable error message reads
// (D4/D5): a language and its aliases (ts/tsx) collapse to one canonical name
// here.
func SupportedLanguageNames() []string {
	seen := map[string]bool{}
	for _, e := range registry.ExposedForSecurity() {
		seen[e.Canonical] = true
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// nothingMeasurableError names BOTH reasons codefit-scan-all has nothing to
// audit (D5): no security provider resolved for the language, AND the DB
// dimension did not run. Naming only one would leave the agent guessing which
// of two independent gaps to fix. When the DB dimension was ATTEMPTED but not
// measured (a configured schema that failed to parse/read, or was narrowed
// out of scope), dbSection carries the specific reason in its Note — that
// text is used verbatim instead of the generic "not configured" clause, so
// the error never claims "nothing configured" over a schema that IS
// configured but could not be read.
func nothingMeasurableError(language string, dbSection *DBSection) error {
	dbReason := "the database dimension did not run (no database.schema_paths configured in .codefit.yaml)"
	if dbSection != nil && dbSection.Note != "" {
		dbReason = dbSection.Note
	}
	return fmt.Errorf("nothing to audit: no security provider for language %q (supported: %s), and %s",
		language, strings.Join(SupportedLanguageNames(), ", "), dbReason)
}

// runDBForScanAll resolves the schema parser by input and runs the DB sensor,
// returning the section, its raw result (for baseline filtering), and whether it
// actually measured. Every not-measured/failure path is SOFT: it returns a
// Measured=false section with a note and ran=false — never an error, so a db
// misconfiguration can never blank the security audit (ADR 0020).
func runDBForScanAll(root, language string, cfg *config.Config, rules []crossrules.Rule, scp scope.Scope) (*DBSection, findings.SensorResult, bool) {
	parser, note := schemasource.ParserForPaths(root, cfg.Database.SchemaPaths, cfg.Database.Type)
	if parser == nil {
		return &DBSection{Measured: false, Note: note}, findings.SensorResult{}, false
	}
	// The DB sensor reads the CONFIGURED schema paths in full, never a narrowed
	// subset: a schema judged from half its migrations is a shrunken census, and
	// the caller has already decided (dbInputsInScope) whether the dimension runs
	// at all this pass. So its context carries a full scope, deliberately.
	ctx := auditctx.AuditContext{ProjectRoot: root, Language: language, Config: cfg, Scope: scope.Full()}
	r, err := dbsensor.New(parser).Audit(ctx)
	if err != nil {
		return &DBSection{Measured: false, Note: "db audit failed: " + err.Error()}, findings.SensorResult{}, false
	}
	if !r.Measured {
		return &DBSection{Measured: false, Note: r.Note}, findings.SensorResult{}, false
	}

	// Code↔schema cross (index-vs-query infra, ADR 0029): the adapter is the only
	// place that knows BOTH the code provider and the parsed schema. It extracts the
	// neutral query filters from the code and runs the neutral cross-runner over
	// (schema, filters), merging its output into the db result. With crossrules.All()
	// empty this slice, this is a proven no-op — the seam, never an output change.
	res := r.Res
	crossF, crossS, crossSkip := runCross(root, language, r.Schema, r.SchemaContent, rules, scp)
	res.Findings = append(res.Findings, crossF...)
	res.Surface = append(res.Surface, crossS...)

	// r.Note carries the sensor's composed audit trace (design SS4 "scanall.go
	// Note Leak" / SS7a) — the completeness inventory plus any 3NF-suppression
	// trace. The unmeasured paths above already carry Note; this was the one
	// path that dropped it. crossSkip (D6) names WHY the cross did not run, so a
	// DB-only pass over a language without a QueryExtractor does not read as the
	// cross simply having nothing to say.
	fullNote := r.Note
	if crossSkip != "" {
		if fullNote != "" {
			fullNote += " " + crossSkip
		} else {
			fullNote = crossSkip
		}
	}
	return &DBSection{Measured: true, Note: fullNote, Score: res.Score}, res, true
}

// runCross extracts the code's query filters (when the language provider implements
// QueryExtractor) and runs the given cross-rule set over the schema and those
// filters, stamping the emitted items so they carry a baseline fingerprint (they
// are produced AFTER the db sensor, so they miss its stamping — content is the
// parsed schema content, exposed on the sensor result). The rule set is INJECTED:
// production passes crossrules.All(); the seam gate passes nil. The cross is SOFT: a
// read/parse hiccup never blanks the db result (ADR 0020) — the walk swallows
// per-file errors, mirroring the security walk's resilience.
//
// The third return is the SKIP REASON (D6): empty when the cross ran, otherwise
// naming why it did not — the schema was not parsed, or (the DB-only path's
// permanent state) the language's provider has no QueryExtractor, so the code
// x schema cross rule family (DB-010/DB-013) was never evaluated. Silence on
// this path would read as "the cross ran and found nothing", which is false.
func runCross(root, language string, schema *db.Schema, content map[string][]byte, rules []crossrules.Rule, scp scope.Scope) ([]findings.Finding, []findings.SurfaceItem, string) {
	if schema == nil {
		return nil, nil, "schema not parsed"
	}
	provider := providerForLanguage(language, nil)
	extractor, ok := provider.(providers.QueryExtractor)
	if !ok {
		return nil, nil, fmt.Sprintf(
			"language %q has no query extractor — the code x schema cross (DB-010/DB-013) was not evaluated", language)
	}
	filters := collectQueryFilters(root, provider.FileExtensions(), extractor, scp)
	fs, surf := crossrules.RunWith(schema, filters, rules)
	dbsensor.StampSurface(surf, content)
	return fs, surf, ""
}

// dbInputsInScope reports whether a pass narrowed to scp has any reason to audit
// the database dimension: whether at least one CONFIGURED schema path is in
// scope. The DB dimension reads database.schema_paths, not a repository walk
// (ADR 0014), so this is the whole of R4's question — a "no" leaves by_dimension
// db null (not measured) through the machinery that already exists, rather than
// scoring an untouched schema 100.
//
// A configured path may name a DIRECTORY of migrations, so a scoped file INSIDE
// one counts. The prefix test is on canonical path SEGMENTS ("db/migrations/"),
// never raw text, so db/migrations-old is not mistaken for db/migrations.
func dbInputsInScope(schemaPaths []string, scp scope.Scope) bool {
	if !scp.Narrows() {
		return true
	}
	for _, p := range schemaPaths {
		configured := scope.Canon(p)
		if scp.Includes(configured) {
			return true
		}
		for _, f := range scp.Files() {
			if strings.HasPrefix(f, configured+"/") {
				return true
			}
		}
	}
	return false
}

// crossSkipDirs are directories never walked for query extraction — the same set
// the security sensor skips.
var crossSkipDirs = map[string]bool{
	".git": true, "vendor": true, "node_modules": true,
	"dist": true, "bin": true, ".codefit": true,
}

// collectQueryFilters walks the project's source files of the provider's extensions
// and unions the query filters each yields. Per-file read/extract errors are
// swallowed (the cross is soft, never fatal to the db result); the file path is
// made project-relative, matching the anchors the security surface uses.
// scp is layer 0, the same narrowing the security walk applies: a partial pass
// extracts filters only from the files in scope. The gate is Narrows, not
// Includes, so an unset scope collects everything (a zero-value Scope includes
// nothing, and reading it as a filter here would silently empty the cross).
func collectQueryFilters(root string, exts []string, ex providers.QueryExtractor, scp scope.Scope) []query.QueryFilter {
	narrowed := scp.Narrows()
	var out []query.QueryFilter
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if crossSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !slices.Contains(exts, filepath.Ext(path)) {
			return nil
		}
		if narrowed {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			if !scp.Includes(filepath.ToSlash(rel)) {
				return nil // layer 0: out of scope, never opened
			}
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		// Same bytes-to-text decode the two sensors do (sensors/db/sources.go,
		// sensors/security/security.go): a BOM-marked source file used to reach
		// the extractor as NUL-interleaved bytes and yield no filter at all.
		// Milder here — the cross emits surface items, never an affirmation, so
		// the cost is questions not asked rather than a false finding — but the
		// same blindness, silent in the same way.
		content, _ := sourcetext.Decode(raw)
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		ff, xerr := ex.ExtractQueryFilters(providers.SourceFile{Path: rel, Content: content})
		if xerr != nil {
			return nil
		}
		out = append(out, ff...)
		return nil
	})
	return out
}
