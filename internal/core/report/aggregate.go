package report

import (
	"sort"
	"strconv"
	"strings"

	"github.com/codefit-cli/codefit/internal/core/findings"
)

// CertaintyLevel ranks how certain codefit is about a concern — the report's
// epistemological honesty. A deterministic finding is an ASSERTION (codefit saw
// a conclusive pattern, confidence 1.0). A surface concern is a QUESTION codefit
// hands to the agent: structurally confirmed (it saw the shape locally) or at the
// frontier (the data left the handler body, codefit could not see). The agent
// must distinguish what codefit affirms from what it asks; it never flattens them.
type CertaintyLevel string

const (
	Deterministic    CertaintyLevel = "deterministic"     // codefit affirms (rule, 1.0)
	SurfaceConfirmed CertaintyLevel = "surface_confirmed" // codefit asks; saw the shape locally
	SurfaceFrontier  CertaintyLevel = "surface_frontier"  // codefit asks; the data left the body
)

// Concern is one thing to review about an endpoint, from either source — a
// deterministic rule finding or a mapped surface item. Affirms records whether
// codefit asserts it (deterministic) or asks it (surface), so the distinction is
// never ambiguous.
type Concern struct {
	Certainty     CertaintyLevel `json:"certainty"`
	Affirms       bool           `json:"affirms"` // true: codefit asserts a fact; false: codefit asks the agent
	Source        string         `json:"source"`  // "rule" | "surface"
	ID            string         `json:"id"`
	Category      string         `json:"category"` // security | idor | authz | overfetch
	Title         string         `json:"title"`
	Description   string         `json:"description,omitempty"` // deterministic finding description
	Signals       []string       `json:"signals,omitempty"`     // surface structural_signals (facts)
	Question      string         `json:"question,omitempty"`    // surface reason_to_review
	Line          int            `json:"line"`
	Confidence    float64        `json:"confidence"`
	Probabilistic bool           `json:"probabilistic"`
	// RefinesAuthz marks an IDOR concern whose endpoint also has an authz concern:
	// IDOR is the structural refinement of authz (the sensitive handler also
	// receives a client id). A fact about structure, not a judgment.
	RefinesAuthz bool `json:"refines_authz,omitempty"`
	// Actionable is true when the concern is a missing/broken control codefit
	// detected (an affirmed finding, no authz/ownership check, or no field limit).
	Actionable bool `json:"actionable"`
	// Gap names the KIND of missing control, hardest first: "affirmed" (a
	// deterministic finding), "access" (no authz/ownership on a sensitive
	// handler), "exposure" (a serialization with no select/omit). Empty when the
	// concern is not an actionable gap (e.g. a checked handler, or the frontier).
	Gap string `json:"gap,omitempty"`
	// Fingerprint is the concern's baseline content identity (findings.Fingerprint).
	// The agent passes it to codefit-baseline-accept / -prune.
	Fingerprint string `json:"fingerprint,omitempty"`
	// Baseline is the concern's delta vs the previous baseline, set by the scan-all
	// handler: "new" | "changed" | "known" | "acknowledged" (empty when no baseline).
	Baseline string `json:"baseline,omitempty"`
}

const (
	gapAffirmed   = "affirmed"
	gapAccess     = "access"
	gapExposure   = "exposure"
	gapEfficiency = "efficiency"
)

// EndpointReport is the complete picture of one handler: all its concerns from
// both sources, ordered by certainty (deterministic → confirmed → frontier).
type EndpointReport struct {
	File            string    `json:"file"`
	Line            int       `json:"line"`             // handler anchor line (0 = module scope)
	Method          string    `json:"method,omitempty"` // GET/POST/... when known
	Actionable      int       `json:"actionable"`       // count of missing/broken-control concerns
	CertainConcerns int       `json:"certain_concerns"` // deterministic + surface_confirmed
	Concerns        []Concern `json:"concerns"`
}

