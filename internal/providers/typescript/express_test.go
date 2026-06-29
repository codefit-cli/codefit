package typescript_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
)

// TestExpressIDOR_IndirectAccess mirrors the dogfood shape: an Express controller
// whose handler reaches the resource through a service call in another file. Per
// cross-file option C, codefit emits indirect_access=true plus indirect_call
// naming the callee, WITHOUT following it across files. Both confirmed IDORs
// (PUT and DELETE /articles/:slug) must surface.
func TestExpressIDOR_IndirectAccess(t *testing.T) {
	items := byCategory(surfaceFromFixture(t, "express_idor_indirect.ts", "src/controllers/article.controller.ts"), "idor")
	byCallee := map[string]findings.SurfaceItem{}
	for _, it := range items {
		byCallee[it.IndirectCall] = it
	}
	for _, fn := range []string{"updateArticle", "deleteArticle"} {
		it, ok := byCallee[fn]
		if !ok {
			t.Fatalf("want an IDOR item with indirect_call=%q, got items: %+v", fn, items)
		}
		if !it.StructuralFacts["indirect_access"] {
			t.Errorf("%s: structural fact indirect_access must be true", fn)
		}
		if it.StructuralFacts["local_access_detected"] {
			t.Errorf("%s: local_access_detected must be false (Prisma lives cross-file)", fn)
		}
		if !strings.Contains(signalsJoined(it), "req.params.slug") {
			t.Errorf("%s: signal must name the client route param req.params.slug, got %q", fn, signalsJoined(it))
		}
		assertFactsNotJudgments(t, it)
	}
}

// TestExpressIDOR_LocalAccess: a handler with an inline Prisma access reached by a
// client route param. local_access_detected=true and no indirect_call.
func TestExpressIDOR_LocalAccess(t *testing.T) {
	items := byCategory(surfaceFromFixture(t, "express_idor_local.ts", "src/routes/user.routes.ts"), "idor")
	if len(items) == 0 {
		t.Fatalf("want >=1 IDOR surface item, got 0")
	}
	it := items[0]
	if !it.StructuralFacts["local_access_detected"] {
		t.Errorf("local_access_detected must be true: %+v", it)
	}
	if it.IndirectCall != "" {
		t.Errorf("indirect_call must be empty for a local access, got %q", it.IndirectCall)
	}
	if !strings.Contains(signalsJoined(it), "req.params") {
		t.Errorf("signal must name the client route param, got %q", signalsJoined(it))
	}
	if !strings.Contains(signalsJoined(it), "prisma.user.findUnique") {
		t.Errorf("signal must name the local Prisma access, got %q", signalsJoined(it))
	}
	assertFactsNotJudgments(t, it)
}

// TestExpressAuthz_MiddlewareDetected: in Express the auth guard is middleware in
// the route registration, not a body call. A route guarded by auth.required must
// set known_authz_detected=true and phrase the signal as route middleware; an
// unguarded route must report none detected, honestly declaring it also checked
// the route middleware (not only the body).
func TestExpressAuthz_MiddlewareDetected(t *testing.T) {
	items := byCategory(surfaceFromFixture(t, "express_authz.ts", "src/routes/article.routes.ts"), "authz")
	var guarded, unguarded *findings.SurfaceItem
	for i := range items {
		switch {
		case strings.Contains(signalsJoined(items[i]), "article.create"):
			guarded = &items[i]
		case strings.Contains(signalsJoined(items[i]), "article.delete"):
			unguarded = &items[i]
		}
	}
	if guarded == nil || unguarded == nil {
		t.Fatalf("want authz items for create (guarded) and delete (unguarded), got: %+v", items)
	}
	if !guarded.StructuralFacts["known_authz_detected"] {
		t.Errorf("guarded route: auth.required middleware must set known_authz_detected=true, got %q", signalsJoined(*guarded))
	}
	if !strings.Contains(strings.ToLower(signalsJoined(*guarded)), "route middleware") {
		t.Errorf("guarded route: signal must state the helper is route middleware, got %q", signalsJoined(*guarded))
	}
	if unguarded.StructuralFacts["known_authz_detected"] {
		t.Errorf("unguarded route: known_authz_detected must be false, got %q", signalsJoined(*unguarded))
	}
	if !strings.Contains(strings.ToLower(signalsJoined(*unguarded)), "route middleware") {
		t.Errorf("unguarded route: none-branch must declare it checked the route middleware too, got %q", signalsJoined(*unguarded))
	}
	assertFactsNotJudgments(t, *guarded)
	assertFactsNotJudgments(t, *unguarded)
}

