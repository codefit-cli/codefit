package mcp

import (
	"fmt"
	"path/filepath"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/core/baseline"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/report"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
	dbsensor "github.com/codefit-cli/codefit/internal/sensors/db"
)

// ScanAllRequest is the input to codefit-scan-all: a project root and language.
// codefit walks the project, runs the deterministic sensor and the surface
// queries, and returns the complete per-endpoint picture.
type ScanAllRequest struct {
	Root     string `json:"root"`
	Language string `json:"language"`
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
	Summary         ScanAllSummary          `json:"summary"`
	Baseline        BaselineDelta           `json:"baseline"`
	Actionable      []report.EndpointReport `json:"actionable"`
	ResolvedClean   ResolvedClean           `json:"resolved_clean"`
	FrontierPending FrontierPending         `json:"frontier_pending"`
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

// ResolvedClean declares the endpoints codefit resolved locally and found clean
// (controls present, no gap). They are NAMED with a verification fact, not
// detailed. This is an affirmation — codefit looked and it is clean — not a
// generic "not detailed" bucket; that is why it is separate from FrontierPending.
type ResolvedClean struct {
	Count     int                            `json:"count"`
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
	// Load the committed baseline ONCE: it provides both the project's registered
	// authz helpers (recognized during the scan) and the previous items the unified
	// diff is computed against (diffBaseline reuses this same load — no double read).
	path := filepath.Join(req.Root, baseline.Name)
	prev, err := baseline.Load(path)
	if err != nil {
		return ScanAllResponse{}, fmt.Errorf("loading baseline: %w", err)
	}

	// Security ALWAYS runs and is the required core: if it fails, scan-all fails.
	secRes, err := runSecurity(req.Root, req.Language, prev.RecognizedAuthzHelpers(req.Language))
	if err != nil {
		return ScanAllResponse{}, err
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
		return ScanAllResponse{}, fmt.Errorf("loading project config: %w", err)
	}
	var dbRes findings.SensorResult
	var dbSection *DBSection
	dbRan := false
	if cfg != nil && len(cfg.Database.SchemaPaths) > 0 {
		dbSection, dbRes, dbRan = runDBForScanAll(req.Root, req.Language, cfg)
	}

	// One unified baseline diff over both sensors' observations, scoped to the
	// categories of the sensors that ran (ADR 0019), persisted once.
	scanned := securityScope(req.Language)
	if dbRan {
		for _, c := range dbsensor.New(nil).OwnedCategories() {
			scanned[c] = true
		}
	}
	diff, delta, err := diffBaseline(prev, path, observedFrom(secRes, dbRes), scanned)
	if err != nil {
		return ScanAllResponse{}, err
	}

	// Two presentations of the same diff: endpoints for security, a flat section for db.
	actionable, clean, frontier := report.ClassifyEndpoints(filterEndpointsByBaseline(endpoints, diff))
	if dbSection != nil && dbSection.Measured {
		dbSection.Findings, dbSection.Surface = filterDBByBaseline(dbRes, diff)
	}

	resolvedLocally := len(actionable) + len(clean)
	return ScanAllResponse{
		Summary: ScanAllSummary{
			Endpoints:             len(endpoints),
			DeterministicFindings: len(secRes.Findings),
			SurfaceItems:          len(secRes.Surface),
			CertainConcerns:       certain,
		},
		Baseline:   delta,
		Actionable: actionable,
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
	}, nil
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
	res, err := runSecurity(req.Root, req.Language, recognizedHelpers(req.Root, req.Language))
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

// providerForLanguage resolves a provider by language name — the MCP adapter is
// the single place that maps language → provider (the core never does). The
// project's registered authz helpers (from the baseline) are passed to the
// provider so surface recognition reflects per-project knowledge (ADR 0013).
func providerForLanguage(lang string, authzHelpers []string) providers.LanguageProvider {
	switch lang {
	case "typescript", "ts", "tsx":
		return typescript.New(typescript.WithAuthzHelpers(authzHelpers))
	default:
		return nil
	}
}

// runDBForScanAll resolves the schema parser by input and runs the DB sensor,
// returning the section, its raw result (for baseline filtering), and whether it
// actually measured. Every not-measured/failure path is SOFT: it returns a
// Measured=false section with a note and ran=false — never an error, so a db
// misconfiguration can never blank the security audit (ADR 0020).
func runDBForScanAll(root, language string, cfg *config.Config) (*DBSection, findings.SensorResult, bool) {
	parser, note := schemaParserForPaths(root, cfg.Database.SchemaPaths)
	if parser == nil {
		return &DBSection{Measured: false, Note: note}, findings.SensorResult{}, false
	}
	ctx := auditctx.AuditContext{ProjectRoot: root, Language: language, Config: cfg}
	r, err := dbsensor.New(parser).Audit(ctx)
	if err != nil {
		return &DBSection{Measured: false, Note: "db audit failed: " + err.Error()}, findings.SensorResult{}, false
	}
	if !r.Measured {
		return &DBSection{Measured: false, Note: r.Note}, findings.SensorResult{}, false
	}
	return &DBSection{Measured: true, Score: r.Res.Score}, r.Res, true
}