// AggregateEndpoints groups deterministic findings and surface items by the
// handler they belong to and assembles the per-endpoint picture: concerns ordered
// by certainty within an endpoint (deterministic → confirmed → frontier), and
// endpoints ordered by their ACTIONABLE structural gaps, hardest kind first
// (affirmed deterministic → missing access control → over-exposure), then by
// certain-concern count. This surfaces the real findings, not the most
// instrumented endpoints. Ordering is by FACT (which control is missing), never
// by severity — the agent judges danger. It invents nothing: every concern comes
// from a real finding or surface item with its id.
func AggregateEndpoints(fs []findings.Finding, surface []findings.SurfaceItem) []EndpointReport {
	anchors := map[string][]int{}
	methodAt := map[string]map[int]string{}
	for _, it := range surface {
		if it.Category != "idor" && it.Category != "authz" {
			continue
		}
		anchors[it.File] = append(anchors[it.File], it.Line)
		if methodAt[it.File] == nil {
			methodAt[it.File] = map[int]string{}
		}
		if m := methodFromSignals(it.StructuralSignals); m != "" {
			methodAt[it.File][it.Line] = m
		}
	}
	for f := range anchors {
		sort.Ints(anchors[f])
	}

	bin := func(file string, line int) int {
		best := 0
		for _, a := range anchors[file] {
			if a <= line {
				best = a
			} else {
				break
			}
		}
		return best
	}

	groups := map[string]*EndpointReport{}
	order := []string{}
	get := func(file string, ln int) *EndpointReport {
		key := file + ":" + strconv.Itoa(ln)
		ep := groups[key]
		if ep == nil {
			ep = &EndpointReport{File: file, Line: ln, Method: methodAt[file][ln]}
			groups[key] = ep
			order = append(order, key)
		}
		return ep
	}

	for _, f := range fs {
		ep := get(f.File, bin(f.File, f.Line))
		ep.Concerns = append(ep.Concerns, concernFromFinding(f))
	}
	for _, it := range surface {
		ep := get(it.File, bin(it.File, it.Line))
		ep.Concerns = append(ep.Concerns, concernFromSurface(it))
	}

	out := make([]EndpointReport, 0, len(order))
	for _, key := range order {
		ep := groups[key]
		sort.SliceStable(ep.Concerns, func(i, j int) bool {
			return certaintyRank(ep.Concerns[i].Certainty) < certaintyRank(ep.Concerns[j].Certainty)
		})
		markRefinement(ep)
		ep.CertainConcerns = countCertain(ep.Concerns)
		ep.Actionable = countActionable(ep.Concerns)
		out = append(out, *ep)
	}
	// Order by ACTIONABLE structural gaps, hardest kind first (affirmed →
	// access → exposure → efficiency), then by certain-concern count. This
	// surfaces the real findings (a missing access check, an affirmed
	// vulnerability) above endpoints that are merely heavily instrumented but
	// protected. It is ordering by FACT (which control is missing), never by
	// severity — the agent judges danger. Access gaps outrank exposure gaps
	// because over-fetch with no select is ubiquitous (every serialization) and
	// would otherwise drown the access-control findings (ADR 0006, validated on
	// Bitácora). Efficiency (N+1) is ranked LAST for the same reason: an N+1
	// must never outrank an access-control gap in the summary.
	sort.SliceStable(out, func(i, j int) bool {
		ai, ci, ei, fi := gapCounts(out[i])
		aj, cj, ej, fj := gapCounts(out[j])
		switch {
		case ai != aj:
			return ai > aj // affirmed deterministic
		case ci != cj:
			return ci > cj // missing access control
		case ei != ej:
			return ei > ej // over-exposure
		case fi != fj:
			return fi > fj // efficiency (N+1) — ranked last
		case out[i].CertainConcerns != out[j].CertainConcerns:
			return out[i].CertainConcerns > out[j].CertainConcerns
		case out[i].File != out[j].File:
			return out[i].File < out[j].File
		default:
			return out[i].Line < out[j].Line
		}
	})
	return out
}

// ActionableEndpoint NAMES an endpoint that codefit resolved locally and found a
// gap in. It is the same discipline FrontierEndpoint has always followed, finally
// applied to the bucket that carries 99% of the payload: the endpoint is named
// with enough for the agent to RANK and CHOOSE, and the ~800-byte-per-concern
// question/signals text is fetched on demand with codefit-scan-endpoint, which is
// stateless and recomputes the identical analysis (ADR 0008, ADR 0054).
//
// "Enough to rank" is the whole contract of this type, and every field earns its
// place against it: how many concerns and of which categories, how many are
// actionable gaps and of which KIND (hardest first), the best certainty codefit
// reached, and whether a deterministic affirmation is in there.
//
// Deterministic is the ONE exception to naming, and it is not a compromise: a
// deterministic finding is a fact codefit already concluded, not a question it is
// handing over. Hiding it behind a second call would make a scan's headline result
// depend on the agent choosing to look. Those concerns stay here IN FULL — they
// are rare by construction and are not what makes the payload big.
type ActionableEndpoint struct {
	File   string `json:"file"`
	Line   int    `json:"line,omitempty"`
	Method string `json:"method,omitempty"`
	// Concerns is how many concerns the endpoint has in total; Actionable how many
	// of them are a missing/broken control; CertainConcerns how many codefit
	// resolved locally (deterministic + surface_confirmed).
	Concerns        int      `json:"concerns"`
	Actionable      int      `json:"actionable"`
	CertainConcerns int      `json:"certain_concerns"`
	Categories      []string `json:"categories"`
	// Gaps names the KINDS of missing control present, hardest first — the same
	// order the endpoint list is ranked by, so an agent can see WHY an endpoint is
	// where it is without fetching it.
	Gaps []string `json:"gaps,omitempty"`
	// HighestCertainty is the best certainty codefit reached on this endpoint.
	HighestCertainty CertaintyLevel `json:"highest_certainty"`
	// HasAffirmation is true when at least one concern is something codefit
	// AFFIRMS rather than asks. An agent must not have to fetch a file to learn a
	// fact exists.
	HasAffirmation bool `json:"has_affirmation"`
	// Deterministic carries the endpoint's deterministic concerns in full (see the
	// type comment). Empty on the ordinary endpoint, whose concerns are all surface.
	Deterministic []Concern `json:"deterministic_concerns,omitempty"`
}

