package typescript_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
)

// A frontier surface item (the data left the handler body, local_access_detected
// =false) must read as an UNRESOLVED candidate the agent has to follow — never as
// an absence/negative result the agent can discard. In real dogfooding the agent
// read the old "No ... detected / could not check / may be ... (follow to confirm)"
// wording as "probable false positive" and dropped ~98 items. These tests pin the
// new framing so the regression cannot return: the limit is stated as an
// AFFIRMATION ("codefit does not follow ..."; "... is NOT verified here"), and the
// instruction to follow the data is its OWN sentence, not a parenthetical aside.

// bannedFrontierPhrases are the negative-result phrasings that made the agent
// discard. They must never appear in a frontier signal again.
var bannedFrontierPhrases = []string{"could not", "may be", "not detected", "(follow"}

// assertUnresolvedFrontierSignal enforces the new wording contract on a frontier
// item's structural_signals, taken together.
func assertUnresolvedFrontierSignal(t *testing.T, it findings.SurfaceItem) {
	t.Helper()
	joined := strings.Join(it.StructuralSignals, " || ")
	low := strings.ToLower(joined)

	for _, banned := range bannedFrontierPhrases {
		if strings.Contains(low, banned) {
			t.Errorf("frontier signal still contains the discardable phrase %q: %q", banned, joined)
		}
	}
	// The limit is an affirmation about what codefit does, not an absence.
	if !strings.Contains(low, "does not follow") {
		t.Errorf("frontier signal must state the limit affirmatively (\"does not follow ...\"), got %q", joined)
	}
	if !strings.Contains(low, "not verified") {
		t.Errorf("frontier signal must say the access/operation is NOT verified here, got %q", joined)
	}
	// The call to action is its own sentence (". Follow ..."), not a parenthetical.
	if !strings.Contains(joined, ". Follow ") {
		t.Errorf("the follow-the-data instruction must be its own sentence (\". Follow ...\"), got %q", joined)
	}
}

// frontierItem runs a surface query over a fixture and returns the single frontier
// item it must enumerate, failing if the count is not exactly 1 (detection must be
// unchanged by the wording).
func frontierItem(t *testing.T, items []findings.SurfaceItem) findings.SurfaceItem {
	t.Helper()
	if len(items) != 1 {
		t.Fatalf("expected exactly 1 frontier item (detection unchanged), got %d: %+v", len(items), items)
	}
	if v, ok := items[0].StructuralFacts["local_access_detected"]; !ok || v {
		t.Fatalf("expected a frontier item (local_access_detected=false), got %+v", items[0].StructuralFacts)
	}
	return items[0]
}

// IDOR frontier: the id flows into a service call, no local Prisma access.
func TestFrontierWording_IDOR(t *testing.T) {
	it := frontierItem(t, idorSurface(t, "app/notes/[id]/route.ts", `
export async function PATCH(req: Request, { params }: { params: Promise<{ id: string }> }) {
  const { id: noteId } = await params;
  const note = await NoteService.markAsRead(noteId);
  return Response.json(note);
}`))
	assertUnresolvedFrontierSignal(t, it)
}

// AUTHZ frontier: a sensitive operation through a service, no local Prisma access.
func TestFrontierWording_Authz(t *testing.T) {
	it := frontierItem(t, authzSurface(t, "app/billing/route.ts", `
export async function POST(req: Request) {
  const body = await req.json();
  return Response.json(await BillingService.charge(body));
}`))
	assertUnresolvedFrontierSignal(t, it)
}

// OVER-FETCH frontier: the serialized value comes from a service, not a local find.
func TestFrontierWording_Overfetch(t *testing.T) {
	it := frontierItem(t, overfetchSurface(t, "app/users/route.ts", `
export async function GET() {
  return Response.json(await UserService.getAll());
}`))
	assertUnresolvedFrontierSignal(t, it)
}
