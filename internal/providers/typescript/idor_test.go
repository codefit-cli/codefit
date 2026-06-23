package typescript_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

func idorSurface(t *testing.T, path, src string) []findings.SurfaceItem {
	t.Helper()
	items, err := typescript.New().AnalyzeSurface(providers.SourceFile{Path: path, Content: []byte(src)})
	if err != nil {
		t.Fatalf("AnalyzeSurface(%s): %v", path, err)
	}
	var idor []findings.SurfaceItem
	for _, it := range items {
		if it.Category == "idor" {
			idor = append(idor, it)
		}
	}
	return idor
}

func signalsJoined(it findings.SurfaceItem) string { return strings.Join(it.StructuralSignals, " || ") }

// judgmentWords are words a structural_signal must never contain — signals are
// FACTS, not judgments. reason_to_review asks a question; it never concludes.
var judgmentWords = []string{"vulnerable", "insecure", "missing authorization", "unauthorized", "exposes", "unsafe"}

func assertFactsNotJudgments(t *testing.T, it findings.SurfaceItem) {
	t.Helper()
	for _, s := range it.StructuralSignals {
		for _, w := range judgmentWords {
			if strings.Contains(strings.ToLower(s), w) {
				t.Errorf("structural_signal is a judgment, not a fact: %q (contains %q)", s, w)
			}
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(it.ReasonToReview), "?") {
		t.Errorf("reason_to_review must be a question, got %q", it.ReasonToReview)
	}
	for _, w := range judgmentWords {
		if strings.Contains(strings.ToLower(it.ReasonToReview), w) {
			t.Errorf("reason_to_review must not assert a judgment: %q", it.ReasonToReview)
		}
	}
}

// Canonical IDOR: params.id + prisma.user.findUnique, no authz in the body.
func TestIDOR_Canonical(t *testing.T) {
	items := idorSurface(t, "app/users/[id]/route.ts", `
export async function GET(req: Request, { params }: { params: { id: string } }) {
  const u = await prisma.user.findUnique({ where: { id: params.id } });
  return Response.json(u);
}`)
	if len(items) != 1 {
		t.Fatalf("want 1 IDOR surface item, got %d: %+v", len(items), items)
	}
	it := items[0]
	sig := signalsJoined(it)
	if !strings.Contains(sig, "params.id") {
		t.Errorf("signal must state it reads params.id, got %q", sig)
	}
	if !strings.Contains(sig, "prisma.user.findUnique") {
		t.Errorf("signal must state the prisma access, got %q", sig)
	}
	if !strings.Contains(strings.ToLower(sig), "no call to a known authorization helper") {
		t.Errorf("signal 3 must honestly state no authz was detected in the body, got %q", sig)
	}
	// Stable id derived from (file, line, category).
	if it.ID != surface.StableID(it.File, it.Line, "idor") {
		t.Errorf("surface id is not the stable id: %q", it.ID)
	}
	assertFactsNotJudgments(t, it)
}

// Variations that stress completeness: searchParams instead of params, delete
// instead of findUnique, an export const arrow handler, the request body.
func TestIDOR_InputAndAccessVariations(t *testing.T) {
	cases := []struct{ name, path, src, wantSignal string }{
		{"searchparams+delete", "app/tasks/route.ts", `
export async function DELETE(req: Request) {
  const id = req.nextUrl.searchParams.get("id");
  await prisma.task.delete({ where: { id } });
}`, "searchParams"},
		{"const-arrow+body+update", "app/orders/route.ts", `
export const POST = async (req: Request) => {
  const { id } = await req.json();
  await prisma.order.update({ where: { id }, data: {} });
}`, "req.json()"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			items := idorSurface(t, c.path, c.src)
			if len(items) != 1 {
				t.Fatalf("%s: want 1 IDOR item, got %d", c.name, len(items))
			}
			sig := signalsJoined(items[0])
			if !strings.Contains(sig, c.wantSignal) {
				t.Errorf("%s: expected input signal %q in %q", c.name, c.wantSignal, sig)
			}
			if !strings.Contains(sig, "prisma.") {
				t.Errorf("%s: expected a prisma access signal in %q", c.name, sig)
			}
			assertFactsNotJudgments(t, items[0])
		})
	}
}

