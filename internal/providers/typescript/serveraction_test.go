package typescript_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// surfaceFromFixture loads a testdata fixture and returns its surface items under
// the given project-relative path. Server Actions are NOT path-gated (they live
// anywhere), so the path is deliberately a non-route file to prove detection is
// by shape, not by filename.
func surfaceFromFixture(t *testing.T, fixture, path string) []findings.SurfaceItem {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixture, err)
	}
	items, err := typescript.New().AnalyzeSurface(providers.SourceFile{Path: path, Content: content})
	if err != nil {
		t.Fatalf("AnalyzeSurface(%s): %v", fixture, err)
	}
	return items
}

func byCategory(items []findings.SurfaceItem, cat string) []findings.SurfaceItem {
	var out []findings.SurfaceItem
	for _, it := range items {
		if it.Category == cat {
			out = append(out, it)
		}
	}
	return out
}

// THE gap case: a Server Action with a real IDOR (receives a client id, deletes a
// resource, no authz) enumerated where before it was invisible. File-level "use
// server", in a lib/ file that is NOT an app/**/route.ts.
func TestServerAction_IDOR_Enumerated(t *testing.T) {
	items := byCategory(surfaceFromFixture(t, "action_idor.ts", "lib/invoices/actions.ts"), "idor")
	if len(items) != 1 {
		t.Fatalf("a Server Action with a client id reaching a resource must be enumerated as IDOR, got %d", len(items))
	}
	it := items[0]
	if !it.StructuralFacts["server_action"] {
		t.Errorf("server_action fact must be true for a Server Action, got %+v", it.StructuralFacts)
	}
	if !it.StructuralFacts["local_access_detected"] {
		t.Errorf("a local Prisma access must set local_access_detected=true, got %+v", it.StructuralFacts)
	}
	if it.StructuralFacts["known_authz_detected"] {
		t.Errorf("no authz helper in body → known_authz_detected must be false, got %+v", it.StructuralFacts)
	}
	sig := signalsJoined(it)
	if !strings.Contains(sig, "id") {
		t.Errorf("signal must name the client-controlled argument, got %q", sig)
	}
	if !strings.Contains(sig, "db.invoice.delete") {
		t.Errorf("signal must name the local Prisma access, got %q", sig)
	}
	assertFactsNotJudgments(t, it)
}

// Object-shaped argument: the id is nested (data.id). Must still enumerate — the
// parameter binding is treated as the client input and data.id flows to the
// access. Proves object args are covered, not a blind spot.
func TestServerAction_IDOR_ObjectArg(t *testing.T) {
	items := byCategory(surfaceFromFixture(t, "action_object_arg.ts", "lib/actions.ts"), "idor")
	if len(items) != 1 {
		t.Fatalf("an action whose id is nested inside an object arg must enumerate, got %d", len(items))
	}
	if !items[0].StructuralFacts["local_access_detected"] {
		t.Errorf("local Prisma update must set local_access_detected=true, got %+v", items[0].StructuralFacts)
	}
	assertFactsNotJudgments(t, items[0])
}

// FormData input: the action receives a FormData; the id-input is read via
// formData.get("key"). Must enumerate with the form-field keys in the signal.
func TestServerAction_IDOR_FormData(t *testing.T) {
	items := byCategory(surfaceFromFixture(t, "action_formdata.ts", "app/settings/actions.ts"), "idor")
	if len(items) != 1 {
		t.Fatalf("a FormData-driven action reaching a resource must enumerate, got %d", len(items))
	}
	sig := strings.ToLower(signalsJoined(items[0]))
	if !strings.Contains(sig, "form") {
		t.Errorf("signal must state the input comes from form data, got %q", sig)
	}
	if !items[0].StructuralFacts["local_access_detected"] {
		t.Errorf("local Prisma update must set local_access_detected=true, got %+v", items[0].StructuralFacts)
	}
	assertFactsNotJudgments(t, items[0])
}

// Inline (function-level) action inside a Server Component page.tsx. Detected by
// the directive in the function body, not by filename.
func TestServerAction_IDOR_Inline(t *testing.T) {
	items := byCategory(surfaceFromFixture(t, "action_inline.tsx", "app/dashboard/page.tsx"), "idor")
	if len(items) != 1 {
		t.Fatalf("an inline function-level Server Action must enumerate, got %d", len(items))
	}
	if !items[0].StructuralFacts["server_action"] {
		t.Errorf("server_action fact must be true, got %+v", items[0].StructuralFacts)
	}
	assertFactsNotJudgments(t, items[0])
}

