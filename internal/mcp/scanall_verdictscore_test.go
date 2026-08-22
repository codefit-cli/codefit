package mcp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/mcp"
)

// addRoute writes one more REAL route file into an existing project, so the
// surface the test reasons about comes out of the real parser and the real
// sensor rather than a hand-built struct.
func addRoute(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "app", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "route.ts"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// scoreOf runs a real scan-all and returns the global score plus the delta.
func scoreOf(t *testing.T, root string) (int, mcp.BaselineDelta) {
	t.Helper()
	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("scan-all: %v", err)
	}
	return resp.Score.Global, resp.Baseline
}

// recordOne records a single verdict on the surface item at index i, through the
// real handler, and returns its fingerprint.
func recordOne(t *testing.T, root string, i int, v surface.Verdict, reasoning string) string {
	t.Helper()
	sec, err := mcp.HandleScanSecurity(mcp.ScanRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatalf("scan-security: %v", err)
	}
	if len(sec.Surface) <= i {
		t.Fatalf("fixture has %d surface items, need index %d", len(sec.Surface), i)
	}
	it := sec.Surface[i]
	resp, err := mcp.HandleBaselineRecordVerdict(mcp.BaselineRecordVerdictRequest{
		Root: root, Language: "typescript",
		Verdicts: []surface.Confirmation{{
			SurfaceID: it.ID, Category: it.Category, File: it.File, Line: it.Line,
			Verdict: v, Reasoning: reasoning, Confidence: 0.9,
		}},
	})
	if err != nil {
		t.Fatalf("record-verdict: %v", err)
	}
	if len(resp.Persisted) != 1 {
		t.Fatalf("want 1 persisted, got %+v", resp)
	}
	return resp.Persisted[0].Fingerprint
}

// R6 — a still-observed `vulnerable` verdict must reach the score.
//
// This is the whole point of the closing protocol and the step that reframes
// issue #2: before it, score.global could read 100 beside hundreds of open
// questions, not because the arithmetic was wrong but because the score was
// computed at a point where the only thing it could count was what codefit
// AFFIRMS on its own. An agent's answer had no road into it. Now it does.
func TestScanAll_AStillObservedVulnerableVerdictDropsTheScore(t *testing.T) {
	root := copyFixture(t)
	before, _ := scoreOf(t, root)

	recordOne(t, root, 0, surface.VerdictVulnerable, "no ownership check before the record is returned")

	after, delta := scoreOf(t, root)
	if after >= before {
		t.Errorf("score.global = %d after a vulnerable verdict, was %d before — an agent's confirmed "+
			"finding must reach the score, or the closing protocol closes nothing", after, before)
	}
	if delta.VerdictsNotScored != 0 {
		t.Errorf("VerdictsNotScored = %d, want 0: this dimension WAS measured", delta.VerdictsNotScored)
	}
}

// R7/D4 — `not_vulnerable` alone never removes alarm, and never cancels a
// conflicting `vulnerable` on the same item.
//
// The asymmetry is the safeguard: recording "this is fine" is a recommendation,
// and a recommendation must never be able to lower the alarm on its own. Only a
// human accepts.
func TestScanAll_NotVulnerableAloneNeverRemovesAlarm(t *testing.T) {
	// Half one: not_vulnerable by itself changes nothing.
	quiet := copyFixture(t)
	baseQuiet, _ := scoreOf(t, quiet)
	recordOne(t, quiet, 0, surface.VerdictNotVulnerable, "the guard is enforced upstream")
	afterQuiet, _ := scoreOf(t, quiet)
	if afterQuiet != baseQuiet {
		t.Errorf("score.global moved from %d to %d on a not_vulnerable verdict alone — a recommendation "+
			"that something is fine must not move the score in either direction", baseQuiet, afterQuiet)
	}

	// Half two: not_vulnerable does not cancel a vulnerable on the SAME item.
	both := copyFixture(t)
	baseBoth, _ := scoreOf(t, both)
	fpA := recordOne(t, both, 0, surface.VerdictVulnerable, "agent one: no ownership check")
	withVuln, _ := scoreOf(t, both)
	fpB := recordOne(t, both, 0, surface.VerdictNotVulnerable, "agent two: the middleware enforces scope")
	if fpA != fpB {
		t.Fatalf("the probe must put both verdicts on the SAME item, got %q and %q", fpA, fpB)
	}
	withBoth, delta := scoreOf(t, both)

	if withVuln >= baseBoth {
		t.Fatalf("probe broken: the vulnerable verdict did not move the score (%d -> %d)", baseBoth, withVuln)
	}
	if withBoth != withVuln {
		t.Errorf("score.global = %d with both verdicts, %d with the vulnerable one alone — a later "+
			"not_vulnerable must NOT suppress the penalty of a conflicting vulnerable on the same item",
			withBoth, withVuln)
	}
	if delta.InConflictCount != 1 {
		t.Errorf("InConflictCount = %d, want 1 — the disagreement must still reach a human", delta.InConflictCount)
	}
}

