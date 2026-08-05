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
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
	dbsensor "github.com/codefit-cli/codefit/internal/sensors/db"
)

// ResponseBudgetBytes is the byte budget codefit-scan-all declares for its own
// serialized response.
//
// What is MEASURED, against a real MCP client (Claude Code, 2026-08-04): a
// 312 692-character response was REJECTED, and a 40 282-byte one was ACCEPTED.
// That is the whole of the evidence — the client's actual cap was never
// observed, only bracketed.
//
// What is DERIVED: MCP clients cap tool output (Claude Code's default is 25 000
// tokens) and this dense JSON runs around 3 bytes per token, which puts the cap
// near 75 KB. 60 KB sits under that and inside the measured bracket.
//
// The two are not the same kind of statement, and the gap is real: 60 KB is
// still 49% above the largest response proven to arrive. It is the number to
// revisit the first time a response is rejected under it — and because the
// budget is declared rather than silently applied, that rejection would be
// visible rather than a quiet half-answer.
//
// The budget is DECLARED in the response (see BudgetBlock) and enforced by
// withholding the lowest-ranked endpoints, never by truncating the payload: a
// clipped response that reads like a complete one is the one outcome forbidden
// (ADR 0054, same principle as ADR 0048).
const ResponseBudgetBytes = 60_000

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
// empty even though Summary.CertainConcerns (computed before filtering) is > 0 —
// the difference is exactly the "known" surface the baseline is silencing.
type ScanAllResponse struct {
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
	// DB is the parallel database-structure section — the db dimension's findings
	// and surface, baseline-filtered. It is NON-endpoint (a table has no route), so
	// it is its own section, not one of the three endpoint buckets. Nil when the
	// project has no database.schema_paths configured, so a project without a
	// database yields a response byte-identical to before db was wired (ADR 0020).
	DB *DBSection `json:"db,omitempty"`
}

// DBSection is the database dimension's result inside scan-all. Measured=false with
// a Note is the honest "not audited" state (disabled / no parser / schema read or
// parse failure) — a db failure is SOFT here, reported but never fatal to the
// security result (ADR 0020). Findings are affirmations (e.g. DB-050); Surface are
// questions; both are already filtered by the baseline.
type DBSection struct {
	Measured bool                   `json:"measured"`
	Note     string                 `json:"note,omitempty"`
	Findings []findings.Finding     `json:"findings,omitempty"`
	Surface  []findings.SurfaceItem `json:"surface,omitempty"`
	Score    int                    `json:"score"`
}

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

