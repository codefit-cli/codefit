package typescript_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// SPEC (issue #149) — an authorization helper whose RESULT IS DISCARDED gates
// nothing locally, and codefit must stop reporting the handler as permitted.
//
// The bug was under-reporting, the direction docs/specs/audit-protocol.md's I3
// calls unforgivable: `known_authz_detected: true` CLEARS the access gap
// (internal/core/report/aggregate.go:489), so a handler that merely MENTIONS a
// guard left the actionable bucket even when the guard decided nothing.
//
// The fix is not to flip that boolean, and the reason is the whole design. A
// discarded result may still gate — `await requireAuth()` is a common shape for
// a helper that THROWS or REDIRECTS — and the helper's body is usually in
// another file, on the far side of the frontier. codefit cannot answer it from
// the handler. So it does what it is built to do: it reports BOTH facts and
// hands the question to the agent.
//
//	known_authz_detected : a recognized helper WAS called here (unchanged, true)
//	authz_result_used    : NEW — its result reached a branch, a return, an
//	                       assignment, or another call. False means the call
//	                       decides nothing at this site.
//
// The access gap now clears only when both are true. Over-reporting a handler
// whose guard throws is noise; under-reporting one whose guard decides nothing
// is a false all-clear.
func facts(t *testing.T, src string) map[string]bool {
	t.Helper()
	items := authzItems(t, typescript.New(), "app/x/route.ts", src)
	if len(items) != 1 {
		t.Fatalf("want exactly 1 authz item, got %d", len(items))
	}
	return items[0].StructuralFacts
}

func TestAuthzResultUsed(t *testing.T) {
	// getServerSession is in the built-in helper set, so these need no
	// registration — the fact under test is about the RESULT, not the name.
	cases := []struct {
		name     string
		body     string
		wantUsed bool
		why      string
	}{
		{
			name:     "discarded, awaited",
			body:     "  await getServerSession();\n  return Response.json(await prisma.user.findMany());",
			wantUsed: false,
			why:      "the call is a statement of its own: nothing branches on it",
		},
		{
			name:     "discarded, not awaited",
			body:     "  getServerSession();\n  return Response.json(await prisma.user.findMany());",
			wantUsed: false,
			why:      "same shape without await",
		},
		{
			name: "used in a branch",
			body: "  const s = await getServerSession();\n  if (!s) return new Response(\"no\", { status: 401 });\n" +
				"  return Response.json(await prisma.user.findMany());",
			wantUsed: true,
			why:      "assigned and then branched on — this actually gates",
		},
		{
			name:     "used inline in the condition",
			body:     "  if (!(await getServerSession())) return new Response(\"no\", { status: 401 });\n  return Response.json(await prisma.user.findMany());",
			wantUsed: true,
			why:      "the result IS the condition",
		},
		{
			name:     "returned",
			body:     "  await prisma.user.findMany();\n  return getServerSession();",
			wantUsed: true,
			why:      "a returned value is used by the caller",
		},
		{
			name:     "passed as an argument",
			body:     "  const u = pick(await getServerSession());\n  return Response.json(await prisma.user.findMany({ where: { id: u } }));",
			wantUsed: true,
			why:      "consumed by another call",
		},
		{
			name: "one discarded call and one used call",
			body: "  await getServerSession();\n  const s = await getServerSession();\n  if (!s) return new Response(\"no\", { status: 401 });\n" +
				"  return Response.json(await prisma.user.findMany());",
			wantUsed: true,
			why:      "ONE gating call is enough; the discarded one does not cancel it",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := facts(t, "export async function GET(req: Request) {\n"+tc.body+"\n}")
			if !f["known_authz_detected"] {
				t.Fatalf("probe broken: the helper call was not detected at all")
			}
			if got := f["authz_result_used"]; got != tc.wantUsed {
				t.Errorf("authz_result_used = %v, want %v — %s", got, tc.wantUsed, tc.why)
			}
		})
	}
}

// The fact must be VISIBLE, not only structural: an agent reading the signals
// has to learn that the guard decided nothing, and why codefit will not answer
// it. Without this the new boolean is invisible to the only reader that matters.
func TestAuthzDiscardedResultIsStatedInTheSignals(t *testing.T) {
	src := `
export async function GET(req: Request) {
  await getServerSession();
  return Response.json(await prisma.user.findMany());
}`
	items := authzItems(t, typescript.New(), "app/x/route.ts", src)
	if len(items) != 1 {
		t.Fatalf("want 1 authz item, got %d", len(items))
	}
	joined := strings.Join(items[0].StructuralSignals, " | ")
	for _, want := range []string{"result", "discard"} {
		if !strings.Contains(strings.ToLower(joined), want) {
			t.Errorf("the signals never mention %q, so the agent cannot see the guard decided nothing:\n%s", want, joined)
		}
	}
	// And it must NOT read as a verdict — codefit reports the fact, the agent
	// reasons whether the helper throws.
	if strings.Contains(strings.ToLower(joined), "unauthorized") ||
		strings.Contains(strings.ToLower(joined), "no authorization") {
		t.Errorf("the signal must state a FACT, never conclude the handler is unprotected:\n%s", joined)
	}
}

// A MIDDLEWARE guard is not subject to this rule and must keep clearing the gap.
// It runs BEFORE the handler by construction, so there is no result for the
// handler body to use — asking whether it was used is the wrong question, and
// answering "not used" would turn every middleware-protected route actionable.
func TestAuthzMiddlewareGuardIsNotAffectedByTheResultRule(t *testing.T) {
	p := typescript.New()
	items := authzItems(t, p, "app/x/route.ts", `
export async function GET(req: Request) {
  return Response.json(await prisma.user.findMany());
}`)
	if len(items) != 1 {
		t.Fatalf("want 1 authz item, got %d", len(items))
	}
	// No helper in the body at all: the handler is unguarded locally, and the
	// new fact must not invent a guard.
	if items[0].StructuralFacts["known_authz_detected"] {
		t.Fatalf("probe broken: no helper is called here")
	}
	if items[0].StructuralFacts["authz_result_used"] {
		t.Errorf("authz_result_used must be false when no helper was called at all")
	}
}
