package mcp_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/mcp"
)

// TestSurfaceAuthzIDORDescriptionsDeclareHelperScope is the agent-facing doc
// lock for the built-in-only helper scope (ADR 0013): codefit-surface-authz
// and codefit-surface-idor compute their helper-dependent facts
// (known_authz_detected) against codefit's BUILT-IN helper set only — never
// the project's registered ones (codefit-baseline-register-authz-helper).
// The description must say so and point to codefit-scan-all/-scan-security
// for the project-aware answer. codefit-surface-overfetch and -nplus1 never
// consult the helper set, so their descriptions must stay silent — declaring
// a limit that does not apply would itself be dishonest.
func TestSurfaceAuthzIDORDescriptionsDeclareHelperScope(t *testing.T) {
	for _, tool := range []string{string(mcp.ToolSurfaceAuthz), string(mcp.ToolSurfaceIDOR)} {
		desc := toolDescription(t, tool)
		if !strings.Contains(desc, "built-in authz-helper set") {
			t.Errorf("%s description must declare the built-in-only helper scope, got:\n%s", tool, desc)
		}
		if !strings.Contains(desc, "codefit-scan-all") || !strings.Contains(desc, "codefit-scan-security") {
			t.Errorf("%s description must point to codefit-scan-all/codefit-scan-security for the project-aware answer, got:\n%s", tool, desc)
		}
	}

	for _, tool := range []string{string(mcp.ToolSurfaceOverfetch), string(mcp.ToolSurfaceNPlus1)} {
		desc := toolDescription(t, tool)
		if strings.Contains(desc, "authz-helper") {
			t.Errorf("%s never consults the helper set — its description must not mention authz-helper scope, got:\n%s", tool, desc)
		}
	}
}

// TestSurfaceHandlerResponsesDeclareHelperScope drives the REAL handlers over
// real file fixtures (not a hand-built SurfaceResponse) and checks the
// marshaled JSON bytes: codefit-surface-authz and codefit-surface-idor must
// carry a non-empty "helper_scope" key; codefit-surface-overfetch and
// -nplus1 must not carry the key AT ALL — present-and-empty is a different
// claim from absent (spec: "no mention" vs "nothing to mention" must not be
// the same bytes).
func TestSurfaceHandlerResponsesDeclareHelperScope(t *testing.T) {
	idorFile := mcp.FileInput{Path: "app/users/[id]/route.ts", Content: `
export async function GET(req: Request, { params }: { params: { id: string } }) {
  const u = await prisma.user.findUnique({ where: { id: params.id } });
  return Response.json(u);
}`}
	authzFile := mcp.FileInput{Path: "app/reports/route.ts", Content: `
export async function GET() { return Response.json(await prisma.report.findMany()); }`}
	overfetchFile := mcp.FileInput{Path: "app/confirmed/route.ts", Content: `
export async function GET() { return Response.json(await prisma.b.findMany()); }`}
	nplus1File := mcp.FileInput{Path: "app/sequential/route.ts", Content: `
export async function GET() {
  for (const id of ids) { await prisma.b.findUnique({ where: { id } }); }
}`}

	tests := []struct {
		name       string
		call       func() (mcp.SurfaceResponse, error)
		wantPresent bool
	}{
		{"authz", func() (mcp.SurfaceResponse, error) {
			return mcp.HandleSurfaceAuthz(mcp.SurfaceIDORRequest{Files: []mcp.FileInput{authzFile}})
		}, true},
		{"idor", func() (mcp.SurfaceResponse, error) {
			return mcp.HandleSurfaceIDOR(mcp.SurfaceIDORRequest{Files: []mcp.FileInput{idorFile}})
		}, true},
		{"overfetch", func() (mcp.SurfaceResponse, error) {
			return mcp.HandleSurfaceOverfetch(mcp.SurfaceIDORRequest{Files: []mcp.FileInput{overfetchFile}})
		}, false},
		{"nplus1", func() (mcp.SurfaceResponse, error) {
			return mcp.HandleSurfaceNPlus1(mcp.SurfaceIDORRequest{Files: []mcp.FileInput{nplus1File}})
		}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := tt.call()
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(resp)
			if err != nil {
				t.Fatal(err)
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatal(err)
			}
			_, present := raw["helper_scope"]
			if tt.wantPresent && !present {
				t.Errorf("%s: response JSON must carry a non-empty \"helper_scope\" key, got:\n%s", tt.name, data)
			}
			if tt.wantPresent && present {
				var v string
				if err := json.Unmarshal(raw["helper_scope"], &v); err != nil {
					t.Fatal(err)
				}
				if v == "" {
					t.Errorf("%s: helper_scope must be non-empty, got empty string", tt.name)
				}
			}
			if !tt.wantPresent && present {
				t.Errorf("%s: response JSON must NOT carry a \"helper_scope\" key at all, got:\n%s", tt.name, data)
			}
		})
	}
}
