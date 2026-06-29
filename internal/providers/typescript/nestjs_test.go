package typescript_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
)

// TestNestIDOR: NestJS controller methods decorated with an HTTP verb are
// discovered as handlers; @Param seeds the client id-input; a local Prisma access
// is IDOR (local), a delegation to a service is IDOR (indirect, option C); and a
// method with no client id is NOT enumerated as IDOR.
func TestNestIDOR(t *testing.T) {
	items := byCategory(surfaceFromFixture(t, "nestjs_idor.ts", "src/users/users.controller.ts"), "idor")

	var local, indirect *findings.SurfaceItem
	for i := range items {
		if items[i].StructuralFacts["local_access_detected"] {
			local = &items[i]
		}
		if items[i].StructuralFacts["indirect_access"] {
			indirect = &items[i]
		}
	}

	if local == nil {
		t.Fatalf("want a local IDOR (findOne: @Param → prisma.user.findUnique), got %+v", items)
	}
	if !strings.Contains(signalsJoined(*local), "prisma.user.findUnique") {
		t.Errorf("local IDOR must name the Prisma access, got %q", signalsJoined(*local))
	}
	if !strings.Contains(signalsJoined(*local), "Param") {
		t.Errorf("local IDOR must name the @Param client input, got %q", signalsJoined(*local))
	}

	if indirect == nil {
		t.Fatalf("want an indirect IDOR (remove → this.usersService.remove), got %+v", items)
	}
	if indirect.IndirectCall == "" {
		t.Errorf("indirect IDOR must name the callee in indirect_call, got %+v", indirect)
	}

	// findAll has no client id → not an IDOR. Only findOne + remove qualify.
	if len(items) != 2 {
		t.Errorf("want exactly 2 IDOR items (findAll has no client id), got %d: %+v", len(items), items)
	}
	for _, it := range items {
		assertFactsNotJudgments(t, it)
	}
}

// TestNestOverfetch: a NestJS handler returns the value (the framework serializes
// it), so the return statement is the over-fetch sink. A whole Prisma model is
// flagged field_limiting=false; a select'd find true; a service-sourced value as
// the frontier (local_access=false).
func TestNestOverfetch(t *testing.T) {
	items := byCategory(surfaceFromFixture(t, "nestjs_overfetch.ts", "src/users/users.controller.ts"), "overfetch")
	var whole, limited, frontier *findings.SurfaceItem
	for i := range items {
		it := &items[i]
		switch {
		case !it.StructuralFacts["local_access_detected"]:
			frontier = it
		case it.StructuralFacts["field_limiting_detected"]:
			limited = it
		default:
			whole = it
		}
	}
	if whole == nil || limited == nil || frontier == nil {
		t.Fatalf("want whole+limited+frontier over-fetch items, got %d: %+v", len(items), items)
	}
	if !strings.Contains(signalsJoined(*whole), "prisma.user.findMany") {
		t.Errorf("whole-model item must name the Prisma find, got %q", signalsJoined(*whole))
	}
	if !strings.Contains(signalsJoined(*frontier), "findAll") {
		t.Errorf("frontier item must name the service callee, got %q", signalsJoined(*frontier))
	}
	assertFactsNotJudgments(t, *whole)
	assertFactsNotJudgments(t, *frontier)
}

// TestNestIDOR_Dogfood is the slice's done criterion: codefit must surface the
// IDORs in the REAL RealWorld NestJS controller (vendored verbatim, see
// testdata/README) — handlers that reach an article by a client @Param slug
// through the service with no ownership check, e.g. DELETE :slug
// (articleService.delete(params.slug)) and PUT :slug.
func TestNestIDOR_Dogfood(t *testing.T) {
	items := byCategory(surfaceFromFixture(t, "real_realworld_nest_article_controller.ts", "src/article/article.controller.ts"), "idor")
	if len(items) < 6 {
		t.Fatalf("want a healthy set of IDOR items in the real controller, got %d: %+v", len(items), items)
	}
	byCallee := map[string]findings.SurfaceItem{}
	var callees []string
	for _, it := range items {
		byCallee[it.IndirectCall] = it
		callees = append(callees, it.IndirectCall)
	}
	// Option C on real code: service-delegated reads reached by a client @Param
	// slug surface as indirect (the resource access lives in article.service).
	for _, fn := range []string{"findOne", "findComments"} {
		it, ok := byCallee[fn]
		if !ok {
			t.Fatalf("done criterion: want an indirect IDOR with indirect_call=%q (option C), got callees=%v", fn, callees)
		}
		if !it.StructuralFacts["indirect_access"] {
			t.Errorf("%s: indirect_access must be true", fn)
		}
	}
	// At least one item names the @Param client input it received.
	var named bool
	for _, it := range items {
		if strings.Contains(signalsJoined(it), "Param") {
			named = true
		}
		assertFactsNotJudgments(t, it)
	}
	if !named {
		t.Errorf("at least one IDOR must name its @Param client input")
	}
}

// TestNestAuthz_Guards: @UseGuards is detected by PRESENCE, class-level guards are
// inherited by every method, and an unguarded handler honestly reports none — the
// none-branch declaring it also checked @UseGuards.
func TestNestAuthz_Guards(t *testing.T) {
	items := byCategory(surfaceFromFixture(t, "nestjs_authz.ts", "src/admin/admin.controller.ts"), "authz")
	bySig := func(sub string) *findings.SurfaceItem {
		for i := range items {
			if strings.Contains(signalsJoined(items[i]), sub) {
				return &items[i]
			}
		}
		return nil
	}
	inherited := bySig("account.findUnique") // class-level @UseGuards, inherited
	methodG := bySig("account.create")       // class guard + method guard
	unguarded := bySig("note.create")        // no guard anywhere
	if inherited == nil || methodG == nil || unguarded == nil {
		t.Fatalf("want authz items for the three handlers, got %+v", items)
	}

	if !inherited.StructuralFacts["known_authz_detected"] {
		t.Errorf("class-level @UseGuards is inherited → known_authz_detected must be true: %q", signalsJoined(*inherited))
	}
	if !strings.Contains(signalsJoined(*inherited), "@UseGuards") || !strings.Contains(signalsJoined(*inherited), "AuthGuard") {
		t.Errorf("inherited-guard signal must name @UseGuards and AuthGuard: %q", signalsJoined(*inherited))
	}
	if !methodG.StructuralFacts["known_authz_detected"] || !strings.Contains(signalsJoined(*methodG), "RolesGuard") {
		t.Errorf("method-level guard must be detected and named: %q", signalsJoined(*methodG))
	}
	if unguarded.StructuralFacts["known_authz_detected"] {
		t.Errorf("unguarded handler: known_authz_detected must be false: %q", signalsJoined(*unguarded))
	}
	if !strings.Contains(signalsJoined(*unguarded), "@UseGuards") {
		t.Errorf("unguarded none-branch must declare it checked @UseGuards: %q", signalsJoined(*unguarded))
	}
	for _, it := range items {
		assertFactsNotJudgments(t, it)
	}
}