// Prisma access is detected by SHAPE (<x>.<model>.<knownPrismaMethod>), not by
// the client being literally named "prisma" — otherwise every project that
// aliases the client (db, this.prisma, an imported db) would have INVISIBLE IDOR
// surface, the exact blind spot surface mapping exists to prevent. The signal
// names the real client it saw (a fact).
func TestIDOR_AliasedPrismaClients(t *testing.T) {
	cases := []struct{ name, path, src, wantAccess string }{
		{"local-new-client", "app/users/[id]/route.ts", `
export async function GET(req: Request, { params }: { params: { id: string } }) {
  const db = new PrismaClient();
  return Response.json(await db.user.findUnique({ where: { id: params.id } }));
}`, "db.user.findUnique"},
		{"imported-db", "app/tasks/[id]/route.ts", `
import { db } from "@/lib/prisma";
export async function PUT(req: Request, { params }: { params: { id: string } }) {
  await db.task.update({ where: { id: params.id }, data: {} });
}`, "db.task.update"},
		{"this-prisma", "app/orders/[id]/route.ts", `
export async function DELETE(req: Request, { params }: { params: { id: string } }) {
  await this.prisma.order.delete({ where: { id: params.id } });
}`, "this.prisma.order.delete"},
		// Honest over-enumeration: cache.session.update has the Prisma shape but is
		// not Prisma. We enumerate it (completeness over noise); the agent discards
		// it in a glance. We do NOT exclude it with fragile logic.
		{"over-match-not-prisma", "app/sessions/[id]/route.ts", `
export async function PATCH(req: Request, { params }: { params: { id: string } }) {
  await cache.session.update({ where: { id: params.id } });
}`, "cache.session.update"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			items := idorSurface(t, c.path, c.src)
			if len(items) != 1 {
				t.Fatalf("%s: aliased client must still be enumerated, got %d", c.name, len(items))
			}
			sig := signalsJoined(items[0])
			if !strings.Contains(sig, c.wantAccess) {
				t.Errorf("%s: access signal must name the real client %q, got %q", c.name, c.wantAccess, sig)
			}
			assertFactsNotJudgments(t, items[0])
		})
	}
}

// Blind spot A: Next 15 async params — `const { id } = await params` — read via
// destructuring, not `params.id`. Must be enumerated (was a false negative).
func TestIDOR_AsyncParamsDestructured(t *testing.T) {
	items := idorSurface(t, "app/accounts/[id]/route.ts", `
export async function GET(req: Request, { params }: { params: Promise<{ id: string }> }) {
  const { id: accountId } = await params;
  return Response.json(await prisma.account.findUnique({ where: { id: accountId } }));
}`)
	if len(items) != 1 {
		t.Fatalf("async-params destructuring must be enumerated, got %d", len(items))
	}
	sig := signalsJoined(items[0])
	if !strings.Contains(strings.ToLower(sig), "param") {
		t.Errorf("id signal must name the route param input, got %q", sig)
	}
	assertFactsNotJudgments(t, items[0])
}

// Blind spot B: query params via `const { searchParams } = new URL(req.url)` —
// the dominant idiom in real code. Must be enumerated (was a false negative).
func TestIDOR_NewURLSearchParams(t *testing.T) {
	items := idorSurface(t, "app/processes/route.ts", `
export async function GET(req: Request) {
  const { searchParams } = new URL(req.url);
  const executionId = searchParams.get('executionId');
  return Response.json(await prisma.processExecution.findMany({ where: { executionId } }));
}`)
	if len(items) != 1 {
		t.Fatalf("new URL().searchParams must be enumerated, got %d", len(items))
	}
	sig := signalsJoined(items[0])
	if !strings.Contains(sig, "searchParams") {
		t.Errorf("id signal must name the query-param input, got %q", sig)
	}
	assertFactsNotJudgments(t, items[0])
}

// Blind spot C: indirect resource access. The id is read and passed to a service
// call; no direct Prisma access in the body. codefit does NOT chase the service
// — it enumerates with an HONEST signal that the access may be indirect, and the
// question sends the agent to follow the data (the frontier, ADR 0005).
func TestIDOR_IndirectAccessViaService(t *testing.T) {
	items := idorSurface(t, "app/notes/[id]/route.ts", `
export async function PATCH(req: Request, { params }: { params: Promise<{ id: string }> }) {
  const { id: noteId } = await params;
  const note = await NoteService.markAsRead(noteId);
  return Response.json(note);
}`)
	if len(items) != 1 {
		t.Fatalf("indirect access (id passed to a service) must be enumerated, got %d", len(items))
	}
	sig := strings.ToLower(signalsJoined(items[0]))
	if !strings.Contains(sig, "service") && !strings.Contains(sig, "indirect") {
		t.Errorf("access signal must honestly say the access may be indirect, got %q", sig)
	}
	// "prisma" may appear in the honest negative ("No direct Prisma access
	// detected"), but codefit must not CLAIM a prisma access here.
	if strings.Contains(sig, "accesses a resource via a prisma") {
		t.Errorf("there is no Prisma access here — codefit must not claim one: %q", sig)
	}
	assertFactsNotJudgments(t, items[0])
}

