package typescript

import (
	"testing"

	ts "github.com/odvcencio/gotreesitter"
)

// TestStopIsReproducible_ClassifiesEveryUpstreamStopReason pins CODEFIT'S
// classification of each stop (ADR 0089, D4): which stopped results may be
// cached. It does NOT pin the parser's ability to produce each stop. Through
// real input this repository reaches only two rows: memory_budget with guard
// "arena" (GOT_PARSE_MEMORY_BUDGET_MB, the security sensor's budget tests) and
// memory_budget with guard "hard_ceiling" (GOT_PARSE_MEMORY_HARD_CEILING_MB,
// TestParseStatus_TransientStopIsReportedButNotCached). Every other row —
// scratch, runtime_heap, runtime_sys, an unnamed guard, node_limit,
// stack_depth_limit, timeout, cancelled, iteration_limit, reuse_budget,
// token_source_eof, invariant_violation — was not reached through real input,
// and this table is the only thing that holds its classification.
//
// The stop reasons come from gotreesitter's exported ParseStop* constants
// (tree.go, v0.54.0). The memory-budget guard names are unexported upstream
// (parseMemoryBudgetStopSource* in parser_memory_budget_runtime.go), so they are
// spelled here as the strings ParseRuntime().MemoryBudgetStopSource carries.
func TestStopIsReproducible_ClassifiesEveryUpstreamStopReason(t *testing.T) {
	cases := []struct {
		name   string
		reason ts.ParseStopReason
		guard  string
		want   bool
	}{
		// Documented deterministic by upstream's issue #454 contract.
		{"memory budget, arena guard", ts.ParseStopMemoryBudget, "arena", true},
		{"memory budget, scratch guard", ts.ParseStopMemoryBudget, "scratch", true},
		{"node limit", ts.ParseStopNodeLimit, "", true},
		{"stack depth limit", ts.ParseStopStackDepthLimit, "", true},

		// Process-heap readings: documented as a function of GC timing.
		{"memory budget, hard ceiling", ts.ParseStopMemoryBudget, "hard_ceiling", false},
		{"memory budget, runtime heap", ts.ParseStopMemoryBudget, "runtime_heap", false},
		{"memory budget, runtime sys", ts.ParseStopMemoryBudget, "runtime_sys", false},
		{"memory budget, no guard named", ts.ParseStopMemoryBudget, "", false},
		{"memory budget, unknown guard", ts.ParseStopMemoryBudget, "codefit_made_up_guard", false},

		// Wall clock or caller, and reasons with no documented determinism.
		{"timeout", ts.ParseStopTimeout, "", false},
		{"cancelled", ts.ParseStopCancelled, "", false},
		{"iteration limit", ts.ParseStopIterationLimit, "", false},
		{"reuse budget", ts.ParseStopReuseBudget, "", false},
		{"token source EOF", ts.ParseStopTokenSourceEOF, "", false},
		{"invariant violation", ts.ParseStopInvariantViolation, "", false},

		// Not ParseStoppedEarly reasons: parseStatusOf never asks about them,
		// and the default branch answers.
		{"accepted", ts.ParseStopAccepted, "", false},
		{"none", ts.ParseStopNone, "", false},
		{"no stacks alive", ts.ParseStopNoStacksAlive, "", false},

		// A reason a later parser release might add: never cached until it is
		// classified on purpose.
		{"unknown future reason", ts.ParseStopReason("codefit_made_up_reason"), "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stopIsReproducible(c.reason, c.guard); got != c.want {
				t.Errorf("stopIsReproducible(%q, %q) = %v, want %v", c.reason, c.guard, got, c.want)
			}
		})
	}
}