// gapOrder is the gap kinds hardest first — the same ranking AggregateEndpoints
// sorts endpoints by, so ActionableEndpoint.Gaps reads in the order that decides
// the endpoint's place in the list.
var gapOrder = []string{gapAffirmed, gapAccess, gapExposure, gapEfficiency}

// NameActionable turns the complete per-endpoint reports into their named
// summaries, in the same order. It is a pure RENDERING narrowing: it reads the
// endpoints, it computes nothing new and it drops no endpoint — the set it
// returns is exactly the set it was given. Anything derived from the audit (the
// score, the baseline delta, the counts) is computed before this and never from
// its output.
func NameActionable(eps []EndpointReport) []ActionableEndpoint {
	out := make([]ActionableEndpoint, 0, len(eps))
	for _, ep := range eps {
		n := ActionableEndpoint{
			File:            ep.File,
			Line:            ep.Line,
			Method:          ep.Method,
			Concerns:        len(ep.Concerns),
			Actionable:      ep.Actionable,
			CertainConcerns: ep.CertainConcerns,
			Categories:      categoriesOf(ep),
		}
		// Concerns are already sorted deterministic → confirmed → frontier, so the
		// first one carries the best certainty codefit reached here.
		if len(ep.Concerns) > 0 {
			n.HighestCertainty = ep.Concerns[0].Certainty
		}
		present := map[string]bool{}
		for _, c := range ep.Concerns {
			if c.Affirms {
				n.HasAffirmation = true
			}
			if c.Certainty == Deterministic {
				n.Deterministic = append(n.Deterministic, c)
			}
			if c.Gap != "" {
				present[c.Gap] = true
			}
		}
		for _, g := range gapOrder {
			if present[g] {
				n.Gaps = append(n.Gaps, g)
			}
		}
		out = append(out, n)
	}
	return out
}

// FrontierEndpoint names a frontier-only endpoint — every one of its concerns is
// surface_frontier (the data left the handler body, local_access_detected=false),
// so codefit concluded nothing locally about it. It is NAMED, not detailed: the
// agent goes to the code to follow the data regardless, so the concern detail
// would not save it the trip (ADR 0008). It carries the file and the categories
// at stake, one line, for the agent to know what is pending and request the full
// detail on demand via codefit-scan-endpoint.
type FrontierEndpoint struct {
	File       string   `json:"file"`
	Line       int      `json:"line,omitempty"`
	Method     string   `json:"method,omitempty"`
	Categories []string `json:"categories"`
}

// ResolvedCleanEndpoint names an endpoint codefit resolved LOCALLY and found
// clean: it accessed data locally (CertainConcerns>0) and codefit verified the
// controls are present — no gap. It is NAMED (file, method) plus one VERIFICATION
// FACT that affirms what codefit checked ("an authorization check is present;
// field selection is present — no gap found"). This is the crux of the three-
// bucket split: resolved_clean is an AFFIRMATION (codefit looked and it is clean),
// epistemologically OPPOSITE to a frontier endpoint (codefit could NOT conclude).
// Flattening the two into a single "not detailed" bucket would be the same error
// as the old frontier wording — so they are kept distinct on purpose.
type ResolvedCleanEndpoint struct {
	File         string `json:"file"`
	Line         int    `json:"line,omitempty"`
	Method       string `json:"method,omitempty"`
	Verification string `json:"verification"`
}