// A human `Ack` silences; an agent verdict does not. This is the ONE place the
// human's decision is honoured in the fold, and it is honoured nowhere else.
func TestScanAll_AnAckedItemsVerdictDoesNotScore(t *testing.T) {
	root := copyFixture(t)
	before, _ := scoreOf(t, root)
	fp := recordOne(t, root, 0, surface.VerdictVulnerable, "no ownership check before the record is returned")
	withVerdict, _ := scoreOf(t, root)
	if withVerdict >= before {
		t.Fatalf("probe broken: the verdict did not move the score (%d -> %d)", before, withVerdict)
	}

	if _, err := mcp.HandleBaselineAccept(mcp.BaselineAcceptRequest{
		Root: root, Fingerprints: []string{fp}, Reason: "reviewed by a human: the caller is trusted",
	}); err != nil {
		t.Fatalf("baseline-accept: %v", err)
	}

	afterAck, _ := scoreOf(t, root)
	if afterAck != before {
		t.Errorf("score.global = %d after a HUMAN accepted the item, want the pre-verdict %d — "+
			"a human decision silences, and the fold must honour it", afterAck, before)
	}
}

// N agreeing verdicts on one item penalise ONCE. Three agents reaching the same
// conclusion is corroboration, not three defects — and the tie-break must be
// deterministic, because a score that reshuffles between identical scans is
// worse than one that ranks imperfectly.
func TestScanAll_AgreeingVerdictsPenaliseExactlyOnce(t *testing.T) {
	one := copyFixture(t)
	recordOne(t, one, 0, surface.VerdictVulnerable, "agent one")
	scoreOne, _ := scoreOf(t, one)

	three := copyFixture(t)
	for _, r := range []string{"agent one", "agent two", "agent three"} {
		recordOne(t, three, 0, surface.VerdictVulnerable, r)
	}
	scoreThree, delta := scoreOf(t, three)

	if scoreThree != scoreOne {
		t.Errorf("score.global = %d with three agreeing verdicts, %d with one — corroboration is not "+
			"three defects", scoreThree, scoreOne)
	}
	if delta.ReasonedByAgent != 1 {
		t.Errorf("ReasonedByAgent = %d, want 1 — three verdicts, one item", delta.ReasonedByAgent)
	}
	// Determinism: the same baseline must score the same on a repeat run.
	again, _ := scoreOf(t, three)
	if again != scoreThree {
		t.Errorf("score.global = %d then %d on identical scans — the tie-break is not deterministic",
			scoreThree, again)
	}
}