// TestExpressOverfetch_ResponseSinks: over-fetching is enumerated at the Express
// response sink res.json()/res.send(). A whole Prisma model is flagged with
// field_limiting_detected=false; a select'd find with true; a service-sourced
// value as the frontier (local_access_detected=false), naming the callee.
func TestExpressOverfetch_ResponseSinks(t *testing.T) {
	items := byCategory(surfaceFromFixture(t, "express_overfetch.ts", "src/routes/user.routes.ts"), "overfetch")
	var whole, limited, frontier *findings.SurfaceItem
	for i := range items {
		sig := signalsJoined(items[i])
		switch {
		case strings.Contains(sig, "getProfile"):
			frontier = &items[i]
		case strings.Contains(sig, "prisma.user.find") && items[i].StructuralFacts["field_limiting_detected"]:
			limited = &items[i]
		case strings.Contains(sig, "prisma.user.find"):
			whole = &items[i]
		}
	}
	if whole == nil || limited == nil || frontier == nil {
		t.Fatalf("want whole+limited+frontier overfetch items, got %d: %+v", len(items), items)
	}
	if whole.StructuralFacts["field_limiting_detected"] || !whole.StructuralFacts["local_access_detected"] {
		t.Errorf("whole-model res.json(user): want field_limiting=false, local=true, got %+v", whole.StructuralFacts)
	}
	if !limited.StructuralFacts["local_access_detected"] {
		t.Errorf("limited res.send(user): want local_access_detected=true, got %+v", limited.StructuralFacts)
	}
	if frontier.StructuralFacts["local_access_detected"] {
		t.Errorf("service-sourced res.json(profile): want local_access_detected=false (frontier), got %+v", frontier.StructuralFacts)
	}
	assertFactsNotJudgments(t, *whole)
	assertFactsNotJudgments(t, *frontier)
}

// TestFastify_ObjectFormHandler: Fastify's options-object route form
// (verb('/path', { handler, preHandler })) is discovered as surface. Inputs key
// off the first handler param (request), the over-fetch sink off the second
// (reply.send), and preHandler feeds the authz guard.
func TestFastify_ObjectFormHandler(t *testing.T) {
	all := surfaceFromFixture(t, "fastify_handler_object.ts", "src/server.ts")
	idor := byCategory(all, "idor")
	var guarded, unguarded *findings.SurfaceItem
	for i := range idor {
		switch {
		case strings.Contains(idor[i].Snippet, "fastify.get"):
			guarded = &idor[i]
		case strings.Contains(idor[i].Snippet, "fastify.delete"):
			unguarded = &idor[i]
		}
	}
	if guarded == nil || unguarded == nil {
		t.Fatalf("object-form Fastify handlers must be discovered as IDOR, got %+v", idor)
	}
	if !guarded.StructuralFacts["local_access_detected"] {
		t.Errorf("guarded GET: want local_access_detected=true, got %+v", guarded.StructuralFacts)
	}
	if !guarded.StructuralFacts["known_authz_detected"] {
		t.Errorf("guarded GET: preHandler auth.required must set known_authz_detected=true, signal=%q", signalsJoined(*guarded))
	}
	if unguarded.StructuralFacts["known_authz_detected"] {
		t.Errorf("unguarded DELETE: known_authz_detected must be false, got %+v", unguarded.StructuralFacts)
	}
	// reply.send(user) is an over-fetch sink (keyed off the second param), whole model.
	var sawWhole bool
	for _, it := range byCategory(all, "overfetch") {
		if strings.Contains(signalsJoined(it), "prisma.user.find") && !it.StructuralFacts["field_limiting_detected"] {
			sawWhole = true
		}
	}
	if !sawWhole {
		t.Fatalf("reply.send(user) must produce a whole-model over-fetch item, got %+v", byCategory(all, "overfetch"))
	}
	for _, it := range idor {
		assertFactsNotJudgments(t, it)
	}
}

// TestExpressIDOR_Dogfood is the slice's done criterion: codefit must surface the
// two confirmed IDORs in the REAL RealWorld Express controller (vendored verbatim,
// see testdata/README) — PUT and DELETE /articles/:slug both reach the article by
// a client slug through a service, with no ownership check.
func TestExpressIDOR_Dogfood(t *testing.T) {
	items := byCategory(surfaceFromFixture(t, "real_realworld_article_controller.ts", "src/controllers/article.controller.ts"), "idor")
	byCallee := map[string]findings.SurfaceItem{}
	var callees []string
	for _, it := range items {
		byCallee[it.IndirectCall] = it
		callees = append(callees, it.IndirectCall)
	}
	for _, fn := range []string{"updateArticle", "deleteArticle"} {
		it, ok := byCallee[fn]
		if !ok {
			t.Fatalf("done criterion: want an IDOR item with indirect_call=%q, got %d items with callees %v", fn, len(items), callees)
		}
		if !it.StructuralFacts["indirect_access"] {
			t.Errorf("%s: indirect_access must be true", fn)
		}
		if it.StructuralFacts["local_access_detected"] {
			t.Errorf("%s: local_access_detected must be false (service is cross-file)", fn)
		}
		if !strings.Contains(signalsJoined(it), "req.params.slug") {
			t.Errorf("%s: must name the client route param req.params.slug, got %q", fn, signalsJoined(it))
		}
		assertFactsNotJudgments(t, it)
	}
}

// TestExpressDiscovery_NegativesNotHandlers locks the shape discriminator: a call
// whose first arg is NOT a string path, or whose last arg is NOT a function, is
// not an Express route handler and must produce no surface — even when the method
// name matches an HTTP verb (map.get / array.get / cache.get).
func TestExpressDiscovery_NegativesNotHandlers(t *testing.T) {
	items := surfaceFromFixture(t, "express_discovery_negatives.ts", "src/lib/cache.ts")
	if len(items) != 0 {
		t.Fatalf("non-route .get() shapes must not be treated as handlers, got %d items: %+v", len(items), items)
	}
}
