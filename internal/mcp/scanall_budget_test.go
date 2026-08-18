package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/report"
	"github.com/codefit-cli/codefit/internal/core/scope"
	"github.com/codefit-cli/codefit/internal/core/scoring"
)

// budgetFixture writes the project the R4 invariance tests run over. It is
// deliberately MIXED — an affirmed deterministic finding, several endpoints with
// a local gap, one endpoint codefit resolves clean, and two frontier-only ones —
// because the whole point of the lock is that the score, the baseline delta and
// the summary keep describing the COMPLETE analysis no matter how much of it the
// rendering spells out. A fixture with a single bucket could not tell the
// difference.
func budgetFixture(t *testing.T, root string) {
	t.Helper()
	files := map[string]string{
		// Affirmed deterministic finding (a hardcoded secret) plus a local access.
		"app/config/route.ts": `
const apiKey = "REDACTED-not-a-real-credential-test-fixture";
export async function GET(req: Request) {
  return Response.json(await prisma.setting.findMany());
}`,
		// IDOR + over-fetch, no authz: a local gap, actionable.
		"app/orders/[id]/route.ts": `
export async function GET(req: Request, { params }: { params: { id: string } }) {
  return Response.json(await prisma.order.findUnique({ where: { id: params.id } }));
}`,
		"app/users/[id]/route.ts": `
export async function PATCH(req: Request, { params }: { params: { id: string } }) {
  const body = await req.json();
  return Response.json(await prisma.user.update({ where: { id: params.id }, data: body }));
}`,
		"app/invoices/[id]/route.ts": `
export async function DELETE(req: Request, { params }: { params: { id: string } }) {
  return Response.json(await prisma.invoice.delete({ where: { id: params.id } }));
}`,
		// Authz present and field selection present, no client id: resolved clean.
		"app/stats/route.ts": `
export async function GET() {
  const session = await getServerSession();
  return Response.json(await prisma.stats.findMany({ select: { count: true } }));
}`,
		// Frontier: the operation leaves the handler body.
		"app/admin/audit/route.ts": `
export async function POST(req: Request) {
  const body = await req.json();
  return Response.json(await AuditService.record(body));
}`,
		"app/reports/route.ts": `
export async function POST(req: Request) {
  const body = await req.json();
  return Response.json(await ReportService.build(body));
}`,
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// responseInvariant is the part of a scan-all response that describes WHAT
// CODEFIT CONCLUDED, as opposed to how much of it the response spells out. R4
// says exactly this much must be byte-identical before and after the change, and
// identical between two runs at different budgets.
//
// What this lock covers and what it does NOT: its axis is complete-analysis vs
// rendered-subset. It compares two runs of the SAME code over the SAME project,
// so any blindness both runs share is invisible to it — which is exactly how it
// carried a `Summary` that counted only the security dimension without ever
// noticing. It is NOT a dimension-completeness control; the summary covering
// every measured dimension is locked by
// TestScanAllBudget_SummaryCountsTheDBDimensionOverADBCarryingProject and by
// TestScanAll_ThePreChangeGoldenTestifiesToTheUndercount, not here. What this
// one now DOES add is that whatever the summary covers, it covers identically at
// both budgets — including summary.db over a DB-carrying project.
type responseInvariant struct {
	Summary  ScanAllSummary       `json:"summary"`
	Scope    ScopeBlock           `json:"scope"`
	Score    scoring.ScoreSummary `json:"score"`
	Baseline BaselineDelta        `json:"baseline"`
}

func invariantOf(r ScanAllResponse) responseInvariant {
	return responseInvariant{Summary: r.Summary, Scope: r.Scope, Score: r.Score, Baseline: r.Baseline}
}

// flatSummary mirrors the summary shape as scanall_prechange_invariant.json
// RECORDED it — four unqualified counts, all security — so the golden's bytes
// stay untouched. Regenerating that file is forbidden (see testdata/README.md:
// it was captured at 79e34b0, and re-emitting it from the current tree turns
// the lock into a tautology), so the shape change is absorbed by an ADAPTER on
// this side, never by an edit on that one.
type flatSummary struct {
	Endpoints             int `json:"endpoints"`
	DeterministicFindings int `json:"deterministic_findings"`
	SurfaceItems          int `json:"surface_items"`
	CertainConcerns       int `json:"certain_concerns"`
}

// preChangeInvariant is responseInvariant in the pre-change summary shape.
type preChangeInvariant struct {
	Summary  flatSummary          `json:"summary"`
	Scope    ScopeBlock           `json:"scope"`
	Score    scoring.ScoreSummary `json:"score"`
	Baseline BaselineDelta        `json:"baseline"`
}

// flatten projects a post-change response back onto the pre-change shape by
// reading summary.security. That projection is not a convenience — it ASSERTS
// the migration the removed requirement promises: "consumers read
// summary.security.* for the old values". If the four security counts ever stop
// being verbatim what the flat summary carried, this comparison against the
// 79e34b0 golden fails.
//
// A nil Security (no provider resolved) flattens to the zero flatSummary, which
// is what the pre-change code would have emitted for that project — the old
// shape had no way to say "not measured", which is the defect, not a behaviour
// this adapter should invent a new answer for.
func flatten(r ScanAllResponse) preChangeInvariant {
	var s flatSummary
	if r.Summary.Security != nil {
		s = flatSummary{
			Endpoints:             r.Summary.Security.Endpoints,
			DeterministicFindings: r.Summary.Security.DeterministicFindings,
			SurfaceItems:          r.Summary.Security.SurfaceItems,
			CertainConcerns:       r.Summary.Security.CertainConcerns,
		}
	}
	return preChangeInvariant{Summary: s, Scope: r.Scope, Score: r.Score, Baseline: r.Baseline}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// freshBaseline is a baseline path in a temp dir: every run in this file starts
// from NOTHING TRACKED, which is the state that produced the 313 KB response.
// Reusing one path across two runs would make the second run's delta depend on
// the first, and the invariance comparison below would be measuring the baseline
// instead of the rendering.
func freshBaseline(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), ".codefit-baseline")
}

func runBudgeted(t *testing.T, root string, budget int) ScanAllResponse {
	t.Helper()
	resp, err := handleScanAllBudgeted(ScanAllRequest{Root: root, Language: "typescript"},
		scope.Full(), freshBaseline(t), budget)
	if err != nil {
		t.Fatalf("scan-all over the fixture at budget %d: %v", budget, err)
	}
	return resp
}

func readGolden(t *testing.T, name string, into any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading the pre-change golden %s: %v", name, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("parsing the pre-change golden %s: %v", name, err)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// namedActionable is the set of endpoints the response NAMES in actionable,
// keyed the way the pre-change golden recorded them.
func namedActionable(resp ScanAllResponse) map[string]bool {
	out := map[string]bool{}
	for _, ep := range resp.Actionable.Endpoints {
		out[ep.File+":"+strconv.Itoa(ep.Line)] = true
	}
	return out
}

// --- Test contract item 2 (R4) -------------------------------------------------

// What codefit CONCLUDED does not move. Two independent locks, because they fail
// for different reasons:
//
//   - against the PRE-CHANGE golden (captured from 79e34b0, before a line of this
//     change existed): the fix must not have shifted the score, the baseline delta,
//     the summary or the scope block by so much as a field.
//     The golden has been hand-edited exactly once since that capture: ADR 0055 gave
//     `practices` a weight, so `by_dimension` grew a sixth key. Only the key
//     `"practices": null` was added — every value in the file, including the global
//     and every other dimension, is the byte the capture produced.
//   - between two runs at wildly different budgets: this is the one that catches
//     the bug the spec is most afraid of. If any of those four were computed over
//     the RENDERED subset instead of the complete analysis, a run that withholds
//     nearly every endpoint would disagree with a run that withholds none.
func TestScanAllBudget_ConclusionsAreComputedOverTheCompleteAnalysis(t *testing.T) {
	root := t.TempDir()
	budgetFixture(t, root)

	full := runBudgeted(t, root, ResponseBudgetBytes)

	// The golden is read into the PRE-CHANGE shape and the live response is
	// projected onto it (flatten). The golden's bytes are evidence and stay
	// untouched; the adapter is what absorbs the per-dimension renest.
	var want preChangeInvariant
	readGolden(t, "scanall_prechange_invariant.json", &want)
	// Guard against a golden that locks nothing: a fixture that produced no
	// endpoints and a zero score would compare equal to anything.
	if want.Summary.Endpoints == 0 || want.Score.Global == 0 || want.Baseline.New == 0 {
		t.Fatalf("the pre-change golden is empty (%+v) — this test would pass over any response", want)
	}
	if got := mustJSON(t, flatten(full)); got != mustJSON(t, want) {
		t.Errorf("the change moved what codefit concluded.\npre-change: %s\npost-change: %s", mustJSON(t, want), got)
	}
	// budgetFixture configures no database, so the DB dimension never runs over
	// it. That used to be an ACCIDENT this file relied on without saying so —
	// the endpoint-anchored equality below survived only because no DB finding
	// could ever reach the count it read. State it, so the day someone gives
	// this fixture a schema, the tests that assume otherwise fail loudly here
	// instead of quietly changing meaning.
	if full.Summary.DB != nil {
		t.Fatalf("budgetFixture must configure no database — summary.db is %+v, so every assertion in this "+
			"file that assumes a security-only project is now measuring something else", full.Summary.DB)
	}
	if full.Summary.Security == nil {
		t.Fatal("budgetFixture is TypeScript with a resolvable provider — summary.security must not be null")
	}

	// budget=1 cannot fit even the empty response, so every endpoint is withheld:
	// the maximum possible divergence between the complete analysis and what is
	// rendered.
	starved := runBudgeted(t, root, 1)
	if len(starved.ResolvedClean.Endpoints) != 0 || len(starved.FrontierPending.Endpoints) != 0 {
		t.Fatalf("a budget of 1 byte still rendered %d clean / %d frontier endpoint(s) — this comparison is "+
			"not measuring a starved response",
			len(starved.ResolvedClean.Endpoints), len(starved.FrontierPending.Endpoints))
	}
	// Only the endpoints carrying a fact codefit AFFIRMED survive a 1-byte budget
	// (R2 pins them); everything else must be gone, or the response below is not
	// starved and the invariance comparison proves nothing.
	for _, ep := range starved.Actionable.Endpoints {
		if len(ep.Deterministic) == 0 {
			t.Fatalf("a budget of 1 byte kept %s:%d, which affirms nothing — the budget is not being enforced",
				ep.File, ep.Line)
		}
	}
	if len(starved.Actionable.Endpoints) >= len(full.Actionable.Endpoints) {
		t.Fatalf("the starved run rendered %d actionable endpoint(s) and the full run %d — nothing was withheld",
			len(starved.Actionable.Endpoints), len(full.Actionable.Endpoints))
	}
	if got, w := mustJSON(t, invariantOf(starved)), mustJSON(t, invariantOf(full)); got != w {
		t.Errorf("withholding endpoints changed what codefit concluded — something is computed over the "+
			"RENDERED subset instead of the complete analysis.\nfull budget: %s\nstarved:     %s", w, got)
	}

	// The bucket COUNTS are part of the conclusion too: they say how many endpoints
	// codefit put in each bucket, not how many it printed.
	if starved.Actionable.Count != full.Actionable.Count ||
		starved.ResolvedClean.Count != full.ResolvedClean.Count ||
		starved.FrontierPending.Count != full.FrontierPending.Count {
		t.Errorf("the bucket counts collapsed onto what was rendered: starved=%d/%d/%d full=%d/%d/%d",
			starved.Actionable.Count, starved.ResolvedClean.Count, starved.FrontierPending.Count,
			full.Actionable.Count, full.ResolvedClean.Count, full.FrontierPending.Count)
	}
}

// --- Test contract item 3 ------------------------------------------------------

// The fix drops DETAIL, never endpoints: the set of endpoints named in actionable
// is exactly the set the pre-change response detailed there.
func TestScanAllBudget_NamesEveryEndpointItUsedToDetail(t *testing.T) {
	root := t.TempDir()
	budgetFixture(t, root)
	resp := runBudgeted(t, root, ResponseBudgetBytes)

	var want []string
	readGolden(t, "scanall_prechange_actionable_endpoints.json", &want)
	if len(want) == 0 {
		t.Fatal("the pre-change golden names no actionable endpoint — this test would pass over an empty response")
	}
	got := namedActionable(resp)
	for _, key := range want {
		if !got[key] {
			t.Errorf("endpoint %q was DETAILED in the pre-change response and is not even named now", key)
		}
	}
	if len(got) != len(want) {
		t.Errorf("actionable names %d endpoint(s), the pre-change response detailed %d: %v vs %v",
			len(got), len(want), sortedKeys(got), want)
	}
}

// --- Test contract item 4 (R2) -------------------------------------------------

// A deterministic finding is a fact codefit already concluded, not a question. It
// stays in the response IN FULL — id, title, description, fingerprint — and is
// never demoted to a name the agent has to spend a second call to read.
//
// The equality below is ENDPOINT-ANCHORED and now says so: it counts concerns
// attached to endpoints, so it can only ever be compared against the SECURITY
// dimension's count. It used to read the unqualified summary.deterministic_findings
// and survived purely because budgetFixture configures no database — the moment a
// db finding existed, that number would have exceeded the endpoint-attached
// concerns and this test would have failed for a reason that had nothing to do
// with what it locks. It reads .Summary.Security explicitly now, and
// TestScanAllBudget_TotalsBreakTheEndpointAnchoredEqualityOverADBProject arms the
// trap: over a DB-carrying project, pointing this same equality at totals fails.
func TestScanAllBudget_DeterministicFindingIsNeverDemotedToAName(t *testing.T) {
	root := t.TempDir()
	budgetFixture(t, root)
	resp := runBudgeted(t, root, ResponseBudgetBytes)

	if resp.Summary.Security.DeterministicFindings == 0 {
		t.Fatal("the fixture produced no deterministic finding — this test would prove nothing")
	}
	var found []report.Concern
	for _, ep := range resp.Actionable.Endpoints {
		found = append(found, ep.Deterministic...)
	}
	if len(found) != resp.Summary.Security.DeterministicFindings {
		t.Fatalf("the response carries %d deterministic concern(s) for %d deterministic finding(s) — "+
			"one was hidden behind a second call", len(found), resp.Summary.Security.DeterministicFindings)
	}
	for _, c := range found {
		if c.Certainty != report.Deterministic || !c.Affirms {
			t.Errorf("a deterministic concern lost its certainty: %+v", c)
		}
		if c.ID == "" || c.Title == "" || c.Description == "" || c.Fingerprint == "" {
			t.Errorf("the deterministic concern is not in FULL (id/title/description/fingerprint): %+v", c)
		}
	}
	// And it survives the budget: a deterministic fact is not what makes the
	// payload big, and it must not be the thing that gets dropped.
	starved := runBudgeted(t, root, 1)
	n := 0
	for _, ep := range starved.Actionable.Endpoints {
		n += len(ep.Deterministic)
	}
	if n != resp.Summary.Security.DeterministicFindings {
		t.Errorf("a starved budget withheld %d of %d deterministic finding(s)",
			resp.Summary.Security.DeterministicFindings-n, resp.Summary.Security.DeterministicFindings)
	}
}

// --- Test contract item 5 ------------------------------------------------------

// Every endpoint named in actionable can be fetched with codefit-scan-endpoint,
// and what comes back is what the old response inlined for it — the same concern
// list, from the same analysis, only recomputed on demand.
func TestScanAllBudget_NamedEndpointIsFetchableAndMatchesTheWithheldDetail(t *testing.T) {
	root := t.TempDir()
	budgetFixture(t, root)

	resp, complete, _, err := buildScanAll(ScanAllRequest{Root: root, Language: "typescript"}, scope.Full(), freshBaseline(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(complete) == 0 {
		t.Fatal("the fixture produced no actionable endpoint — nothing to fetch")
	}
	rendered, _ := withNamedActionable(resp, complete, ResponseBudgetBytes)
	named := namedActionable(rendered)
	if len(named) != len(complete) {
		t.Fatalf("the response names %d endpoint(s) for %d complete one(s)", len(named), len(complete))
	}

	for _, want := range complete {
		key := want.File + ":" + strconv.Itoa(want.Line)
		if !named[key] {
			t.Errorf("%s is not named in actionable", key)
			continue
		}
		got, err := HandleScanEndpoint(ScanEndpointRequest{Root: root, Language: "typescript", File: want.File})
		if err != nil {
			t.Fatalf("scan-endpoint %s: %v", want.File, err)
		}
		if !got.Found {
			t.Errorf("scan-endpoint could not find %q, an endpoint scan-all named", want.File)
			continue
		}
		var match *report.EndpointReport
		for i := range got.Endpoints {
			if got.Endpoints[i].Line == want.Line {
				match = &got.Endpoints[i]
			}
		}
		if match == nil {
			t.Errorf("scan-endpoint returned no endpoint at %s", key)
			continue
		}
		// The baseline annotation is scan-all's, not the file's: scan-endpoint is
		// stateless and reads no baseline, so it is stripped before comparing. Every
		// other byte of the withheld detail must be there.
		if a, b := mustJSON(t, stripBaseline(match.Concerns)), mustJSON(t, stripBaseline(want.Concerns)); a != b {
			t.Errorf("scan-endpoint's detail for %s differs from the detail scan-all withheld.\nwithheld: %s\nfetched:  %s", key, b, a)
		}
	}
}

func stripBaseline(cs []report.Concern) []report.Concern {
	out := make([]report.Concern, len(cs))
	copy(out, cs)
	for i := range out {
		out[i].Baseline = ""
	}
	return out
}

// --- Test contract item 6 (R3) -------------------------------------------------

// Naming instead of inlining is a constant-factor win, not a bound. When the
// endpoint list still does not fit, the response says so: how many endpoints were
// withheld, and on what ordering. Silent truncation is the one forbidden outcome.
func TestScanAllBudget_WithholdingIsDeclaredNeverSilent(t *testing.T) {
	root := t.TempDir()
	budgetFixture(t, root)

	generous := runBudgeted(t, root, ResponseBudgetBytes)
	if generous.Budget.Bytes != ResponseBudgetBytes {
		t.Errorf("the response does not declare its budget: got %d, want %d", generous.Budget.Bytes, ResponseBudgetBytes)
	}
	if generous.Budget.Withheld != 0 {
		t.Errorf("the fixture fits the real budget; withheld should be 0, got %d", generous.Budget.Withheld)
	}
	if generous.Budget.Note == "" {
		t.Error("a response that withheld nothing must SAY it is complete, or an agent cannot tell it apart from one that was cut")
	}

	total := generous.Actionable.Count + generous.ResolvedClean.Count + generous.FrontierPending.Count
	if total < 3 {
		t.Fatalf("the fixture has %d endpoint(s): too few for a withholding test", total)
	}
	// Below the fixture's full size, above its withheld-everything FLOOR. The
	// floor is not a constant of the fixture: it is the size of a response with
	// every droppable endpoint withheld, so every always-present block counts
	// against it.
	//
	// Measured over this fixture, len(json.Marshal(resp)) — the same length
	// fitsBudget uses — with the two trees read at the same budget so the digit
	// count of the number embedded in budget.note cannot move the result:
	//
	//	                       origin/main    this change    delta
	//	floor (all droppable
	//	withheld, 1 endpoint)         4016           4546     +530
	//	full response                 4747           5277     +530
	//	serialized `summary`            83            613     +530
	//
	// The floor grew 530 bytes, and the three deltas agreeing is the point: the
	// whole growth IS the summary block. Of those 530, the always-present note
	// prose costs 440 (438 runes) and the per-dimension nesting the remaining 90.
	// About 1.3% of the 40 000-byte budget — a real, declared cost, paid once per
	// response.
	//
	// So 4000 stopped working for this test not "because the floor moved from
	// under 4000": on origin/main a 4000-byte budget FIT, at exactly 4000 bytes
	// with 2 endpoints rendered. At HEAD the smallest response this fixture can
	// produce is 4546, so a 4000-byte budget lands in the impossible-budget case
	// instead of the partial-withholding one this test is about. 4800 sits inside
	// the [4546, 5277] window: it renders 3 of 8 endpoints at 4774 bytes.
	// (4530 is NOT the floor — it is the size at a 4530-byte budget, where 2
	// endpoints fit; an earlier revision of this comment called it the floor.)
	// The guards immediately below fail loudly if this number ever stops
	// producing partial withholding.
	const tightBudget = 4800
	tight := runBudgeted(t, root, tightBudget)
	rendered := len(tight.Actionable.Endpoints) + len(tight.ResolvedClean.Endpoints) + len(tight.FrontierPending.Endpoints)
	if rendered == total {
		t.Fatalf("a %d-byte budget rendered all %d endpoint(s) — the budget is not being enforced", tightBudget, total)
	}
	if tight.Budget.Withheld != total-rendered {
		t.Errorf("the response withheld %d endpoint(s) but declares %d", total-rendered, tight.Budget.Withheld)
	}
	if tight.Budget.Ordering == "" {
		t.Error("a response that withheld endpoints must state the ORDERING it kept, or the agent cannot know what it is holding")
	}
	if !strings.Contains(strings.ToLower(tight.Budget.Note), "withheld") {
		t.Errorf("the budget note must say endpoints were withheld, got %q", tight.Budget.Note)
	}
	// Per bucket too: a bucket whose count exceeds what it rendered must account
	// for the difference itself, not leave the agent to subtract.
	for _, b := range []struct {
		name     string
		count    int
		rendered int
		withheld int
	}{
		{"actionable", tight.Actionable.Count, len(tight.Actionable.Endpoints), tight.Actionable.Withheld},
		{"resolved_clean", tight.ResolvedClean.Count, len(tight.ResolvedClean.Endpoints), tight.ResolvedClean.Withheld},
		{"frontier_pending", tight.FrontierPending.Count, len(tight.FrontierPending.Endpoints), tight.FrontierPending.Withheld},
	} {
		if b.withheld != b.count-b.rendered {
			t.Errorf("%s: count=%d rendered=%d but withheld=%d", b.name, b.count, b.rendered, b.withheld)
		}
	}

	// And the budget is actually met.
	raw, err := json.Marshal(tight)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > tightBudget {
		t.Errorf("the response is %d bytes over a %d-byte budget", len(raw)-tightBudget, tightBudget)
	}

	// The floor: a budget so small that withholding every droppable endpoint is
	// not enough. codefit will not start clipping a section to make a number look
	// right — it says the response is over. An implementation that silently
	// returned an oversized response with a satisfied-looking note would pass every
	// assertion above and fail here.
	impossible := runBudgeted(t, root, 500)
	tiny, err := json.Marshal(impossible)
	if err != nil {
		t.Fatal(err)
	}
	if len(tiny) <= 500 {
		t.Fatalf("a 500-byte response is not the case this checks (got %d bytes) — pick a smaller budget", len(tiny))
	}
	if !strings.Contains(impossible.Budget.Note, "WARNING") {
		t.Errorf("a response that is STILL over budget after withholding everything must say so, got %q",
			impossible.Budget.Note)
	}
}

// --- Test contract item 7 ------------------------------------------------------

// The per-endpoint summary must carry enough to RANK and CHOOSE without fetching:
// how many concerns and of which categories, the highest certainty present, and
// whether a deterministic affirmation is there.
func TestScanAllBudget_NamedEndpointCarriesEnoughToRank(t *testing.T) {
	root := t.TempDir()
	budgetFixture(t, root)

	resp, complete, _, err := buildScanAll(ScanAllRequest{Root: root, Language: "typescript"}, scope.Full(), freshBaseline(t))
	if err != nil {
		t.Fatal(err)
	}
	rendered, _ := withNamedActionable(resp, complete, ResponseBudgetBytes)
	named := rendered.Actionable.Endpoints
	if len(named) == 0 {
		t.Fatal("no actionable endpoint — this test would prove nothing")
	}
	byKey := map[string]report.EndpointReport{}
	for _, ep := range complete {
		byKey[ep.File+":"+strconv.Itoa(ep.Line)] = ep
	}

	sawAffirmation, sawWithout := false, false
	for _, n := range named {
		key := n.File + ":" + strconv.Itoa(n.Line)
		want, ok := byKey[key]
		if !ok {
			t.Fatalf("named endpoint %q is not in the complete analysis", key)
		}
		if n.Concerns != len(want.Concerns) {
			t.Errorf("%s: summary says %d concern(s), the endpoint has %d", key, n.Concerns, len(want.Concerns))
		}
		if n.Actionable != want.Actionable || n.CertainConcerns != want.CertainConcerns {
			t.Errorf("%s: summary says actionable=%d certain=%d, the endpoint has %d/%d",
				key, n.Actionable, n.CertainConcerns, want.Actionable, want.CertainConcerns)
		}
		wantCats := map[string]bool{}
		for _, c := range want.Concerns {
			if c.Category != "" {
				wantCats[c.Category] = true
			}
		}
		gotCats := map[string]bool{}
		for _, c := range n.Categories {
			gotCats[c] = true
		}
		if mustJSON(t, sortedKeys(gotCats)) != mustJSON(t, sortedKeys(wantCats)) {
			t.Errorf("%s: summary names categories %v, the endpoint has %v", key, sortedKeys(gotCats), sortedKeys(wantCats))
		}
		if n.HighestCertainty != want.Concerns[0].Certainty {
			t.Errorf("%s: summary says highest certainty %q, the endpoint's best is %q",
				key, n.HighestCertainty, want.Concerns[0].Certainty)
		}
		affirmed := false
		for _, c := range want.Concerns {
			if c.Affirms {
				affirmed = true
			}
		}
		if n.HasAffirmation != affirmed {
			t.Errorf("%s: summary says has_affirmation=%v, the endpoint's truth is %v", key, n.HasAffirmation, affirmed)
		}
		if affirmed {
			sawAffirmation = true
		} else {
			sawWithout = true
		}
		if len(n.Gaps) == 0 {
			t.Errorf("%s: an actionable endpoint must name the KIND of gap it has", key)
		}
	}
	// Both sides of the affirmation flag were exercised, or a hardcoded `true` (or
	// `false`) would pass the loop above.
	if !sawAffirmation || !sawWithout {
		t.Errorf("the fixture exercised has_affirmation=true:%v false:%v — a constant would pass this test",
			sawAffirmation, sawWithout)
	}
}

// --- I4 honesty lock (audit-protocol.md) ---------------------------------------

// manyActionableFixture writes n distinct IDOR-shaped endpoints (local access,
// no authz, no field selection — each one lands in `actionable` with a certain
// concern). n is chosen by the caller to be large enough that the rendered
// response overflows ResponseBudgetBytes regardless of what that constant is
// currently tuned to — the test below verifies that overflow actually happened
// instead of assuming it, so a future re-calibration cannot silently turn this
// into a no-op.
func manyActionableFixture(t *testing.T, root string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		rel := fmt.Sprintf("app/resource%d/[id]/route.ts", i)
		content := fmt.Sprintf(`
export async function GET(req: Request, { params }: { params: { id: string } }) {
  return Response.json(await prisma.resource%d.findUnique({ where: { id: params.id } }));
}`, i)
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// TestScanAllBudget_HonestyPersistsWhenTheBudgetForcesWithholding locks
// invariant I4 (docs/specs/audit-protocol.md): "a result that is a prefix of
// the truth must say which prefix". It is deliberately independent of
// TestScanAllBudget_ConclusionsAreComputedOverTheCompleteAnalysis, which only
// checks that two runs at DIFFERENT budgets agree with EACH OTHER — a mutation
// that broke both runs identically (e.g. hardcoding every bucket's Count to 0)
// would pass that comparison. This test instead checks each bucket's Count
// against a KNOWN GROUND TRUTH: the exact number of endpoints the fixture
// created, established independently of any rendering.
//
// Mutation-proved: with report.ClassifyEndpoints/NameActionable's Count wired
// from len(rendered Endpoints) instead of the complete pre-render slice (the
// exact defect ADR 0054 R4 forbids), this test fails — actionableCount no
// longer equals n once withholding drops entries, and Budget.Withheld no
// longer accounts for the gap. Restoring the wiring turns it green again.
func TestScanAllBudget_HonestyPersistsWhenTheBudgetForcesWithholding(t *testing.T) {
	root := t.TempDir()
	const n = 450
	manyActionableFixture(t, root, n)

	resp := runBudgeted(t, root, ResponseBudgetBytes)

	// The rendered response always fits (fitToBudget withholds exactly until it
	// does), so the guard that this fixture actually FORCES withholding is on
	// Budget.Withheld, not on the marshaled size exceeding the budget. If a
	// future re-calibration raises the budget past what n endpoints produce,
	// this guard fails loudly instead of the invariant quietly going untested.
	if resp.Budget.Withheld == 0 {
		t.Fatalf("the %d-endpoint fixture fit entirely within the %d-byte budget — this test no longer "+
			"forces withholding; grow the fixture", n, ResponseBudgetBytes)
	}
	if raw, err := json.Marshal(resp); err != nil {
		t.Fatal(err)
	} else if len(raw) > ResponseBudgetBytes {
		t.Errorf("the rendered response is %d bytes, %d over the declared %d-byte budget",
			len(raw), len(raw)-ResponseBudgetBytes, ResponseBudgetBytes)
	}

	// I4, ground truth: Actionable.Count is the COMPLETE number codefit
	// classified — every one of the n endpoints the fixture created, none of
	// them clean or frontier by construction (IDOR with no authz is always a
	// certain, actionable gap).
	if resp.Actionable.Count != n {
		t.Fatalf("the fixture created %d actionable endpoint(s), the response's Actionable.Count says %d — "+
			"Count drifted from the complete classification", n, resp.Actionable.Count)
	}
	if resp.ResolvedClean.Count != 0 || resp.FrontierPending.Count != 0 {
		t.Fatalf("the fixture is actionable-only by construction, got resolved_clean=%d frontier_pending=%d",
			resp.ResolvedClean.Count, resp.FrontierPending.Count)
	}

	// I4, the arithmetic a reader relies on: what is rendered plus what is
	// declared withheld must reconstruct the complete count, per bucket and in
	// the roll-up Budget.Withheld.
	rendered := len(resp.Actionable.Endpoints)
	if rendered >= n {
		t.Fatalf("the response rendered all %d endpoint(s) despite overflowing the budget — nothing was withheld", rendered)
	}
	if resp.Actionable.Withheld != resp.Actionable.Count-rendered {
		t.Errorf("actionable: count=%d rendered=%d but withheld=%d", resp.Actionable.Count, rendered, resp.Actionable.Withheld)
	}
	if resp.Budget.Withheld != n-rendered {
		t.Errorf("Budget.Withheld=%d, want %d (the %d endpoints the fixture created minus the %d rendered)",
			resp.Budget.Withheld, n-rendered, n, rendered)
	}
	if !strings.Contains(strings.ToLower(resp.Budget.Note), "withheld") {
		t.Errorf("Budget.Note must say endpoints were withheld, got %q", resp.Budget.Note)
	}
}

// --- summary covers every measured dimension (I4) ------------------------------

// dbCarryingFixture is a SIBLING of budgetFixture, never an edit to it.
//
// budgetFixture has TEN pre-existing call sites — seven in this file and three in
// scanall_writegate_test.go, counted with `git grep -c 'budgetFixture(t, root)'
// origin/main -- internal/mcp/`, not from memory; the design and tasks artifacts
// said nine and were wrong — and two goldens captured at 79e34b0 that
// testdata/README.md forbids regenerating. Giving it a schema would move EVERY
// field of scanall_prechange_invariant.json — summary, score.by_dimension.db
// (null -> a number), baseline.new, scope.audited/auditable_total — and there is
// no pre-change tree left to re-capture from. budgetFixture writes no
// .codefit.yaml at all, so a sibling that does is purely additive: zero effect on
// those ten call sites. This function is the eleventh, and the only one this
// change added.
//
// The shape is the same MIXED shape, plus a Prisma schema chosen to produce BOTH
// kinds of db output — a table with no primary key (DB-050, a deterministic
// affirmation) and a table with no audit timestamps (db-no-timestamps, a surface
// question). assertDBCarryingFixtureProducesBoth verifies that by CONTENT: a
// fixture is verified by what it produces, never by its name.
func dbCarryingFixture(t *testing.T, root string) {
	t.Helper()
	budgetFixture(t, root)
	files := map[string]string{
		".codefit.yaml": `version: "1"
project:
  name: t
  language: typescript
  framework: next
database:
  type: postgresql
  schema_paths:
    - prisma/schema.prisma
`,
		"prisma/schema.prisma": `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model NoKey {
  name String
}
`,
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// assertDBCarryingFixtureProducesBoth is the CONTENT verification of
// dbCarryingFixture. Every control in this section reasons about a db finding and
// a db surface item reaching the summary; if the fixture stopped producing either
// one, those controls would go green over nothing at all — the exact
// passes-for-the-wrong-reason failure this whole change exists to eliminate. So
// this is a Fatal, and it is called first in every test that depends on it.
func assertDBCarryingFixtureProducesBoth(t *testing.T, resp ScanAllResponse) {
	t.Helper()
	if resp.DB == nil || !resp.DB.Measured {
		t.Fatalf("dbCarryingFixture must produce a MEASURED db section, got %+v", resp.DB)
	}
	if resp.Summary.DB == nil {
		t.Fatal("dbCarryingFixture measured the db dimension but summary.db is null")
	}
	if resp.Summary.DB.DeterministicFindings < 1 {
		t.Fatalf("dbCarryingFixture must produce at least one DB FINDING; summary.db says %d — every "+
			"assertion in this section would otherwise pass over nothing",
			resp.Summary.DB.DeterministicFindings)
	}
	if resp.Summary.DB.SurfaceItems < 1 {
		t.Fatalf("dbCarryingFixture must produce at least one DB SURFACE item; summary.db says %d — every "+
			"assertion in this section would otherwise pass over nothing", resp.Summary.DB.SurfaceItems)
	}
	if resp.Summary.Security == nil || resp.Summary.Security.Endpoints == 0 {
		t.Fatalf("dbCarryingFixture must keep budgetFixture's endpoints — the point is a project where BOTH "+
			"dimensions have something to say, got %+v", resp.Summary.Security)
	}
}

// TestScanAllBudget_SummaryCountsTheDBDimensionOverADBCarryingProject is the
// per-dimension count control, driven through the real handler over a project
// where both dimensions produce output.
//
// Mutation that MUST turn it red: make summary.db read secRes instead of dbRes.
func TestScanAllBudget_SummaryCountsTheDBDimensionOverADBCarryingProject(t *testing.T) {
	root := t.TempDir()
	dbCarryingFixture(t, root)
	resp := runBudgeted(t, root, ResponseBudgetBytes)
	assertDBCarryingFixtureProducesBoth(t, resp)

	// Each sub-block counts ITS OWN dimension's raw result and nothing else.
	if got, want := resp.Summary.DB.DeterministicFindings, len(resp.DB.Findings); got != want {
		t.Errorf("summary.db.deterministic_findings = %d, the db section holds %d finding(s)", got, want)
	}
	if got, want := resp.Summary.DB.SurfaceItems, resp.DB.Count; got != want {
		t.Errorf("summary.db.surface_items = %d, the db section's own Count says %d surface item(s)", got, want)
	}
	// The sharpest form of the mutation this catches: if summary.db were fed
	// secRes, its counts would equal the security ones. Over this fixture they
	// are different numbers, so equality is the tell.
	if resp.Summary.DB.DeterministicFindings == resp.Summary.Security.DeterministicFindings &&
		resp.Summary.DB.SurfaceItems == resp.Summary.Security.SurfaceItems {
		t.Errorf("summary.db is identical to summary.security (%+v) — it is being fed the security result",
			resp.Summary.DB)
	}
	// schema_sources is the db dimension's own scale unit: the one configured
	// schema this pass read, shared with the scope census.
	if resp.Summary.DB.SchemaSources != 1 {
		t.Errorf("summary.db.schema_sources = %d, want 1 (the single configured schema)", resp.Summary.DB.SchemaSources)
	}
	// totals is derived and strictly larger than either dimension alone.
	wantDet := resp.Summary.Security.DeterministicFindings + resp.Summary.DB.DeterministicFindings
	wantSurf := resp.Summary.Security.SurfaceItems + resp.Summary.DB.SurfaceItems
	if resp.Summary.Totals.DeterministicFindings != wantDet || resp.Summary.Totals.SurfaceItems != wantSurf {
		t.Errorf("totals = %+v, want {deterministic_findings:%d surface_items:%d} — totals is not derived "+
			"from the sub-blocks", resp.Summary.Totals, wantDet, wantSurf)
	}
	if resp.Summary.Totals.SurfaceItems <= resp.Summary.Security.SurfaceItems {
		t.Errorf("totals.surface_items (%d) does not exceed the security count (%d) — the db surface is "+
			"still invisible in the roll-up", resp.Summary.Totals.SurfaceItems, resp.Summary.Security.SurfaceItems)
	}
}

// TestScanAllBudget_TotalsBreakTheEndpointAnchoredEqualityOverADBProject ARMS
// the trap that TestScanAllBudget_DeterministicFindingIsNeverDemotedToAName
// survived by accident.
//
// That test asserts "every deterministic finding is carried in full on an
// endpoint" as an equality between endpoint-attached concerns and a summary
// count. Endpoint-attached concerns are security by definition — a table has no
// route — so the equality is only ever true against the SECURITY sub-block. Over
// a DB-carrying project the unqualified/rolled-up number is strictly larger, and
// this test proves it: it evaluates the same equality against totals and requires
// it to FAIL. Without this, the fix would have preserved the accidental survival
// (a fixture with no database) instead of removing it.
func TestScanAllBudget_TotalsBreakTheEndpointAnchoredEqualityOverADBProject(t *testing.T) {
	root := t.TempDir()
	dbCarryingFixture(t, root)
	resp := runBudgeted(t, root, ResponseBudgetBytes)
	assertDBCarryingFixtureProducesBoth(t, resp)

	endpointAttached := 0
	for _, ep := range resp.Actionable.Endpoints {
		endpointAttached += len(ep.Deterministic)
	}

	// The equality as it must be written: against the security sub-block. The
	// message names BOTH candidate right-hand sides, so a mutation that points
	// this at totals produces a transcript a reader can act on.
	if endpointAttached != resp.Summary.Security.DeterministicFindings {
		t.Fatalf("the endpoint-anchored equality must hold against summary.security and ONLY there: "+
			"%d endpoint-attached deterministic concern(s); summary.security says %d, summary.totals says %d",
			endpointAttached, resp.Summary.Security.DeterministicFindings, resp.Summary.Totals.DeterministicFindings)
	}
	// The same equality pointed at totals must BREAK here. If it does not, this
	// project is not exercising the trap and every claim above is untested.
	if endpointAttached == resp.Summary.Totals.DeterministicFindings {
		t.Fatalf("the endpoint-anchored equality still holds against summary.totals (%d == %d) — this "+
			"fixture is not DB-carrying enough to arm the trap, so the accidental survival is preserved "+
			"rather than fixed", endpointAttached, resp.Summary.Totals.DeterministicFindings)
	}
	if resp.Summary.Totals.DeterministicFindings <= endpointAttached {
		t.Errorf("totals.deterministic_findings (%d) must EXCEED the endpoint-attached count (%d): the "+
			"difference is exactly the db dimension's affirmations, which have no endpoint to attach to",
			resp.Summary.Totals.DeterministicFindings, endpointAttached)
	}
}

// TestScanAllBudget_ConclusionsAreComputedOverTheCompleteAnalysis_DBCarrying
// extends R4's full-vs-starved lock over a project where the db dimension has
// something to say, so its `Summary` field covers BOTH dimensions instead of
// silently covering one.
//
// What this adds and what it does not: R4's axis is complete-analysis vs
// rendered-subset, and both runs it compares share whatever blindness the code
// has — so it cannot, alone, prove the summary covers every measured dimension
// (that is TestScanAllBudget_SummaryCountsTheDBDimensionOverADBCarryingProject's
// job). What it adds is that summary.db is a CONCLUSION, not a rendering
// artifact: withholding endpoints must not move it.
//
// There is deliberately no pre-change golden here. None exists for a DB-carrying
// fixture and none can be created — the pre-change trees are gone. So this
// covers the full-vs-starved axis only, and says so rather than implying a
// "nothing moved" guarantee it cannot give.
func TestScanAllBudget_ConclusionsAreComputedOverTheCompleteAnalysis_DBCarrying(t *testing.T) {
	root := t.TempDir()
	dbCarryingFixture(t, root)

	full := runBudgeted(t, root, ResponseBudgetBytes)
	assertDBCarryingFixtureProducesBoth(t, full)

	starved := runBudgeted(t, root, 1)
	if len(starved.ResolvedClean.Endpoints) != 0 || len(starved.FrontierPending.Endpoints) != 0 {
		t.Fatalf("a budget of 1 byte still rendered %d clean / %d frontier endpoint(s) — this comparison is "+
			"not measuring a starved response",
			len(starved.ResolvedClean.Endpoints), len(starved.FrontierPending.Endpoints))
	}
	if len(starved.Actionable.Endpoints) >= len(full.Actionable.Endpoints) {
		t.Fatalf("the starved run rendered %d actionable endpoint(s) and the full run %d — nothing was withheld",
			len(starved.Actionable.Endpoints), len(full.Actionable.Endpoints))
	}
	if got, w := mustJSON(t, invariantOf(starved)), mustJSON(t, invariantOf(full)); got != w {
		t.Errorf("withholding endpoints changed what codefit concluded over a DB-carrying project — "+
			"something is computed over the RENDERED subset instead of the complete analysis.\n"+
			"full budget: %s\nstarved:     %s", w, got)
	}
	// Read THROUGH invariantOf, deliberately, not off the responses. Reading
	// full.Summary.DB directly would be a different control: it would still pass
	// with Summary.DB dropped from responseInvariant, leaving this lock quietly
	// reduced to the security-only one it used to be while looking green. Going
	// through the projection is what makes "the invariant covers the db
	// dimension" the thing actually asserted.
	fullInv, starvedInv := invariantOf(full), invariantOf(starved)
	if fullInv.Summary.DB == nil || starvedInv.Summary.DB == nil {
		t.Fatalf("responseInvariant does not carry summary.db over a project whose db dimension ran "+
			"(full=%v starved=%v) — the R4 lock has been reduced to a security-only one, and a db count "+
			"could drift between budgets unnoticed", fullInv.Summary.DB, starvedInv.Summary.DB)
	}
	if a, b := mustJSON(t, fullInv.Summary.DB), mustJSON(t, starvedInv.Summary.DB); a != b {
		t.Errorf("summary.db differs between a full and a starved budget: full %s, starved %s", a, b)
	}
}

// --- wiring --------------------------------------------------------------------

// The public handler an agent actually calls carries the declared budget. Without
// this, every test above could pass while HandleScanAll never applied a budget.
func TestHandleScanAll_CarriesTheDeclaredBudget(t *testing.T) {
	root := t.TempDir()
	budgetFixture(t, root)
	resp, err := HandleScanAll(ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Budget.Bytes != ResponseBudgetBytes {
		t.Errorf("HandleScanAll declares budget %d, want the package's %d", resp.Budget.Bytes, ResponseBudgetBytes)
	}
	if resp.Budget.Note == "" || resp.Budget.Ordering == "" {
		t.Errorf("HandleScanAll returned a budget block with no note/ordering: %+v", resp.Budget)
	}
}
