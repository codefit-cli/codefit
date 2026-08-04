package mcp

import (
	"encoding/json"
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
type responseInvariant struct {
	Summary  ScanAllSummary       `json:"summary"`
	Scope    ScopeBlock           `json:"scope"`
	Score    scoring.ScoreSummary `json:"score"`
	Baseline BaselineDelta        `json:"baseline"`
}

func invariantOf(r ScanAllResponse) responseInvariant {
	return responseInvariant{Summary: r.Summary, Scope: r.Scope, Score: r.Score, Baseline: r.Baseline}
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

	var want responseInvariant
	readGolden(t, "scanall_prechange_invariant.json", &want)
	// Guard against a golden that locks nothing: a fixture that produced no
	// endpoints and a zero score would compare equal to anything.
	if want.Summary.Endpoints == 0 || want.Score.Global == 0 || want.Baseline.New == 0 {
		t.Fatalf("the pre-change golden is empty (%+v) — this test would pass over any response", want)
	}
	if got := mustJSON(t, invariantOf(full)); got != mustJSON(t, want) {
		t.Errorf("the change moved what codefit concluded.\npre-change: %s\npost-change: %s", mustJSON(t, want), got)
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
func TestScanAllBudget_DeterministicFindingIsNeverDemotedToAName(t *testing.T) {
	root := t.TempDir()
	budgetFixture(t, root)
	resp := runBudgeted(t, root, ResponseBudgetBytes)

	if resp.Summary.DeterministicFindings == 0 {
		t.Fatal("the fixture produced no deterministic finding — this test would prove nothing")
	}
	var found []report.Concern
	for _, ep := range resp.Actionable.Endpoints {
		found = append(found, ep.Deterministic...)
	}
	if len(found) != resp.Summary.DeterministicFindings {
		t.Fatalf("the response carries %d deterministic concern(s) for %d deterministic finding(s) — "+
			"one was hidden behind a second call", len(found), resp.Summary.DeterministicFindings)
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
	if n != resp.Summary.DeterministicFindings {
		t.Errorf("a starved budget withheld %d of %d deterministic finding(s)",
			resp.Summary.DeterministicFindings-n, resp.Summary.DeterministicFindings)
	}
}

// --- Test contract item 5 ------------------------------------------------------

// Every endpoint named in actionable can be fetched with codefit-scan-endpoint,
// and what comes back is what the old response inlined for it — the same concern
// list, from the same analysis, only recomputed on demand.
func TestScanAllBudget_NamedEndpointIsFetchableAndMatchesTheWithheldDetail(t *testing.T) {
	root := t.TempDir()
	budgetFixture(t, root)

	resp, complete, err := buildScanAll(ScanAllRequest{Root: root, Language: "typescript"}, scope.Full(), freshBaseline(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(complete) == 0 {
		t.Fatal("the fixture produced no actionable endpoint — nothing to fetch")
	}
	named := namedActionable(withNamedActionable(resp, complete, ResponseBudgetBytes))
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
	const tightBudget = 4000 // below the fixture's full size, above its withheld-everything floor
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

	resp, complete, err := buildScanAll(ScanAllRequest{Root: root, Language: "typescript"}, scope.Full(), freshBaseline(t))
	if err != nil {
		t.Fatal(err)
	}
	named := withNamedActionable(resp, complete, ResponseBudgetBytes).Actionable.Endpoints
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