// ClassifyEndpoints splits aggregated endpoints into three buckets, one per
// resolution level — all by facts codefit already computes, no new judgment:
//
//   - actionable     — resolved locally AND has a gap (CertainConcerns>0,
//     Actionable>0): full detail, the agent acts on these.
//   - resolved_clean — resolved locally, NO gap (CertainConcerns>0, Actionable==0):
//     named + a verification fact; codefit checked and it is clean.
//   - frontier       — not resolved locally (CertainConcerns==0): named; the data
//     left the body, the agent follows it in the code.
//
// The order matters: CertainConcerns==0 → frontier first, so a frontier-only
// endpoint that happens to carry an access-gap signal is still named as frontier
// (codefit did not resolve it locally), never promoted to actionable. Actionable
// endpoints are returned WHOLE (all their concerns, including any frontier concern
// of the same endpoint, because the agent reasons the endpoint). Input order
// (hardest gap first) is preserved within each bucket.
func ClassifyEndpoints(eps []EndpointReport) (actionable []EndpointReport, resolvedClean []ResolvedCleanEndpoint, frontier []FrontierEndpoint) {
	for _, ep := range eps {
		switch {
		case ep.CertainConcerns == 0:
			frontier = append(frontier, FrontierEndpoint{
				File:       ep.File,
				Line:       ep.Line,
				Method:     ep.Method,
				Categories: categoriesOf(ep),
			})
		case ep.Actionable > 0:
			actionable = append(actionable, ep)
		default:
			resolvedClean = append(resolvedClean, ResolvedCleanEndpoint{
				File:         ep.File,
				Line:         ep.Line,
				Method:       ep.Method,
				Verification: verificationFact(ep),
			})
		}
	}
	return actionable, resolvedClean, frontier
}

// verificationFact phrases, as an affirmation, what codefit checked on a resolved-
// clean endpoint. Because the endpoint has no gap, every authz/idor concern has an
// authorization check present and every over-fetch concern has field-limiting
// present — so the clauses are derived from the categories at play plus the no-gap
// invariant, no prose parsing. It is a fact ("codefit verified locally: ..."), the
// honest opposite of the frontier "could not conclude".
func verificationFact(ep EndpointReport) string {
	cats := map[string]bool{}
	for _, c := range ep.Concerns {
		cats[c.Category] = true
	}
	var parts []string
	if cats["idor"] || cats["authz"] {
		parts = append(parts, "an authorization check is present")
	}
	if cats["overfetch"] {
		parts = append(parts, "field selection (select/omit) is present")
	}
	if len(parts) == 0 {
		return "codefit resolved this endpoint locally — no gap found in the handler body"
	}
	return "codefit verified locally: " + strings.Join(parts, " and ") + " — no gap found in the handler body"
}

// categoriesOf returns the distinct concern categories of an endpoint, in first-
// seen order, so a frontier entry names what is at stake without the full detail.
func categoriesOf(ep EndpointReport) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range ep.Concerns {
		if c.Category != "" && !seen[c.Category] {
			seen[c.Category] = true
			out = append(out, c.Category)
		}
	}
	return out
}

// gapCounts returns an endpoint's count of affirmed, access, exposure, and
// efficiency gaps. Efficiency (N+1) is returned LAST and ranked last in the
// endpoint sort — an N+1 must never outrank an access-control gap, the same
// rationale ADR 0006 used for exposure-vs-access.
func gapCounts(ep EndpointReport) (affirmed, access, exposure, efficiency int) {
	for _, c := range ep.Concerns {
		switch c.Gap {
		case gapAffirmed:
			affirmed++
		case gapAccess:
			access++
		case gapExposure:
			exposure++
		case gapEfficiency:
			efficiency++
		}
	}
	return affirmed, access, exposure, efficiency
}

func countActionable(cs []Concern) int {
	n := 0
	for _, c := range cs {
		if c.Actionable {
			n++
		}
	}
	return n
}

func concernFromFinding(f findings.Finding) Concern {
	return Concern{
		Certainty:     Deterministic,
		Affirms:       !f.Probabilistic, // a deterministic finding affirms; a probabilistic one asks
		Source:        "rule",
		ID:            f.ID,
		Category:      string(f.Dimension),
		Title:         f.Title,
		Description:   f.Description,
		Line:          f.Line,
		Confidence:    f.Confidence,
		Probabilistic: f.Probabilistic,
		Actionable:    true, // a deterministic finding is an affirmed, actionable problem
		Gap:           gapAffirmed,
		Fingerprint:   f.Fingerprint,
	}
}