// THE DIMENSION GATE. The security sensor owns the `nplus1` category, so a plain
// TypeScript project with no configured schema observes nplus1 surface while the
// db dimension never runs.
//
// A verdict on such an item must be DECLARED, not folded and not dropped.
// Folding it would make scoring.Compute pass over it in silence — under-reporting,
// I3's unforgivable direction. Appending DimensionDB to `measured` to compensate
// would claim a sensor ran that did not.
func TestScanAll_AVerdictOnAnUnmeasuredDimensionIsDeclaredNeverDropped(t *testing.T) {
	root := copyFixture(t)
	addRoute(t, root, "loop", "export async function GET() {\n"+
		"  for (const id of ids) {\n"+
		"    await prisma.user.findUnique({ where: { id } });\n"+
		"  }\n}\n")

	sec, err := mcp.HandleScanSecurity(mcp.ScanRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	idx := -1
	for i, it := range sec.Surface {
		if it.Category == "nplus1" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("probe broken: the fixture produced no nplus1 surface item (%d items)", len(sec.Surface))
	}

	before, beforeDelta := scoreOf(t, root)
	if beforeDelta.VerdictsNotScored != 0 {
		t.Fatalf("probe broken: nothing is reasoned yet, got VerdictsNotScored=%d", beforeDelta.VerdictsNotScored)
	}
	recordOne(t, root, idx, surface.VerdictVulnerable, "the query runs once per element of the list")

	after, delta := scoreOf(t, root)
	if delta.VerdictsNotScored != 1 {
		t.Errorf("VerdictsNotScored = %d, want 1 — a verdict codefit cannot score must be DECLARED, "+
			"never silently dropped", delta.VerdictsNotScored)
	}
	if delta.VerdictsNotScoredNote == "" {
		t.Error("the declaration must say WHICH dimension and WHY, not just a number")
	}
	if !strings.Contains(delta.VerdictsNotScoredNote, "db") {
		t.Errorf("the note must name the unmeasured dimension: %q", delta.VerdictsNotScoredNote)
	}
	if after != before {
		t.Errorf("score.global moved from %d to %d — a verdict in an unmeasured dimension must not "+
			"affect the score", before, after)
	}
	// And the dimension must STILL read as not measured: compensating by adding it
	// to `measured` would claim a sensor ran that did not.
	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Score.ByDimension["db"] != nil {
		t.Errorf("by_dimension.db = %v, want null — the db sensor did not run and the fold must not "+
			"pretend it did", *resp.Score.ByDimension["db"])
	}
}

// R6's OTHER half: a verdict stops scoring the moment the code it describes
// changes.
//
// This is what makes persisting a verdict safe at all. The baseline's identity
// is a content hash (ADR 0009), so an edit produces a different fingerprint and
// the old reasoning no longer matches anything observed. The guarantee is only
// real if something checks it — the fold intersects against this pass's observed
// set, and this is the control on that intersection.
func TestScanAll_AVerdictStopsScoringWhenTheCodeChanges(t *testing.T) {
	root := copyFixture(t)
	clean, _ := scoreOf(t, root)
	recordOne(t, root, 0, surface.VerdictVulnerable, "no ownership check before the record is returned")
	withVerdict, _ := scoreOf(t, root)
	if withVerdict >= clean {
		t.Fatalf("probe broken: the verdict did not move the score (%d -> %d)", clean, withVerdict)
	}

	// Rewrite the reasoned handler. Same file, different content -> different
	// content-hash fingerprint -> the recorded verdict describes code that is no
	// longer there.
	sec, err := mcp.HandleScanSecurity(mcp.ScanRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	reasoned := sec.Surface[0].File
	target := filepath.Join(root, filepath.FromSlash(reasoned))
	if err := os.WriteFile(target, []byte("export {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Positive control on the EDIT, not just on the score: the handler must
	// genuinely stop producing that surface, or this test would pass for the
	// wrong reason.
	again, err := mcp.HandleScanSecurity(mcp.ScanRequest{Root: root, Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range again.Surface {
		if it.Fingerprint == sec.Surface[0].Fingerprint {
			t.Fatalf("probe broken: %s still produces the reasoned item after the edit", reasoned)
		}
	}

	after, delta := scoreOf(t, root)
	if after == withVerdict {
		t.Errorf("score.global is still %d after the reasoned code changed — a verdict must stop "+
			"scoring once its item is no longer observed", after)
	}
	if delta.VerdictsNotScored != 0 {
		t.Errorf("VerdictsNotScored = %d, want 0 — a verdict on code that CHANGED is stale, not "+
			"unmeasurable; only an unmeasured dimension belongs in that count", delta.VerdictsNotScored)
	}
}