// Authz: an action that calls a known authz helper sets known_authz_detected;
// one that does not (and mutates) is enumerated with the fact false.
func TestServerAction_Authz(t *testing.T) {
	clean := byCategory(surfaceFromFixture(t, "action_clean.ts", "lib/invoices/actions.ts"), "authz")
	if len(clean) != 1 || !clean[0].StructuralFacts["known_authz_detected"] {
		t.Fatalf("an action calling auth() must set known_authz_detected=true, got %+v", clean)
	}
	if !strings.Contains(signalsJoined(clean[0]), "Server Action") {
		t.Errorf("authz operation signal must name the Server Action subject, got %q", signalsJoined(clean[0]))
	}
	assertFactsNotJudgments(t, clean[0])

	unchecked := byCategory(surfaceFromFixture(t, "action_idor.ts", "lib/invoices/actions.ts"), "authz")
	if len(unchecked) != 1 || unchecked[0].StructuralFacts["known_authz_detected"] {
		t.Fatalf("an action that mutates with no authz helper must enumerate with known_authz_detected=false, got %+v", unchecked)
	}
	assertFactsNotJudgments(t, unchecked[0])
}

// Over-fetching: an action that returns a Prisma find with no select/omit is
// over-fetch surface — the return value is what the framework serializes.
func TestServerAction_Overfetch(t *testing.T) {
	items := byCategory(surfaceFromFixture(t, "action_overfetch.ts", "app/dashboard/actions.ts"), "overfetch")
	if len(items) != 1 {
		t.Fatalf("an action returning an unselected Prisma find must enumerate as over-fetch, got %d", len(items))
	}
	if items[0].StructuralFacts["field_limiting_detected"] {
		t.Errorf("findMany with no select/omit → field_limiting_detected must be false, got %+v", items[0].StructuralFacts)
	}
	assertFactsNotJudgments(t, items[0])
}

// THE discipline case: an arbitrary exported async function with the SAME body as
// a Server Action but WITHOUT the "use server" directive must NOT enumerate. The
// directive — not the shape of an async function — is what makes it auditable
// surface; otherwise every helper in the codebase would be enumerated.
func TestServerAction_NotArbitraryFunctions(t *testing.T) {
	noDirective := `
import { db } from "@/lib/db";
export async function helper(id: string) {
  await db.user.findUnique({ where: { id } });
}`
	for _, path := range []string{"lib/helpers.ts", "src/services/user.ts", "app/dashboard/page.tsx"} {
		items := idorSurface(t, path, noDirective)
		if len(items) != 0 {
			t.Errorf("%s: an async function with no \"use server\" directive must NOT enumerate, got %d", path, len(items))
		}
	}

	withDirective := `
"use server";
import { db } from "@/lib/db";
export async function helper(id: string) {
  await db.user.findUnique({ where: { id } });
}`
	if items := idorSurface(t, "lib/helpers.ts", withDirective); len(items) != 1 {
		t.Errorf("the same function WITH \"use server\" must enumerate, got %d", len(items))
	}
}

// Indirect access from an object arg: the id leaves the body to a service. Must
// enumerate with local_access_detected=false (the frontier handed to the agent).
func TestServerAction_IndirectAccess(t *testing.T) {
	src := `
"use server";
export async function archive(data: { id: string }) {
  await InvoiceService.archive(data.id);
}`
	items := idorSurface(t, "lib/actions.ts", src)
	if len(items) != 1 {
		t.Fatalf("an action whose id leaves the body to a service must enumerate, got %d", len(items))
	}
	if items[0].StructuralFacts["local_access_detected"] {
		t.Errorf("an indirect service access must set local_access_detected=false, got %+v", items[0].StructuralFacts)
	}
	sig := strings.ToLower(signalsJoined(items[0]))
	if !strings.Contains(sig, "service") && !strings.Contains(sig, "indirect") {
		t.Errorf("signal must honestly say the access may be indirect, got %q", sig)
	}
}
