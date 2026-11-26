package typescript

import (
	ts "github.com/odvcencio/gotreesitter"

	"github.com/codefit-cli/codefit/internal/core/findings"
)

// parseStatusOf reports whether tree covers its whole file (ADR 0089).
//
// gotreesitter is error-recovering: it never fails on a form its grammar does
// not accept, it returns a tree with the damage marked in it. This reads those
// marks, from the SAME tree the findings and the surface were computed from, and
// applies its checks in a FIXED order — the first that holds is the reason:
//
//  1. stopped:<reason> — the parse stopped before accepting the input
//     (Tree.ParseStoppedEarly: a memory budget, a node or iteration limit, a
//     timeout, …). Measured on v0.54.0 with GOT_PARSE_MEMORY_BUDGET_MB=6: stop
//     reason memory_budget, a one-node ERROR root, error flag set. It comes
//     first because the stop is the CAUSE; the ERROR root it leaves is the
//     symptom, and naming the symptom would hide the knob that produced it.
//  2. error_root — the root itself is an ERROR node. No input measured on
//     v0.54.0 reaches this arm: no committed fixture and none of 68 synthetic
//     inputs tried for it parses to an ERROR root without a stop, and the stop
//     outranks it. It is kept so a future parser that returns an ERROR root
//     without a stop is still named for what it is; removing it would fall
//     through to error_node, still incomplete.
//  3. error_node — an ERROR node anywhere under the root, reported at the
//     first one in document order. This is the shape a healthy-looking
//     `program` root hides: root type alone proves nothing.
//  4. missing_node — no ERROR node, but a zero-width MISSING node the parser
//     inserted (the tagged-template residual of ADR 0088 after `await`).
//  5. error_flag — the tree says it holds an error but neither kind of node is
//     found. Defensive: no measured input produces it; without it an unlocated
//     error would read as complete.
//
// An ERROR node outranks a MISSING node regardless of position: an ERROR node
// can swallow source text (residual (c) loses the finding on the next line),
// while a MISSING node only stands in for a token.
//
// Line is the 1-based start line of the node the reason names; for a stop, of
// the first ERROR or MISSING node, else of the end of what was parsed.
//
// Measured and rejected as a further signal: root.EndByte() against the content
// length. On every committed fixture, every still-an-error row and every
// deterministic budget stop the root ends exactly at the end of the content;
// the only short root measured (a heap-ceiling stop, not deterministic) is also
// a stop, which check 1 already reports. A signal with no shape it alone
// catches would be untestable, so it is not added.
//
// What this does NOT see: valid code the parser misreads into an ERROR-free,
// MISSING-free tree of the wrong shape; that tree reads as complete.
//
// Cost: the error flag on the root is O(1); a clean tree also gets one walk over
// its nodes, because a MISSING node is not guaranteed to set the flag
// (HasErrorOrMissing). The first-node walks below run only on a tree that holds
// a problem.
func parseStatusOf(tree *ts.Tree) findings.ParseStatus {
	root := tree.RootNode()
	if tree.ParseStoppedEarly() {
		line := 0
		if n := firstNode(root, func(n *ts.Node) bool { return n.IsError() || n.IsMissing() }); n != nil {
			line = startLine(n)
		} else {
			line = int(root.EndPoint().Row) + 1
		}
		return findings.ParseIncomplete(findings.ParseReasonStoppedPrefix+string(tree.ParseStopReason()), line)
	}
	if root.IsError() {
		return findings.ParseIncomplete(findings.ParseReasonErrorRoot, startLine(root))
	}
	if !root.HasErrorOrMissing() {
		return findings.ParseComplete()
	}
	if n := firstNode(root, (*ts.Node).IsError); n != nil {
		return findings.ParseIncomplete(findings.ParseReasonErrorNode, startLine(n))
	}
	if n := firstNode(root, (*ts.Node).IsMissing); n != nil {
		return findings.ParseIncomplete(findings.ParseReasonMissingNode, startLine(n))
	}
	return findings.ParseIncomplete(findings.ParseReasonErrorFlag, 0)
}

// firstNode returns the first node, in document order (pre-order, children left
// to right), for which match holds, or nil. It walks with an explicit stack so a
// deeply nested tree cannot exhaust the goroutine stack.
func firstNode(root *ts.Node, match func(*ts.Node) bool) *ts.Node {
	stack := []*ts.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}
		if match(n) {
			return n
		}
		for i := n.ChildCount() - 1; i >= 0; i-- {
			stack = append(stack, n.Child(i))
		}
	}
	return nil
}

func startLine(n *ts.Node) int { return int(n.StartPoint().Row) + 1 }