// ScanAllSummary is the at-a-glance count, not a judgment.
type ScanAllSummary struct {
	Endpoints             int `json:"endpoints"`
	DeterministicFindings int `json:"deterministic_findings"`
	SurfaceItems          int `json:"surface_items"`
	CertainConcerns       int `json:"certain_concerns"`
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
	resp, actionable, err := buildScanAll(req, scp, baselinePath)
	if err != nil {
		return ScanAllResponse{}, err
	}
	return withNamedActionable(resp, actionable, budget), nil
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
func buildScanAll(req ScanAllRequest, scp scope.Scope, baselinePath string) (ScanAllResponse, []report.EndpointReport, error) {
	// Load the committed baseline ONCE: it provides both the project's registered
	// authz helpers (recognized during the scan) and the previous items the unified
	// diff is computed against (diffBaseline reuses this same load — no double read).
	path := baselinePath
	prev, err := baseline.Load(path)
	if err != nil {
		return ScanAllResponse{}, nil, fmt.Errorf("loading baseline: %w", err)
	}

	// Security runs only when a language provider resolves for req.Language — the
	// DB dimension does not need one (its schema parser is picked from the
	// configured schema paths, not the language). secRan is a PREDICATE: the
	// provider itself is discarded here, resolved again inside runSecurity. A nil
	// provider is the only thing made non-fatal; a config-load or sensor error
	// inside runSecurity stays a hard error (D1).
	secRan := providerForLanguage(req.Language, nil) != nil
	var secRes findings.SensorResult
	if secRan {
		var err error
		secRes, err = runSecurity(req.Root, req.Language, prev.RecognizedAuthzHelpers(req.Language), scp)
		if err != nil {
			return ScanAllResponse{}, nil, err
		}
	}
	endpoints := report.AggregateEndpoints(secRes.Findings, secRes.Surface)
	certain := 0
	for _, ep := range endpoints {
		certain += ep.CertainConcerns
	}

	// DB runs only when database.schema_paths is configured (the dimension applies
	// to this project). A DB failure is SOFT — reported in its section, never fatal
	// to security (ADR 0020). No schema_paths → no DB section → byte-identical to
	// before db was wired.
	cfg, err := config.LoadOptional(filepath.Join(req.Root, ".codefit.yaml"))
	if err != nil {
		return ScanAllResponse{}, nil, fmt.Errorf("loading project config: %w", err)
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
		return ScanAllResponse{}, nil, nothingMeasurableError(req.Language, dbSection)
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
	diff, delta, err := diffBaseline(prev, path, observedFrom(secRes, dbRes), scanned, scp)
	if err != nil {
		return ScanAllResponse{}, nil, err
	}

	// Two presentations of the same diff: endpoints for security, a flat section for db.
	actionable, clean, frontier := report.ClassifyEndpoints(filterEndpointsByBaseline(endpoints, diff))
	if dbSection != nil && dbSection.Measured {
		dbSection.Findings, dbSection.Surface = filterDBByBaseline(dbRes, diff)
	}

	// Scoring (ADR 0021), over RAW findings (not baseline-filtered) so the global
	// equals the flat security score exactly when db is absent — no value
	// regression. measured/scored were HOISTED above (see the nothing-measurable
	// guard). The guard below fails loudly if a measured dimension has no weight
	// (a wiring bug), never a silently incomplete score.
	if missing := scoring.MissingWeights(measured, scoring.DefaultWeights()); len(missing) > 0 {
		return ScanAllResponse{}, nil, fmt.Errorf("codefit internal: measured dimension(s) without a weight: %v", missing)
	}
	score := scoring.Compute(measured, scored, scoring.DefaultWeights())

	// The scope block unions what BOTH dimensions examined: the security walk's
	// files and, when the db dimension ran, the configured schema sources it read.
	// Without the union a requested schema path would be reported unmatched even
	// though the audit read it — the security walk does not open .prisma files.
	block := scopeBlockFor(scp, append(append([]string{}, secRes.AuditedFiles...), dbRes.AuditedFiles...), secRes.AuditableTotal)
	if err := block.Validate(); err != nil {
		return ScanAllResponse{}, nil, err
	}

	// Every count below is taken from the COMPLETE classification, never from what
	// the rendering will end up showing (R4).
	resolvedLocally := len(actionable) + len(clean)
	return ScanAllResponse{
		Scope: block,
		Summary: ScanAllSummary{
			Endpoints:             len(endpoints),
			DeterministicFindings: len(secRes.Findings),
			SurfaceItems:          len(secRes.Surface),
			CertainConcerns:       certain,
		},
		Score:    score,
		Baseline: delta,
		Actionable: ActionableSection{
			Count: len(actionable),
			Note:  actionableNote(len(actionable)),
			// Endpoints is filled by the rendering step (withNamedActionable), from
			// the complete list returned alongside this response.
		},
		ResolvedClean: ResolvedClean{
			Count:     len(clean),
			Note:      resolvedCleanNote(len(clean)),
			Endpoints: clean,
		},
		FrontierPending: FrontierPending{
			Count:     len(frontier),
			Note:      frontierNote(len(frontier), resolvedLocally),
			Endpoints: frontier,
		},
		DB: dbSection,
	}, actionable, nil
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
func actionableNote(count int) string {
	if count == 0 {
		return ""
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
// a codefit-scan-endpoint call away.
func resolvedCleanNote(count int) string {
	if count == 0 {
		return ""
	}
	return fmt.Sprintf("%d endpoint(s) codefit resolved locally and found clean: the controls are present "+
		"(authorization and/or field selection) and no gap was detected in the handler body. They are named "+
		"with the verification fact, not detailed — this is a positive check, not an absence of conclusion. "+
		"Request %s with one of these files for its full detail.", count, ToolScanEndpoint)
}

// frontierNote phrases what the frontier-pending list means, honestly. When there
// are no frontier endpoints it is silent. When some endpoints were resolved and
// some are frontier, it explains they are named-only and how to fetch them. When
// NOTHING was resolved locally (all frontier), it states emphatically that codefit
// concluded nothing locally — this is NOT a clean result, every endpoint requires
// following the data in the code — the same principle as the frontier signal
// wording: absence of actionable items is not "clean".
func frontierNote(frontierCount, resolvedLocallyCount int) string {
	if frontierCount == 0 {
		return ""
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

// languageProviders is THE supported set — the single source providerForLanguage
// and SupportedLanguageNames both read, so the two can never hand-diverge into a
// fourth list (D4). "typescript", "ts", "tsx" all alias the one canonical
// TypeScript provider; adding an entry here is the one place that expands what
// codefit-scan-all can resolve a security provider for (Lock A guards this
// exact set staying {typescript, ts, tsx} until a future change deliberately
// grows it).
var languageProviders = map[string]func(authzHelpers []string) providers.LanguageProvider{
	"typescript": func(authzHelpers []string) providers.LanguageProvider {
		return typescript.New(typescript.WithAuthzHelpers(authzHelpers))
	},
	"ts":  func(authzHelpers []string) providers.LanguageProvider { return typescript.New(typescript.WithAuthzHelpers(authzHelpers)) },
	"tsx": func(authzHelpers []string) providers.LanguageProvider { return typescript.New(typescript.WithAuthzHelpers(authzHelpers)) },
}

// providerForLanguage resolves a provider by language name — the MCP adapter is
// the single place that maps language → provider (the core never does). The
// project's registered authz helpers (from the baseline) are passed to the
// provider so surface recognition reflects per-project knowledge (ADR 0013).
func providerForLanguage(lang string, authzHelpers []string) providers.LanguageProvider {
	ctor, ok := languageProviders[lang]
	if !ok {
		return nil
	}
	return ctor(authzHelpers)
}

// SupportedLanguageNames returns the canonical, deduplicated, sorted set of
// language names codefit-scan-all can resolve a security provider for — DERIVED
// from what languageProviders actually constructs (each entry's Language()),
// never a hand-written literal. This is the single source the nothing-measurable
// error message reads (D4/D5): a language and its aliases (ts/tsx) collapse to
// one canonical name here.
func SupportedLanguageNames() []string {
	seen := map[string]bool{}
	for _, ctor := range languageProviders {
		seen[ctor(nil).Language()] = true
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
	parser, note := schemaParserForPaths(root, cfg.Database.SchemaPaths, cfg.Database.Type)
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
	crossF, crossS := runCross(root, language, r.Schema, r.SchemaContent, rules, scp)
	res.Findings = append(res.Findings, crossF...)
	res.Surface = append(res.Surface, crossS...)

	// r.Note carries the sensor's composed audit trace (design SS4 "scanall.go
	// Note Leak" / SS7a) — the completeness inventory plus any 3NF-suppression
	// trace. The unmeasured paths above already carry Note; this was the one
	// path that dropped it.
	return &DBSection{Measured: true, Note: r.Note, Score: res.Score}, res, true
}

// runCross extracts the code's query filters (when the language provider implements
// QueryExtractor) and runs the given cross-rule set over the schema and those
// filters, stamping the emitted items so they carry a baseline fingerprint (they
// are produced AFTER the db sensor, so they miss its stamping — content is the
// parsed schema content, exposed on the sensor result). The rule set is INJECTED:
// production passes crossrules.All(); the seam gate passes nil. Returns nothing
// when the schema is nil or the provider has no extractor. The cross is SOFT: a
// read/parse hiccup never blanks the db result (ADR 0020) — the walk swallows
// per-file errors, mirroring the security walk's resilience.
func runCross(root, language string, schema *db.Schema, content map[string][]byte, rules []crossrules.Rule, scp scope.Scope) ([]findings.Finding, []findings.SurfaceItem) {
	if schema == nil {
		return nil, nil
	}
	provider := providerForLanguage(language, nil)
	extractor, ok := provider.(providers.QueryExtractor)
	if !ok {
		return nil, nil
	}
	filters := collectQueryFilters(root, provider.FileExtensions(), extractor, scp)
	fs, surf := crossrules.RunWith(schema, filters, rules)
	dbsensor.StampSurface(surf, content)
	return fs, surf
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
