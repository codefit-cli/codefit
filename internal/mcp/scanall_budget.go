package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/codefit-cli/codefit/internal/core/report"
)

// withNamedActionable is scan-all's RENDERING step, and it is the ONLY thing this
// change added to the pipeline. It takes the response buildScanAll already
// finished computing plus the complete actionable detail, names that detail, and
// cuts the endpoint lists down to the declared byte budget.
//
// It reads conclusions and never writes them. The score, `by_dimension`, the
// baseline delta, the summary, the scope block and every bucket COUNT are already
// in resp, computed over the whole audit; nothing here recomputes one from what
// survived the cut. That is the entire content of R4, and the reason the naming
// and the fitting live behind one function that takes a finished response.
//
// The second return, stillOver, is R1's third and last check (baseline-write-gate
// spec): true when the response does not fit its budget even after withholding
// every droppable endpoint. The caller uses it to decide whether the baseline
// may be persisted — a response the client cannot receive complete must not
// advance codefit's memory of what it saw.
func withNamedActionable(resp ScanAllResponse, actionable []report.EndpointReport, budget int) (ScanAllResponse, bool) {
	resp.Actionable.Endpoints = report.NameActionable(actionable)
	return fitToBudget(resp, budget)
}

// fitToBudget returns resp with as many endpoints rendered as the budget allows,
// and the withholding DECLARED. Naming instead of inlining is a large
// constant-factor win, not a bound: a monorepo with thousands of endpoints
// exceeds any budget eventually, and the failure mode must not be a clipped
// response that reads like a complete one.
//
// The search is a binary search on how many endpoints to withhold, over the
// really serialized bytes — the same bytes the client receives, not an estimate.
// Response size is monotone in the number of endpoints rendered except for the
// note prose, whose numbers gain and lose digits, so the binary search is
// followed by a forward walk that makes the result true rather than nearly true.
func fitToBudget(resp ScanAllResponse, budget int) (ScanAllResponse, bool) {
	max := maxDroppable(resp)

	lo, hi := 0, max
	for lo < hi {
		mid := lo + (hi-lo)/2
		if fitsBudget(withheldBy(resp, mid, budget, false), budget) {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	for lo < max && !fitsBudget(withheldBy(resp, lo, budget, false), budget) {
		lo++
	}
	// Withholding every endpoint is all this step can do. If the response is STILL
	// over budget, what is left is the summary, the notes and the db section — and
	// the honest move is to say so, not to start clipping a section whose shape the
	// agent has no way to check.
	over := !fitsBudget(withheldBy(resp, lo, budget, false), budget)
	return withheldBy(resp, lo, budget, over), over
}

func fitsBudget(resp ScanAllResponse, budget int) bool {
	raw, err := json.Marshal(resp)
	if err != nil {
		// A response that cannot be serialized cannot be delivered either; treating
		// it as "does not fit" makes the search withhold everything instead of
		// claiming a size it never measured.
		return false
	}
	return len(raw) <= budget
}

// withheldBy renders resp with `drop` endpoints withheld, taken off the END of
// the lists — which is the low-ranked end, because AggregateEndpoints already
// sorted them hardest gap first.
//
// Buckets are emptied in ascending order of what the agent loses by not seeing
// them: resolved_clean first (an affirmation that codefit checked and found
// nothing missing — real information, but nothing to do), then frontier_pending
// (codefit concluded nothing; the agent must go to the code either way, and the
// entry only names the file), and actionable last (a gap codefit resolved locally
// and found — the reason the scan was run).
//
// Counts are NOT touched: a bucket keeps declaring how many endpoints codefit put
// in it, and Withheld says how many of those are not printed.
func withheldBy(resp ScanAllResponse, drop, budget int, stillOver bool) ScanAllResponse {
	dropClean := min(drop, len(resp.ResolvedClean.Endpoints))
	rest := drop - dropClean
	dropFrontier := min(rest, len(resp.FrontierPending.Endpoints))
	rest -= dropFrontier
	keptActionable, dropActionable := dropActionableTail(resp.Actionable.Endpoints, rest)

	out := resp
	out.ResolvedClean.Endpoints = resp.ResolvedClean.Endpoints[:len(resp.ResolvedClean.Endpoints)-dropClean]
	out.ResolvedClean.Withheld = dropClean
	out.FrontierPending.Endpoints = resp.FrontierPending.Endpoints[:len(resp.FrontierPending.Endpoints)-dropFrontier]
	out.FrontierPending.Withheld = dropFrontier
	out.Actionable.Endpoints = keptActionable
	out.Actionable.Withheld = dropActionable

	total := len(resp.Actionable.Endpoints) + len(resp.ResolvedClean.Endpoints) + len(resp.FrontierPending.Endpoints)
	out.Budget = BudgetBlock{
		Bytes:    budget,
		Withheld: dropClean + dropFrontier + dropActionable,
		Ordering: endpointOrdering,
		Note:     budgetNote(budget, total, dropActionable, dropClean, dropFrontier, stillOver),
	}
	return out
}

// maxDroppable is how many endpoints the budget is ALLOWED to withhold. It is
// not the endpoint count: an actionable endpoint carrying a deterministic
// finding is never droppable (see dropActionableTail), so the search must know
// its own ceiling — otherwise it walks past it and reports a withholding it did
// not perform.
func maxDroppable(resp ScanAllResponse) int {
	n := len(resp.ResolvedClean.Endpoints) + len(resp.FrontierPending.Endpoints)
	for _, ep := range resp.Actionable.Endpoints {
		if len(ep.Deterministic) == 0 {
			n++
		}
	}
	return n
}

// dropActionableTail withholds up to `quota` actionable endpoints, taken from the
// low-ranked END, and SKIPS every endpoint carrying a deterministic finding.
//
// That exception is R2 enforced where it actually gets tested. A deterministic
// finding is a fact codefit already concluded, and the spec refuses to hide one
// behind a second call because that makes a scan's headline result depend on the
// agent choosing to look. Dropping it for a byte budget is the same failure and
// worse: the agent would not even know to look. They are rare by construction —
// one, in the 313 KB response that motivated all this — so pinning them costs
// almost nothing, and when it does cost, the budget note says the response is
// over rather than quietly trading a fact for a number.
//
// Order is preserved: what survives is the ranking with holes at the bottom,
// never a reshuffle.
func dropActionableTail(eps []report.ActionableEndpoint, quota int) ([]report.ActionableEndpoint, int) {
	if quota <= 0 || len(eps) == 0 {
		return eps, 0
	}
	drop := make([]bool, len(eps))
	dropped := 0
	for i := len(eps) - 1; i >= 0 && dropped < quota; i-- {
		if len(eps[i].Deterministic) > 0 {
			continue // a fact codefit affirmed: never withheld
		}
		drop[i] = true
		dropped++
	}
	kept := make([]report.ActionableEndpoint, 0, len(eps)-dropped)
	for i, ep := range eps {
		if !drop[i] {
			kept = append(kept, ep)
		}
	}
	return kept, dropped
}

// budgetNote states what the response did about its own size. It is never silent:
// "nothing was withheld" is an affirmation the agent needs as much as the
// withholding itself, because otherwise a complete response and a cut one are
// distinguished only by an absence.
//
// THE FIT CLAIM IS DERIVED FROM THE MEASURED SIZE, NEVER FROM THE WITHHELD
// COUNT. Those are different questions, and this note used to answer the second
// while asserting the first: with `withheld == 0` it returned "the complete list
// fit" without consulting stillOver, which fitToBudget had already computed by
// SERIALIZING the response. A DB-heavy project with no security provider — zero
// endpoints, a large db.surface — therefore received a response that exceeded its
// declared budget while affirming it fit. Nothing had to be measured to fix it;
// the measurement was already an argument.
func budgetNote(budget, total, dropActionable, dropClean, dropFrontier int, stillOver bool) string {
	withheld := dropActionable + dropClean + dropFrontier
	if withheld == 0 && !stillOver {
		return fmt.Sprintf("the complete endpoint list fit within this response's %d-byte budget: NOTHING was "+
			"withheld, every endpoint codefit classified is named here.", budget)
	}
	if withheld == 0 {
		// Over budget with NOTHING that could be withheld. The two causes are
		// different facts and lead an agent to different next steps, so they are
		// never conflated: either the response has no endpoints at all, or every
		// endpoint it has carries a deterministic finding, which the budget is
		// forbidden to hide (dropActionableTail).
		//
		// DECLARED LIMIT, stated rather than papered over: the budget governs the
		// ENDPOINT LISTS only, so there is no lever here for the excess, which is
		// in the non-endpoint content. Two of the three things this note used to
		// say were missing now EXIST: db.surface is already the light index
		// (surfaceindex — every item named, nothing withheld, declared via
		// DBSection.Count/Withheld/WithheldNote) and a "fetch the rest" path for a
		// named db item is codefit-scan-db's `detail: [ids]`. What is STILL
		// missing, and is the one thing extending withholding to db.surface would
		// need, is a stable RANKING for db surface items to withhold BY — there is
		// no severity field and no common axis across its 18 disjoint categories
		// (roadmap P0-4's remaining half, unchanged by the index). What this note
		// buys meanwhile is real: the baseline never advances on an over-budget
		// response (ADR 0061), so the residual is a failed tool call rather than
		// corrupted memory, and for the responses that DO arrive it is the whole
		// difference between an agent that knows it is reading a prefix and one
		// that does not.
		cause := "there are no endpoints in this response"
		if total > 0 {
			cause = "every endpoint carries a deterministic finding, which codefit will not hide behind a byte count"
		}
		return fmt.Sprintf("this response does NOT fit its %d-byte budget, and NOTHING could be withheld to make "+
			"it fit (%s). The budget governs the endpoint lists only, so the excess is in the NON-endpoint "+
			"content — the summary, the notes and the db section. Narrow the scan with `changed_files`; codefit "+
			"will not clip a section silently to make a number look right.", budget, cause)
	}
	note := fmt.Sprintf("this response did NOT fit its %d-byte budget: %d of %d endpoint(s) are withheld and "+
		"NOT named here — actionable %d, resolved_clean %d, frontier_pending %d. Each bucket's `count` is still "+
		"the complete number codefit classified, and `withheld` says how many of those are missing from the "+
		"list, so what you are reading is a PREFIX of the ranking, not the whole audit. To see the rest, re-run "+
		"%s narrowed with `changed_files` to the area you care about; any single endpoint's full concerns are "+
		"always available from %s.",
		budget, withheld, total, dropActionable, dropClean, dropFrontier, ToolScanAll, ToolScanEndpoint)
	if stillOver {
		note += fmt.Sprintf(" WARNING: even with every endpoint withheld this response exceeds %d bytes — what "+
			"remains (the summary, the notes and the db section) is over on its own. Narrow the scan with "+
			"`changed_files`; codefit will not clip a section silently to make a number look right.", budget)
	}
	return note
}