func concernFromSurface(it findings.SurfaceItem) Concern {
	certainty := SurfaceConfirmed
	if v, ok := it.StructuralFacts["local_access_detected"]; ok && !v {
		certainty = SurfaceFrontier // the data left the body; codefit could not see the access
	}
	actionable, gap := surfaceGap(it)
	return Concern{
		Certainty:     certainty,
		Affirms:       false, // surface is always a question
		Source:        "surface",
		ID:            it.ID,
		Category:      it.Category,
		Title:         it.Category,
		Signals:       it.StructuralSignals,
		Question:      it.ReasonToReview,
		Line:          it.Line,
		Confidence:    0,
		Probabilistic: true,
		Actionable:    actionable,
		Gap:           gap,
		Fingerprint:   it.Fingerprint,
	}
}

// surfaceGap classifies the actionable structural gap of a surface concern, as a
// fact. IDOR and authz are DIFFERENT questions with DIFFERENT gates (ADR 0006
// amended):
//
//   - authz asks "is the caller PERMITTED to do this?" — a known authz helper
//     answers it, so known_authz_detected=true clears the authz gap.
//   - IDOR asks "does the caller OWN this specific resource?" — codefit cannot
//     verify ownership from structure (it sees the local access, not whether the
//     where-clause is owner-scoped). A present authz helper proves authentication/
//     permission, NOT ownership, so it does NOT clear the IDOR gap. An id that
//     reaches a local resource is actionable until a reviewer confirms ownership
//     (ADR 0005: codefit reports a fact, never a "clean" verdict it cannot prove —
//     a false green is worse than an honest red).
//
// An over-fetch with no select/omit is an EXPOSURE gap. A frontier IDOR (the id
// left the body, local_access_detected=false) is not a local gap — the endpoint is
// classified as frontier and the agent follows the data.
//
// An N+1 (category "nplus1") is an EFFICIENCY gap — codefit found a query inside a
// loop. This case is the single most dangerous wiring detail in the N+1 change: an
// endpoint whose only surface item is an N+1 concern MUST be actionable, never fall
// through to resolved_clean (which would print a false "no gap found" affirmation
// over a real N+1 — worse than not detecting it at all).
func surfaceGap(it findings.SurfaceItem) (bool, string) {
	switch it.Category {
	case "idor":
		if it.StructuralFacts["local_access_detected"] {
			return true, gapAccess
		}
	case "authz":
		if !it.StructuralFacts["known_authz_detected"] {
			return true, gapAccess
		}
		// A detected guard answers the authz question only when it DECIDED
		// something here. A helper whose RESULT IS DISCARDED gates nothing at
		// this site, and clearing the gap on it was under-reporting — the
		// direction audit-protocol's I3 calls unforgivable (issue #149).
		//
		// The two-value lookup is load-bearing, not defensive. A producer that
		// never examined result usage omits the key, and an absent key reads as
		// false: raising the gap on absence would make codefit assert "the
		// result was not used" about a scan that never looked — the vacuous
		// claim ADR 0067 forbids, and the exact reason the Go provider omits
		// known_authz_detected against an empty helper set. Only a fact that is
		// PRESENT and false raises the gap.
		if used, stated := it.StructuralFacts["authz_result_used"]; stated && !used {
			return true, gapAccess
		}
	case "overfetch":
		if !it.StructuralFacts["field_limiting_detected"] {
			return true, gapExposure
		}
	case "nplus1":
		return true, gapEfficiency
	}
	return false, ""
}

func certaintyRank(c CertaintyLevel) int {
	switch c {
	case Deterministic:
		return 0
	case SurfaceConfirmed:
		return 1
	default:
		return 2
	}
}

func countCertain(cs []Concern) int {
	n := 0
	for _, c := range cs {
		if c.Certainty == Deterministic || c.Certainty == SurfaceConfirmed {
			n++
		}
	}
	return n
}

// markRefinement marks the IDOR concern of an endpoint that also carries an authz
// concern: IDOR refines authz (a structural fact).
func markRefinement(ep *EndpointReport) {
	hasAuthz := false
	for _, c := range ep.Concerns {
		if c.Category == "authz" {
			hasAuthz = true
		}
	}
	if !hasAuthz {
		return
	}
	for i := range ep.Concerns {
		if ep.Concerns[i].Category == "idor" {
			ep.Concerns[i].RefinesAuthz = true
		}
	}
}

// methodFromSignals extracts the HTTP method from a "Handler GET ..." signal.
func methodFromSignals(signals []string) string {
	for _, s := range signals {
		if strings.HasPrefix(s, "Handler ") {
			parts := strings.Fields(s)
			if len(parts) >= 2 {
				return parts[1]
			}
		}
	}
	return ""
}
