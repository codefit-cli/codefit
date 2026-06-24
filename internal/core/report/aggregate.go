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
}

const (
	gapAffirmed = "affirmed"
	gapAccess   = "access"
	gapExposure = "exposure"
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
	// access → exposure), then by certain-concern count. This surfaces the real
	// findings (a missing access check, an affirmed vulnerability) above
	// endpoints that are merely heavily instrumented but protected. It is ordering
	// by FACT (which control is missing), never by severity — the agent judges
	// danger. Access gaps outrank exposure gaps because over-fetch with no select
	// is ubiquitous (every serialization) and would otherwise drown the
	// access-control findings (ADR 0006, validated on Bitácora).
	sort.SliceStable(out, func(i, j int) bool {
		ai, ci, ei := gapCounts(out[i])
		aj, cj, ej := gapCounts(out[j])
		switch {
		case ai != aj:
			return ai > aj // affirmed deterministic
		case ci != cj:
			return ci > cj // missing access control
		case ei != ej:
			return ei > ej // over-exposure
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

// PartitionByResolution splits aggregated endpoints into the ones codefit resolved
// LOCALLY — at least one deterministic finding or surface_confirmed concern
// (CertainConcerns>0) — and the frontier-only ones, where every concern is
// surface_frontier. The criterion is the FACT local_access_detected, already
// folded into CertainConcerns; it is not an arbitrary cut. Actionable endpoints
// are returned WHOLE (all their concerns, including any frontier concern of the
// same endpoint, because the agent reasons the endpoint, not loose concerns).
// Frontier-only endpoints are reduced to FrontierEndpoint (named, not detailed).
// Order is preserved from the aggregated input (hardest gap first).
func PartitionByResolution(eps []EndpointReport) (actionable []EndpointReport, frontier []FrontierEndpoint) {
	for _, ep := range eps {
		if ep.CertainConcerns > 0 {
			actionable = append(actionable, ep)
			continue
		}
		frontier = append(frontier, FrontierEndpoint{
			File:       ep.File,
			Line:       ep.Line,
			Method:     ep.Method,
			Categories: categoriesOf(ep),
		})
	}
	return actionable, frontier
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

// gapCounts returns an endpoint's count of affirmed, access, and exposure gaps.
func gapCounts(ep EndpointReport) (affirmed, access, exposure int) {
	for _, c := range ep.Concerns {
		switch c.Gap {
		case gapAffirmed:
			affirmed++
		case gapAccess:
			access++
		case gapExposure:
			exposure++
		}
	}
	return affirmed, access, exposure
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
	}
}

// surfaceGap classifies the actionable structural gap of a surface concern, as a
// fact: an idor/authz handler with no known authz check is an ACCESS gap; an
// over-fetch with no select/omit is an EXPOSURE gap. A checked handler or a
// limited query has no gap.
func surfaceGap(it findings.SurfaceItem) (bool, string) {
	switch it.Category {
	case "idor", "authz":
		if !it.StructuralFacts["known_authz_detected"] {
			return true, gapAccess
		}
	case "overfetch":
		if !it.StructuralFacts["field_limiting_detected"] {
			return true, gapExposure
		}
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
