package mcp_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/report"
	"github.com/codefit-cli/codefit/internal/mcp"
)

func TestHandleRegisterAuthzHelper_PersistsAndIsIdempotent(t *testing.T) {
	root := t.TempDir()

	resp, err := mcp.HandleBaselineRegisterAuthzHelper(mcp.BaselineRegisterAuthzHelperRequest{
		Root: root, Language: "typescript", HelperName: "requirePermission", Reason: "project RBAC wrapper",
	})
	if err != nil || !resp.Registered {
		t.Fatalf("first register must succeed, got registered=%v err=%v", resp.Registered, err)
	}

	// Visible in baseline-list.
	list, err := mcp.HandleBaselineList(mcp.BaselineListRequest{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.AuthzHelpers) != 1 || list.AuthzHelpers[0].Name != "requirePermission" || list.AuthzHelpers[0].By != "human" {
		t.Fatalf("registered helper must appear in baseline-list, by:human, got %+v", list.AuthzHelpers)
	}

	// Idempotent.
	resp, err = mcp.HandleBaselineRegisterAuthzHelper(mcp.BaselineRegisterAuthzHelperRequest{
		Root: root, Language: "typescript", HelperName: "requirePermission", Reason: "again",
	})
	if err != nil || resp.Registered {
		t.Errorf("re-register must be a no-op, got registered=%v err=%v", resp.Registered, err)
	}
}

func TestHandleRegisterAuthzHelper_RequiresReason(t *testing.T) {
	root := t.TempDir()
	if _, err := mcp.HandleBaselineRegisterAuthzHelper(mcp.BaselineRegisterAuthzHelperRequest{
		Root: root, Language: "typescript", HelperName: "x", Reason: "",
	}); err == nil {
		t.Error("register without a reason must error (human justification mandatory)")
	}
}

func TestHandleUnregisterAuthzHelper(t *testing.T) {
	root := t.TempDir()
	_, _ = mcp.HandleBaselineRegisterAuthzHelper(mcp.BaselineRegisterAuthzHelperRequest{
		Root: root, Language: "typescript", HelperName: "requirePermission", Reason: "r",
	})
	resp, err := mcp.HandleBaselineUnregisterAuthzHelper(mcp.BaselineUnregisterAuthzHelperRequest{
		Root: root, Language: "typescript", HelperName: "requirePermission",
	})
	if err != nil || !resp.Unregistered {
		t.Fatalf("unregister must succeed, got %+v err=%v", resp, err)
	}
	list, _ := mcp.HandleBaselineList(mcp.BaselineListRequest{Root: root})
	if len(list.AuthzHelpers) != 0 {
		t.Errorf("helper must be gone after unregister, got %+v", list.AuthzHelpers)
	}
}

// THE Safeguard-2 barrier, end-to-end at the scan level: registering a project
// authz helper moves an AUTHZ-only endpoint to resolved_clean (the permission
// question is answered), but an IDOR endpoint guarded by the SAME helper STAYS
// actionable (ownership is unverifiable from structure — a registered helper clears
// the authz gap, never the IDOR one). No real IDOR is silenced.
func TestHandleScanAll_RegisteredHelperClearsAuthzNotIDOR(t *testing.T) {
	fixture := func(root string) {
		// authz-only: a sensitive mutation with no client identifier.
		mustWrite(t, root, "app/admin/route.ts", `
export async function POST(req: Request) {
  await requirePermission("admin");
  await prisma.setting.update({ where: { key: "x" }, data: { value: "y" } });
  return Response.json({ ok: true });
}`)
		// IDOR: a client id reaches a resource (ownership question), same helper.
		mustWrite(t, root, "app/orders/[id]/route.ts", `
export async function GET(req: Request, { params }: { params: { id: string } }) {
  await requirePermission("orders:read");
  return Response.json(await prisma.order.findUnique({ where: { id: params.id } }));
}`)
	}

	// WITHOUT registration: requirePermission is unknown → both endpoints actionable.
	rootA := t.TempDir()
	fixture(rootA)
	respA, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: rootA, Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}
	if respA.Actionable.Count != 2 || len(respA.Actionable.Endpoints) != 2 {
		t.Fatalf("without registration both endpoints must be actionable, got count=%d rendered=%d: %+v",
			respA.Actionable.Count, len(respA.Actionable.Endpoints), respA.Actionable)
	}

	// WITH registration (fresh root, helper registered BEFORE the first scan so
	// nothing is baseline-silenced yet).
	rootB := t.TempDir()
	fixture(rootB)
	if _, err := mcp.HandleBaselineRegisterAuthzHelper(mcp.BaselineRegisterAuthzHelperRequest{
		Root: rootB, Language: "typescript", HelperName: "requirePermission", Reason: "project RBAC wrapper",
	}); err != nil {
		t.Fatal(err)
	}
	respB, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: rootB, Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}

	if !hasCleanFile(respB.ResolvedClean.Endpoints, "app/admin/route.ts") {
		t.Errorf("the authz-only endpoint must be resolved_clean after registering its helper; clean=%+v actionable=%+v",
			respB.ResolvedClean.Endpoints, filesOf(respB.Actionable.Endpoints))
	}
	if !hasActionableFile(respB.Actionable.Endpoints, "app/orders/[id]/route.ts") {
		t.Errorf("the IDOR endpoint must STAY actionable after registering the helper (ownership unverified); got actionable=%+v",
			filesOf(respB.Actionable.Endpoints))
	}
	// Honesty: the IDOR endpoint's authz signal labels the helper as registered.
	// scan-all NAMES its actionable endpoints now, so the signals are read where an
	// agent reads them — from codefit-scan-endpoint, which re-runs the same analysis
	// (ADR 0054). If the naming ever dropped an endpoint, this fetch finds nothing.
	for _, ep := range respB.Actionable.Endpoints {
		if ep.File == "app/orders/[id]/route.ts" {
			joined := ""
			for _, c := range fetchConcerns(t, rootB, ep.File, ep.Line) {
				joined += strings.Join(c.Signals, " | ")
			}
			if !strings.Contains(strings.ToLower(joined), "registered for this project") {
				t.Errorf("the IDOR endpoint must name the registered helper in its signals, got %q", joined)
			}
		}
	}
}

func hasCleanFile(eps []report.ResolvedCleanEndpoint, file string) bool {
	for _, e := range eps {
		if e.File == file {
			return true
		}
	}
	return false
}

func hasActionableFile(eps []report.ActionableEndpoint, file string) bool {
	for _, e := range eps {
		if e.File == file {
			return true
		}
	}
	return false
}

func filesOf(eps []report.ActionableEndpoint) []string {
	out := make([]string, 0, len(eps))
	for _, e := range eps {
		out = append(out, e.File)
	}
	return out
}