// Calibration (1): a Zod schema validation call (schema.parse/safeParse) is NOT
// a resource access — passing the id to it must not be reported as an indirect
// access callee. An id that only reaches a validation call (and no real access)
// is not enumerated.
func TestIDOR_ZodParseIsNotAccess(t *testing.T) {
	// Only a parse call → not surface (validation is not an access).
	items := idorSurface(t, "app/notes/route.ts", `
export async function GET(req: Request) {
  const { searchParams } = new URL(req.url);
  const filters = listNotesQuerySchema.parse({ unread: searchParams.get('unread') });
  return Response.json(filters);
}`)
	if len(items) != 0 {
		t.Errorf("an id that only flows to a Zod parse is not a resource access; must not enumerate, got %+v", items)
	}

	// parse AND a real service call → enumerated, naming the service, NOT parse.
	items = idorSurface(t, "app/things/[id]/route.ts", `
export async function GET(req: Request, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const valid = idSchema.parse(id);
  return Response.json(await ThingService.getById(id));
}`)
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	sig := signalsJoined(items[0])
	if strings.Contains(sig, ".parse") || strings.Contains(sig, "Schema") {
		t.Errorf("the indirect callee must be the service, not the Zod parse: %q", sig)
	}
	if !strings.Contains(sig, "ThingService.getById") {
		t.Errorf("the real service callee must be named: %q", sig)
	}
}

// A handler that reads an id but does nothing with it (no access, no call taking
// it) is not IDOR surface — the id goes nowhere.
func TestIDOR_IdReadButUnused(t *testing.T) {
	items := idorSurface(t, "app/echo/[id]/route.ts", `
export async function GET(req: Request, { params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return Response.json({ ok: true });
}`)
	if len(items) != 0 {
		t.Errorf("an id that is read but never used as an access or argument is not surface, got %+v", items)
	}
}

// Honesty of the authz signal, inline: getServerSession IS in the body → signal
// reports it was detected (never omitted).
func TestIDOR_AuthzPresentInline(t *testing.T) {
	items := idorSurface(t, "app/orders/[id]/route.ts", `
export async function GET(req: Request, { params }: { params: { id: string } }) {
  const session = await getServerSession();
  const o = await prisma.order.findUnique({ where: { id: params.id } });
  return Response.json(o);
}`)
	if len(items) != 1 {
		t.Fatalf("want 1 IDOR item (still enumerated even with authz present), got %d", len(items))
	}
	sig := signalsJoined(items[0])
	if !strings.Contains(sig, "getServerSession") || !strings.Contains(strings.ToLower(sig), "was detected") {
		t.Errorf("signal 3 must honestly report getServerSession WAS detected, got %q", sig)
	}
	assertFactsNotJudgments(t, items[0])
}

// THE honesty case: authz exists, but in a helper called from the handler, NOT
// inline. codefit does not follow the call, so the signal must honestly say "no
// authz detected in the body" — and reason_to_review sends the agent to verify.
func TestIDOR_AuthzInHelperNotInline(t *testing.T) {
	items := idorSurface(t, "app/accounts/[id]/route.ts", `
async function getUser() {
  const session = await getServerSession();
  return session?.user;
}
export async function GET(req: Request, { params }: { params: { id: string } }) {
  const user = await getUser();
  const a = await prisma.account.findUnique({ where: { id: params.id } });
  return Response.json(a);
}`)
	if len(items) != 1 {
		t.Fatalf("want 1 IDOR item, got %d", len(items))
	}
	sig := signalsJoined(items[0])
	if !strings.Contains(strings.ToLower(sig), "no call to a known authorization helper") {
		t.Errorf("authz is in a helper, not the body — signal must honestly say none was detected in the body, got %q", sig)
	}
	// getServerSession may appear in the declared "searched:" list, but codefit
	// must NOT claim to have DETECTED it (it never looked inside the helper).
	if strings.Contains(sig, "An authorization helper call was detected") {
		t.Errorf("getServerSession is in the helper, NOT the handler body — codefit must not claim detection: %q", sig)
	}
	assertFactsNotJudgments(t, items[0])
}

// No id→resource pattern → not enumerated (a health check that touches nothing).
func TestIDOR_NoPatternNotEnumerated(t *testing.T) {
	items := idorSurface(t, "app/health/route.ts", `
export async function GET() {
  return Response.json({ ok: true });
}`)
	if len(items) != 0 {
		t.Errorf("a handler with no id→resource pattern must not be enumerated, got %+v", items)
	}
}

// Path discipline: the same code outside app/**/route.ts is not a Next route
// handler and must not be enumerated.
func TestIDOR_OnlyNextRouteFiles(t *testing.T) {
	src := `
export async function GET(req: Request, { params }: { params: { id: string } }) {
  return prisma.user.findUnique({ where: { id: params.id } });
}`
	for _, path := range []string{"lib/users.ts", "app/users/[id]/page.tsx", "src/handlers/users.ts"} {
		if items := idorSurface(t, path, src); len(items) != 0 {
			t.Errorf("%s is not an app/**/route.ts file; must not enumerate, got %d", path, len(items))
		}
	}
}
